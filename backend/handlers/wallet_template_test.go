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

func TestWalletPageResolvesTransferRecipientAutomatically(t *testing.T) {
	data := WalletPageData{
		Wallet:       &models.Wallet{ID: 1, FarmerID: 7, Balance: "5000.00", Currency: "NGN"},
		WalletNumber: "8000000007",
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		`id="transfer-recipient" name="recipient"`,
		`id="transfer-recipient-name" placeholder="Completed after the account is found" readonly`,
		`id="transfer-start" disabled`,
		`data-transfer-step="confirm" hidden`,
		`id="transfer-submit"`,
		`id="transfer-code" name="code"`,
		"phone or Agro-Shield wallet number",
		"Confirm and send for approval",
		"sent a 6-digit code to the phone number registered on your account",
		`id="wallet-number"`,
		"8000000007",
		`data-copy-target="wallet-number"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the transfer form did not render %q", want)
		}
	}
	if strings.Contains(body, `id="verify-transfer-recipient"`) {
		t.Error("the transfer form still renders a separate verify-recipient action")
	}
}

func TestWalletPageConfirmsWithdrawalWithOTPBeforeSubmitting(t *testing.T) {
	data := WalletPageData{
		Wallet:        &models.Wallet{ID: 1, FarmerID: 7, Balance: "5000.00", Currency: "NGN"},
		BankName:      "Access Bank",
		AccountName:   "James Inalegwu",
		AccountNumber: "0106787237",
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		`id="withdrawal-form"`,
		`id="withdrawal-amount" name="amount"`,
		`id="withdrawal-start"`,
		`data-withdrawal-step="confirm" hidden`,
		`id="withdrawal-code" name="code"`,
		`id="withdrawal-submit"`,
		"Confirm and request approval",
		"sent a 6-digit code to the phone number registered on your account",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the withdrawal form did not render %q", want)
		}
	}
	if strings.Contains(body, ">Send code</button>") {
		t.Error("the withdrawal form still asks for the passcode before the confirmation step")
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

// A bare "pending" left a farmer unable to tell whether a person or the payment
// provider was holding the money, and gave them no way out of a request they no
// longer wanted.
func TestWalletPageNamesWhoAPendingRequestIsWaitingFor(t *testing.T) {
	created := time.Date(2026, 10, 17, 5, 44, 0, 0, time.UTC)
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		Deposits: []models.WalletDeposit{{
			Amount: "5000.00", Currency: "NGN", Reference: "WALLET-7-abc",
			Status: models.DepositPending, CreatedAt: created,
		}},
		Withdrawals: []models.Withdrawal{{
			ID: 5, FarmerID: 7, Amount: "4000.00", Currency: "NGN",
			Status: models.WithdrawalPending, Reference: "WITHDRAW-7-abc",
			CreatedAt: created,
		}},
		Transfers: []models.Transfer{{
			ID: 9, SenderID: 7, SenderName: "Bola Farms",
			RecipientID: 42, RecipientName: "Ada Obi",
			Amount: "250.00", Currency: "NGN", Status: models.TransferPending,
			Reference: "TRANSFER-7-abc", CreatedAt: created,
		}},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		"Pending since 05:44 — waiting for the payment provider",
		"Pending since 05:44 — waiting for admin approval",
		`name="withdrawal_id" value="5"`,
		`/wallet/withdraw/cancel`,
		`name="transfer_id" value="9"`,
		`/wallet/transfer/cancel`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the pending request did not render %q", want)
		}
	}
}

// Cancelling is the sender's action. Offering it on a transfer they are
// receiving would point at money they never controlled.
func TestWalletPageOnlyOffersCancelOnTheSendersPendingTransfer(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		Transfers: []models.Transfer{
			{
				ID: 11, SenderID: 42, SenderName: "Ada Obi",
				RecipientID: 7, RecipientName: "Bola Farms",
				Amount: "100.00", Currency: "NGN", Status: models.TransferPending,
				Reference: "TRANSFER-42-in", CreatedAt: time.Now(),
			},
			{
				ID: 12, SenderID: 7, SenderName: "Bola Farms",
				RecipientID: 42, RecipientName: "Ada Obi",
				Amount: "100.00", Currency: "NGN", Status: models.TransferCompleted,
				Reference: "TRANSFER-7-done", CreatedAt: time.Now(),
			},
		},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, unwanted := range []string{
		`name="transfer_id" value="11"`,
		`name="transfer_id" value="12"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a cancel form was rendered for %s, which the viewer cannot cancel", unwanted)
		}
	}
}

