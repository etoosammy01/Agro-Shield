package services

import (
	"log"
	"strings"
)

// SMSSender delivers a short text message to a phone number. The `to` argument
// is expected in the international digits-only form produced by
// NormalizeNigerianPhone.
//
// Keeping this an interface means the OTP flow never depends on a specific
// vendor, and tests can substitute a recording sender instead of making
// network calls.
type SMSSender interface {
	Send(to, message string) error
}

// LogSender writes messages to the server log instead of sending them.
//
// It exists so one-time codes work end to end in local development without a
// vendor account, and it is the default when SMS_PROVIDER is unset. It must
// never be selected in production: the code it prints is a live credential,
// and anything with log access could read it.
type LogSender struct{}

func (LogSender) Send(to, message string) error {
	log.Printf("[sms:log] DEV TRANSPORT — no SMS sent. to=%s message=%q", to, message)
	return nil
}

// NewSMSSender builds the configured transport. Unknown or empty provider
// names fall back to LogSender; config.Load validates the value at startup, so
// anything reaching here has already been checked.
func NewSMSSender(provider, termiiAPIKey, termiiSenderID, termiiBaseURL string) SMSSender {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "termii":
		return NewTermiiSender(termiiAPIKey, termiiSenderID, termiiBaseURL)
	default:
		return LogSender{}
	}
}
