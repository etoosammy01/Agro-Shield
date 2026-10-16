package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/models"
	"backend/render"
)

// The wallet pages are the only place several template behaviours are used —
// calling a method with an argument inside a range, and reaching into the page
// data from inside that range. A mistake there is a runtime template error on
// the page a user opens to see their money, so it is worth rendering the real
// templates rather than trusting that they parsed.
func TestWalletPageRendersTransfers(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		Transfers: []models.Transfer{
			{
				ID: 1, SenderID: 42, SenderName: "Ada Obi",
				RecipientID: 7, RecipientName: "Bola Farms",
				Amount: "250.00", Currency: "NGN", Status: models.TransferPending,
				Note: "seed money", Reference: "TRANSFER-7-abc", CreatedAt: time.Now(),
			},
			{
				ID: 2, SenderID: 7, SenderName: "Bola Farms",
				RecipientID: 42, RecipientName: "Ada Obi",
				Amount: "100.00", Currency: "NGN", Status: models.TransferCompleted,
				Reference: "TRANSFER-7-def", CreatedAt: time.Now(),
			},
		},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}

	body := recorder.Body.String()
	// The viewer is farmer 7, so the first transfer is incoming and the second
	// outgoing. Getting these the wrong way round would misreport money moving
	// in each direction.
	if !strings.Contains(body, "from Ada Obi") {
		t.Error("an incoming transfer was not labelled as coming from the sender")
	}
	if !strings.Contains(body, "to Ada Obi") {
		t.Error("an outgoing transfer was not labelled as going to the recipient")
	}
	if !strings.Contains(body, "seed money") {
		t.Error("the transfer note was not rendered")
	}
}

func TestWalletPageRendersWithoutTransfers(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "0.00", Currency: "NGN"},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "another member will appear here") {
		t.Error("the empty transfers placeholder was not rendered")
	}
}

func TestWalletAdminPageRendersPendingTransfers(t *testing.T) {
	data := WalletAdminPageData{
		Transfers: []models.Transfer{{
			ID: 9, SenderID: 7, SenderName: "Bola Farms",
			RecipientID: 42, RecipientName: "Ada Obi", RecipientPhone: "2348012345678",
			Amount: "250.00", Currency: "NGN", Status: models.TransferPending,
			Reference: "TRANSFER-7-xyz", CreatedAt: time.Now(),
		}},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `name="transfer_id" value="9"`) {
		t.Error("the pending transfer's ID was not rendered into the approve form")
	}
	if !strings.Contains(body, "/admin/wallet-transfers/approve") {
		t.Error("the approve action was not rendered")
	}
}

func TestWalletAdminPageRendersRevenueWhenCommissionOn(t *testing.T) {
	data := WalletAdminPageData{
		RevenueTotal: "1234.56", RevenueCount: 3,
		CommissionEnabled: true, CommissionLabel: "2.50",
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}

	body := recorder.Body.String()
	for _, want := range []string{"2.50%", "₦1234.56", "3 sales"} {
		if !strings.Contains(body, want) {
			t.Errorf("the revenue panel did not render %q", want)
		}
	}
	if strings.Contains(body, "Commission is switched off") {
		t.Error("the revenue panel reported commission as off while it is on")
	}
}

// TestWalletAdminPageRendersRevenueWhenCommissionOff pins the default state:
// COMMISSION_PERCENT is unset out of the box, and the page must say so rather
// than imply the platform is collecting nothing while appearing to collect.
func TestWalletAdminPageRendersRevenueWhenCommissionOff(t *testing.T) {
	data := WalletAdminPageData{
		RevenueTotal: "0.00", RevenueCount: 0, CommissionEnabled: false,
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "Commission is switched off") {
		t.Error("the revenue panel did not report commission as off")
	}
	if !strings.Contains(body, "₦0.00") {
		t.Error("the revenue panel did not render a zero total")
	}
	// Pluralised correctly, so the common "off" state doesn't read "0 sale".
	if !strings.Contains(body, "0 sales") {
		t.Error("a zero sale count was not pluralised")
	}
}

// TestWalletAdminPagePluralisesASingleSale covers the boundary the placeholder
// above deliberately sits away from.
func TestWalletAdminPagePluralisesASingleSale(t *testing.T) {
	data := WalletAdminPageData{
		RevenueTotal: "2.13", RevenueCount: 1, CommissionEnabled: true, CommissionLabel: "2.50",
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "1 sale") {
		t.Error("a single sale was not rendered in the singular")
	}
}
