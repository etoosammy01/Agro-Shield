package otp

import (
	"context"
	"log"
)

// Provider delivers application-generated codes. The service verifies the
// stored hash locally, including its account, purpose, expiry, and use limits.
type Provider interface {
	Send(ctx context.Context, phone, code, purpose string) error
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

func (LogProvider) DevelopmentDelivery() bool { return true }
