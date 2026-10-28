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
	value := digits.String()

	switch {
	case value == "":
		return ""
	case strings.HasPrefix(value, "234"):
		if len(value) == 13 {
			return value
		}
		return ""
	case strings.HasPrefix(value, "0"):
		if len(value) == 11 {
			return "234" + value[1:]
		}
		return ""
	default:
		if len(value) == 10 {
			return "234" + value
		}
		return ""
	}
}

// VerifyPhone consumes the OTP challenge and atomically marks phone verification in persistence.
func (s *Service) VerifyPhone(ctx context.Context, farmerID int, phone, code string) error {
	if NormalizePhone(phone) == "" {
		return ErrUnusablePhone
	}
	return s.verify(ctx, farmerID, models.OTPPurposePhoneVerification, code, phone)
}
