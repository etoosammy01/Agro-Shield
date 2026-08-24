package services

import (
	"errors"
	"fmt"

	"backend/internal/models"
	"backend/internal/repository"
)

// CartService handles all business logic related to the buyer's cart.
type CartService struct {
	cartRepo  *repository.CartRepository
	cropRepo  *repository.CropRepository
	eventRepo *repository.MarketEventRepository
}

// NewCartService creates and returns a new CartService.
//
// Responsibility:
// - Connect the cart service to the cart repository.
// - Connect the cart service to the crop repository.
func NewCartService(
	cartRepo *repository.CartRepository,
	cropRepo *repository.CropRepository,
	eventRepo *repository.MarketEventRepository,
) *CartService {
	return &CartService{
		cartRepo:  cartRepo,
		cropRepo:  cropRepo,
		eventRepo: eventRepo,
	}
}

// AddFromNegotiation adds an accepted negotiation to the buyer's cart.
//
// Responsibility:
// - Make sure the negotiation exists.
// - Make sure the negotiation was accepted.
// - Make sure the agreed quantity is valid.
// - Make sure the crop still exists.
// - Make sure enough quantity is still available.
// - Preserve the negotiated price.
func (s *CartService) AddFromNegotiation(
	negotiation *models.Negotiation,
	agreedPrice float64,
) error {

	if negotiation == nil {
		return errors.New("negotiation is required")
	}

	if negotiation.Status != "accepted" {
		return errors.New("only accepted negotiations can be added to cart")
	}

	if negotiation.Quantity <= 0 {
		return errors.New("invalid negotiation quantity")
	}

	if agreedPrice <= 0 {
		return errors.New("agreed price must be greater than zero")
	}

	// Check that the crop still exists.
	crop, err := s.cropRepo.GetByID(negotiation.CropID)
	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New("product no longer exists")
	}

	// Check that the agreed quantity is still available.
	if negotiation.Quantity > crop.Quantity {
		return errors.New("the requested quantity is no longer available")
	}

	item := &models.CartItem{
		BuyerID:       negotiation.BuyerID,
		CropID:        negotiation.CropID,
		NegotiationID: negotiation.ID,

		Quantity:     negotiation.Quantity,
		PricePerUnit: agreedPrice,
		TotalPrice:   negotiation.Quantity * agreedPrice,
	}

	if err := s.cartRepo.Create(item); err != nil {
		return err
	}
	if s.eventRepo != nil {
		uid := item.BuyerID
		cid := item.CropID
		_ = s.eventRepo.Record(&models.MarketEvent{EventType: "cart_item_added", CropID: &cid, UserID: &uid, Metadata: fmt.Sprintf(`{"quantity":%g}`, item.Quantity)})
	}
	return nil
}

// MyCart returns all cart items belonging to a buyer.
//
// Responsibility:
// - Load the buyer's current cart.
func (s *CartService) MyCart(buyerID int) ([]models.CartItem, error) {
	if buyerID <= 0 {
		return nil, errors.New("invalid buyer")
	}

	return s.cartRepo.ListByBuyer(buyerID)
}

// GetCartItem retrieves one cart item belonging to a buyer.
//
// Responsibility:
// - Get one specific cart item.
// - Prevent a buyer from accessing another buyer's cart item.
func (s *CartService) GetCartItem(
	buyerID,
	cartID int,
) (*models.CartItem, error) {

	if buyerID <= 0 {
		return nil, errors.New("invalid buyer")
	}

	if cartID <= 0 {
		return nil, errors.New("invalid cart item")
	}

	item, err := s.cartRepo.GetByID(cartID, buyerID)
	if err != nil {
		return nil, err
	}

	if item == nil {
		return nil, errors.New("cart item not found")
	}

	return item, nil
}

// RemoveFromCart removes a cart item belonging to the buyer.
//
// Responsibility:
// - Allow the buyer to remove an unwanted cart item.
// - Prevent removing another buyer's cart item.
func (s *CartService) RemoveFromCart(
	buyerID,
	cartID int,
) error {

	if buyerID <= 0 {
		return errors.New("invalid buyer")
	}

	if cartID <= 0 {
		return errors.New("invalid cart item")
	}

	item, err := s.cartRepo.GetByID(cartID, buyerID)
	if err != nil {
		return err
	}

	if item == nil {
		return errors.New("cart item not found")
	}

	return s.cartRepo.Delete(cartID, buyerID)
}
