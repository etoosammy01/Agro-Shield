package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/escrow"
	"backend/internal/models"
	"backend/internal/money"
	"backend/internal/repository"
)

var (
	ErrRefundAdminOnly        = errors.New("administrator access required")
	ErrRefundStoreUnavailable = errors.New("refund repository unavailable")
	ErrDisputeAdminOnly       = errors.New("administrator access required")
	ErrInvalidDisputeInput    = errors.New("invalid dispute details")
	ErrInvalidDisputeReason   = errors.New("unsupported dispute reason")
	ErrInvalidSellerResponse  = errors.New("seller response is required")
)

type orderDisputeStore interface {
	Create(context.Context, *models.OrderDispute) error
	GetByID(context.Context, int64) (*models.OrderDispute, error)
	ListByBuyer(context.Context, int) ([]models.OrderDispute, error)
	ListBySeller(context.Context, int) ([]models.OrderDispute, error)
	AddSellerResponse(context.Context, int64, int, string) error
	ListEvents(context.Context, int64) ([]models.OrderDisputeEvent, error)
	StartAdminReview(context.Context, int64, int, ...int64) error
	Reject(context.Context, int64, int, string, ...int64) error
}

// orderRefundStore is implemented by the refund repository.
// Keeping it separate avoids changing existing dispute-service test doubles.
type orderRefundStore interface {
	ApproveRefund(
		context.Context,
		int64,
		int,
		string,
		string,
		...int64,
	) (*models.Refund, error)

	UpdateRefundStatus(
		context.Context,
		int64,
		string,
		string,
	) error
}

type OrderDisputeService struct {
	repo    orderDisputeStore
	admins  disputeAdminChecker
	settler escrow.Settler
}

func (s *OrderDisputeService) ConfigureReview(access *repository.DisputeReviewAccess) {
	s.admins = access
	if repo, ok := s.repo.(interface {
		ConfigureReviewAccess(*repository.DisputeReviewAccess)
	}); ok {
		repo.ConfigureReviewAccess(access)
	}
}
func (s *OrderDisputeService) SetSettler(settler escrow.Settler) { s.settler = settler }

// Workflows returns the shared persistence contract for authenticated handlers.
// Each administrative write still authorizes inside its transaction.
func (s *OrderDisputeService) Workflows() *repository.OrderDisputeRepository {
	r, _ := s.repo.(*repository.OrderDisputeRepository)
	return r
}

func (s *OrderDisputeService) GetForActor(ctx context.Context, id int64, actor int) (*models.OrderDispute, error) {
	d, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if d == nil || (d.BuyerID != actor && d.SellerID != actor && (s.admins == nil || !s.admins.IsAdmin(actor))) {
		return nil, repository.ErrDisputeNotFound
	}
	return d, nil
}

func (s *OrderDisputeService) SettleRefund(ctx context.Context, id int64, actor int, version ...int64) (*models.Refund, error) {
	if s.admins == nil || !s.admins.IsAdmin(actor) {
		return nil, ErrRefundAdminOnly
	}
	if s.settler == nil {
		return nil, ErrRefundStoreUnavailable
	}
	store, ok := s.repo.(interface {
		GetRefundByID(context.Context, int64) (*models.Refund, error)
	})
	if !ok {
		return nil, ErrRefundStoreUnavailable
	}
	r, err := store.GetRefundByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.ApprovedAmount == nil {
		return nil, repository.ErrRefundNotFound
	}
	amount, err := money.ParsePositive(*r.ApprovedAmount)
	if err != nil {
		return nil, repository.ErrRefundInvalidAmount
	}
	var expected *int64
	if len(version) > 0 {
		expected = &version[0]
	}
	_, err = s.settler.RefundOrder(ctx, escrow.RefundRequest{RefundID: r.ID, OrderID: r.OrderID, Amount: amount, ActorID: actor, ExpectedVersion: expected})
	if err != nil && !errors.Is(err, escrow.ErrRefundRequiresReview) {
		return r, err
	}
	return store.GetRefundByID(ctx, id)
}

type disputeAdminChecker interface {
	IsAdmin(int) bool
}

func NewOrderDisputeService(
	repo orderDisputeStore,
	admins ...disputeAdminChecker,
) *OrderDisputeService {
	var adminChecker disputeAdminChecker

	if len(admins) > 0 {
		adminChecker = admins[0]
	}

	return &OrderDisputeService{
		repo:   repo,
		admins: adminChecker,
	}
}

type CreateOrderDisputeInput struct {
	OrderID         int64
	DeliveryID      *int64
	ReasonCode      string
	Description     string
	RequestedAmount string
}

// These are the proposed initial dispute reasons.
// The product owner can approve or amend this list.
var validDisputeReasons = map[string]bool{
	models.DisputeReasonNonDelivery:     true,
	models.DisputeReasonMissingQuantity: true,
	models.DisputeReasonQualityIssue:    true,
	"item_not_received":                 true,
	"damaged_item":                      true,
	"wrong_item":                        true,
	"other":                             true,
}

