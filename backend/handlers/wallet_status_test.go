package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The wallet status endpoint is a read, but it must refuse anything that is not
// an authenticated GET: a status read is still private account data.
func TestWalletStatusRefusesWrongMethod(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/wallet/status", nil)
			recorder := httptest.NewRecorder()

			(&WalletHandler{}).Status(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestWalletStatusRefusesAnonymousGet(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/wallet/status", nil)
	recorder := httptest.NewRecorder()

	(&WalletHandler{}).Status(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}
