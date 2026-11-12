package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"backend/internal/models"

	"github.com/jackc/pgx/v5/pgconn"
)

// defaultPhotoURL is the generic portrait a closed account is left with. It is
// kept here rather than imported from the auth service so the repository does
// not depend on the layer above it.
const defaultPhotoURL = "/static/assets/images/un.jpeg"

// ErrPhoneExists means an account is already registered with this phone number.
//
// Phone is UNIQUE in the farmers table, so Create hits this when two signups
// race past the pre-check in AuthService.Register. Callers match it with
// errors.Is rather than comparing error strings.
var ErrPhoneExists = errors.New("phone number already registered")

// Account-closure refusals. A closed account keeps its order, sale, and wallet
// ledger history for record-keeping, so deletion must not be possible while
// money is still held, a provider account is attached, or a transfer could land
// after the owner has lost access.
var (
	ErrAccountNotFound          = errors.New("account not found")
	ErrAccountHasBalance        = errors.New("account still has a wallet balance")
	ErrAccountHasPendingRequest = errors.New("account has pending wallet activity")
	ErrAccountHasFundingAccount = errors.New("account has a funding account")
	ErrNoPendingPhoneChange     = errors.New("no pending phone change")
	ErrPhoneAlreadyCurrent      = errors.New("the current phone number cannot be staged for a change")
)

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

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
		// Only phone is UNIQUE on this table, so a 23505 here means this
		// number was claimed between the pre-check and this insert.
		if isUniqueViolation(err) {
			return ErrPhoneExists
		}
		return err
	}

	// The wallet number is derived from the generated id so it is permanent
	// and cannot collide across members. It is written in the same transaction
	// as the account, so a created farmer always has one.
	if farmer.ID > 999_999_999 {
		return fmt.Errorf("wallet number space exhausted")
	}
	farmer.WalletNumber = fmt.Sprintf("8%09d", farmer.ID)
	if _, err := tx.Exec(`UPDATE farmers SET wallet_number = $1 WHERE id = $2`, farmer.WalletNumber, farmer.ID); err != nil {
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
        COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''), COALESCE(wallet_number, ''),
        created_at,
        updated_at, selfie_captured_at, phone_verified_at, pending_phone,
        EXISTS (SELECT 1 FROM wallets w WHERE w.farmer_id = farmers.id AND COALESCE(w.transaction_pin_hash, '') <> '')
    FROM farmers
    WHERE phone = $1
    AND deleted_at IS NULL
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
		&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber, &farmer.WalletNumber,
		&farmer.CreatedAt,
		&farmer.UpdatedAt, &farmer.SelfieCapturedAt, &farmer.PhoneVerifiedAt, &farmer.PendingPhone, &farmer.HasTransactionPIN,
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
        COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''), COALESCE(wallet_number, ''),
        created_at,
        updated_at, selfie_captured_at, phone_verified_at, pending_phone,
        EXISTS (SELECT 1 FROM wallets w WHERE w.farmer_id = farmers.id AND COALESCE(w.transaction_pin_hash, '') <> '')
    FROM farmers
    WHERE id = $1
    AND deleted_at IS NULL
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
		&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber, &farmer.WalletNumber,
		&farmer.CreatedAt,
		&farmer.UpdatedAt, &farmer.SelfieCapturedAt, &farmer.PhoneVerifiedAt, &farmer.PendingPhone, &farmer.HasTransactionPIN,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &farmer, nil
}

