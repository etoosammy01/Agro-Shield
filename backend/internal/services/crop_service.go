package services

import (
	"errors"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
)

// CropService contains the business logic for crops/products.
type CropService struct {
	repo      *repository.CropRepository
	eventRepo *repository.MarketEventRepository
}

// NewCropService creates a new CropService.
//
// Responsibility:
// - Connect the service to the CropRepository.
func NewCropService(repo *repository.CropRepository, eventRepo *repository.MarketEventRepository) *CropService {
	return &CropService{
		repo:      repo,
		eventRepo: eventRepo,
	}
}

// AddCrop creates a new crop/product listing.
//
// Responsibility:
// - Validate the product information.
// - Require at least one product picture.
// - Allow multiple product pictures.
// - Create the product through the repository.
func (s *CropService) AddCrop(
	farmerID int,
	name string,
	unit string,
	location string,
	quantity float64,
	price float64,
	listForSale bool,
	imageURLs []string,
	latitude, longitude, accuracy float64,
) error {

	name = strings.TrimSpace(name)
	unit = strings.TrimSpace(unit)
	location = strings.TrimSpace(location)

	if farmerID <= 0 {
		return errors.New("invalid farmer")
	}

	if name == "" {
		return errors.New("crop name is required")
	}

	if unit == "" {
		return errors.New("unit is required")
	}

	if location == "" {
		return errors.New("location is required")
	}

	// A product must have at least one picture.
	// More pictures are allowed.
	var validImages []string

	for _, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)

		if imageURL != "" {
			validImages = append(validImages, imageURL)
		}
	}

	if len(validImages) == 0 {
		return errors.New("at least one crop picture is required")
	}

	// Validate GPS location.
	if latitude < -90 ||
		latitude > 90 ||
		longitude < -180 ||
		longitude > 180 ||
		(latitude == 0 && longitude == 0) ||
		accuracy <= 0 {
		return errors.New("valid farm GPS location is required")
	}

	if quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}

	if listForSale && price <= 0 {
		return errors.New("price must be greater than zero to list for sale")
	}

	// Create the crop record.
	crop := &models.Crop{
		FarmerID:         farmerID,
		Name:             name,
		Quantity:         quantity,
		Unit:             unit,
		Location:         location,
		PricePerUnit:     price,
		ListedForSale:    listForSale,
		ImageURL:         validImages[0],
		Latitude:         latitude,
		Longitude:        longitude,
		LocationAccuracy: accuracy,
	}

	if err := s.repo.Create(crop); err != nil {
		return err
	}

	// Save all product pictures.
	if err := s.repo.AddImages(crop.ID, validImages); err != nil {
		return err
	}

	return nil
}

// MyCrops returns all products belonging to a farmer.
//
// Responsibility:
// - Return both listed and unlisted products.
func (s *CropService) MyCrops(farmerID int) ([]models.Crop, error) {
	if farmerID <= 0 {
		return nil, errors.New("invalid farmer")
	}

	return s.repo.ListByFarmer(farmerID)
}

// AvailableCrops returns products currently available
// for buyers in the marketplace.
func (s *CropService) AvailableCrops() ([]models.Crop, error) {
	return s.repo.ListAvailable()
}

// GetCrop retrieves one product by ID.
func (s *CropService) GetCrop(cropID int) (*models.Crop, error) {
	if cropID <= 0 {
		return nil, errors.New("invalid crop ID")
	}

	return s.repo.GetByID(cropID)
}

// GetCropImages retrieves all images belonging to a product.
//
// Responsibility:
// - Return all product pictures.
// - Keep image retrieval inside the service layer.
func (s *CropService) GetCropImages(cropID int) ([]models.CropImage, error) {
	if cropID <= 0 {
		return nil, errors.New("invalid crop ID")
	}

	return s.repo.ListImages(cropID)
}

