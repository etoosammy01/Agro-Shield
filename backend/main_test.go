package main

import (
	"testing"

	"backend/internal/config"
	"backend/internal/otp"
)

func TestStartupSelectsConfiguredOTPTransport(t *testing.T) {
	provider, err := newOTPProvider(config.Config{
		SMSProvider: "otpdev", OTPDevAPIKey: "test-key", OTPDevSender: "OTP Dev", OTPDevTemplateID: "a724614a-3a7c-4625-930e-2a8b45141474",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(*otp.OTPDevProvider); !ok {
		t.Fatal("configured OTP.dev transport did not select the real SMS provider")
	}
	provider, err = newOTPProvider(config.Config{SMSProvider: "log"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(otp.LogProvider); !ok {
		t.Fatal("development did not select the log transport")
	}
	for _, cfg := range []config.Config{{SMSProvider: "unknown"}, {SMSProvider: "otpdev"}} {
		if _, err := newOTPProvider(cfg); err == nil {
			t.Fatal("invalid provider configuration silently selected a transport")
		}
	}
}
