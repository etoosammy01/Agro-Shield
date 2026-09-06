package services

import (
	"errors"

	"backend/internal/models"
	"backend/internal/repository"
)

type OrderService struct {
	orderRepo *repository.OrderRepository
	cropRepo  *repository.CropRepository
}

func NewOrderService(orderRepo *repository.OrderRepository, cropRepo *repository.CropRepository) *OrderService {
	return &OrderService{orderRepo: orderRepo, cropRepo: cropRepo}
}

// PlaceOrder lets a buyer purchase a quantity of a listed crop. This executes
// immediately (no accept/reject step from the farmer) to keep the first
// version simple — see the README for how to extend this into a
// request/approve flow later if you want farmers to confirm orders first.
func (s *OrderService) PlaceOrder(buyerID, cropID int, quantity float64) error {
	if quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}

	crop, err := s.cropRepo.GetByID(cropID)
	if err != nil {
		return err
	}
	if crop == nil || !crop.ListedForSale {
		return errors.New("this produce is not available for purchase")
	}
	if crop.FarmerID == buyerID {
		return errors.New("you can't buy your own produce")
	}
	if quantity > crop.Quantity {
		return errors.New("not enough quantity available")
	}

	order := &models.Order{
		BuyerID:    buyerID,
		CropID:     cropID,
		Quantity:   quantity,
		TotalPrice: quantity * crop.PricePerUnit,
		Status:     "completed",
	}

	if err := s.orderRepo.Create(order); err != nil {
		return err
	}

	return s.cropRepo.ReduceQuantity(cropID, quantity)
}

func (s *OrderService) MyPurchases(buyerID int) ([]models.Order, error) {
	return s.orderRepo.ListByBuyer(buyerID)
}

func (s *OrderService) MySales(farmerID int) ([]models.Order, error) {
	return s.orderRepo.ListSalesByFarmer(farmerID)
}

// GetByID returns a single order (used by the order detail page).
func (s *OrderService) GetByID(id int) (*models.Order, error) {
	return s.orderRepo.GetByID(id)
}
