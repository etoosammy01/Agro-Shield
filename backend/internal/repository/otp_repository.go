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

// Consume marks a code used. The consumed_at IS NULL guard makes this
// idempotent-safe: a code can only ever be spent once, even if two requests
// race.
func (r *OTPRepository) Consume(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE otp_codes SET consumed_at = $1 WHERE id = $2 AND consumed_at IS NULL`,
		time.Now(), id,
	)
	return err
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
