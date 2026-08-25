package repository

import (
	"backend/internal/models"
	"database/sql"
	"fmt"
)

// CropRepository handles all database operations related to crops/products.
type CropRepository struct {
	db *sql.DB
}

// NewCropRepository creates and returns a new CropRepository.
//
// Responsibility:
// - Connect the crop repository to the application's database connection.
func NewCropRepository(db *sql.DB) *CropRepository {
	return &CropRepository{db: db}
}

// Create saves a new crop/product listing to the database.
//
// Responsibility:
// - Insert a farmer's new product into the crops table.
// - Return the newly generated product ID.
func (r *CropRepository) Create(crop *models.Crop) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO crops (
			farmer_id,
			name,
			quantity,
			unit,
			location,
			price_per_unit,
			listed_for_sale,
			image_url, lga, state, country, latitude, longitude, location_accuracy, initial_listed_quantity, first_listed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $5, COALESCE((SELECT state FROM farmers WHERE id=$1),'Benue'), COALESCE((SELECT country FROM farmers WHERE id=$1),'Nigeria'), $9, $10, $11, CASE WHEN $7 THEN $3 ELSE NULL END, CASE WHEN $7 THEN CURRENT_TIMESTAMP ELSE NULL END)
		RETURNING id
	`

	err = tx.QueryRow(
		query,
		crop.FarmerID,
		crop.Name,
		crop.Quantity,
		crop.Unit,
		crop.Location,
		crop.PricePerUnit,
		crop.ListedForSale,
		crop.ImageURL,
		crop.Latitude, crop.Longitude, crop.LocationAccuracy,
	).Scan(&crop.ID)
	if err != nil {
		return err
	}
	if crop.PricePerUnit > 0 {
		if _, err = tx.Exec(`INSERT INTO produce_price_history (crop_id,price_per_unit,unit,source) VALUES ($1,$2,$3,'Agro-Shield')`, crop.ID, crop.PricePerUnit, crop.Unit); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListByFarmer retrieves all products belonging to a specific farmer.
//
// Responsibility:
// - Return both listed and unlisted products.
// - Used when a farmer wants to view their own products/storage.
func (r *CropRepository) ListByFarmer(farmerID int) ([]models.Crop, error) {
	query := `
		SELECT
			id,
			farmer_id,
			name,
			quantity,
			unit,
			location,
			price_per_unit,
			listed_for_sale,
			image_url,
			initial_listed_quantity, first_listed_at, first_order_at, sold_out_at,
			created_at,
			updated_at
		FROM crops
		WHERE farmer_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var crops []models.Crop

	for rows.Next() {
		var crop models.Crop

		if err := rows.Scan(
			&crop.ID,
			&crop.FarmerID,
			&crop.Name,
			&crop.Quantity,
			&crop.Unit,
			&crop.Location,
			&crop.PricePerUnit,
			&crop.ListedForSale,
			&crop.ImageURL,
			&crop.InitialListedQuantity, &crop.FirstListedAt, &crop.FirstOrderAt, &crop.SoldOutAt,
			&crop.CreatedAt,
			&crop.UpdatedAt,
		); err != nil {
			return nil, err
		}

		crops = append(crops, crop)
	}

	return crops, rows.Err()
}

