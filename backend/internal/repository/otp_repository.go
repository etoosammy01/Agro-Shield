package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"backend/internal/models"
)

// ErrNoActiveOTP means there is no usable code for this farmer and purpose —
// either none was requested, it already expired, or it was already used.
var ErrNoActiveOTP = errors.New("no active one-time passcode")

type OTPRepository struct {
	db *sql.DB
}

func NewOTPRepository(db *sql.DB) *OTPRepository {
	return &OTPRepository{db: db}
}

// Issue invalidates any code still outstanding for this farmer and purpose,
// then stores the new one. Both steps share a transaction so a farmer can
// never end up with two usable codes at once.
func (r *OTPRepository) Issue(ctx context.Context, farmerID int, phone, purpose, codeHash string, expiresAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE otp_codes SET consumed_at = $1
		WHERE farmer_id = $2 AND purpose = $3 AND consumed_at IS NULL`,
		time.Now(), farmerID, purpose,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO otp_codes (farmer_id, phone, purpose, code_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		farmerID, phone, purpose, codeHash, expiresAt,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// Invalidate burns an undelivered challenge by its exact hash. A concurrent
// resend has a different bcrypt hash and must remain usable.
func (r *OTPRepository) Invalidate(ctx context.Context, farmerID int, purpose, codeHash string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE otp_codes SET consumed_at = $1
		WHERE farmer_id = $2 AND purpose = $3 AND code_hash = $4 AND consumed_at IS NULL`,
		time.Now(), farmerID, purpose, codeHash)
	return err
}

// LatestActive returns the newest code that is neither used nor expired.
func (r *OTPRepository) LatestActive(ctx context.Context, farmerID int, purpose string) (*models.OTPCode, error) {
	var code models.OTPCode
	err := r.db.QueryRowContext(ctx, `
		SELECT id, farmer_id, phone, purpose, code_hash, attempts, expires_at, consumed_at, created_at
		FROM otp_codes
		WHERE farmer_id = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		farmerID, purpose, time.Now(),
	).Scan(
		&code.ID, &code.FarmerID, &code.Phone, &code.Purpose, &code.CodeHash,
		&code.Attempts, &code.ExpiresAt, &code.ConsumedAt, &code.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveOTP
	}
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// IncrementAttempts records a failed verification against this code.
func (r *OTPRepository) IncrementAttempts(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

// Consume atomically spends an unexpired code. A concurrent verifier must
// observe failure if another request already consumed it.
func (r *OTPRepository) Consume(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE otp_codes SET consumed_at = $1
		WHERE id = $2 AND consumed_at IS NULL AND expires_at > $1`,
		time.Now(), id,
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNoActiveOTP
	}
	return nil
}

// ConsumeAndVerifyPhone spends the exact challenge and marks the matching
// account number verified atomically. Lock the account before the challenge,
// matching UpdateProfile's lock order, so changing a phone cannot race success.
func (r *OTPRepository) ConsumeAndVerifyPhone(ctx context.Context, id int64, farmerID int, phone string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var accountID int
	if err := tx.QueryRowContext(ctx, `SELECT id FROM farmers
		WHERE id = $1 AND phone = $2 AND selfie_captured_at IS NOT NULL AND deleted_at IS NULL
		FOR UPDATE`, farmerID, phone).Scan(&accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoActiveOTP
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE otp_codes SET consumed_at = $1
		WHERE id = $2 AND farmer_id = $3 AND phone = $4 AND purpose = $5
		AND consumed_at IS NULL AND expires_at > $1 AND attempts < 5`,
		time.Now(), id, farmerID, phone, models.OTPPurposePhoneVerification)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNoActiveOTP
	}
	if _, err := tx.ExecContext(ctx, `UPDATE farmers SET phone_verified_at = CURRENT_TIMESTAMP,
		updated_at = CURRENT_TIMESTAMP WHERE id = $1`, farmerID); err != nil {
		return err
	}
	return tx.Commit()
}

// CountIssuedSince powers the issuance rate limit.
func (r *OTPRepository) CountIssuedSince(ctx context.Context, farmerID int, purpose string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM otp_codes
		WHERE farmer_id = $1 AND purpose = $2 AND created_at >= $3`,
		farmerID, purpose, since,
	).Scan(&count)
	return count, err
}
