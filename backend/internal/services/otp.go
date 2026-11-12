package services

import "backend/internal/otp"

// Type aliases and exported symbols so existing handlers and service callers
// remain fully compatible while all OTP logic lives in backend/internal/otp.
type OTPService = otp.Service
type OTPProvider = otp.Provider
type LogOTPProvider = otp.LogProvider

var (
	ErrOTPInvalid            = otp.ErrInvalid
	ErrOTPUnavailable        = otp.ErrUnavailable
	ErrOTPTooManyAttempts    = otp.ErrTooManyAttempts
	ErrOTPRateLimited        = otp.ErrRateLimited
	ErrOTPUnusablePhone      = otp.ErrUnusablePhone
	ErrPhoneChangeNotPending = otp.ErrPhoneChangeNotPending
	NewOTPService            = otp.NewService
)
