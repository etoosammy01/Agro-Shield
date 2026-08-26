package services

import (
	"errors"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
)

// CropService contains the business logic for crops/products.
type CropService struct {
	repo *repository.CropRepository
}

// NewCropService creates a new CropService.
//
// Responsibility:
// - Connect the service to the CropRepository.
func NewCropService(repo *repository.CropRepository) *CropService {
	return &CropService{
		repo: repo,
	}
}

// AddCrop creates a new crop/product listing.
//
// Responsibility:
// - Validate the product information.
// - Create the product through the repository.
func (s *CropService) AddCrop(
	farmerID int,
	name string,
	unit string,
	location string,
	quantity float64,
	price float64,
	listForSale bool,
	imageURL string,
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

	if strings.TrimSpace(imageURL) == "" {
		return errors.New("crop picture is required")
	}

	if unit == "" {
		return errors.New("unit is required")
	}

	if location == "" {
		return errors.New("location is required")
	}
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || (latitude == 0 && longitude == 0) || accuracy <= 0 {
		return errors.New("valid farm GPS location is required")
	}

	if quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}

	if listForSale && price <= 0 {
		return errors.New("price must be greater than zero to list for sale")
	}

	crop := &models.Crop{
		FarmerID:      farmerID,
		Name:          name,
		Quantity:      quantity,
		Unit:          unit,
		Location:      location,
		PricePerUnit:  price,
		ListedForSale: listForSale,
		ImageURL:      imageURL,
		Latitude:      latitude, Longitude: longitude, LocationAccuracy: accuracy,
	}

	return s.repo.Create(crop)
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
//
// Responsibility:
// - Return only products that are listed and have quantity remaining.
func (s *CropService) AvailableCrops() ([]models.Crop, error) {
	return s.repo.ListAvailable()
}

// GetCrop retrieves one product by ID.
//
// Responsibility:
// - Return the product requested by the caller.
func (s *CropService) GetCrop(cropID int) (*models.Crop, error) {
	if cropID <= 0 {
		return nil, errors.New("invalid crop ID")
	}

	return s.repo.GetByID(cropID)
}

// UpdateCrop updates a farmer's own product.
//
// Responsibility:
// - Verify the farmer owns the product.
// - Validate the new product information.
// - Save the changes.
func (s *CropService) UpdateCrop(
	farmerID int,
	cropID int,
	name string,
	unit string,
	location string,
	quantity float64,
	price float64,
	listForSale bool,
	imageURL string,
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
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || (latitude == 0 && longitude == 0) || accuracy <= 0 {
		return errors.New("valid farm GPS location is required")
	}

	if quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}

	if listForSale && price <= 0 {
		return errors.New("price must be greater than zero to list for sale")
	}

	// Make sure this product belongs to the logged-in farmer.
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

	crop.Name = name
	crop.Unit = unit
	crop.Location = location
	crop.Quantity = quantity
	crop.PricePerUnit = price
	crop.ListedForSale = listForSale
	crop.Latitude, crop.Longitude, crop.LocationAccuracy = latitude, longitude, accuracy

	// Only replace the image when a new image was provided.
	if strings.TrimSpace(imageURL) != "" {
		crop.ImageURL = imageURL
	}

	return s.repo.Update(crop)
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
//   - Verify ownership.
//   - Prevent deletion of products that already have
//     business history.
//
// NOTE:
// The transaction-history protection will be expanded when
// we connect the crop lifecycle to orders and negotiations.
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
