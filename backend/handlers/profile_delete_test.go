package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The delete-account form is a destructive POST. A browser refresh or a
// bookmarked link must never trigger it, and an anonymous request must not
// reach the deletion service at all.
func TestDeleteAccountRequiresPost(t *testing.T) {
	handler := NewProfileEditHandler(nil)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/profile/delete", nil)
			handler.DeleteHandler(recorder, request)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestDeleteAccountRedirectsAnonymousUserToLogin(t *testing.T) {
	handler := NewProfileEditHandler(nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/profile/delete", strings.NewReader("confirm_phone=&password="))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.DeleteHandler(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/login" {
		t.Fatalf("redirect = %q, want /login", location)
	}
}