// The overview is what tells an admin at a glance whether anything is late;
// without it they have to read every row to find that out.
func TestWalletAdminPageSummarisesWhatIsWaiting(t *testing.T) {
	rendered := func(t *testing.T, aged int) string {
		t.Helper()
		data := WalletAdminPageData{
			Withdrawals: []models.Withdrawal{{ID: 1}, {ID: 2}},
			Transfers:   []models.Transfer{{ID: 3}},
			PendingDeposits: []models.PendingDeposit{{
				Reference: "WALLET-7-abc", CreatedAt: time.Now().Add(-2 * time.Hour),
			}},
			AgedRequests: aged,
		}
		recorder := httptest.NewRecorder()
		if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
			t.Fatalf("render wallet-admin.html: %v", err)
		}
		return recorder.Body.String()
	}

	body := rendered(t, 2)
	for _, want := range []string{
		"Withdrawals waiting", "Transfers waiting", "Deposits unconfirmed", "Waiting over 4h",
		`href="#withdrawals"`, `href="#transfers"`, `href="#deposits"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview did not render %q", want)
		}
	}
	if !strings.Contains(body, "wallet-admin-stat-warn") {
		t.Error("an overdue request did not mark the ageing card")
	}

	if strings.Contains(rendered(t, 0), "wallet-admin-stat-warn") {
		t.Error("nothing was overdue, but the ageing card was still marked")
	}
}

// Managing admins from the page is what removes the database client from the
// path. The viewer and a configured admin must be shown without a remove
// button, because neither removal would stick.
func TestWalletAdminPageManagesAdminsFromThePage(t *testing.T) {
	data := WalletAdminPageData{
		AdminID: 7,
		Admins: []models.AdminAccount{
			{FarmerID: 7, FullName: "Bola Farms", Phone: "08010000001"},
			{FarmerID: 9, FullName: "Ada Obi", Phone: "08020000002"},
			{FarmerID: 2, FullName: "Configured Admin", FromConfig: true},
		},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		"Wallet admins", "Bola Farms", "Ada Obi", "Configured Admin",
		`action="/admin/wallet-admins"`, "Make admin", "Configured", "You",
		`name="farmer_id" value="9"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin section did not render %q", want)
		}
	}
	for _, unwanted := range []string{
		`name="farmer_id" value="7"`, // you cannot remove yourself
		`name="farmer_id" value="2"`, // a configured admin cannot be removed here
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a remove form was rendered for %s", unwanted)
		}
	}
}

// The exception list exists so a deposit stuck at the provider is visible to a
// person, rather than accumulating silently behind a "pending" badge.
func TestWalletAdminPageListsDepositsAwaitingTheProvider(t *testing.T) {
	data := WalletAdminPageData{
		PendingDeposits: []models.PendingDeposit{{
			FarmerID: 7, FullName: "Bola Farms",
			Amount: "5000.00", Currency: "NGN",
			Reference: "WALLET-7-abc",
			CreatedAt: time.Now().Add(-2 * time.Hour),
		}},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		"Deposits awaiting confirmation",
		"WALLET-7-abc",
		"Bola Farms (#7)",
		"2h",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the exception list did not render %q", want)
		}
	}
}

