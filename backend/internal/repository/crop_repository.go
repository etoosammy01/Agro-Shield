package repository

import (
	"backend/internal/models"
	"database/sql"
	"fmt"
	"strings"
)

// CropRepository handles all database operations related to crops/products.
type CropRepository struct {
	db *sql.DB
}

// NewCropRepository creates a new CropRepository.
func NewCropRepository(db *sql.DB) *CropRepository {
	return &CropRepository{
		db: db,
	}
}

// ============================================================
// CREATE CROP
// ============================================================
//
// All required crop fields are inserted directly.
// No COALESCE is used for required fields.
//
// The first image is stored in crops.image_url.
// Additional images are stored in crop_images.
// ============================================================

func (r *CropRepository) Create(crop *models.Crop) error {
	if crop == nil {
		return fmt.Errorf("crop cannot be nil")
	}

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
			image_url,
			lga,
			state,
			country,
			latitude,
			longitude,
			location_accuracy,
			initial_listed_quantity,
			first_listed_at
		)
		VALUES (
			$1::integer,
			$2::text,
			$3::double precision,
			$4::text,
			$5::text,
			$6::double precision,
			$7::boolean,
			$8::text,
			$5::text,
			'Benue'::text,
			'Nigeria'::text,
			$9::double precision,
			$10::double precision,
			$11::double precision,
			CASE
				WHEN $7::boolean = TRUE
				THEN $3::double precision
				ELSE NULL::double precision
			END,
			CASE
				WHEN $7::boolean = TRUE
				THEN CURRENT_TIMESTAMP
				ELSE NULL::timestamp
			END
		)
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
		crop.Latitude,
		crop.Longitude,
		crop.LocationAccuracy,
	).Scan(&crop.ID)

	if err != nil {
		return fmt.Errorf("failed to create crop: %w", err)
	}

	// Save initial price history.
	if crop.PricePerUnit > 0 {
		_, err = tx.Exec(`
			INSERT INTO produce_price_history (
				crop_id,
				price_per_unit,
				unit,
				source
			)
			VALUES ($1, $2, $3, $4)
		`,
			crop.ID,
			crop.PricePerUnit,
			crop.Unit,
			"Agro-Shield",
		)

		if err != nil {
			return fmt.Errorf("failed to save price history: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit crop creation: %w", err)
	}

	return nil
}

// ============================================================
// LIST FARMER CROPS
// ============================================================

