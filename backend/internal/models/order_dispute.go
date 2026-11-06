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
	RefundRequested      = "requested"
	RefundProcessing     = "processing"
	RefundSucceeded      = "succeeded"
	RefundFailed         = "failed"
	RefundRequiresReview = "requires_review"
)

const (
	RefundMethodWallet      = "wallet"
	RefundMethodFlutterwave = "flutterwave"
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

// OrderDisputeEvidence records evidence attached to a dispute.
// StorageKey identifies the file in private storage.
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

// OrderDisputeEvent provides a history of actions taken on a dispute.
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

// OrderRefund records an approved refund and its settlement status.
type OrderRefund struct {
	ID               int64
	DisputeID        int64
	OrderID          int64
	PaymentID        *int64
	PaymentMethod    string
	Amount           string
	Currency         string
	Status           string
	IdempotencyKey   string
	Reference        string
	ProviderRefundID *string
	RequestedBy      int
	FailureReason    *string
	ProcessedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