// GetByWalletNumber finds the member who owns a permanent Agro-Shield wallet
// number. Unlike a bank account number, this value is generated by Agro-Shield
// and belongs to exactly one member.
func (r *FarmerRepository) GetByWalletNumber(walletNumber string) (*models.Farmer, error) {
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
        COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''), COALESCE(wallet_number, ''),
        created_at,
        updated_at, selfie_captured_at, phone_verified_at, pending_phone,
        EXISTS (SELECT 1 FROM wallets w WHERE w.farmer_id = farmers.id AND COALESCE(w.transaction_pin_hash, '') <> '')
    FROM farmers
    WHERE wallet_number = $1
    AND deleted_at IS NULL
    `
	var farmer models.Farmer
	err := r.db.QueryRow(query, walletNumber).Scan(
		&farmer.ID,
		&farmer.FullName,
		&farmer.Phone,
		&farmer.Email,
		&farmer.PasswordHash,
		&farmer.Location,
		&farmer.LGA, &farmer.State, &farmer.Country,
		&farmer.Role,
		&farmer.PhotoURL,
		&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber, &farmer.WalletNumber,
		&farmer.CreatedAt,
		&farmer.UpdatedAt, &farmer.SelfieCapturedAt, &farmer.PhoneVerifiedAt, &farmer.PendingPhone, &farmer.HasTransactionPIN,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &farmer, nil
}

// FindByAccountNumber returns users whose saved payout bank account number
// matches. Account numbers are not globally unique across banks, so callers
// must treat more than one result as ambiguous rather than guessing.
//
// The query stops at two rows: the caller only needs to distinguish zero, one,
// and "more than one", and a matching number should never fan out further.
func (r *FarmerRepository) FindByAccountNumber(accountNumber string) ([]*models.Farmer, error) {
	rows, err := r.db.Query(`
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
			COALESCE(bank_name, ''), COALESCE(bank_code, ''), COALESCE(account_name, ''), COALESCE(account_number, ''), COALESCE(wallet_number, ''),
			created_at,
			updated_at, selfie_captured_at, phone_verified_at, pending_phone,
			EXISTS (SELECT 1 FROM wallets w WHERE w.farmer_id = farmers.id AND COALESCE(w.transaction_pin_hash, '') <> '')
		FROM farmers
		WHERE account_number = $1 AND account_number <> '' AND deleted_at IS NULL
		ORDER BY id
		LIMIT 2`, accountNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var farmers []*models.Farmer
	for rows.Next() {
		var farmer models.Farmer
		if err := rows.Scan(
			&farmer.ID,
			&farmer.FullName,
			&farmer.Phone,
			&farmer.Email,
			&farmer.PasswordHash,
			&farmer.Location,
			&farmer.LGA, &farmer.State, &farmer.Country,
			&farmer.Role,
			&farmer.PhotoURL,
			&farmer.BankName, &farmer.BankCode, &farmer.AccountName, &farmer.AccountNumber, &farmer.WalletNumber,
			&farmer.CreatedAt,
			&farmer.UpdatedAt, &farmer.SelfieCapturedAt, &farmer.PhoneVerifiedAt, &farmer.PendingPhone, &farmer.HasTransactionPIN,
		); err != nil {
			return nil, err
		}
		farmers = append(farmers, &farmer)
	}
	return farmers, rows.Err()
}

// ListForChat returns registered users that can be contacted in general chat.
func (r *FarmerRepository) ListForChat(excludeID int, search string) ([]models.Farmer, error) {
	rows, err := r.db.Query(`
        SELECT id, full_name, phone, COALESCE(email, ''),
            location, COALESCE(lga, location), COALESCE(state, ''), COALESCE(country, ''),
            role, COALESCE(photo_url, ''), COALESCE(bank_name, ''), COALESCE(bank_code, ''),
            COALESCE(account_name, ''), COALESCE(account_number, ''), COALESCE(wallet_number, ''), created_at, updated_at, selfie_captured_at, phone_verified_at, pending_phone,
            EXISTS (SELECT 1 FROM wallets w WHERE w.farmer_id = farmers.id AND COALESCE(w.transaction_pin_hash, '') <> '')
        FROM farmers
        WHERE id <> $1 AND deleted_at IS NULL
        AND ($2 = '' OR full_name ILIKE '%' || $2 || '%' OR role ILIKE '%' || $2 || '%' OR location ILIKE '%' || $2 || '%')
        ORDER BY full_name ASC
        LIMIT 50`, excludeID, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.Farmer
	for rows.Next() {
		var user models.Farmer
		if err := rows.Scan(&user.ID, &user.FullName, &user.Phone, &user.Email, &user.Location, &user.LGA, &user.State, &user.Country, &user.Role, &user.PhotoURL, &user.BankName, &user.BankCode, &user.AccountName, &user.AccountNumber, &user.WalletNumber, &user.CreatedAt, &user.UpdatedAt, &user.SelfieCapturedAt, &user.PhoneVerifiedAt, &user.PendingPhone, &user.HasTransactionPIN); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// UpdateProfileDetails updates the editable personal details without changing
// the verified phone. Phone replacement is handled by phone-change staging and
// verification instead.
func (r *FarmerRepository) UpdateProfileDetails(id int, fullName, email, community, lga, state, country string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var updated int
	if err := tx.QueryRow(`SELECT id FROM farmers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountNotFound
		}
		return err
	}
	query := `
	UPDATE farmers
	SET full_name = $1, email = $2, location = $3, lga = $4, state = $5, country = $6, updated_at = CURRENT_TIMESTAMP
	WHERE id = $7 AND deleted_at IS NULL`
	if _, err := tx.Exec(query, fullName, email, community, lga, state, country, id); err != nil {
		return err
	}
	return tx.Commit()
}

