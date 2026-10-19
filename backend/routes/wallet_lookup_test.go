package routes

import (
	"os"
	"strings"
	"testing"
)

func readFrontendFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// The payout form and the route table are edited in different languages, in
// different directories. Nothing else catches a path renamed on one side only,
// and the symptom would be a bank lookup that silently never fires - the form
// would still save, just without a verified name.
func TestWalletLookupAccountPathMatchesTheFrontend(t *testing.T) {
	const path = "/wallet/lookup-account"

	if routes := readFrontendFile(t, "Routes.go"); !strings.Contains(routes, `"`+path+`"`) {
		t.Errorf("Routes.go does not register %s", path)
	}
	if script := readFrontendFile(t, "../../frontend/js/wallet.js"); !strings.Contains(script, `"`+path+`"`) {
		t.Errorf("wallet.js does not call %s", path)
	}
}

// The account number comes first because it can identify the bank on its own.
// Flutterwave still needs the bank code to resolve the holder, so the form
// either fills that in from a successful lookup or lets the user search for it.
// Asking for a free-text name would undermine the verification.
func TestWalletPayoutFormAsksForNumberThenBankThenName(t *testing.T) {
	page := readFrontendFile(t, "../../frontend/pages/wallet.html")

	formStart := strings.Index(page, `action="/wallet/bank"`)
	if formStart < 0 {
		t.Fatal("the payout form was not found on the wallet page")
	}
	form := page[formStart:]

	previous := -1
	for _, field := range []string{`id="account-number"`, `id="bank-search"`, `id="account-name"`} {
		at := strings.Index(form, field)
		if at < 0 {
			t.Fatalf("the payout form has no %s field", field)
		}
		if at < previous {
			t.Errorf("the payout form asks for %s out of order", field)
		}
		previous = at
	}

	if !strings.Contains(form, `role="combobox"`) || !strings.Contains(form, `role="listbox"`) {
		t.Error("the bank field is not a searchable combobox backed by the full provider list")
	}
	if !strings.Contains(form, `id="account-number-status"`) {
		t.Error("the account number has no status line for automatic bank suggestions")
	}
	if !strings.Contains(form, `id="account-name" name="account_name"`) || !strings.Contains(form, "readonly") {
		t.Error("the account name must be a read-only value filled in by verification")
	}

	// The bank code is submitted, not the bank name: the server rejects a code
	// that is not Flutterwave's for the selected bank, so it has to be carried
	// in a field the bank search box keeps in step.
	if !strings.Contains(form, `id="bank-code"`) || !strings.Contains(form, `name="bank_code"`) {
		t.Error("the payout form does not submit a bank code alongside the bank name")
	}
}
