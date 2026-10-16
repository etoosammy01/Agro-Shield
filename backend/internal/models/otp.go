package models

import (
	"database/sql"
	"time"
)

// Purposes an OTP can be issued for. These values are constrained by the
// otp_codes.purpose CHECK constraint, so they must stay in sync with
// database.go.
const (
	OTPPurposeWithdrawal    = "withdrawal"
	OTPPurposeTransfer      = "transfer"
	OTPPurposePasswordReset = "password_reset"
)

// OTPCode is one issued one-time passcode. The code itself is only ever held
// as a hash; nothing stores or logs the plaintext after it is sent.
type OTPCode struct {
	ID         int64
	FarmerID   int
	Phone      string
	Purpose    string
	CodeHash   string
	Attempts   int
	ExpiresAt  time.Time
	ConsumedAt sql.NullTime
	CreatedAt  time.Time
}

// ValidOTPPurpose reports whether purpose is one this application issues.
func ValidOTPPurpose(purpose string) bool {
	switch purpose {
	case OTPPurposeWithdrawal, OTPPurposeTransfer, OTPPurposePasswordReset:
		return true
	default:
		return false
	}
}
