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
	"time"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/session"
	"backend/middleware"
)

const onboardingTestToken = "onboarding-test-token"

type onboardingAccount struct{ farmer *models.Farmer }

func (a onboardingAccount) GetByID(int) (*models.Farmer, error) { return a.farmer, nil }

type onboardingTokenValidator map[string]int

func (v onboardingTokenValidator) Validate(_ context.Context, token string) (int, error) {
	farmerID, ok := v[token]
	if !ok {
		return 0, session.ErrNotFound
	}
	return farmerID, nil
}

type onboardingOTP struct {
	issuedPhone, verifiedPhone, purpose, code string
	issues, verifications                     int
	err                                       error
}

func (o *onboardingOTP) Issue(_ context.Context, _ int, phone, purpose string) error {
	o.issues++
	o.issuedPhone, o.purpose = phone, purpose
	return o.err
}
func (o *onboardingOTP) VerifyPhone(_ context.Context, _ int, phone, code string) error {
	o.verifications++
	o.verifiedPhone, o.code = phone, code
	return o.err
}

// onboardingSessionRevoker records persistent session revocation attempts.
type onboardingSessionRevoker struct {
	calls int
	token string
	err   error
}

func (s *onboardingSessionRevoker) Revoke(_ context.Context, token string) error {
	s.calls++
	s.token = token
	return s.err
}

func onboardingRequest(t *testing.T, farmer *models.Farmer, handler http.HandlerFunc, method, path string, form url.Values) (*httptest.ResponseRecorder, string) {
	t.Helper()
	token := onboardingTestToken
	authenticator := middleware.NewAuthenticator(
		onboardingAccount{farmer},
		onboardingTokenValidator{token: farmer.ID},
	)
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cookies := httptest.NewRecorder()
	middleware.SetSessionCookie(cookies, r, token)
	r.AddCookie(cookies.Result().Cookies()[0])
	w := httptest.NewRecorder()
	authenticator.RequireAuth(handler)(w, r)
	return w, token
}

func TestPhoneVerificationCannotSkipSelfie(t *testing.T) {
	o := &onboardingOTP{}
	w, _ := onboardingRequest(t, &models.Farmer{ID: 10}, NewVerifyPhoneHandler(o, nil).Handler, "POST", "/verify-phone", url.Values{"action": {"send_code"}})
	if w.Header().Get("Location") != "/complete-profile" || o.issues != 0 {
		t.Fatal("phone verification allowed before selfie")
	}
}

