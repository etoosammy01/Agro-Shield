package handlers

import (
	"errors"
	"strings"
	"testing"

	"backend/internal/services"
)

func TestConsentGivenOnlyAcceptsAnExplicitYes(t *testing.T) {
	for _, value := range []string{"yes", "on", "true", "1", " YES "} {
		if !consentGiven(value) {
			t.Errorf("consentGiven(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "no", "false", "0", "maybe"} {
		if consentGiven(value) {
			t.Errorf("consentGiven(%q) = true, want false", value)
		}
	}
}

func TestVirtualAccountErrorMessageNeverLeaksProviderDetail(t *testing.T) {
	for _, err := range []error{
		services.ErrVirtualAccountConsentRequired,
		services.ErrVirtualAccountIdentityInvalid,
		services.ErrVirtualAccountEmailRequired,
		services.ErrVirtualAccountNameRequired,
		services.ErrVirtualAccountRejected,
		services.ErrVirtualAccountPending,
		services.ErrVirtualAccountUnavailable,
	} {
		message := virtualAccountErrorMessage(err)
		if message == "" {
			t.Errorf("%v produced an empty message", err)
		}
		if strings.Contains(message, "22222222222") {
			t.Errorf("%v produced a message containing an identity number: %q", err, message)
		}
	}

	providerDetail := errors.New(`provider body: {"account_number":"0123456789","bvn":"22222222222"}`)
	message := virtualAccountErrorMessage(providerDetail)
	if strings.Contains(message, "0123456789") || strings.Contains(message, "22222222222") ||
		strings.Contains(message, "provider body") {
		t.Fatalf("provider detail reached the user-facing message: %q", message)
	}
	if !strings.Contains(message, "technical issue") {
		t.Errorf("the fallback message did not describe a technical failure: %q", message)
	}

	// A wrapped sentinel still maps to its actionable message.
	wrapped := errors.Join(services.ErrVirtualAccountIdentityInvalid, errors.New("internal detail"))
	if got := virtualAccountErrorMessage(wrapped); got != services.ErrVirtualAccountIdentityInvalid.Error() {
		t.Errorf("wrapped error message = %q, want the actionable sentinel", got)
	}
}