// ListAvailable retrieves products currently available in the marketplace.
//
// Responsibility:
// - Return only products listed for sale.
// - Exclude products whose quantity is zero.
// - Include the seller's name for marketplace display.
func (r *CropRepository) ListAvailable() ([]models.Crop, error) {
	query := `
		SELECT
			crops.id,
			crops.farmer_id,
			crops.name,
			crops.quantity,
			crops.unit,
			crops.location,
			crops.price_per_unit,
			crops.listed_for_sale,
			crops.image_url,
			crops.created_at,
			crops.updated_at,
			farmers.full_name
		FROM crops
		JOIN farmers ON farmers.id = crops.farmer_id
		WHERE crops.listed_for_sale = TRUE
		  AND crops.quantity > 0
		ORDER BY crops.created_at DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var crops []models.Crop

	for rows.Next() {
		var crop models.Crop

		// The Scan order must match the SELECT order exactly.
		if err := rows.Scan(
			&crop.ID,
			&crop.FarmerID,
			&crop.Name,
			&crop.Quantity,
			&crop.Unit,
			&crop.Location,
			&crop.PricePerUnit,
			&crop.ListedForSale,
			&crop.ImageURL,
			&crop.CreatedAt,
			&crop.UpdatedAt,
			&crop.SellerName,
		); err != nil {
			return nil, err
		}

		crops = append(crops, crop)
	}

	return crops, rows.Err()
}

// GetByID retrieves one product using its ID.
//
// Responsibility:
// - Find a specific product.
// - Return nil when the product does not exist.
func (r *CropRepository) GetByID(id int) (*models.Crop, error) {
	query := `
		SELECT
			id,
			farmer_id,
			name,
			quantity,
			unit,
			location,
			price_per_unit,
			listed_for_sale,
			image_url,
			initial_listed_quantity, first_listed_at, first_order_at, sold_out_at,
			created_at,
			updated_at
		FROM crops
		WHERE id = $1
	`

	var crop models.Crop

	err := r.db.QueryRow(query, id).Scan(
		&crop.ID,
		&crop.FarmerID,
		&crop.Name,
		&crop.Quantity,
		&crop.Unit,
		&crop.Location,
		&crop.PricePerUnit,
		&crop.ListedForSale,
		&crop.ImageURL,
		&crop.InitialListedQuantity, &crop.FirstListedAt, &crop.FirstOrderAt, &crop.SoldOutAt,
		&crop.CreatedAt,
		&crop.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &crop, nil
}

// Update modifies an existing product.
//
// Responsibility:
// - Update only a product owned by the specified farmer.
// - Update product information and marketplace status.
// - Update the updated_at timestamp.
func (r *CropRepository) Update(crop *models.Crop) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		UPDATE crops
		SET
			name = $1,
			quantity = $2,
			unit = $3,
			location = $4,
			price_per_unit = $5,
			listed_for_sale = $6,
			image_url = $7,
			lga = $4,
			state = COALESCE((SELECT state FROM farmers WHERE id=$9), state, 'Benue'),
			country = COALESCE((SELECT country FROM farmers WHERE id=$9), country, 'Nigeria'),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $8
		  AND farmer_id = $9
	`

	result, err := tx.Exec(
		query,
		crop.Name,
		crop.Quantity,
		crop.Unit,
		crop.Location,
		crop.PricePerUnit,
		crop.ListedForSale,
		crop.ImageURL,
		crop.ID,
		crop.FarmerID,
	)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	if crop.PricePerUnit > 0 {
		_, err = tx.Exec(`INSERT INTO produce_price_history (crop_id,price_per_unit,unit,source) VALUES ($1,$2,$3,'Agro-Shield')`, crop.ID, crop.PricePerUnit, crop.Unit)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Unlist removes a product from the marketplace.
//
// Responsibility:
// - Hide the product from buyers.
// - Keep the product in the farmer's storage.
// - Allow the farmer to relist it later.
func (r *CropRepository) Unlist(cropID, farmerID int) error {
	result, err := r.db.Exec(`
		UPDATE crops
		SET
			listed_for_sale = FALSE,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND farmer_id = $2
	`, cropID, farmerID)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Relist puts a farmer's existing product back on the marketplace.
//
// Responsibility:
// - Make the product visible to buyers again.
// - Keep the existing product record.
func (r *CropRepository) Relist(cropID, farmerID int) error {
	result, err := r.db.Exec(`
		UPDATE crops
		SET
			listed_for_sale = TRUE,
			first_listed_at = COALESCE(first_listed_at, CURRENT_TIMESTAMP),
			initial_listed_quantity = CASE WHEN initial_listed_quantity IS NULL OR initial_listed_quantity < quantity THEN quantity ELSE initial_listed_quantity END,
			sold_out_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND farmer_id = $2
		  AND quantity > 0
	`, cropID, farmerID)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete permanently removes a product.
//
// Responsibility:
// - Delete a product owned by the specified farmer.
//
// NOTE:
//   - This should only be used when the product has no transaction
//     history that needs to be preserved.
//   - Products involved in completed business transactions should
//     eventually be archived/unlisted instead.
func (r *CropRepository) Delete(cropID, farmerID int) error {
	result, err := r.db.Exec(`
		DELETE FROM crops
		WHERE id = $1
		  AND farmer_id = $2
	`, cropID, farmerID)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// ReduceQuantity decreases the available quantity of a product.
//
// Responsibility:
// - Reduce quantity after a successful purchase/order.
// - Prevent quantity from becoming negative.
// - Update the updated_at timestamp.
//
// The database performs the quantity check itself.
func (r *CropRepository) ReduceQuantity(cropID int, amount float64) error {
	if cropID <= 0 || amount <= 0 {
		return fmt.Errorf("quantity reduction must be positive")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before float64
	if err := tx.QueryRow(`SELECT quantity FROM crops WHERE id=$1 FOR UPDATE`, cropID).Scan(&before); err != nil {
		return err
	}
	result, err := tx.Exec(`
		UPDATE crops
		SET
			quantity = quantity - $1,
			first_order_at = COALESCE(first_order_at, CURRENT_TIMESTAMP),
			sold_out_at = CASE WHEN quantity - $1 <= 0 THEN CURRENT_TIMESTAMP ELSE sold_out_at END,
			listed_for_sale = CASE WHEN quantity - $1 <= 0 THEN FALSE ELSE listed_for_sale END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		  AND quantity >= $1
	`, amount, cropID)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	_, err = tx.Exec(`INSERT INTO inventory_history (crop_id, quantity_before, quantity_change, quantity_after, reason) VALUES ($1,$2,$3,$4,'order')`, cropID, before, -amount, before-amount)
	if err != nil {
		return err
	}
	return tx.Commit()
}