func TestPhoneVerificationUsesRegisteredNumberAndPurpose(t *testing.T) {
	o := &onboardingOTP{}
	f := &models.Farmer{ID: 11, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(o, nil).Handler, "POST", "/verify-phone", url.Values{"action": {"send_code"}, "phone": {"08099999999"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/verify-phone?sent=1" || o.issuedPhone != f.Phone || o.purpose != models.OTPPurposePhoneVerification {
		t.Fatalf("incorrect SMS destination or purpose: %+v", o)
	}
}

func TestPhoneVerificationSuccessContinuesToWalletPINSetup(t *testing.T) {
	o := &onboardingOTP{}
	f := &models.Farmer{ID: 12, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, session := onboardingRequest(t, f, NewVerifyPhoneHandler(o, nil).Handler, "POST", "/verify-phone", url.Values{"action": {"verify_code"}, "code": {"123456"}, "phone": {"08099999999"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/create-transaction-pin" || o.verifiedPhone != f.Phone || o.code != "123456" {
		t.Fatal("verification did not continue to wallet PIN setup")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("phone verification ended the session before PIN setup")
	}
	f.PhoneVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	authenticator := middleware.NewAuthenticator(
		onboardingAccount{f},
		onboardingTokenValidator{session: f.ID},
	)
	r := httptest.NewRequest("GET", "/create-transaction-pin", nil)
	cookie := httptest.NewRecorder()
	middleware.SetSessionCookie(cookie, r, session)
	r.AddCookie(cookie.Result().Cookies()[0])
	check := httptest.NewRecorder()
	authenticator.RequireAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })(check, r)
	if check.Code != http.StatusNoContent {
		t.Fatal("onboarding session could not reach wallet PIN setup")
	}
}

func TestPhoneVerificationRevokesSessionWhenOnboardingIsComplete(t *testing.T) {
	o := &onboardingOTP{}
	sessions := &onboardingSessionRevoker{}

	farmer := &models.Farmer{
		ID:                20,
		Phone:             "08012345678",
		SelfieCapturedAt:  sql.NullTime{Time: time.Now(), Valid: true},
		PhoneVerifiedAt:   sql.NullTime{Time: time.Now(), Valid: true},
		HasTransactionPIN: true,
	}

	w, session := onboardingRequest(
		t,
		farmer,
		NewVerifyPhoneHandler(o, sessions).Handler,
		"GET",
		"/verify-phone",
		nil,
	)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?verified=1" {
		t.Fatal("completed onboarding did not redirect to login")
	}

	if sessions.calls != 1 || sessions.token != session {
		t.Fatal("persistent session was not revoked")
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("session cookie was not cleared")
	}
}

func TestPhoneVerificationHandlesSessionRevocationFailure(t *testing.T) {
	o := &onboardingOTP{}
	sessions := &onboardingSessionRevoker{
		err: errors.New("database unavailable"),
	}

	farmer := &models.Farmer{
		ID:                21,
		Phone:             "08012345678",
		SelfieCapturedAt:  sql.NullTime{Time: time.Now(), Valid: true},
		PhoneVerifiedAt:   sql.NullTime{Time: time.Now(), Valid: true},
		HasTransactionPIN: true,
	}

	w, session := onboardingRequest(
		t,
		farmer,
		NewVerifyPhoneHandler(o, sessions).Handler,
		"GET",
		"/verify-phone",
		nil,
	)

	if sessions.calls != 1 || sessions.token != session {
		t.Fatal("session revocation was not attempted")
	}

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d", w.Code)
	}

	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cookie was cleared despite failed revocation")
	}
}

func TestPhoneVerificationErrorsKeepUserOnStep(t *testing.T) {
	f := &models.Farmer{ID: 13, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	for _, err := range []error{otp.ErrInvalid, otp.ErrUnavailable, otp.ErrTooManyAttempts, errors.New("provider unavailable")} {
		o := &onboardingOTP{err: err}
		w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(o, nil).Handler, "POST", "/verify-phone", url.Values{"action": {"verify_code"}, "code": {"123456"}})
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" || !strings.Contains(html.UnescapeString(w.Body.String()), otpVerificationMessage(err)) {
			t.Fatalf("verification failure not displayed: %d %s", w.Code, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed verification logged out user")
		}
	}
}

func TestPhoneVerificationPageHasReadOnlyPhoneAndLoginNotice(t *testing.T) {
	f := &models.Farmer{ID: 14, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(&onboardingOTP{}, nil).Handler, "GET", "/verify-phone", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `value="08012345678" readonly`) || !strings.Contains(w.Body.String(), `autocomplete="one-time-code"`) {
		t.Fatal("verification page did not render")
	}
	login := httptest.NewRecorder()
	NewLoginHandler(nil, nil).LoginHandler(login, httptest.NewRequest("GET", "/login?verified=1", nil))
	if !strings.Contains(login.Body.String(), "Phone verified. Log in to continue.") {
		t.Fatal("login notice missing")
	}
}

func TestRegistrationRejectsUnusablePhoneBeforeSaving(t *testing.T) {
	page := newRegisterPage()
	page.FirstName, page.LastName, page.Phone = "Test", "User", "+14155550123"
	page.Community, page.LGA, page.State = "Community", "LGA", "Lagos"
	validateRegistration(&page, "password123", "password123")
	if page.FieldErrors["phone"] == "" {
		t.Fatal("unusable phone accepted")
	}
}