func (s *OrderDisputeService) Create(
	ctx context.Context,
	buyerID int,
	input CreateOrderDisputeInput,
) (*models.OrderDispute, error) {
	if buyerID <= 0 || input.OrderID <= 0 {
		return nil, ErrInvalidDisputeInput
	}

	reason := strings.TrimSpace(input.ReasonCode)
	description := strings.TrimSpace(input.Description)
	amount := strings.TrimSpace(input.RequestedAmount)

	if !validDisputeReasons[reason] {
		return nil, ErrInvalidDisputeReason
	}

	if description == "" || len(description) > 5000 {
		return nil, ErrInvalidDisputeInput
	}

	if _, err := money.ParsePositive(amount); err != nil {
		return nil, ErrInvalidDisputeInput
	}

	if input.DeliveryID != nil && *input.DeliveryID <= 0 {
		return nil, ErrInvalidDisputeInput
	}

	dispute := &models.OrderDispute{
		OrderID:         input.OrderID,
		DeliveryID:      input.DeliveryID,
		BuyerID:         buyerID,
		ReasonCode:      reason,
		Description:     description,
		RequestedAmount: amount,
	}

	if err := s.repo.Create(ctx, dispute); err != nil {
		return nil, err
	}

	return dispute, nil
}

func (s *OrderDisputeService) Respond(
	ctx context.Context,
	disputeID int64,
	sellerID int,
	response string,
) error {
	response = strings.TrimSpace(response)

	if disputeID <= 0 || sellerID <= 0 {
		return ErrInvalidDisputeInput
	}

	if response == "" || len(response) > 5000 {
		return ErrInvalidSellerResponse
	}

	return s.repo.AddSellerResponse(ctx, disputeID, sellerID, response)
}

func (s *OrderDisputeService) GetByID(
	ctx context.Context,
	disputeID int64,
) (*models.OrderDispute, error) {
	if disputeID <= 0 {
		return nil, ErrInvalidDisputeInput
	}

	dispute, err := s.repo.GetByID(ctx, disputeID)
	if err != nil {
		return nil, fmt.Errorf("get dispute: %w", err)
	}
	return dispute, nil
}

func (s *OrderDisputeService) ListByBuyer(
	ctx context.Context,
	buyerID int,
) ([]models.OrderDispute, error) {
	if buyerID <= 0 {
		return nil, ErrInvalidDisputeInput
	}
	return s.repo.ListByBuyer(ctx, buyerID)
}

func (s *OrderDisputeService) ListBySeller(
	ctx context.Context,
	sellerID int,
) ([]models.OrderDispute, error) {
	if sellerID <= 0 {
		return nil, ErrInvalidDisputeInput
	}
	return s.repo.ListBySeller(ctx, sellerID)
}

func (s *OrderDisputeService) ListEvents(
	ctx context.Context,
	disputeID int64,
) ([]models.OrderDisputeEvent, error) {
	if disputeID <= 0 {
		return nil, ErrInvalidDisputeInput
	}
	return s.repo.ListEvents(ctx, disputeID)
}

func (s *OrderDisputeService) StartAdminReview(
	ctx context.Context,
	disputeID int64,
	adminID int,
	version ...int64,
) error {
	if disputeID <= 0 || adminID <= 0 {
		return ErrInvalidDisputeInput
	}

	if s.admins == nil || !s.admins.IsAdmin(adminID) {
		return ErrDisputeAdminOnly
	}

	return s.repo.StartAdminReview(ctx, disputeID, adminID, version...)
}

func (s *OrderDisputeService) Reject(
	ctx context.Context,
	disputeID int64,
	adminID int,
	reason string,
	version ...int64,
) error {
	reason = strings.TrimSpace(reason)

	if disputeID <= 0 || adminID <= 0 {
		return ErrInvalidDisputeInput
	}
	if s.admins == nil || !s.admins.IsAdmin(adminID) {
		return ErrDisputeAdminOnly
	}
	if reason == "" || len(reason) > 5000 {
		return ErrInvalidDisputeInput
	}

	return s.repo.Reject(ctx, disputeID, adminID, reason, version...)
}

// ApproveRefund authorizes a refund after checking administrator access.
// The approved record is settled through the shared wallet contract.
func (s *OrderDisputeService) ApproveRefund(
	ctx context.Context,
	disputeID int64,
	adminID int,
	approvedAmount string,
	approvalNote string,
	version ...int64,
) (*models.Refund, error) {
	approvedAmount = strings.TrimSpace(approvedAmount)
	approvalNote = strings.TrimSpace(approvalNote)

	if disputeID <= 0 || adminID <= 0 ||
		approvedAmount == "" ||
		approvalNote == "" ||
		len(approvalNote) > 3000 {
		return nil, ErrInvalidDisputeInput
	}

	if s.admins == nil || !s.admins.IsAdmin(adminID) {
		return nil, ErrRefundAdminOnly
	}

	store, ok := s.repo.(orderRefundStore)
	if !ok {
		return nil, ErrRefundStoreUnavailable
	}

	refund, err := store.ApproveRefund(
		ctx,
		disputeID,
		adminID,
		approvedAmount,
		approvalNote,
		version...,
	)
	if err != nil {
		return nil, err
	}
	// Approval advances the case version. Settlement must use that approved
	// revision, so evidence uploaded after approval forces another review.
	settlementVersion := version
	if len(version) > 0 {
		settlementVersion = []int64{version[0] + 1}
	}
	settled, err := s.SettleRefund(ctx, refund.ID, adminID, settlementVersion...)
	if err != nil {
		// The approval committed separately. Preserve its identity for the caller
		// even when the settlement infrastructure is unavailable.
		return refund, err
	}
	return settled, nil
}

// UpdateRefundStatus is an internal settlement hook.
// It must only be called after a trusted wallet settlement result.
func (s *OrderDisputeService) UpdateRefundStatus(
	ctx context.Context,
	refundID int64,
	status string,
	detail string,
) error {
	if refundID <= 0 {
		return ErrInvalidDisputeInput
	}

	store, ok := s.repo.(orderRefundStore)
	if !ok {
		return ErrRefundStoreUnavailable
	}

	return store.UpdateRefundStatus(ctx, refundID, status, detail)
}
