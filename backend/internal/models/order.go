package models

import "time"

type Order struct {
	ID         int
	BuyerID    int
	CropID     int
	Quantity   float64
	TotalPrice float64
	Status     string
	CreatedAt  time.Time

	// ---- Display-only fields (not stored in the orders table) ----
	CropName   string
	SellerName string
	BuyerName  string
	SellerID   int    // useful when you need the farmer who owns the crop
	CropUnit   string // e.g. "kg", "bags"
	ImageURL   string // primary crop image for order history
}
