package repository

import (
	"backend/internal/models"
	"database/sql"
	"time"
)

type DeliveryRepository struct {
	db *sql.DB
}

func NewDeliveryRepository(db *sql.DB) *DeliveryRepository {
	return &DeliveryRepository{db: db}
}

// Create inserts a new delivery record and sets the generated ID on the struct.
func (r *DeliveryRepository) Create(d *models.Delivery) error {
	query := `
		INSERT INTO deliveries (
			order_id, buyer_id, farmer_id, crop_id, quantity,
			delivery_address, lga, state, country,
			latitude, longitude,
			status, courier_name, courier_phone, tracking_number, vehicle_info,
			scheduled_at, estimated_arrival, notes
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19
		)
		RETURNING id, created_at, updated_at
	`

	return r.db.QueryRow(
		query,
		d.OrderID, d.BuyerID, d.FarmerID, d.CropID, d.Quantity,
		d.DeliveryAddress, nullString(d.LGA), nullString(d.State), d.Country,
		nullFloat(d.Latitude), nullFloat(d.Longitude),
		d.Status, nullString(d.CourierName), nullString(d.CourierPhone),
		nullString(d.TrackingNumber), nullString(d.VehicleInfo),
		nullTime(d.ScheduledAt), nullTime(d.EstimatedArrival), nullString(d.Notes),
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

// GetByID returns a single delivery or nil if not found.
func (r *DeliveryRepository) GetByID(id int) (*models.Delivery, error) {
	query := `
		SELECT
			d.id, d.order_id, d.buyer_id, d.farmer_id, d.crop_id, d.quantity,
			d.delivery_address, d.lga, d.state, d.country,
			d.latitude, d.longitude,
			d.status, d.courier_name, d.courier_phone, d.tracking_number, d.vehicle_info,
			d.scheduled_at, d.estimated_arrival, d.picked_up_at, d.delivered_at, d.cancelled_at,
			d.notes, d.proof_of_delivery, d.failure_reason,
			d.created_at, d.updated_at,
			c.name, c.unit, c.image_url,
			buyer.full_name, seller.full_name
		FROM deliveries d
		JOIN crops   c      ON c.id = d.crop_id
		JOIN farmers buyer  ON buyer.id = d.buyer_id
		JOIN farmers seller ON seller.id = d.farmer_id
		WHERE d.id = $1
	`

	var d models.Delivery
	err := r.scanDelivery(r.db.QueryRow(query, id), &d)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListByBuyer returns all deliveries for a buyer (most recent first).
func (r *DeliveryRepository) ListByBuyer(buyerID int) ([]models.Delivery, error) {
	query := `
		SELECT
			d.id, d.order_id, d.buyer_id, d.farmer_id, d.crop_id, d.quantity,
			d.delivery_address, d.lga, d.state, d.country,
			d.latitude, d.longitude,
			d.status, d.courier_name, d.courier_phone, d.tracking_number, d.vehicle_info,
			d.scheduled_at, d.estimated_arrival, d.picked_up_at, d.delivered_at, d.cancelled_at,
			d.notes, d.proof_of_delivery, d.failure_reason,
			d.created_at, d.updated_at,
			c.name, c.unit, c.image_url,
			buyer.full_name, seller.full_name
		FROM deliveries d
		JOIN crops   c      ON c.id = d.crop_id
		JOIN farmers buyer  ON buyer.id = d.buyer_id
		JOIN farmers seller ON seller.id = d.farmer_id
		WHERE d.buyer_id = $1
		ORDER BY d.created_at DESC
	`
	return r.queryDeliveries(query, buyerID)
}

// ListByFarmer returns all deliveries the farmer needs to fulfil.
func (r *DeliveryRepository) ListByFarmer(farmerID int) ([]models.Delivery, error) {
	query := `
		SELECT
			d.id, d.order_id, d.buyer_id, d.farmer_id, d.crop_id, d.quantity,
			d.delivery_address, d.lga, d.state, d.country,
			d.latitude, d.longitude,
			d.status, d.courier_name, d.courier_phone, d.tracking_number, d.vehicle_info,
			d.scheduled_at, d.estimated_arrival, d.picked_up_at, d.delivered_at, d.cancelled_at,
			d.notes, d.proof_of_delivery, d.failure_reason,
			d.created_at, d.updated_at,
			c.name, c.unit, c.image_url,
			buyer.full_name, seller.full_name
		FROM deliveries d
		JOIN crops   c      ON c.id = d.crop_id
		JOIN farmers buyer  ON buyer.id = d.buyer_id
		JOIN farmers seller ON seller.id = d.farmer_id
		WHERE d.farmer_id = $1
		ORDER BY d.created_at DESC
	`
	return r.queryDeliveries(query, farmerID)
}

// ListByOrder returns the delivery (if any) linked to an order.
func (r *DeliveryRepository) ListByOrder(orderID int) ([]models.Delivery, error) {
	query := `
		SELECT
			d.id, d.order_id, d.buyer_id, d.farmer_id, d.crop_id, d.quantity,
			d.delivery_address, d.lga, d.state, d.country,
			d.latitude, d.longitude,
			d.status, d.courier_name, d.courier_phone, d.tracking_number, d.vehicle_info,
			d.scheduled_at, d.estimated_arrival, d.picked_up_at, d.delivered_at, d.cancelled_at,
			d.notes, d.proof_of_delivery, d.failure_reason,
			d.created_at, d.updated_at,
			c.name, c.unit, c.image_url,
			buyer.full_name, seller.full_name
		FROM deliveries d
		JOIN crops   c      ON c.id = d.crop_id
		JOIN farmers buyer  ON buyer.id = d.buyer_id
		JOIN farmers seller ON seller.id = d.farmer_id
		WHERE d.order_id = $1
		ORDER BY d.created_at DESC
	`
	return r.queryDeliveries(query, orderID)
}

// UpdateStatus changes the delivery status and sets the matching timestamp.
func (r *DeliveryRepository) UpdateStatus(id int, status string) error {
	var extra string
	switch status {
	case "picked_up":
		extra = ", picked_up_at = CURRENT_TIMESTAMP"
	case "delivered":
		extra = ", delivered_at = CURRENT_TIMESTAMP"
	case "cancelled":
		extra = ", cancelled_at = CURRENT_TIMESTAMP"
	}

	query := `
		UPDATE deliveries
		SET status = $1, updated_at = CURRENT_TIMESTAMP` + extra + `
		WHERE id = $2
	`
	_, err := r.db.Exec(query, status, id)
	return err
}

// UpdateTracking sets courier / tracking details.
func (r *DeliveryRepository) UpdateTracking(id int, courierName, courierPhone, trackingNumber, vehicleInfo string) error {
	_, err := r.db.Exec(`
		UPDATE deliveries
		SET courier_name    = $1,
		    courier_phone   = $2,
		    tracking_number = $3,
		    vehicle_info    = $4,
		    updated_at      = CURRENT_TIMESTAMP
		WHERE id = $5
	`, nullString(courierName), nullString(courierPhone),
		nullString(trackingNumber), nullString(vehicleInfo), id)
	return err
}

// MarkDelivered sets status to delivered and stores proof of delivery.
func (r *DeliveryRepository) MarkDelivered(id int, proofURL string) error {
	_, err := r.db.Exec(`
		UPDATE deliveries
		SET status            = 'delivered',
		    delivered_at      = CURRENT_TIMESTAMP,
		    proof_of_delivery = $1,
		    updated_at        = CURRENT_TIMESTAMP
		WHERE id = $2
	`, nullString(proofURL), id)
	return err
}

// MarkFailed records a failed delivery.
func (r *DeliveryRepository) MarkFailed(id int, reason string) error {
	_, err := r.db.Exec(`
		UPDATE deliveries
		SET status         = 'failed',
		    failure_reason = $1,
		    updated_at     = CURRENT_TIMESTAMP
		WHERE id = $2
	`, nullString(reason), id)
	return err
}

// ---------- helpers ----------

func (r *DeliveryRepository) queryDeliveries(query string, args ...any) ([]models.Delivery, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Delivery
	for rows.Next() {
		var d models.Delivery
		if err := r.scanDelivery(rows, &d); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (r *DeliveryRepository) scanDelivery(scanner interface {
	Scan(dest ...any) error
}, d *models.Delivery) error {
	var (
		lga, state, courierName, courierPhone, trackingNumber, vehicleInfo  sql.NullString
		notes, proof, failureReason                                         sql.NullString
		lat, lng                                                            sql.NullFloat64
		scheduledAt, estimatedArrival, pickedUpAt, deliveredAt, cancelledAt sql.NullTime
		cropName, cropUnit, imageURL, buyerName, sellerName                 string
	)

	err := scanner.Scan(
		&d.ID, &d.OrderID, &d.BuyerID, &d.FarmerID, &d.CropID, &d.Quantity,
		&d.DeliveryAddress, &lga, &state, &d.Country,
		&lat, &lng,
		&d.Status, &courierName, &courierPhone, &trackingNumber, &vehicleInfo,
		&scheduledAt, &estimatedArrival, &pickedUpAt, &deliveredAt, &cancelledAt,
		&notes, &proof, &failureReason,
		&d.CreatedAt, &d.UpdatedAt,
		&cropName, &cropUnit, &imageURL,
		&buyerName, &sellerName,
	)
	if err != nil {
		return err
	}

	d.LGA = lga.String
	d.State = state.String
	d.CourierName = courierName.String
	d.CourierPhone = courierPhone.String
	d.TrackingNumber = trackingNumber.String
	d.VehicleInfo = vehicleInfo.String
	d.Notes = notes.String
	d.ProofOfDelivery = proof.String
	d.FailureReason = failureReason.String

	if lat.Valid {
		d.Latitude = &lat.Float64
	}
	if lng.Valid {
		d.Longitude = &lng.Float64
	}
	if scheduledAt.Valid {
		d.ScheduledAt = &scheduledAt.Time
	}
	if estimatedArrival.Valid {
		d.EstimatedArrival = &estimatedArrival.Time
	}
	if pickedUpAt.Valid {
		d.PickedUpAt = &pickedUpAt.Time
	}
	if deliveredAt.Valid {
		d.DeliveredAt = &deliveredAt.Time
	}
	if cancelledAt.Valid {
		d.CancelledAt = &cancelledAt.Time
	}

	d.CropName = cropName
	d.CropUnit = cropUnit
	d.ImageURL = imageURL
	d.BuyerName = buyerName
	d.SellerName = sellerName

	return nil
}

// null helpers keep the INSERT/UPDATE calls tidy
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullFloat(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
