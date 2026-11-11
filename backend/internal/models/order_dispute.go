package models

import "time"

const (
	DisputeSubmitted        = "submitted"
	DisputeSellerResponded  = "seller_responded"
	DisputeUnderReview      = "under_review"
	DisputeResolvedRefund   = "resolved_refund"
	DisputeResolvedRejected = "resolved_rejected"
	DisputeWithdrawn        = "withdrawn"
)

const (
	DisputeReasonNonDelivery     = "non_delivery"
	DisputeReasonMissingQuantity = "missing_quantity"
	DisputeReasonQualityIssue    = "quality_issue"
)

const (
	RefundRequested      = "requested"
	RefundProcessing     = "processing"
	RefundSucceeded      = "succeeded"
	RefundFailed         = "failed"
	RefundRequiresReview = "requires_review"
)

// OrderDispute represents a buyer's complaint about an order
// or a specific delivery associated with that order.
type OrderDispute struct {
	ID              int64
	OrderID         int64
	DeliveryID      *int64
	BuyerID         int
	SellerID        int
	ReasonCode      string
	Description     string
	RequestedAmount string
	Status          string
	SellerResponse  *string
	ResolutionNote  *string
	ResolvedBy      *int
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// OrderDisputeEvidence stores evidence associated with a dispute.
// StorageKey points to a privately stored file.
type OrderDisputeEvidence struct {
	ID               int64
	DisputeID        int64
	UploadedBy       int
	StorageKey       string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	CreatedAt        time.Time
}

// OrderDisputeEvent preserves the history of dispute actions.
type OrderDisputeEvent struct {
	ID         int64
	DisputeID  int64
	ActorID    *int
	ActorRole  string
	EventType  string
	FromStatus *string
	ToStatus   *string
	Note       *string
	Metadata   []byte
	CreatedAt  time.Time
}

// Refund is the Member 5 refund record.
// RequestedAmount is what the buyer claims.
// ApprovedAmount is what the administrator authorizes.
// Neither amount moves money by itself.
type Refund struct {
	ID              int64
	DisputeID       int64
	OrderID         int64
	BuyerID         int
	SellerID        int
	ReasonCode      string
	RequestedAmount string
	ApprovedAmount  *string
	Currency        string
	Status          string
	IdempotencyKey  string
	Reference       string
	RequestedBy     int
	ApprovedBy      *int
	ApprovalNote    *string
	FailureReason   *string
	ProcessedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Compatibility alias for any existing code referring to OrderRefund.
type OrderRefund = Refund
