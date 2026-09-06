package services

import (
	"errors"

	"backend/internal/models"
	"backend/internal/repository"
)

type DeliveryService struct {
	deliveryRepo *repository.DeliveryRepository
	orderRepo    *repository.OrderRepository
}

func NewDeliveryService(
	deliveryRepo *repository.DeliveryRepository,
	orderRepo *repository.OrderRepository,
) *DeliveryService {
	return &DeliveryService{
		deliveryRepo: deliveryRepo,
		orderRepo:    orderRepo,
	}
}

// CreateFromOrder creates a delivery record for an existing order.
// The buyer supplies the delivery address; the farmer is taken from the order.
func (s *DeliveryService) CreateFromOrder(
	orderID, buyerID int,
	address, lga, state, country string,
	quantity float64,
) (*models.Delivery, error) {

	if address == "" {
		return nil, errors.New("delivery address is required")
	}
	if quantity <= 0 {
		return nil, errors.New("quantity must be greater than zero")
	}

	order, err := s.orderRepo.GetByID(orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.New("order not found")
	}
	if order.BuyerID != buyerID {
		return nil, errors.New("you can only create a delivery for your own order")
	}
	if quantity > order.Quantity {
		return nil, errors.New("delivery quantity cannot exceed the ordered quantity")
	}

	if country == "" {
		country = "Nigeria"
	}

	delivery := &models.Delivery{
		OrderID:         orderID,
		BuyerID:         order.BuyerID,
		FarmerID:        order.SellerID,
		CropID:          order.CropID,
		Quantity:        quantity,
		DeliveryAddress: address,
		LGA:             lga,
		State:           state,
		Country:         country,
		Status:          "pending",
	}

	if err := s.deliveryRepo.Create(delivery); err != nil {
		return nil, err
	}

	return delivery, nil
}

// GetByID returns a single delivery (with display fields).
func (s *DeliveryService) GetByID(id int) (*models.Delivery, error) {
	return s.deliveryRepo.GetByID(id)
}

// MyDeliveries returns all deliveries for a buyer.
func (s *DeliveryService) MyDeliveries(buyerID int) ([]models.Delivery, error) {
	return s.deliveryRepo.ListByBuyer(buyerID)
}

// MyOutgoing returns all deliveries a farmer must fulfil.
func (s *DeliveryService) MyOutgoing(farmerID int) ([]models.Delivery, error) {
	return s.deliveryRepo.ListByFarmer(farmerID)
}

// UpdateStatus changes the delivery status.
// Only the farmer who owns the delivery (or the system) should call this.
func (s *DeliveryService) UpdateStatus(deliveryID, farmerID int, status string) error {
	allowed := map[string]bool{
		"pending":    true,
		"confirmed":  true,
		"picked_up":  true,
		"in_transit": true,
		"delivered":  true,
		"failed":     true,
		"cancelled":  true,
	}
	if !allowed[status] {
		return errors.New("invalid delivery status")
	}

	delivery, err := s.deliveryRepo.GetByID(deliveryID)
	if err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("delivery not found")
	}
	if delivery.FarmerID != farmerID {
		return errors.New("you can only update your own deliveries")
	}

	return s.deliveryRepo.UpdateStatus(deliveryID, status)
}

// UpdateTracking lets the farmer attach courier / tracking details.
func (s *DeliveryService) UpdateTracking(
	deliveryID, farmerID int,
	courierName, courierPhone, trackingNumber, vehicleInfo string,
) error {
	delivery, err := s.deliveryRepo.GetByID(deliveryID)
	if err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("delivery not found")
	}
	if delivery.FarmerID != farmerID {
		return errors.New("you can only update your own deliveries")
	}

	return s.deliveryRepo.UpdateTracking(deliveryID, courierName, courierPhone, trackingNumber, vehicleInfo)
}

// MarkDelivered records a successful delivery and optional proof image.
func (s *DeliveryService) MarkDelivered(deliveryID, farmerID int, proofURL string) error {
	delivery, err := s.deliveryRepo.GetByID(deliveryID)
	if err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("delivery not found")
	}
	if delivery.FarmerID != farmerID {
		return errors.New("you can only update your own deliveries")
	}

	return s.deliveryRepo.MarkDelivered(deliveryID, proofURL)
}

// MarkFailed records a failed delivery with a reason.
func (s *DeliveryService) MarkFailed(deliveryID, farmerID int, reason string) error {
	if reason == "" {
		return errors.New("failure reason is required")
	}

	delivery, err := s.deliveryRepo.GetByID(deliveryID)
	if err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("delivery not found")
	}
	if delivery.FarmerID != farmerID {
		return errors.New("you can only update your own deliveries")
	}

	return s.deliveryRepo.MarkFailed(deliveryID, reason)
}

// Cancel lets the buyer cancel a delivery that is still pending/confirmed.
func (s *DeliveryService) Cancel(deliveryID, buyerID int) error {
	delivery, err := s.deliveryRepo.GetByID(deliveryID)
	if err != nil {
		return err
	}
	if delivery == nil {
		return errors.New("delivery not found")
	}
	if delivery.BuyerID != buyerID {
		return errors.New("you can only cancel your own deliveries")
	}
	if delivery.Status != "pending" && delivery.Status != "confirmed" {
		return errors.New("only pending or confirmed deliveries can be cancelled")
	}

	return s.deliveryRepo.UpdateStatus(deliveryID, "cancelled")
}
