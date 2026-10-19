package services

import (
	"context"
	"net/http"
	"testing"

	"backend/internal/models"
)

type depositFundingStore struct {
	walletStore
	created bool
}

func (s *depositFundingStore) CreateDeposit(ctx context.Context, farmerID int, provider, reference, amount string) error {
	s.created = true
	return nil
}

func TestInitiateDepositUsesASecureCardCheckoutWithTheBrandedName(t *testing.T) {
	captured := &capturedRequest{}
	client := clientWithTransport(&fakeTransport{
		status:   http.StatusOK,
		body:     `{"status":"success","data":{"link":"https://checkout.example"}}`,
		captured: captured,
	})
	store := &depositFundingStore{}
	service := &WalletService{repo: store, flutter: client}

	if _, err := service.InitiateDeposit(context.Background(), &models.Farmer{
		ID: 7, FullName: "James Inalegwu", Email: "james@example.test",
	}, "5000"); err != nil {
		t.Fatalf("InitiateDeposit() error = %v", err)
	}
	if !store.created {
		t.Fatal("InitiateDeposit() did not record the pending deposit")
	}
	if captured.body["payment_options"] != "card" {
		t.Errorf("payment_options = %v, want card only", captured.body["payment_options"])
	}
	customizations, ok := captured.body["customizations"].(map[string]any)
	if !ok {
		t.Fatalf("customizations = %#v, want the checkout title and description", captured.body["customizations"])
	}
	if customizations["title"] != "AgroShield-James Inalegwu" || customizations["description"] != "AgroShield-James Inalegwu" {
		t.Errorf("customizations = %#v, want the branded account name", customizations)
	}
}
