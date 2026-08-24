package models

import "time"

// Crop represents a farmer's agricultural product listing.
type Crop struct {
	ID int

	// FarmerID identifies the farmer who owns this listing.
	FarmerID int

	// Product information.
	Name     string
	Quantity float64
	Unit     string

	// Location where the product is available.
	Location            string
	LGA, State, Country string

	// Price charged per unit of the product.
	PricePerUnit float64

	// Indicates whether the product is currently available
	// as a marketplace listing.
	ListedForSale bool

	// Optional image representing the product.
	ImageURL string

	// Timestamps for tracking the listing lifecycle.
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
	InitialListedQuantity                  float64
	FirstListedAt, FirstOrderAt, SoldOutAt *time.Time

	// SellerName is populated when retrieving marketplace
	// results. It is not stored directly on the crops table.
	SellerName string
}
