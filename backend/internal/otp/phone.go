package otp

import (
	"context"
	"fmt"

	"backend/internal/models"
	phoneformat "backend/internal/phone"
)

// NormalizePhone normalizes Nigerian phone numbers to canonical international format "234...".
func NormalizePhone(phone string) string {
	return phoneformat.Normalize(phone)
}

// VerifyPhone consumes the OTP challenge and atomically marks phone verification in persistence.
func (s *Service) VerifyPhone(ctx context.Context, farmerID int, phone, code string) error {
	if NormalizePhone(phone) == "" {
		return ErrUnusablePhone
	}
	return s.verify(ctx, farmerID, models.OTPPurposePhoneVerification, code, phone)
}

// RequestPhoneChange issues a phone-change challenge for a number the profile
// service has already staged as pending_phone. It reuses the standard issuance
// path, including hashing, expiry, rate limiting, and delivery.
func (s *Service) RequestPhoneChange(ctx context.Context, farmerID int, phone string) error {
	if farmerID <= 0 {
		return fmt.Errorf("invalid account for one-time passcode")
	}
	recipient := NormalizePhone(phone)
	if recipient == "" {
		return ErrUnusablePhone
	}
	pending, err := s.repo.PendingPhone(ctx, farmerID)
	if err != nil {
		return fmt.Errorf("load pending phone: %w", err)
	}
	if pending != recipient {
		return ErrPhoneChangeNotPending
	}
	return s.Issue(ctx, farmerID, recipient, models.OTPPurposePhoneChange)
}

// VerifyPhoneChange consumes a phone-change challenge and atomically applies
// the pending number through the repository.
func (s *Service) VerifyPhoneChange(ctx context.Context, farmerID int, phone, code string) error {
	recipient := NormalizePhone(phone)
	if recipient == "" {
		return ErrUnusablePhone
	}
	return s.verify(ctx, farmerID, models.OTPPurposePhoneChange, code, recipient)
}
