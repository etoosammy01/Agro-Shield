package services

import (
	"context"
	"log"
)

// OTPProvider delivers an application-generated code and checks it with the
// delivery provider. OTPService also enforces local hashes, expiry, attempt
// limits and single use, scoped to the account and purpose.
type OTPProvider interface {
	Send(ctx context.Context, phone, code, purpose string) error
	Verify(ctx context.Context, phone, code string) error
}

// LogOTPProvider writes codes to the server log instead of sending them.
//
// It exists so one-time codes work end to end in local development without a
// vendor account, and it is the default when SMS_PROVIDER is unset. It must
// never be selected in production: the code it prints is a live credential,
// and anything with log access could read it.
type LogOTPProvider struct{}

func (LogOTPProvider) Send(ctx context.Context, phone, code, purpose string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	log.Printf("[sms:log] DEV TRANSPORT — no SMS sent. to=%s purpose=%s code=%s expires_in=%s", phone, purpose, code, otpTTL)
	return nil
}

// Development codes are checked locally by OTPService; there is no remote
// provider to contact in log mode.
func (LogOTPProvider) Verify(ctx context.Context, phone, code string) error {
	return ctx.Err()
}
