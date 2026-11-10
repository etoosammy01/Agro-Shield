package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"
)

// Session lifetime matches the intended browser cookie lifetime.
const sessionLifetime = 24 * time.Hour

// DefaultSessionCleanupInterval is used by RunCleanup when the caller passes
// a non-positive interval.
const DefaultSessionCleanupInterval = time.Hour

// sessionRepository describes the database operations required by this service.
// Using an interface keeps the service independent of the concrete repository.
type sessionRepository interface {
	Create(ctx context.Context, tokenHash string, farmerID int, expiresAt time.Time) error
	FindValid(ctx context.Context, tokenHash string) (int, error)
	Revoke(ctx context.Context, tokenHash string) error
	RevokeAllForFarmer(ctx context.Context, farmerID int) error
	DeleteExpired(ctx context.Context) (int64, error)
}

// SessionService handles session creation, validation and revocation.
type SessionService struct {
	repo sessionRepository
}

// NewSessionService creates a session service.
func NewSessionService(repo sessionRepository) *SessionService {
	return &SessionService{
		repo: repo,
	}
}

// Create generates a secure session token and stores only its SHA-256 hash.
// The original token is returned so it can be placed in the browser cookie.
func (s *SessionService) Create(
	ctx context.Context,
	farmerID int,
) (string, time.Time, error) {

	// Generate 32 cryptographically secure random bytes.
	randomBytes := make([]byte, 32)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", time.Time{}, err
	}

	// Convert the random bytes into a token safe for a cookie.
	token := hex.EncodeToString(randomBytes)

	// Never store the original session token in the database.
	tokenHash := hashSessionToken(token)

	// Set the server-side expiry to 24 hours from now.
	expiresAt := time.Now().UTC().Add(sessionLifetime)

	if err := s.repo.Create(ctx, tokenHash, farmerID, expiresAt); err != nil {
		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

// Validate checks whether a token belongs to a valid session.
func (s *SessionService) Validate(
	ctx context.Context,
	token string,
) (int, error) {
	tokenHash := hashSessionToken(token)

	return s.repo.FindValid(ctx, tokenHash)
}

// Revoke invalidates one session, for example during logout.
func (s *SessionService) Revoke(
	ctx context.Context,
	token string,
) error {
	tokenHash := hashSessionToken(token)

	return s.repo.Revoke(ctx, tokenHash)
}

// RevokeAllForFarmer invalidates every session belonging to a farmer.
func (s *SessionService) RevokeAllForFarmer(
	ctx context.Context,
	farmerID int,
) error {
	return s.repo.RevokeAllForFarmer(ctx, farmerID)
}

// CleanupExpired removes expired and revoked sessions.
func (s *SessionService) CleanupExpired(
	ctx context.Context,
) (int64, error) {
	return s.repo.DeleteExpired(ctx)
}

// RunCleanup deletes expired and revoked sessions until ctx is cancelled. It
// runs once at startup as well, so rows left behind while the server was down
// do not wait a whole interval before being removed.
func (s *SessionService) RunCleanup(
	ctx context.Context,
	interval time.Duration,
) {
	interval = effectiveCleanupInterval(interval)

	s.cleanupPass(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupPass(ctx)
		}
	}
}

// cleanupPass performs one deletion round. Errors are logged instead of
// returned so a transient database failure cannot terminate the worker.
func (s *SessionService) cleanupPass(ctx context.Context) {
	deleted, err := s.CleanupExpired(ctx)
	if err != nil {
		log.Printf("session cleanup: %v", err)
		return
	}

	log.Printf("session cleanup removed %d expired or revoked sessions", deleted)
}

// effectiveCleanupInterval falls back to the default interval when the caller
// passes zero or a negative duration, because time.NewTicker would panic.
func effectiveCleanupInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return DefaultSessionCleanupInterval
	}

	return interval
}

// hashSessionToken converts a raw token into its SHA-256 hexadecimal hash.
func hashSessionToken(token string) string {
	hash := sha256.Sum256([]byte(token))

	return hex.EncodeToString(hash[:])
}