func (r *CropRepository) ListByFarmer(
	farmerID int,
) ([]models.Crop, error) {

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
			COALESCE(initial_listed_quantity, 0),
			first_listed_at,
			first_order_at,
			sold_out_at,
			COALESCE(created_at, CURRENT_TIMESTAMP),
			COALESCE(updated_at, CURRENT_TIMESTAMP)
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

		err := rows.Scan(
			&crop.ID,
			&crop.FarmerID,
			&crop.Name,
			&crop.Quantity,
			&crop.Unit,
			&crop.Location,
			&crop.PricePerUnit,
			&crop.ListedForSale,
			&crop.ImageURL,
			&crop.InitialListedQuantity,
			&crop.FirstListedAt,
			&crop.FirstOrderAt,
			&crop.SoldOutAt,
			&crop.CreatedAt,
			&crop.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		crops = append(crops, crop)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return crops, nil
}

// ============================================================
// LIST AVAILABLE CROPS
// ============================================================

func (r *CropRepository) ListAvailable() ([]models.Crop, error) {

	query := `
		SELECT
			crops.id,
			crops.farmer_id,
			crops.name,
			crops.quantity,
			crops.unit,
			crops.location,
			COALESCE(crops.latitude, 0),
			COALESCE(crops.longitude, 0),
			COALESCE(crops.location_accuracy, 0),
			crops.price_per_unit,
			crops.listed_for_sale,
			crops.image_url,
			COALESCE(crops.created_at, CURRENT_TIMESTAMP),
			COALESCE(crops.updated_at, CURRENT_TIMESTAMP),
			COALESCE(farmers.full_name, ''),
			COALESCE(farmers.photo_url, '')
		FROM crops
		INNER JOIN farmers
			ON farmers.id = crops.farmer_id
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

		err := rows.Scan(
			&crop.ID,
			&crop.FarmerID,
			&crop.Name,
			&crop.Quantity,
			&crop.Unit,
			&crop.Location,
			&crop.Latitude,
			&crop.Longitude,
			&crop.LocationAccuracy,
			&crop.PricePerUnit,
			&crop.ListedForSale,
			&crop.ImageURL,
			&crop.CreatedAt,
			&crop.UpdatedAt,
			&crop.SellerName,
			&crop.SellerPhotoURL,
		)

		if err != nil {
			return nil, err
		}

		crops = append(crops, crop)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return crops, nil
}

// ============================================================
// GET CROP BY ID
// ============================================================

func (r *CropRepository) GetByID(
	id int,
) (*models.Crop, error) {

	query := `
		SELECT
			id,
			farmer_id,
			name,
			quantity,
			unit,
			location,
			COALESCE(latitude, 0),
			COALESCE(longitude, 0),
			COALESCE(location_accuracy, 0),
			price_per_unit,
			listed_for_sale,
			image_url,
			COALESCE(initial_listed_quantity, 0),
			first_listed_at,
			first_order_at,
			sold_out_at,
			COALESCE(created_at, CURRENT_TIMESTAMP),
			COALESCE(updated_at, CURRENT_TIMESTAMP)
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
		&crop.Latitude,
		&crop.Longitude,
		&crop.LocationAccuracy,
		&crop.PricePerUnit,
		&crop.ListedForSale,
		&crop.ImageURL,
		&crop.InitialListedQuantity,
		&crop.FirstListedAt,
		&crop.FirstOrderAt,
		&crop.SoldOutAt,
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

// ============================================================
// UPDATE CROP
// ============================================================

func (r *CropRepository) Update(
	crop *models.Crop,
) error {

	if crop == nil {
		return fmt.Errorf("crop cannot be nil")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		UPDATE crops
		SET
			name = $1::text,
			quantity = $2::double precision,
			unit = $3::text,
			location = $4::text,
			price_per_unit = $5::double precision,
			listed_for_sale = $6::boolean,
			image_url = $7::text,
			lga = $4::text,
			state = 'Benue'::text,
			country = 'Nigeria'::text,
			latitude = $10::double precision,
			longitude = $11::double precision,
			location_accuracy = $12::double precision,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $8::integer
		AND farmer_id = $9::integer
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
		crop.Latitude,
		crop.Longitude,
		crop.LocationAccuracy,
	)

	if err != nil {
		return fmt.Errorf("failed to update crop: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Save new price history.
	if crop.PricePerUnit > 0 {
		_, err = tx.Exec(`
			INSERT INTO produce_price_history (
				crop_id,
				price_per_unit,
				unit,
				source
			)
			VALUES ($1, $2, $3, $4)
		`,
			crop.ID,
			crop.PricePerUnit,
			crop.Unit,
			"Agro-Shield",
		)

		if err != nil {
			return fmt.Errorf("failed to save price history: %w", err)
		}
	}

	return tx.Commit()
}

// ============================================================
// UNLIST CROP
// ============================================================

func (r *CropRepository) Unlist(
	cropID int,
	farmerID int,
) error {

	result, err := r.db.Exec(`
		UPDATE crops
		SET
			listed_for_sale = FALSE,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		AND farmer_id = $2
	`,
		cropID,
		farmerID,
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

	return nil
}

// ============================================================
// RELIST CROP
// ============================================================

func (r *CropRepository) Relist(
	cropID int,
	farmerID int,
) error {

	result, err := r.db.Exec(`
		UPDATE crops
		SET
			listed_for_sale = TRUE,
			first_listed_at = COALESCE(
				first_listed_at,
				CURRENT_TIMESTAMP
			),
			initial_listed_quantity =
				CASE
					WHEN initial_listed_quantity IS NULL
					OR initial_listed_quantity < quantity
					THEN quantity
					ELSE initial_listed_quantity
				END,
			sold_out_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		AND farmer_id = $2
		AND quantity > 0
	`,
		cropID,
		farmerID,
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

	return nil
}

// ============================================================
// DELETE CROP
// ============================================================

func (r *CropRepository) Delete(
	cropID int,
	farmerID int,
) error {

	result, err := r.db.Exec(`
		DELETE FROM crops
		WHERE id = $1
		AND farmer_id = $2
	`,
		cropID,
		farmerID,
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

	return nil
}

// ============================================================
// REDUCE QUANTITY
// ============================================================

func (r *CropRepository) ReduceQuantity(
	cropID int,
	amount float64,
) error {

	if cropID <= 0 {
		return fmt.Errorf("invalid crop ID")
	}

	if amount <= 0 {
		return fmt.Errorf("quantity reduction must be positive")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var before float64

	err = tx.QueryRow(`
		SELECT quantity
		FROM crops
		WHERE id = $1
		FOR UPDATE
	`,
		cropID,
	).Scan(&before)

	if err != nil {
		return err
	}

	result, err := tx.Exec(`
		UPDATE crops
		SET
			quantity = quantity - $1,
			first_order_at = COALESCE(
				first_order_at,
				CURRENT_TIMESTAMP
			),
			sold_out_at =
				CASE
					WHEN quantity - $1 <= 0
					THEN CURRENT_TIMESTAMP
					ELSE sold_out_at
				END,
			listed_for_sale =
				CASE
					WHEN quantity - $1 <= 0
					THEN FALSE
					ELSE listed_for_sale
				END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		AND quantity >= $1
	`,
		amount,
		cropID,
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

	_, err = tx.Exec(`
		INSERT INTO inventory_history (
			crop_id,
			quantity_before,
			quantity_change,
			quantity_after,
			reason
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$order_reason
		)
	`,
		cropID,
		before,
		-amount,
		before-amount,
	)

	// PostgreSQL does not support named parameters such as
	// $order_reason through database/sql.
	// Therefore this block should never be reached.
	if err != nil {
		// Retry using the correct positional parameter.
		_, err = tx.Exec(`
			INSERT INTO inventory_history (
				crop_id,
				quantity_before,
				quantity_change,
				quantity_after,
				reason
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				$5
			)
		`,
			cropID,
			before,
			-amount,
			before-amount,
			"order",
		)

		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ============================================================
// ADD CROP IMAGES
// ============================================================
//
// At least one valid image is required.
// The first valid image becomes the primary image.
// ============================================================

func (r *CropRepository) AddImages(
	cropID int,
	imageURLs []string,
) error {

	if cropID <= 0 {
		return fmt.Errorf("invalid crop ID")
	}

	if len(imageURLs) == 0 {
		return fmt.Errorf("at least one crop image is required")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	validCount := 0

	for _, imageURL := range imageURLs {

		imageURL = strings.TrimSpace(imageURL)

		if imageURL == "" {
			continue
		}

		isPrimary := validCount == 0

		_, err := tx.Exec(`
			INSERT INTO crop_images (
				crop_id,
				image_url,
				is_primary,
				sort_order
			)
			VALUES (
				$1,
				$2,
				$3,
				$4
			)
		`,
			cropID,
			imageURL,
			isPrimary,
			validCount,
		)

		if err != nil {
			return err
		}

		validCount++
	}

	if validCount == 0 {
		return fmt.Errorf("at least one valid crop image is required")
	}

	return tx.Commit()
}

// ============================================================
// LIST CROP IMAGES
// ============================================================

func (r *CropRepository) ListImages(
	cropID int,
) ([]models.CropImage, error) {

	if cropID <= 0 {
		return nil, fmt.Errorf("invalid crop ID")
	}

	rows, err := r.db.Query(`
		SELECT
			id,
			crop_id,
			image_url,
			is_primary,
			sort_order,
			created_at
		FROM crop_images
		WHERE crop_id = $1
		ORDER BY sort_order ASC, id ASC
	`,
		cropID,
	)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []models.CropImage

	for rows.Next() {

		var image models.CropImage

		err := rows.Scan(
			&image.ID,
			&image.CropID,
			&image.ImageURL,
			&image.IsPrimary,
			&image.SortOrder,
			&image.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		images = append(images, image)
	}

	return images, rows.Err()
}