// The approvals page is an admin console, not a farmer page. It must not offer
// the farmer navigation, and every money action has to name what it will do
// before the form is sent so a mis-tap on a small phone is recoverable.
func TestWalletAdminPageRendersTheAdminConsoleShell(t *testing.T) {
	data := WalletAdminPageData{
		AdminID:   7,
		AdminName: "Bola Farms",
		Withdrawals: []models.Withdrawal{{
			ID: 3, FarmerID: 9, Amount: "5000", Currency: "NGN", Status: "pending",
			BankName: "Access Bank", BankCode: "044", AccountName: "Ada Obi",
			AccountNumber: "0690000032", Reference: "WITHDRAW-7-1", CreatedAt: time.Now(),
		}},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet-admin.html", data); err != nil {
		t.Fatalf("render wallet-admin.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		`class="wallet-admin-body"`,
		"Admin console",
		"Bola Farms",
		`href="#overview"`,
		`href="#admins"`,
		`href="/logout"`,
		`href="/profile"`,
		"Login &amp; security",
		`data-confirm="Approve payout of ₦5,000 NGN to Ada Obi?"`,
		`data-copy="WITHDRAW-7-1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin console did not render %q", want)
		}
	}

	// A farmer's navigation here would compete with the admin sections and
	// invite an admin to leave a money decision half-made.
	for _, unwanted := range []string{
		`class="navbar"`,
		`href="/marketplace"`,
		`href="/orders"`,
		`href="/deliveries"`,
		`href="/dashboard"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the admin console still rendered %q", unwanted)
		}
	}
}

// The permanent funding account is the only place on the wallet page that
// handles identity data. These renders pin what the farmer sees in each state,
// and that a state never offers an action that would do nothing.
func TestWalletPageRendersTheActiveFundingAccount(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7",
			AccountNumber: "0123456789",
			BankName:      "Flutterwave Bank",
			AccountName:   "ADAEZE OKONKWO",
			Currency:      "NGN",
			Status:        models.VirtualAccountActive,
		},
		HasEmail: true,
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		"0123456789",
		"Flutterwave Bank",
		"ADAEZE OKONKWO",
		"Identity verified and funding account active",
		`data-copy-target="funding-account-number"`,
		"How transfers work",
		"/static/assets/logo/Agro-Shield_logo.png",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the active funding account did not render %q", want)
		}
	}
	// An active account must not ask for identity data again: resubmitting could
	// only produce a second account request.
	for _, unwanted := range []string{
		`action="/wallet/virtual-account"`,
		`name="identity_number"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("an active account rendered the setup form %q", unwanted)
		}
	}
}

func TestWalletPageFallsBackToTheProfileNameForTheFundingAccount(t *testing.T) {
	data := WalletPageData{
		Wallet:   &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		FullName: "Adaeze Okonkwo",
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7",
			AccountNumber: "0123456789",
			BankName:      "Flutterwave Bank",
			Currency:      "NGN",
			Status:        models.VirtualAccountActive,
		},
		HasEmail: true,
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	if !strings.Contains(body, "AgroShield-Adaeze Okonkwo") {
		t.Fatalf("the funding account did not fall back to the profile name: %s", body)
	}
	if strings.Contains(body, "Not returned by Flutterwave — use your registered name") {
		t.Error("the funding account still rendered the empty-name placeholder")
	}
}

func TestWalletPageFundsThroughADebitCardOrBankTransferDialog(t *testing.T) {
	data := WalletPageData{
		Wallet:   &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		FullName: "James Inalegwu",
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7",
			AccountNumber: "0123456789", BankName: "Flutterwave MFB",
			AccountName: "AgroShield-James Inalegwu", Currency: "NGN",
			Status: models.VirtualAccountActive,
		},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		`data-open-funding`,
		"Fund account",
		`id="fund-account-dialog"`,
		`data-funding-choice="card"`,
		`data-funding-choice="banktransfer"`,
		`action="/wallet/deposit"`,
		"Continue to secure card checkout",
		"No amount is needed here",
		`id="dialog-account-number"`,
		"AgroShield-James Inalegwu",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the funding dialog did not render %q", want)
		}
	}
	if got := strings.Count(body, `id="deposit-amount"`); got != 1 {
		t.Errorf("amount input rendered %d times, want only in the debit-card option", got)
	}
	for _, unwanted := range []string{"Top up your wallet", "wallet-deposit-card"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the removed top-up card still rendered %q", unwanted)
		}
	}
}

// Flutterwave can return the narration with its hyphen, spacing and casing
// stripped. The wallet page should still show the branded account name built
// from the member's profile, not the provider's run-together version.
func TestWalletPageShowsBrandedFundingAccountName(t *testing.T) {
	data := WalletPageData{
		Wallet:   &models.Wallet{ID: 1, FarmerID: 7, Balance: "1500.00", Currency: "NGN"},
		FullName: "James Inalegwu",
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7",
			AccountNumber: "0123456789", BankName: "Flutterwave MFB",
			AccountName: "agroshieldjames inalegwu", Currency: "NGN",
			Status: models.VirtualAccountActive,
		},
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	if !strings.Contains(body, "AgroShield-James Inalegwu") {
		t.Fatal("the funding account did not use the branded profile name")
	}
	if strings.Contains(body, "agroshieldjames inalegwu") {
		t.Error("the funding account still rendered the provider's run-together name")
	}
}

func TestWalletPageRendersAPendingFundingAccountHonestly(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "0.00", Currency: "NGN"},
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7", Status: models.VirtualAccountPending,
		},
		HasEmail: true,
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{
		"waiting for Flutterwave",
		`action="/wallet/virtual-account/check"`,
		"Check account status",
		`action="/wallet/virtual-account"`,
		`name="consent"`,
		`pattern="[0-9]{11}"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the pending funding account did not render %q", want)
		}
	}
	if strings.Contains(body, "Identity verified") {
		t.Error("a pending account was rendered as verified")
	}
}

