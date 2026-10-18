package handlers

import (
	"errors"
	"testing"

	"backend/internal/services"
)

// A wrong or expired code must be described as such. The old mapping reused
// the "couldn't send a code" text, which was wrong and hid what to do next.
func TestResetCodeMessageDescribesVerificationFailure(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{services.ErrOTPInvalid, "That verification code is not correct. Check it and try again."},
		{services.ErrOTPUnavailable, "That verification code has expired or was already used. Request a new one."},
		{services.ErrOTPTooManyAttempts, "Too many incorrect attempts. Request a new verification code."},
		{services.ErrOTPRateLimited, "You have requested too many codes. Please wait a few minutes and try again."},
		{errors.New("SQLSTATE 08006 connection failure"), "We couldn't verify that code right now. Please try again in a moment."},
	}
	for _, tc := range cases {
		if got := resetCodeMessage(tc.err); got != tc.want {
			t.Errorf("resetCodeMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
