package handlers

import (
	"database/sql"
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

// An admin must not be able to approve their own request, and the page should
// not offer a button that the service would refuse: a control the admin cannot
// see the reason for reads as a broken page rather than as a rule.
func TestWalletAdminPageWithholdsApprovalOnTheAdminsOwnRequest(t *testing.T) {
	withdrawal := models.Withdrawal{
		ID: 3, FarmerID: 7, Amount: "5000", Currency: "NGN", Status: "pending",
		BankName: "Access Bank", BankCode: "044", AccountName: "Bola Farms",
		AccountNumber: "0690000032", Reference: "WITHDRAW-7-1", CreatedAt: time.Now(),
	}
	transfer := models.Transfer{
		ID: 9, SenderID: 7, SenderName: "Bola Farms",
		RecipientID: 42, RecipientName: "Ada Obi", RecipientPhone: "2348012345678",
		Amount: "250.00", Currency: "NGN", Status: models.TransferPending,
		Reference: "TRANSFER-7-xyz", CreatedAt: time.Now(),
	}

	for _, viewer := range []struct {
		name    string
		adminID int
		own     bool
	}{
		{name: "the admin who requested them", adminID: 7, own: true},
		{name: "another admin", adminID: 2, own: false},
	} {
		t.Run(viewer.name, func(t *testing.T) {
			data := WalletAdminPageData{
				Withdrawals: []models.Withdrawal{withdrawal},
				Transfers:   []models.Transfer{transfer},
				AdminID:     viewer.adminID,
			}

			recorder := httptest.NewRecorder()
			if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
				t.Fatalf("render wallet-admin.html: %v", err)
			}
			body := recorder.Body.String()

			for _, action := range []string{
				"/admin/wallet-withdrawals/approve",
				"/admin/wallet-transfers/approve",
			} {
				if got := strings.Contains(body, action); got == viewer.own {
					t.Errorf("rendered %q = %v, want %v for %s", action, got, !viewer.own, viewer.name)
				}
			}
			if viewer.own {
				// Withholding the button silently would leave the admin
				// wondering why. Both rows have to say who can act instead.
				if !strings.Contains(body, "you requested it") {
					t.Error("the admin's own withdrawal did not say another admin has to approve it")
				}
				if !strings.Contains(body, "you sent it") {
					t.Error("the admin's own transfer did not say another admin has to approve it")
				}
				// Refusing self-approval must not strand the money: rejecting is
				// what returns it, so that has to stay available to the requester.
				for _, action := range []string{
					"/admin/wallet-withdrawals/reject",
					"/admin/wallet-transfers/reject",
				} {
					if !strings.Contains(body, action) {
						t.Errorf("%s was withheld, leaving the requester with no way to release their funds", action)
					}
				}
			}
		})
	}
}

// The dead end this button exists for: a payout that never started leaves the
// row processing with funds reserved, and Reject only matches pending.
func TestWalletAdminPageOffersTheWayBackForAStalledPayout(t *testing.T) {
	withdrawal := models.Withdrawal{
		ID: 3, FarmerID: 7, Amount: "5000", Currency: "NGN", Status: "processing",
		BankName: "Access Bank", BankCode: "044", AccountName: "Bola Farms",
		AccountNumber: "0690000032", Reference: "WITHDRAW-7-1", CreatedAt: time.Now(),
	}

	for _, test := range []struct {
		name       string
		transferID sql.NullString
		wantReturn bool
	}{
		{name: "a payout the provider never confirmed", wantReturn: true},
		{
			name:       "a payout with a saved transfer",
			transferID: sql.NullString{String: "999", Valid: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			withdrawal.TransferID = test.transferID
			data := WalletAdminPageData{
				Withdrawals: []models.Withdrawal{withdrawal},
				AdminID:     2,
			}

			recorder := httptest.NewRecorder()
			if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
				t.Fatalf("render wallet-admin.html: %v", err)
			}
			body := recorder.Body.String()

			returnToQueue := strings.Contains(body, "/admin/wallet-withdrawals/return-to-queue")
			if returnToQueue != test.wantReturn {
				t.Errorf("rendered the return-to-queue action = %v, want %v", returnToQueue, test.wantReturn)
			}
			// A payout that is already on its way must be reconciled, never
			// returned - returning it would let it be rejected and refunded
			// while the provider also pays it.
			reconcile := strings.Contains(body, "/admin/wallet-withdrawals/reconcile")
			if reconcile == test.wantReturn {
				t.Errorf("rendered the reconcile action = %v, want %v", reconcile, !test.wantReturn)
			}
		})
	}
}

