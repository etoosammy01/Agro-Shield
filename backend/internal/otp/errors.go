package otp

import "errors"

// Sentinel errors returned by the OTP service and providers.
var (
	// ErrInvalid means the supplied code was wrong or malformed.
	ErrInvalid = errors.New("that verification code is not correct")

	// ErrUnavailable means there is no active code to verify against.
	ErrUnavailable = errors.New("that verification code has expired or was already used. Request a new one")

	// ErrTooManyAttempts means the code exceeded max retry attempts and was burned.
	ErrTooManyAttempts = errors.New("too many incorrect attempts. Request a new verification code")

	// ErrRateLimited means too many codes were requested in a short time.
	ErrRateLimited = errors.New("you have requested too many codes. Please wait a few minutes and try again")

	// ErrUnusablePhone means the phone number is invalid for SMS delivery.
	ErrUnusablePhone = errors.New("your profile needs a valid Nigerian phone number before you can receive a verification code")

	// ErrDeliveryDisabled means external SMS delivery is currently unavailable.
	ErrDeliveryDisabled = errors.New("SMS verification is unavailable right now. Please contact support")
)
