package repository

import (
	"backend/internal/models"
	"database/sql"
	"strings"
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
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT INTO farmers
	(full_name, phone, email, password_hash, location, lga, state, country, role, photo_url, bank_name, bank_code, account_name, account_number)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	RETURNING id
	`
	err = tx.QueryRow(
		query,
		farmer.FullName,
		farmer.Phone,
		farmer.Email,
		farmer.PasswordHash,
		farmer.Location,
		farmer.LGA,
		farmer.State,
		farmer.Country,
		farmer.Role,
		farmer.PhotoURL,
		farmer.BankName,
		farmer.BankCode,
		farmer.AccountName,
		farmer.AccountNumber,
	).Scan(&farmer.ID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO wallets (farmer_id) VALUES ($1)`, farmer.ID); err != nil {
		return err
	}
	return tx.Commit()
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
		COALESCE(lga, location), COALESCE(state, ''), COALESCE(country, ''),
		role,
		COALESCE(photo_url, '') AS photo_url,
		COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''),
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
		&farmer.LGA, &farmer.State, &farmer.Country,
		&farmer.Role,
		&farmer.PhotoURL,
		&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber,
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
		COALESCE(lga, location), COALESCE(state, ''), COALESCE(country, ''),
		role,
		COALESCE(photo_url, '') AS photo_url,
		COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''),
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
		&farmer.LGA, &farmer.State, &farmer.Country,
		&farmer.Role,
		&farmer.PhotoURL,
		&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber,
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

// ListForChat returns registered users that can be contacted in general chat.
func (r *FarmerRepository) ListForChat(excludeID int, search string) ([]models.Farmer, error) {
	rows, err := r.db.Query(`
		SELECT id, full_name, phone, COALESCE(email, ''), password_hash,
			location, COALESCE(lga, location), COALESCE(state, ''), COALESCE(country, ''),
			role, COALESCE(photo_url, ''), COALESCE(bank_name, ''), COALESCE(bank_code, ''),
			COALESCE(account_name, ''), COALESCE(account_number, ''), created_at, updated_at
		FROM farmers
		WHERE id <> $1 AND ($2 = '' OR full_name ILIKE '%' || $2 || '%' OR role ILIKE '%' || $2 || '%' OR location ILIKE '%' || $2 || '%')
		ORDER BY full_name ASC`, excludeID, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.Farmer
	for rows.Next() {
		var user models.Farmer
		if err := rows.Scan(&user.ID, &user.FullName, &user.Phone, &user.Email, &user.PasswordHash, &user.Location, &user.LGA, &user.State, &user.Country, &user.Role, &user.PhotoURL, &user.BankName, &user.BankCode, &user.AccountName, &user.AccountNumber, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// UpdateProfile updates the editable personal details.
func (r *FarmerRepository) UpdateProfile(id int, fullName, phone, email, community, lga, state, country string) error {
	query := `
	UPDATE farmers
	SET full_name = $1, phone = $2, email = $3, location = $4, lga = $5, state = $6, country = $7, updated_at = CURRENT_TIMESTAMP
	WHERE id = $8
	`
	_, err := r.db.Exec(query, fullName, phone, email, community, lga, state, country, id)
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

// UpdateBankDetails sets a farmer's payout bank details. Any field may be
// blank — payout details are optional and can be added or edited later from
// the profile page.
func (r *FarmerRepository) UpdateBankDetails(id int, bankName, bankCode, accountName, accountNumber string) error {
	_, err := r.db.Exec(
		`UPDATE farmers SET bank_name = $1, bank_code = $2, account_name = $3, account_number = $4, updated_at = CURRENT_TIMESTAMP WHERE id = $5`,
		bankName, bankCode, accountName, accountNumber, id,
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
