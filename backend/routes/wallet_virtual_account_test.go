package routes

import (
	"strings"
	"testing"
)

// The setup form, the status check and the route table are edited in different
// files. A path renamed on one side would leave the funding card posting into a
// 404, which looks like a provider failure to the farmer.
func TestWalletVirtualAccountPathsMatchTheFrontend(t *testing.T) {
	routes := readFrontendFile(t, "Routes.go")
	page := readFrontendFile(t, "../../frontend/pages/wallet.html")

	for _, path := range []string{"/wallet/virtual-account", "/wallet/virtual-account/check"} {
		if !strings.Contains(routes, `"`+path+`"`) {
			t.Errorf("Routes.go does not register %s", path)
		}
		if !strings.Contains(page, `action="`+path+`"`) {
			t.Errorf("wallet.html has no form posting to %s", path)
		}
	}

	// Setup posts, never links: an identity number must not end up in a URL,
	// browser history or a link preview.
	if strings.Contains(page, `href="/wallet/virtual-account"`) ||
		strings.Contains(page, `href="/wallet/virtual-account/check"`) {
		t.Error("a funding-account action was rendered as a link instead of a POST form")
	}

	// Both actions require a session. A logged-out visitor must be sent to
	// login rather than reaching a handler that reads a farmer from context.
	if count := strings.Count(routes, "authenticator.RequireAuth(walletHandler.CreateVirtualAccount)"); count != 1 {
		t.Errorf("CreateVirtualAccount is wrapped by persistent authentication %d times, want once", count)
	}
	if count := strings.Count(routes, "authenticator.RequireAuth(walletHandler.CheckVirtualAccount)"); count != 1 {
		t.Errorf("CheckVirtualAccount is wrapped by persistent authentication %d times, want once", count)
	}
}

// The copy control is local to the browser. It must point at the rendered
// account number, and the identity form must be submitted as a normal POST so
// the server can redirect back with an honest result.
func TestWalletFundingCardWiresCopyAndSubmitBehaviour(t *testing.T) {
	page := readFrontendFile(t, "../../frontend/pages/wallet.html")
	script := readFrontendFile(t, "../../frontend/js/wallet.js")

	for _, want := range []string{
		`id="funding-account-number"`,
		`data-copy-target="funding-account-number"`,
		`data-virtual-account-form`,
		`name="identity_kind"`,
		`name="identity_number"`,
		`name="consent"`,
		`type="checkbox"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("wallet.html is missing %q", want)
		}
	}

	for _, want := range []string{
		`document.querySelectorAll("[data-copy-target]")`,
		`navigator.clipboard.writeText(value)`,
		`document.querySelectorAll("[data-virtual-account-form]")`,
		`submit.disabled = true`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("wallet.js is missing %q", want)
		}
	}
}
