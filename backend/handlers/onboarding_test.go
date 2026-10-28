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
	"backend/internal/services"
	"backend/middleware"
)

type onboardingAccount struct{ farmer *models.Farmer }

func (a onboardingAccount) GetByID(int) (*models.Farmer, error) { return a.farmer, nil }

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

func onboardingRequest(t *testing.T, farmer *models.Farmer, handler http.HandlerFunc, method, path string, form url.Values) (*httptest.ResponseRecorder, string) {
	t.Helper()
	session := middleware.CreateSession(farmer.ID)
	t.Cleanup(func() { middleware.DeleteSession(session) })
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cookies := httptest.NewRecorder()
	middleware.SetSessionCookie(cookies, session)
	r.AddCookie(cookies.Result().Cookies()[0])
	w := httptest.NewRecorder()
	middleware.RequireAuth(onboardingAccount{farmer}, handler)(w, r)
	return w, session
}

func TestPhoneVerificationCannotSkipSelfie(t *testing.T) {
	o := &onboardingOTP{}
	w, _ := onboardingRequest(t, &models.Farmer{ID: 10}, NewVerifyPhoneHandler(o).Handler, "POST", "/verify-phone", url.Values{"action": {"send_code"}})
	if w.Header().Get("Location") != "/complete-profile" || o.issues != 0 {
		t.Fatal("phone verification allowed before selfie")
	}
}

func TestPhoneVerificationUsesRegisteredNumberAndPurpose(t *testing.T) {
	o := &onboardingOTP{}
	f := &models.Farmer{ID: 11, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(o).Handler, "POST", "/verify-phone", url.Values{"action": {"send_code"}, "phone": {"08099999999"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/verify-phone?sent=1" || o.issuedPhone != f.Phone || o.purpose != models.OTPPurposePhoneVerification {
		t.Fatalf("incorrect SMS destination or purpose: %+v", o)
	}
}

func TestPhoneVerificationSuccessRequiresLoginAgain(t *testing.T) {
	o := &onboardingOTP{}
	f := &models.Farmer{ID: 12, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, session := onboardingRequest(t, f, NewVerifyPhoneHandler(o).Handler, "POST", "/verify-phone", url.Values{"action": {"verify_code"}, "code": {"123456"}, "phone": {"08099999999"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?verified=1" || o.verifiedPhone != f.Phone || o.code != "123456" {
		t.Fatal("verification did not finish at login")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("session cookie was not cleared")
	}
	// Reusing the former cookie must not authenticate, even after the account
	// has both completion timestamps.
	f.PhoneVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: cookies[0].Name, Value: session})
	check := httptest.NewRecorder()
	middleware.RequireAuth(onboardingAccount{f}, func(http.ResponseWriter, *http.Request) { t.Fatal("old onboarding session still valid") })(check, r)
	if check.Header().Get("Location") != "/login" {
		t.Fatal("old session not rejected")
	}
}

func TestPhoneVerificationErrorsKeepUserOnStep(t *testing.T) {
	f := &models.Farmer{ID: 13, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	for _, err := range []error{services.ErrOTPInvalid, services.ErrOTPUnavailable, services.ErrOTPTooManyAttempts, errors.New("provider unavailable")} {
		o := &onboardingOTP{err: err}
		w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(o).Handler, "POST", "/verify-phone", url.Values{"action": {"verify_code"}, "code": {"123456"}})
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" || !strings.Contains(html.UnescapeString(w.Body.String()), resetCodeMessage(err)) {
			t.Fatalf("verification failure not displayed: %d %s", w.Code, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed verification logged out user")
		}
	}
}

func TestPhoneVerificationPageHasReadOnlyPhoneAndLoginNotice(t *testing.T) {
	f := &models.Farmer{ID: 14, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(&onboardingOTP{}).Handler, "GET", "/verify-phone", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `value="08012345678" readonly`) || !strings.Contains(w.Body.String(), `autocomplete="one-time-code"`) {
		t.Fatal("verification page did not render")
	}
	login := httptest.NewRecorder()
	NewLoginHandler(nil).LoginHandler(login, httptest.NewRequest("GET", "/login?verified=1", nil))
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
