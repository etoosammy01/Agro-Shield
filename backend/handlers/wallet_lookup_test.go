package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A bank lookup carries a full account number, so it must not be reachable by
// GET: a GET would put that number in access logs, browser history and any
// Referer header leaving the page. The handler refuses the method before it
// touches auth or the service, so no wallet service is needed to prove it.
func TestLookupAccountRefusesGet(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/wallet/lookup-account", nil)
			recorder := httptest.NewRecorder()

			(&WalletHandler{}).LookupAccount(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

// The search fallback in the payout form is only usable if the endpoint refuses
// an unauthenticated caller, so this pins that a POST without a session never
// reaches the service. The handler's auth check runs before the form is read,
// so a malformed body cannot be used to probe the service either.
func TestLookupAccountRefusesAnonymousPost(t *testing.T) {
	body := url.Values{"account_number": {"0690000032"}}.Encode()
	request := httptest.NewRequest(http.MethodPost, "/wallet/lookup-account", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	(&WalletHandler{}).LookupAccount(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
