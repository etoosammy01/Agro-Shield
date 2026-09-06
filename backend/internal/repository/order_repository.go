package repository

import (
	"backend/internal/models"
	"database/sql"
)

type OrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

func (r *OrderRepository) Create(order *models.Order) error {
	query := `
		INSERT INTO orders (buyer_id, crop_id, quantity, total_price, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	return r.db.QueryRow(
		query,
		order.BuyerID,
		order.CropID,
		order.Quantity,
		order.TotalPrice,
		order.Status,
	).Scan(&order.ID, &order.CreatedAt)
}

// GetByID returns a single order with crop + seller info, or nil if not found.
func (r *OrderRepository) GetByID(id int) (*models.Order, error) {
	query := `
		SELECT
			orders.id, orders.buyer_id, orders.crop_id,
			orders.quantity, orders.total_price, orders.status, orders.created_at,
			crops.name, crops.unit, crops.image_url, crops.farmer_id,
			seller.full_name, buyer.full_name
		FROM orders
		JOIN crops   ON crops.id = orders.crop_id
		JOIN farmers seller ON seller.id = crops.farmer_id
		JOIN farmers buyer  ON buyer.id  = orders.buyer_id
		WHERE orders.id = $1
	`

	var o models.Order
	err := r.db.QueryRow(query, id).Scan(
		&o.ID, &o.BuyerID, &o.CropID,
		&o.Quantity, &o.TotalPrice, &o.Status, &o.CreatedAt,
		&o.CropName, &o.CropUnit, &o.ImageURL, &o.SellerID,
		&o.SellerName, &o.BuyerName,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ListByBuyer returns everything a buyer has purchased.
func (r *OrderRepository) ListByBuyer(buyerID int) ([]models.Order, error) {
	query := `
		SELECT
			orders.id, orders.buyer_id, orders.crop_id,
			orders.quantity, orders.total_price, orders.status, orders.created_at,
			crops.name, crops.unit, crops.image_url, crops.farmer_id,
			seller.full_name, buyer.full_name
		FROM orders
		JOIN crops   ON crops.id = orders.crop_id
		JOIN farmers seller ON seller.id = crops.farmer_id
		JOIN farmers buyer  ON buyer.id  = orders.buyer_id
		WHERE orders.buyer_id = $1
		ORDER BY orders.created_at DESC
	`
	return r.queryOrders(query, buyerID)
}

// ListSalesByFarmer returns everything sold from a farmer's crops.
func (r *OrderRepository) ListSalesByFarmer(farmerID int) ([]models.Order, error) {
	query := `
		SELECT
			orders.id, orders.buyer_id, orders.crop_id,
			orders.quantity, orders.total_price, orders.status, orders.created_at,
			crops.name, crops.unit, crops.image_url, crops.farmer_id,
			seller.full_name, buyer.full_name
		FROM orders
		JOIN crops   ON crops.id = orders.crop_id
		JOIN farmers seller ON seller.id = crops.farmer_id
		JOIN farmers buyer  ON buyer.id  = orders.buyer_id
		WHERE crops.farmer_id = $1
		ORDER BY orders.created_at DESC
	`
	return r.queryOrders(query, farmerID)
}

// ---------- helper ----------

func (r *OrderRepository) queryOrders(query string, args ...any) ([]models.Order, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []models.Order
	for rows.Next() {
		var o models.Order
		if err := rows.Scan(
			&o.ID, &o.BuyerID, &o.CropID,
			&o.Quantity, &o.TotalPrice, &o.Status, &o.CreatedAt,
			&o.CropName, &o.CropUnit, &o.ImageURL, &o.SellerID,
			&o.SellerName, &o.BuyerName,
		); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
