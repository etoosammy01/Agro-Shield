package otp

import (
	"context"
	"log"
)

// Provider abstracts the delivery and verification of OTP codes with an external vendor.
type Provider interface {
	Send(ctx context.Context, phone, code, purpose string) error
	Verify(ctx context.Context, phone, code string) error
}

// LogProvider is a local development transport that writes codes to the console/log.
type LogProvider struct{}

func (LogProvider) Send(ctx context.Context, phone, code, purpose string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	log.Printf("[otp:log] DEV TRANSPORT — no SMS sent. to=%s purpose=%s code=%s expires_in=%s", phone, purpose, code, codeTTL)
	return nil
}

func (LogProvider) Verify(ctx context.Context, phone, code string) error {
	return ctx.Err()
}

func (LogProvider) DevelopmentDelivery() bool { return true }
