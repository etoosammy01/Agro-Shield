package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// Service coordinates issuing, hashing, storing, and validating one-time passcodes.
type Service struct {
	repo     Store
	provider Provider
}

// NewService constructs an OTP Service with persistence and provider transports.
func NewService(repo Store, provider Provider) *Service {
	return &Service{repo: repo, provider: provider}
}

// Issue generates a fresh 6-digit passcode, invalidates previous codes for the same purpose,
// stores a bcrypt hash, and dispatches the code through the provider.
func (s *Service) Issue(ctx context.Context, farmerID int, phone, purpose string) error {
	if farmerID <= 0 {
		return errors.New("invalid account for one-time passcode")
	}
	if !models.ValidOTPPurpose(purpose) {
		return fmt.Errorf("unsupported one-time passcode purpose %q", purpose)
	}

	recipient := NormalizePhone(phone)
	if recipient == "" {
		return ErrUnusablePhone
	}

	issued, err := s.repo.CountIssuedSince(ctx, farmerID, purpose, time.Now().Add(-issueWindow))
	if err != nil {
		return fmt.Errorf("check one-time passcode rate limit: %w", err)
	}
	if issued >= maxIssuesPerWindow {
		return ErrRateLimited
	}

	code, err := generateCode()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash one-time passcode: %w", err)
	}

	expiresAt := time.Now().Add(codeTTL)
	if err := s.repo.Issue(ctx, farmerID, phone, purpose, string(hash), expiresAt); err != nil {
		return fmt.Errorf("store one-time passcode: %w", err)
	}

	if err := s.provider.Send(ctx, recipient, code, purpose); err != nil {
		return fmt.Errorf("send one-time passcode: %w", err)
	}
	return nil
}

// Verify checks a submitted passcode against the stored hash and provider.
func (s *Service) Verify(ctx context.Context, farmerID int, purpose, code string) error {
	return s.verify(ctx, farmerID, purpose, code, "")
}

func (s *Service) verify(ctx context.Context, farmerID int, purpose, code, phone string) error {
	if farmerID <= 0 {
		return errors.New("invalid account for one-time passcode")
	}
	if !models.ValidOTPPurpose(purpose) {
		return fmt.Errorf("unsupported one-time passcode purpose %q", purpose)
	}
	code = strings.TrimSpace(code)
	if !isWellFormedCode(code) {
		return ErrInvalid
	}

	active, err := s.repo.LatestActive(ctx, farmerID, purpose)
	if errors.Is(err, repository.ErrNoActiveOTP) {
		return ErrUnavailable
	}
	if err != nil {
		return fmt.Errorf("load one-time passcode: %w", err)
	}
	if phone != "" && active.Phone != phone {
		return ErrUnavailable
	}

	if active.Attempts >= maxAttempts {
		if err := s.repo.Consume(ctx, active.ID); err != nil {
			return fmt.Errorf("burn exhausted one-time passcode: %w", err)
		}
		return ErrTooManyAttempts
	}

	verificationErr := bcrypt.CompareHashAndPassword([]byte(active.CodeHash), []byte(code))
	if verificationErr == nil {
		verificationErr = s.provider.Verify(ctx, active.Phone, code)
		if verificationErr != nil && !errors.Is(verificationErr, ErrInvalid) {
			return fmt.Errorf("verify one-time passcode with provider: %w", verificationErr)
		}
	}

	if verificationErr != nil {
		attempts := active.Attempts
		if err := s.repo.IncrementAttempts(ctx, active.ID); err != nil {
			return fmt.Errorf("record failed one-time passcode attempt: %w", err)
		}
		if attempts+1 >= maxAttempts {
			if err := s.repo.Consume(ctx, active.ID); err != nil {
				return fmt.Errorf("burn exhausted one-time passcode: %w", err)
			}
			return ErrTooManyAttempts
		}
		return ErrInvalid
	}

	if phone != "" {
		err = s.repo.ConsumeAndVerifyPhone(ctx, active.ID, farmerID, active.Phone)
	} else {
		err = s.repo.Consume(ctx, active.ID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrNoActiveOTP) {
			return ErrUnavailable
		}
		return fmt.Errorf("consume one-time passcode: %w", err)
	}
	return nil
}

// DevelopmentDelivery reports if the current provider is purely development logging.
func (s *Service) DevelopmentDelivery() bool {
	provider, ok := s.provider.(interface{ DevelopmentDelivery() bool })
	return ok && provider.DevelopmentDelivery()
}