// StagePhoneChange stores a pending phone number and retires any unconsumed
// phone-change challenge for the account when the pending number changes.
// Re-staging the same pending number keeps the number and its active
// challenges intact. The verified phone is never changed here;
// ConsumeAndApplyPhoneChange performs the replacement later.
func (r *FarmerRepository) StagePhoneChange(ctx context.Context, farmerID int, phone string) error {
	if farmerID <= 0 {
		return ErrAccountNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int
	var currentPhone string
	var pendingPhone sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT id, phone, pending_phone FROM farmers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, farmerID).Scan(&id, &currentPhone, &pendingPhone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAccountNotFound
		}
		return err
	}
	var ownerID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM farmers WHERE phone = $1 AND id <> $2 AND deleted_at IS NULL LIMIT 1`, phone, farmerID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ownerID = 0
		} else {
			return err
		}
	}
	if ownerID != 0 && ownerID != int64(farmerID) {
		return ErrPhoneExists
	}
	if phone == currentPhone {
		return ErrPhoneAlreadyCurrent
	}
	if pendingPhone.Valid && pendingPhone.String == phone {
		// The caller is retrying the same pending number; keep the staged
		// number and its active OTPs so a rate-limited retry does not strand
		// a valid code.
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE farmers SET pending_phone = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL`, phone, farmerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE otp_codes SET consumed_at = CURRENT_TIMESTAMP
		WHERE farmer_id = $1 AND purpose = $2 AND consumed_at IS NULL`, farmerID, models.OTPPurposePhoneChange); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelPhoneChange clears a pending phone number and retires its unconsumed
// phone-change challenge atomically. It reports ErrNoPendingPhoneChange when no
// replacement is waiting on an OTP.
func (r *FarmerRepository) CancelPhoneChange(ctx context.Context, farmerID int) error {
	if farmerID <= 0 {
		return ErrAccountNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pending sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT pending_phone FROM farmers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, farmerID).Scan(&pending); err != nil {
		return err
	}
	if !pending.Valid || pending.String == "" {
		return ErrNoPendingPhoneChange
	}
	if _, err := tx.ExecContext(ctx, `UPDATE farmers SET pending_phone = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL`, farmerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE otp_codes SET consumed_at = CURRENT_TIMESTAMP
		WHERE farmer_id = $1 AND purpose = $2 AND consumed_at IS NULL`, farmerID, models.OTPPurposePhoneChange); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSelfie records the photo and completion of the capture step together.