func TestWalletPageRendersAFailedFundingAccountWithARetry(t *testing.T) {
	data := WalletPageData{
		Wallet: &models.Wallet{ID: 1, FarmerID: 7, Balance: "0.00", Currency: "NGN"},
		VirtualAccount: &models.WalletVirtualAccount{
			ID: 1, FarmerID: 7, TxRef: "AGRO-VA-7", Status: models.VirtualAccountFailed,
		},
		HasEmail: true,
	}

	recorder := httptest.NewRecorder()
	if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
		t.Fatalf("render wallet.html: %v", err)
	}
	body := recorder.Body.String()

	for _, want := range []string{"Setup did not complete", "Try again", `name="consent"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the failed funding account did not render %q", want)
		}
	}
	// A failed attempt has nothing to check; the way forward is a retry.
	if strings.Contains(body, "/wallet/virtual-account/check") {
		t.Error("a failed account offered a status check that cannot succeed")
	}
}

func TestWalletPageOffersSetupOnlyWhenTheProfileHasAnEmail(t *testing.T) {
	renderPage := func(t *testing.T, hasEmail bool) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		data := WalletPageData{
			Wallet:   &models.Wallet{ID: 1, FarmerID: 7, Balance: "0.00", Currency: "NGN"},
			HasEmail: hasEmail,
		}
		if err := render.RenderTemplates(recorder, "wallet.html", data); err != nil {
			t.Fatalf("render wallet.html: %v", err)
		}
		return recorder.Body.String()
	}

	withEmail := renderPage(t, true)
	for _, want := range []string{
		"Receive money into your wallet",
		`action="/wallet/virtual-account"`,
		`name="consent"`,
		`name="identity_kind"`,
		`name="identity_number"`,
		"Agro-Shield does not store the BVN or NIN",
	} {
		if !strings.Contains(withEmail, want) {
			t.Errorf("the setup form did not render %q", want)
		}
	}

	withoutEmail := renderPage(t, false)
	if strings.Contains(withoutEmail, `action="/wallet/virtual-account"`) {
		t.Error("the setup form was offered without a profile email")
	}
	if !strings.Contains(withoutEmail, `href="/profile/edit"`) {
		t.Error("the page did not tell the farmer where to add an email")
	}
}
