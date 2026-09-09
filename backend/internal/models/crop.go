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
	Latitude, Longitude float64
	LocationAccuracy    float64
	LGA, State, Country string

	// Price charged per unit of the product.
	PricePerUnit float64

	// Indicates whether the product is currently available
	// as a marketplace listing.
	ListedForSale bool

	// Product images.
	//
	// ImageURL is kept temporarily for backward compatibility
	// with the existing crops.image_url column.
	//
	// Images contains all product pictures.
	// A product must have at least one image when created.
	ImageURL string
	Images   []CropImage

	// Timestamps for tracking the listing lifecycle.
	CreatedAt                              time.Time
	UpdatedAt                              time.Time
	InitialListedQuantity                  float64
	FirstListedAt, FirstOrderAt, SoldOutAt *time.Time

	// SellerName is populated when retrieving marketplace
	// results. It is not stored directly on the crops table.
	SellerName     string
	SellerPhotoURL string
}

// CropImage represents one picture belonging to a crop/product.
type CropImage struct {
	ID        int
	CropID    int
	ImageURL  string
	IsPrimary bool
	SortOrder int
	CreatedAt time.Time
}
