package services

import (
	"backend/internal/session"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"
)

// Session lifetime matches the intended browser cookie lifetime.
const sessionLifetime = 24 * time.Hour

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
	return s.create(ctx, farmerID, nil)
}

// CreateForLogin preserves the account revision checked with the password.
// A password reset or suspension/reactivation must not revive a stale login.
func (s *SessionService) CreateForLogin(ctx context.Context, farmerID int, revision time.Time) (string, time.Time, error) {
	return s.create(ctx, farmerID, &revision)
}

func (s *SessionService) create(ctx context.Context, farmerID int, revision *time.Time) (string, time.Time, error) {
	if s == nil || s.repo == nil {
		return "", time.Time{}, session.ErrUnavailable
	}

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

	var err error
	if revision != nil {
		repo, ok := s.repo.(interface {
			CreateForRevision(context.Context, string, int, time.Time, time.Time) error
		})
		if !ok {
			return "", time.Time{}, session.ErrUnavailable
		}
		err = repo.CreateForRevision(ctx, tokenHash, farmerID, expiresAt, *revision)
	} else {
		err = s.repo.Create(ctx, tokenHash, farmerID, expiresAt)
	}
	if err != nil {
		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

// Validate checks whether a token belongs to a valid session.
func (s *SessionService) Validate(
	ctx context.Context,
	token string,
) (int, error) {
	if s == nil || s.repo == nil {
		return 0, session.ErrUnavailable
	}
	tokenHash := hashSessionToken(token)

	return s.repo.FindValid(ctx, tokenHash)
}

// Revoke invalidates one session, for example during logout.
func (s *SessionService) Revoke(
	ctx context.Context,
	token string,
) error {
	if s == nil || s.repo == nil {
		return session.ErrUnavailable
	}
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

// RunCleanup removes expired/revoked sessions at startup and periodically.
// Authentication enforces expiry even when a cleanup pass fails.
func (s *SessionService) RunCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		pass, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.CleanupExpired(pass)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Print("session cleanup unavailable; expiry remains enforced on requests")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// hashSessionToken converts a raw token into its SHA-256 hexadecimal hash.
func hashSessionToken(token string) string {
	hash := sha256.Sum256([]byte(token))

	return hex.EncodeToString(hash[:])
}
