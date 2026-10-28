package handlers

import (
	"errors"

	"backend/internal/otp"
)

// otpUserMessage turns an OTP error into a user-friendly string.
func otpUserMessage(err error) string {
	switch {
	case errors.Is(err, otp.ErrRateLimited), errors.Is(err, otp.ErrUnusablePhone):
		return err.Error()
	case errors.Is(err, otp.ErrDeliveryDisabled):
		return otp.ErrDeliveryDisabled.Error() + "."
	default:
		return "We couldn't send a verification code right now. Please try again in a moment."
	}
}

// otpVerificationMessage turns an OTP verification failure into an explanatory string.
func otpVerificationMessage(err error) string {
	switch {
	case errors.Is(err, otp.ErrInvalid):
		return "That verification code is not correct. Check it and try again."
	case errors.Is(err, otp.ErrUnavailable):
		return "That verification code has expired or was already used. Request a new one."
	case errors.Is(err, otp.ErrTooManyAttempts):
		return "Too many incorrect attempts. Request a new verification code."
	case errors.Is(err, otp.ErrRateLimited):
		return "You have requested too many codes. Please wait a few minutes and try again."
	default:
		return "We couldn't verify that code right now. Please try again in a moment."
	}
}
