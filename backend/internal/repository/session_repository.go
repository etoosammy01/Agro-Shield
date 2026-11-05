package repository

import (
	"backend/internal/session"
	"context"
	"database/sql"
	"errors"
	"time"
)

// Keep this alias so existing repository tests and callers remain compatible.
var ErrSessionNotFound = session.ErrNotFound

type SessionRepository struct {
	db *sql.DB
}

// NewSessionRepository creates a repository for persistent sessions.
func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create stores a new session token hash in the database.
func (r *SessionRepository) Create(
	ctx context.Context,
	tokenHash string,
	farmerID int,
	expiresAt time.Time,
) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (
			session_token_hash,
			farmer_id,
			expires_at
		)
		VALUES ($1, $2, $3)
	`, tokenHash, farmerID, expiresAt)

	return err
}

// FindValid retrieves a farmer ID for a valid session.
// Expired and revoked sessions cannot authenticate users.
func (r *SessionRepository) FindValid(
	ctx context.Context,
	tokenHash string,
) (int, error) {
	var farmerID int

	err := r.db.QueryRowContext(ctx, `
		UPDATE sessions
		SET last_seen_at = CURRENT_TIMESTAMP
		WHERE session_token_hash = $1
		  AND expires_at > CURRENT_TIMESTAMP
		  AND revoked_at IS NULL
		RETURNING farmer_id
	`, tokenHash).Scan(&farmerID)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, session.ErrNotFound
	}

	if err != nil {
		return 0, err
	}

	return farmerID, nil
}

// Revoke invalidates one session.
func (r *SessionRepository) Revoke(
	ctx context.Context,
	tokenHash string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE session_token_hash = $1
		  AND revoked_at IS NULL
	`, tokenHash)

	return err
}

// RevokeAllForFarmer invalidates every active session belonging to a farmer.
func (r *SessionRepository) RevokeAllForFarmer(
	ctx context.Context,
	farmerID int,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE farmer_id = $1
		  AND revoked_at IS NULL
	`, farmerID)

	return err
}

// DeleteExpired removes expired sessions and sessions that were revoked.
func (r *SessionRepository) DeleteExpired(
	ctx context.Context,
) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE expires_at <= CURRENT_TIMESTAMP
		   OR revoked_at IS NOT NULL
	`)

	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
