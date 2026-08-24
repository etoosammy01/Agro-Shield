package repository

import (
	"backend/internal/models"
	"database/sql"
)

type FarmerRepository struct {
	db *sql.DB
}

func NewFarmerRepository(db *sql.DB) *FarmerRepository {
	return &FarmerRepository{
		db: db,
	}
}

func (r *FarmerRepository) Create(farmer *models.Farmer) error {
	query := `
	INSERT INTO farmers
	(full_name, phone, email, password_hash, location, role, photo_url)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id
	`
	_, err := r.db.Exec(
		query,
		farmer.FullName,
		farmer.Phone,
		farmer.Email,
		farmer.PasswordHash,
		farmer.Location,
		farmer.Role,
		farmer.PhotoURL,
	)
	return err
}

func (r *FarmerRepository) GetByPhone(phone string) (*models.Farmer, error) {
	query := `
	SELECT
		id,
		full_name,
		phone,
		COALESCE(email, '') AS email,
		password_hash,
		location,
		role,
		COALESCE(photo_url, '') AS photo_url,
		created_at,
		updated_at
	FROM farmers
	WHERE phone = $1
	`
	var farmer models.Farmer
	err := r.db.QueryRow(query, phone).Scan(
		&farmer.ID,
		&farmer.FullName,
		&farmer.Phone,
		&farmer.Email,
		&farmer.PasswordHash,
		&farmer.Location,
		&farmer.Role,
		&farmer.PhotoURL,
		&farmer.CreatedAt,
		&farmer.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &farmer, nil
}

func (r *FarmerRepository) GetByID(id int) (*models.Farmer, error) {
	query := `
	SELECT
		id,
		full_name,
		phone,
		COALESCE(email, '') AS email,
		password_hash,
		location,
		role,
		COALESCE(photo_url, '') AS photo_url,
		created_at,
		updated_at
	FROM farmers
	WHERE id = $1
	`
	var farmer models.Farmer
	err := r.db.QueryRow(query, id).Scan(
		&farmer.ID,
		&farmer.FullName,
		&farmer.Phone,
		&farmer.Email,
		&farmer.PasswordHash,
		&farmer.Location,
		&farmer.Role,
		&farmer.PhotoURL,
		&farmer.CreatedAt,
		&farmer.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &farmer, nil
}

// UpdateProfile updates the editable personal details.
func (r *FarmerRepository) UpdateProfile(id int, fullName, phone, email, location string) error {
	query := `
	UPDATE farmers
	SET full_name = $1, phone = $2, email = $3, location = $4, updated_at = CURRENT_TIMESTAMP
	WHERE id = $5
	`
	_, err := r.db.Exec(query, fullName, phone, email, location, id)
	return err
}

// UpdatePhoto sets the farmer's passport photograph URL.
func (r *FarmerRepository) UpdatePhoto(id int, photoURL string) error {
	_, err := r.db.Exec(
		`UPDATE farmers SET photo_url = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		photoURL,
		id,
	)
	return err
}

// UpdatePassword sets a new password hash.
func (r *FarmerRepository) UpdatePassword(id int, passwordHash string) error {
	_, err := r.db.Exec(
		`UPDATE farmers SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		passwordHash,
		id,
	)
	return err
}
