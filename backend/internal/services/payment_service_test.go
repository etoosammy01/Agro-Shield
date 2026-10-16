package services

import (
	"testing"

	"backend/internal/models"
)

func TestVerifiedPaymentStatus(t *testing.T) {
	tests := []struct {
		providerStatus string
		want           string
	}{
		{providerStatus: "successful", want: models.PaymentSuccessful},
		{providerStatus: "FAILED", want: models.PaymentFailed},
		{providerStatus: "cancelled", want: models.PaymentFailed},
		{providerStatus: "processing", want: models.PaymentPending},
		{providerStatus: "", want: models.PaymentPending},
	}
	for _, test := range tests {
		t.Run(test.providerStatus, func(t *testing.T) {
			if got := verifiedPaymentStatus(test.providerStatus); got != test.want {
				t.Fatalf("verifiedPaymentStatus(%q) = %q, want %q", test.providerStatus, got, test.want)
			}
		})
	}
}

func TestVerifyTransactionRejectsInvalidIDsBeforeNetworkCall(t *testing.T) {
	client := NewFlutterwaveClient("test-key", "test-hash", "http://localhost/callback", "card,banktransfer")
	for _, id := range []string{"", "abc", "0", "-1", "1/../../payments"} {
		t.Run(id, func(t *testing.T) {
			if _, err := client.VerifyTransaction(id); err == nil {
				t.Fatalf("VerifyTransaction(%q) expected an error", id)
			}
		})
	}
}
