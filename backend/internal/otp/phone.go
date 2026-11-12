package otp

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/models"
)

// NormalizePhone normalizes Nigerian phone numbers to canonical international format "234...".
func NormalizePhone(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	value := strings.TrimPrefix(digits.String(), "00")
	value = strings.TrimPrefix(value, "234")
	value = strings.TrimPrefix(value, "0")
	// Match Agrilinks' accepted international and trunk formats. Nigerian
	// mobile subscriber numbers have ten digits starting with 7, 8, or 9.
	if len(value) != 10 || value[0] < '7' || value[0] > '9' {
		return ""
	}
	return "234" + value
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