// A payout that reached Flutterwave but never recorded its transfer id can be
// settled in neither direction, because settlement matches on that id. The page
// has to ask the admin for it.
func TestWalletAdminPageOffersToRecordTheProviderTransfer(t *testing.T) {
	withdrawal := models.Withdrawal{
		ID: 3, FarmerID: 7, Amount: "5000", Currency: "NGN", Status: "processing",
		BankName: "Access Bank", BankCode: "044", AccountName: "Bola Farms",
		AccountNumber: "0690000032", Reference: "WITHDRAW-7-1", CreatedAt: time.Now(),
	}

	render := func(t *testing.T, transferID sql.NullString) string {
		t.Helper()
		withdrawal.TransferID = transferID
		recorder := httptest.NewRecorder()
		data := WalletAdminPageData{Withdrawals: []models.Withdrawal{withdrawal}, AdminID: 2}
		if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
			t.Fatalf("render wallet-admin.html: %v", err)
		}
		return recorder.Body.String()
	}

	t.Run("no transfer recorded", func(t *testing.T) {
		body := render(t, sql.NullString{})

		if !strings.Contains(body, "/admin/wallet-withdrawals/record-transfer") {
			t.Error("the record-transfer action was not offered for a payout with no saved transfer")
		}
		if !strings.Contains(body, `name="provider_transfer_id"`) {
			t.Error("the form did not ask for the transfer id")
		}
		if !strings.Contains(body, `name="withdrawal_id" value="3"`) {
			t.Error("the form did not carry the withdrawal it applies to")
		}
	})

	t.Run("transfer already recorded", func(t *testing.T) {
		body := render(t, sql.NullString{String: "999", Valid: true})

		if strings.Contains(body, "/admin/wallet-withdrawals/record-transfer") {
			t.Error("the record-transfer action was offered for a payout that already has its transfer")
		}
		if !strings.Contains(body, "/admin/wallet-withdrawals/reconcile") {
			t.Error("a payout with a saved transfer must offer the reconcile action")
		}
	})
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
	for _, want := range []string{"2.50%", "₦1,234.56", "3 sales"} {
		if !strings.Contains(body, want) {
			t.Errorf("the revenue panel did not render %q", want)
		}
	}
	if strings.Contains(body, "Commission is switched off") {
		t.Error("the revenue panel reported commission as off while it is on")
	}
}

// The verdict is the only place the payout-details check reaches a human, so it
// has to survive the template. A mismatch in particular must be visible before
// the admin approves, and must carry the holder the bank actually has - that is
// what tells them the money would have gone to the wrong person's account.
func TestWalletAdminPageRendersThePayoutDetailsVerdict(t *testing.T) {
	tests := []struct {
		name     string
		withdraw models.Withdrawal
		want     []string
	}{
		{
			name: "mismatch",
			withdraw: models.Withdrawal{
				ID: 4, FarmerID: 7, Amount: "5000.00", Currency: "NGN",
				BankName: "Access Bank", BankCode: "044", AccountName: "CHIDI NWOYE",
				AccountNumber: "0690000032", Reference: "WITHDRAW-7-1",
				Status: "pending", NameCheckStatus: models.NameCheckMismatch,
				NameCheckResolved: "ADAEZE OKONKWO",
				NameCheckDetail:   `the bank has "ADAEZE OKONKWO" registered on this account`,
			},
			want: []string{
				"Bank details do not match",
				"wallet-name-check-bad",
				`the bank has &#34;ADAEZE OKONKWO&#34; registered on this account`,
			},
		},
		{
			name: "unverified",
			withdraw: models.Withdrawal{
				ID: 5, FarmerID: 7, Amount: "5000.00", Currency: "NGN",
				BankName: "Access Bank", BankCode: "044", AccountName: "CHIDI NWOYE",
				AccountNumber: "0690000032", Reference: "WITHDRAW-7-2",
				Status: "pending", NameCheckStatus: models.NameCheckUnverified,
				NameCheckDetail: "the bank could not be reached to confirm the account name",
			},
			want: []string{"Not confirmed with the bank", "wallet-name-check-warn"},
		},
		{
			// Every withdrawal written before this check existed. It must not
			// borrow the words or the colour of a pass.
			name: "never checked",
			withdraw: models.Withdrawal{
				ID: 6, FarmerID: 7, Amount: "5000.00", Currency: "NGN",
				BankName: "Access Bank", BankCode: "044", AccountName: "CHIDI NWOYE",
				AccountNumber: "0690000032", Reference: "WITHDRAW-7-3",
				Status: "pending",
			},
			want: []string{"Never checked", "wallet-name-check-unknown"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			if err := render.RenderTemplates(recorder, "wallet-admin.html",
				WalletAdminPageData{Withdrawals: []models.Withdrawal{test.withdraw}}); err != nil {
				t.Fatalf("render wallet-admin.html: %v", err)
			}

			body := recorder.Body.String()
			for _, want := range test.want {
				if !strings.Contains(body, want) {
					t.Errorf("the verdict did not render %q", want)
				}
			}
			if test.withdraw.NameCheckStatus != models.NameCheckVerified &&
				strings.Contains(body, "Bank details checked") {
				t.Error("an unchecked payout rendered as checked")
			}
		})
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
	if !strings.Contains(body, "₦0<") {
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
