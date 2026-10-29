package otp

import (
	"context"
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
