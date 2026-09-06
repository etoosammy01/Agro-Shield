package models

import "time"

type Delivery struct {
	ID       int
	OrderID  int
	BuyerID  int
	FarmerID int
	CropID   int
	Quantity float64

	// Address snapshot
	DeliveryAddress string
	LGA             string
	State           string
	Country         string
	Latitude        *float64
	Longitude       *float64

	// Logistics
	Status         string
	CourierName    string
	CourierPhone   string
	TrackingNumber string
	VehicleInfo    string

	// Timing
	ScheduledAt      *time.Time
	EstimatedArrival *time.Time
	PickedUpAt       *time.Time
	DeliveredAt      *time.Time
	CancelledAt      *time.Time

	// Extra
	Notes           string
	ProofOfDelivery string
	FailureReason   string

	CreatedAt time.Time
	UpdatedAt time.Time

	// ---- Display-only fields (not stored in the deliveries table) ----
	CropName    string
	BuyerName   string
	SellerName  string
	CropUnit    string
	ImageURL    string
	OrderStatus string
}
