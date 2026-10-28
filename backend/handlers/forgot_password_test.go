package handlers

import (
	"errors"
	"testing"

	"backend/internal/otp"
)

// A wrong or expired code must be described as such. The old mapping reused
// the "couldn't send a code" text, which was wrong and hid what to do next.
func TestResetCodeMessageDescribesVerificationFailure(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{otp.ErrInvalid, "That verification code is not correct. Check it and try again."},
		{otp.ErrUnavailable, "That verification code has expired or was already used. Request a new one."},
		{otp.ErrTooManyAttempts, "Too many incorrect attempts. Request a new verification code."},
		{otp.ErrRateLimited, "You have requested too many codes. Please wait a few minutes and try again."},
		{errors.New("SQLSTATE 08006 connection failure"), "We couldn't verify that code right now. Please try again in a moment."},
	}
	for _, tc := range cases {
		if got := otpVerificationMessage(tc.err); got != tc.want {
			t.Errorf("otpVerificationMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