func (r *FarmerRepository) UpdateSelfie(id int, photoURL string) error {
	result, err := r.db.Exec(`UPDATE farmers
		SET photo_url = $1, selfie_captured_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND deleted_at IS NULL`, photoURL, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return ErrAccountNotFound
	}
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

// DeleteAccount closes an account by anonymising it rather than removing the
// row. Orders, sales, and wallet ledger entries must keep a stable account id,
// so the row stays and the personal details on it are replaced: name, phone,
// email, photo, location, payout details, password, and admin flag. The caller
// must have proved control of the account before reaching here.
//
// The closure is refused while money or a provider account is still attached,
// because a transfer that lands after the owner loses access would be stranded:
//
//   - a non-zero wallet balance,
//   - a pending/processing deposit, withdrawal, or transfer,
//   - a virtual funding account that would keep receiving bank transfers.
//
// Everything is one transaction: the account is either fully anonymised or not
// touched at all. Repeating the call returns ErrAccountNotFound.
func (r *FarmerRepository) DeleteAccount(ctx context.Context, farmerID int) error {
	if farmerID <= 0 {
		return ErrAccountNotFound
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id int
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM farmers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, farmerID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return err
	}

	// The balance and every in-flight request are read under the same lock
	// DeleteAccount writes under, so a credit cannot slip in between the check
	// and the anonymisation.
	var balance string
	err = tx.QueryRowContext(ctx,
		`SELECT balance::text FROM wallets WHERE farmer_id = $1 FOR UPDATE`, farmerID,
	).Scan(&balance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && balance != "0.00" && balance != "0" && balance != "0.0" {
		return ErrAccountHasBalance
	}

	var pending int
	if err := tx.QueryRowContext(ctx, `
        SELECT
            (SELECT COUNT(*) FROM wallet_deposits d
                JOIN wallets w ON w.id = d.wallet_id
                WHERE w.farmer_id = $1 AND d.status IN ('pending', 'processing'))
            + (SELECT COUNT(*) FROM wallet_withdrawals
                WHERE farmer_id = $1 AND status IN ('pending', 'processing'))
            + (SELECT COUNT(*) FROM wallet_transfers
                WHERE (sender_id = $1 OR recipient_id = $1) AND status = 'pending')`,
		farmerID,
	).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return ErrAccountHasPendingRequest
	}

	var fundingAccounts int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wallet_virtual_accounts WHERE farmer_id = $1`, farmerID,
	).Scan(&fundingAccounts); err != nil {
		return err
	}
	if fundingAccounts > 0 {
		return ErrAccountHasFundingAccount
	}

	// Personal data with no record-keeping value is removed outright. Cart rows
	// are transient; negotiations are the conversation around an offer, and the
	// order itself keeps the agreed price and quantity.
	for _, stmt := range []string{
		`DELETE FROM cart_items WHERE buyer_id = $1`,
		`DELETE FROM negotiations WHERE buyer_id = $1 OR farmer_id = $1`,
		`DELETE FROM negotiation_messages WHERE sender_id = $1`,
		// Other buyers may still hold this seller's produce in a cart. The
		// listing cannot stay purchasable once the account closes, and the
		// cart row would block removing it.
		`DELETE FROM cart_items WHERE crop_id IN (SELECT id FROM crops WHERE farmer_id = $1)`,
		`DELETE FROM chat_messages WHERE sender_id = $1`,
		`DELETE FROM conversation_members WHERE user_id = $1`,
		`DELETE FROM notifications WHERE user_id = $1`,
		`DELETE FROM feedback WHERE user_id = $1`,
		`DELETE FROM diagnoses WHERE farmer_id = $1`,
		`DELETE FROM otp_codes WHERE farmer_id = $1`,
		`UPDATE market_events SET user_id = NULL WHERE user_id = $1`,
		`UPDATE marketplace_searches SET user_id = NULL WHERE user_id = $1`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, farmerID); err != nil {
			return err
		}
	}

	// Personal data with no record-keeping value is removed outright. Cart rows
	// are transient; negotiations are the conversation around an offer, and the
	// order itself keeps the agreed price and quantity.
	for _, stmt := range []string{
		`DELETE FROM cart_items WHERE buyer_id = $1`,
		`DELETE FROM negotiations WHERE buyer_id = $1 OR farmer_id = $1`,
		`DELETE FROM negotiation_messages WHERE sender_id = $1`,
		// Other buyers may still hold this seller's produce in a cart. The
		// listing cannot stay purchasable once the account closes, and the
		// cart row would block removing it.
		`DELETE FROM cart_items WHERE crop_id IN (SELECT id FROM crops WHERE farmer_id = $1)`,
		`DELETE FROM chat_messages WHERE sender_id = $1`,
		`DELETE FROM conversation_members WHERE user_id = $1`,
		`DELETE FROM notifications WHERE user_id = $1`,
		`DELETE FROM feedback WHERE user_id = $1`,
		`DELETE FROM diagnoses WHERE farmer_id = $1`,
		`DELETE FROM otp_codes WHERE farmer_id = $1`,
		`UPDATE market_events SET user_id = NULL WHERE user_id = $1`,
		`UPDATE marketplace_searches SET user_id = NULL WHERE user_id = $1`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, farmerID); err != nil {
			return err
		}
	}

	// Listings nobody has ordered can go. A crop that appears in an order or
	// delivery is kept for that record, but is taken off the marketplace so a
	// closed account is not still selling produce.
	if _, err := tx.ExecContext(ctx, `
        DELETE FROM crops c
        WHERE c.farmer_id = $1
        AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.crop_id = c.id)
        AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.crop_id = c.id)`, farmerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE crops SET listed_for_sale = FALSE, updated_at = CURRENT_TIMESTAMP WHERE farmer_id = $1`, farmerID,
	); err != nil {
		return err
	}

	// The phone number is freed for a future registration. The placeholder is
	// unique because it includes the id, and it can never pass the Nigerian
	// phone rules used by login and OTP.
	placeholderPhone := fmt.Sprintf("deleted-%d", farmerID)
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET transaction_pin_hash = NULL,
		transaction_pin_created_at = NULL, transaction_pin_failed_attempts = 0,
		transaction_pin_locked_until = NULL WHERE farmer_id = $1`, farmerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE farmers
        SET full_name = 'Deleted account',
            phone = $2,
            email = NULL,
            password_hash = 'deleted',
            location = 'Deleted account',
            lga = NULL,
            state = NULL,
            country = 'Nigeria',
            photo_url = $3,
            bank_name = NULL,
            bank_code = NULL,
            account_name = NULL,
            account_number = NULL,
            is_admin = FALSE,
            deleted_at = CURRENT_TIMESTAMP,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1 AND deleted_at IS NULL`,
		farmerID, placeholderPhone, defaultPhotoURL,
	); err != nil {
		return err
	}
	// The phone number is freed for a future registration. The placeholder is
	// unique because it includes the id, and it can never pass the Nigerian
	// phone rules used by login and OTP.
	placeholderPhone = fmt.Sprintf("deleted-%d", farmerID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE farmers
		SET full_name = 'Deleted account',
			phone = $2,
			email = NULL,
			password_hash = 'deleted',
			location = 'Deleted account',
			lga = NULL,
			state = NULL,
			country = 'Nigeria',
			photo_url = $3,
			bank_name = NULL,
			bank_code = NULL,
			account_name = NULL,
			account_number = NULL,
			is_admin = FALSE,
			deleted_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL`,
		farmerID, placeholderPhone, defaultPhotoURL,
	); err != nil {
		return err
	}

	return tx.Commit()
}
