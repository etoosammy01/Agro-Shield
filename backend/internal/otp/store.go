package otp

import (
	"context"
	"time"

	"backend/internal/models"
)

// Store defines persistence operations required for issuing and verifying codes.
// Typically satisfied by *repository.OTPRepository.
type Store interface {
	Issue(ctx context.Context, farmerID int, phone, purpose, codeHash string, expiresAt time.Time) error
	LatestActive(ctx context.Context, farmerID int, purpose string) (*models.OTPCode, error)
	IncrementAttempts(ctx context.Context, id int64) error
	Consume(ctx context.Context, id int64) error
	ConsumeAndVerifyPhone(ctx context.Context, id int64, farmerID int, phone string) error
	CountIssuedSince(ctx context.Context, farmerID int, purpose string, since time.Time) (int, error)
}
