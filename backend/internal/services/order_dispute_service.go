package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/models"
)

var (
	ErrDisputeAdminOnly = errors.New("administrator access required")
	ErrInvalidDisputeInput   = errors.New("invalid dispute details")
	ErrInvalidDisputeReason  = errors.New("unsupported dispute reason")
	ErrInvalidSellerResponse = errors.New("seller response is required")
)

type orderDisputeStore interface {
	Create(context.Context, *models.OrderDispute) error
	GetByID(context.Context, int64) (*models.OrderDispute, error)
	ListByBuyer(context.Context, int) ([]models.OrderDispute, error)
	ListBySeller(context.Context, int) ([]models.OrderDispute, error)
	AddSellerResponse(context.Context, int64, int, string) error
	ListEvents(context.Context, int64) ([]models.OrderDisputeEvent, error)
	StartAdminReview(context.Context, int64, int) error
	Reject(context.Context, int64, int, string) error
}

type OrderDisputeService struct {
	repo   orderDisputeStore
	admins disputeAdminChecker
}
type disputeAdminChecker interface {
	IsAdmin(int) bool
}

func NewOrderDisputeService(
	repo orderDisputeStore,
	admins disputeAdminChecker,
) *OrderDisputeService {
	return &OrderDisputeService{
		repo:   repo,
		admins: admins,
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
	"item_not_received": true,
	"damaged_item":      true,
	"wrong_item":        true,
	"missing_quantity":  true,
	"quality_issue":     true,
	"other":             true,
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

	if amount == "" {
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
) error {
	if disputeID <= 0 || adminID <= 0 {
		return ErrInvalidDisputeInput
	}

	if s.admins == nil || !s.admins.IsAdmin(adminID) {
		return ErrDisputeAdminOnly
	}

	return s.repo.StartAdminReview(ctx, disputeID, adminID)
}

func (s *OrderDisputeService) Reject(
	ctx context.Context,
	disputeID int64,
	adminID int,
	reason string,
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

	return s.repo.Reject(ctx, disputeID, adminID, reason)
}