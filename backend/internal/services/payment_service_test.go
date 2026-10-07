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