// UpdateCrop updates a farmer's own product.
//
// Responsibility:
// - Verify the farmer owns the product.
// - Validate the new product information.
// - Allow multiple product pictures.
// - Keep existing pictures when no new pictures are supplied.
func (s *CropService) UpdateCrop(
	farmerID int,
	cropID int,
	name string,
	unit string,
	location string,
	quantity float64,
	price float64,
	listForSale bool,
	imageURLs []string,
	latitude, longitude, accuracy float64,
) error {

	if farmerID <= 0 {
		return errors.New("invalid farmer")
	}

	if cropID <= 0 {
		return errors.New("invalid crop ID")
	}

	name = strings.TrimSpace(name)
	unit = strings.TrimSpace(unit)
	location = strings.TrimSpace(location)

	if name == "" {
		return errors.New("crop name is required")
	}

	if unit == "" {
		return errors.New("unit is required")
	}

	if location == "" {
		return errors.New("location is required")
	}

	// Validate GPS location.
	if latitude < -90 ||
		latitude > 90 ||
		longitude < -180 ||
		longitude > 180 ||
		(latitude == 0 && longitude == 0) ||
		accuracy <= 0 {
		return errors.New("valid farm GPS location is required")
	}

	if quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}

	if listForSale && price <= 0 {
		return errors.New("price must be greater than zero to list for sale")
	}

	// Find the existing product.
	crop, err := s.repo.GetByID(cropID)
	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New("crop not found")
	}

	// Make sure the logged-in farmer owns the product.
	if crop.FarmerID != farmerID {
		return errors.New("you are not allowed to modify this product")
	}

	// Update product information.
	crop.Name = name
	crop.Unit = unit
	crop.Location = location
	crop.Quantity = quantity
	crop.PricePerUnit = price
	crop.ListedForSale = listForSale
	crop.Latitude = latitude
	crop.Longitude = longitude
	crop.LocationAccuracy = accuracy

	// Process new pictures.
	var validImages []string

	for _, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)

		if imageURL != "" {
			validImages = append(validImages, imageURL)
		}
	}

	// If new pictures were supplied,
	// make the first one the main product picture.
	if len(validImages) > 0 {
		crop.ImageURL = validImages[0]
	}

	// Update the main crop record.
	if err := s.repo.Update(crop); err != nil {
		return err
	}

	// Save any new pictures.
	if len(validImages) > 0 {
		if err := s.repo.AddImages(crop.ID, validImages); err != nil {
			return err
		}
	}

	// Record this update as an activity event for the dashboard.
	if s.eventRepo != nil {
		cid := crop.ID
		uid := farmerID
		_ = s.eventRepo.Record(&models.MarketEvent{
			EventType: "crop_updated",
			CropID:    &cid,
			UserID:    &uid,
		})
	}

	return nil
}

// UnlistCrop removes a farmer's product from the marketplace.
//
// Responsibility:
// - Verify ownership.
// - Hide the product from buyers.
// - Keep the product in the farmer's storage.
func (s *CropService) UnlistCrop(
	farmerID int,
	cropID int,
) error {

	if farmerID <= 0 {
		return errors.New("invalid farmer")
	}

	if cropID <= 0 {
		return errors.New("invalid crop ID")
	}

	crop, err := s.repo.GetByID(cropID)
	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New("crop not found")
	}

	if crop.FarmerID != farmerID {
		return errors.New("you are not allowed to modify this product")
	}

	if !crop.ListedForSale {
		return errors.New("product is already unlisted")
	}

	return s.repo.Unlist(cropID, farmerID)
}

// RelistCrop puts an existing product back on the marketplace.
//
// Responsibility:
// - Verify ownership.
// - Make sure quantity is still available.
// - Put the product back on the marketplace.
func (s *CropService) RelistCrop(
	farmerID int,
	cropID int,
) error {

	if farmerID <= 0 {
		return errors.New("invalid farmer")
	}

	if cropID <= 0 {
		return errors.New("invalid crop ID")
	}

	crop, err := s.repo.GetByID(cropID)
	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New("crop not found")
	}

	if crop.FarmerID != farmerID {
		return errors.New("you are not allowed to modify this product")
	}

	if crop.Quantity <= 0 {
		return errors.New("cannot relist a product with no quantity available")
	}

	if crop.ListedForSale {
		return errors.New("product is already listed")
	}

	return s.repo.Relist(cropID, farmerID)
}

// DeleteCrop permanently deletes a farmer's product.
//
// Responsibility:
// - Verify ownership.
// - Delete the product through the repository.
func (s *CropService) DeleteCrop(
	farmerID int,
	cropID int,
) error {

	if farmerID <= 0 {
		return errors.New("invalid farmer")
	}

	if cropID <= 0 {
		return errors.New("invalid crop ID")
	}

	crop, err := s.repo.GetByID(cropID)
	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New("crop not found")
	}

	if crop.FarmerID != farmerID {
		return errors.New("you are not allowed to delete this product")
	}

	return s.repo.Delete(cropID, farmerID)
}

// ListImages retrieves all images belonging to a crop.
func (s *CropService) ListImages(cropID int) ([]models.CropImage, error) {
	if cropID <= 0 {
		return nil, errors.New("invalid crop ID")
	}

	return s.repo.ListImages(cropID)
}