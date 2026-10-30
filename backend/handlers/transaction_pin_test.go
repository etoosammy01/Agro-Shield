package handlers

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
)

type onboardingPIN struct {
	calls, owner      int
	pin, confirmation string
	err               error
}

func (p *onboardingPIN) Create(_ context.Context, owner int, pin, confirmation string) error {
	p.calls++
	p.owner, p.pin, p.confirmation = owner, pin, confirmation
	return p.err
}

func pinOnboardingFarmer() *models.Farmer {
	return &models.Farmer{ID: 15, WalletNumber: "8000000015", SelfieCapturedAt: sql.NullTime{Valid: true}, PhoneVerifiedAt: sql.NullTime{Valid: true}}
}

func TestWalletPINSetupCannotSkipSelfieOrPhoneVerification(t *testing.T) {
	for _, tc := range []struct {
		farmer *models.Farmer
		want   string
	}{
		{&models.Farmer{ID: 15}, "/complete-profile"},
		{&models.Farmer{ID: 15, SelfieCapturedAt: sql.NullTime{Valid: true}}, "/verify-phone"},
	} {
		pins := &onboardingPIN{}
		w, _ := onboardingRequest(t, tc.farmer, NewTransactionPINHandler(pins).Handler, "POST", "/create-transaction-pin", url.Values{"pin": {"0456"}, "confirm_pin": {"0456"}})
		if w.Header().Get("Location") != tc.want || pins.calls != 0 {
			t.Fatal("PIN setup bypassed an earlier onboarding step")
		}
	}
}

func TestWalletPINSetupUsesSessionOwnerAndEndsOnboardingSession(t *testing.T) {
	farmer, pins := pinOnboardingFarmer(), &onboardingPIN{}
	w, session := onboardingRequest(t, farmer, NewTransactionPINHandler(pins).Handler, "POST", "/create-transaction-pin",
		url.Values{"pin": {"0456"}, "confirm_pin": {"0456"}, "farmer_id": {"42"}, "wallet_id": {"42"}})
	if pins.owner != farmer.ID || pins.pin != "0456" || pins.confirmation != "0456" || w.Header().Get("Location") != "/login?onboarded=1" {
		t.Fatal("PIN setup used another wallet owner or did not complete onboarding")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("onboarding session cookie was not cleared")
	}
	farmer.HasTransactionPIN = true
	r := httptest.NewRequest("GET", "/wallet", nil)
	r.AddCookie(&http.Cookie{Name: cookies[0].Name, Value: session})
	check := httptest.NewRecorder()
	middleware.RequireAuth(onboardingAccount{farmer}, func(http.ResponseWriter, *http.Request) { t.Fatal("old onboarding session still authorized") })(check, r)
	if check.Header().Get("Location") != "/login" {
		t.Fatal("old onboarding session was not invalidated")
	}
}

func TestWalletPINSetupErrorsNeverEchoPINOrCompleteOnboarding(t *testing.T) {
	for _, tc := range []struct {
		err      error
		status   int
		location string
	}{
		{services.ErrInvalidTransactionPIN, http.StatusBadRequest, ""},
		{services.ErrTransactionPINMismatch, http.StatusBadRequest, ""},
		{errors.New("database failure containing 3948"), http.StatusInternalServerError, ""},
		{repository.ErrTransactionPINSetupUnavailable, http.StatusSeeOther, "/verify-phone"},
	} {
		pins := &onboardingPIN{err: tc.err}
		w, _ := onboardingRequest(t, pinOnboardingFarmer(), NewTransactionPINHandler(pins).Handler, "POST", "/create-transaction-pin", url.Values{"pin": {"3948"}, "confirm_pin": {"3948"}})
		if w.Code != tc.status || w.Header().Get("Location") != tc.location || strings.Contains(w.Body.String(), "3948") || len(w.Result().Cookies()) != 0 {
			t.Fatalf("unsafe PIN error response: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestWalletPINSetupCannotOverwriteExistingPIN(t *testing.T) {
	farmer := pinOnboardingFarmer()
	farmer.HasTransactionPIN = true
	for _, method := range []string{"GET", "POST"} {
		pins := &onboardingPIN{}
		w, _ := onboardingRequest(t, farmer, NewTransactionPINHandler(pins).Handler, method, "/create-transaction-pin", url.Values{"pin": {"9999"}, "confirm_pin": {"9999"}})
		if pins.calls != 0 || w.Header().Get("Location") != "/dashboard" {
			t.Fatal("setup was allowed to replace an existing wallet PIN")
		}
	}
}

func TestWalletPINSetupPageAndLoginCompletionNotice(t *testing.T) {
	w, _ := onboardingRequest(t, pinOnboardingFarmer(), NewTransactionPINHandler(&onboardingPIN{}).Handler, "GET", "/create-transaction-pin", nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "8000000015") || !strings.Contains(w.Body.String(), `name="pin" type="password"`) {
		t.Fatal("wallet PIN form did not render securely")
	}
	login := httptest.NewRecorder()
	NewLoginHandler(nil).LoginHandler(login, httptest.NewRequest("GET", "/login?onboarded=1", nil))
	if !strings.Contains(html.UnescapeString(login.Body.String()), "Your wallet transaction PIN is ready.") {
		t.Fatal("onboarding completion notice missing")
	}
}

func TestWalletPINTransferErrorsHaveSafeMessages(t *testing.T) {
	for _, err := range []error{repository.ErrTransactionPINIncorrect, repository.ErrTransactionPINLocked, repository.ErrTransactionPINNotSet} {
		if !strings.Contains(walletUserMessage(err), err.Error()) {
			t.Fatal("wallet PIN failure was not explained")
		}
	}
	err := errors.Join(services.ErrTransactionPINUnavailable, errors.New("internal database text"))
	if strings.Contains(walletUserMessage(err), "internal database text") {
		t.Fatal("PIN verification failure exposed infrastructure details")
	}
}
