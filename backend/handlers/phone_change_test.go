package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/services"
	"database/sql"
)

type phoneChangeTestAuth struct {
	err       error
	staged    int
	cancelled int
}

func (a *phoneChangeTestAuth) RequestPhoneChange(context.Context, int, string) error {
	a.staged++
	return a.err
}

func (a *phoneChangeTestAuth) CancelPhoneChange(context.Context, int) error {
	a.cancelled++
	return a.err
}

type phoneChangeTestOTP struct {
	err          error
	requested    int
	verified     int
	requestPhone string
	verifyPhone  string
	verifyCode   string
}

func (o *phoneChangeTestOTP) RequestPhoneChange(context.Context, int, string) error {
	o.requested++
	return o.err
}

func (o *phoneChangeTestOTP) VerifyPhoneChange(_ context.Context, _ int, phone, code string) error {
	o.verified++
	o.verifyPhone, o.verifyCode = phone, code
	return o.err
}

func decodePhoneChangeResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode phone-change response: %v", err)
	}
	return result
}

func TestPhoneChangeRequestStagesBeforeIssuingOTP(t *testing.T) {
	auth := &phoneChangeTestAuth{}
	otp := &phoneChangeTestOTP{}
	handler := NewPhoneChangeHandler(auth, otp)
	farmer := &models.Farmer{ID: 42, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}

	w, _ := onboardingRequest(t, farmer, handler.RequestHandler, "POST", "/phone-change/request", url.Values{"phone": {"08012345678"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	result := decodePhoneChangeResponse(t, w)
	if result["message"] != "Verification code sent. It expires in 5 minutes." {
		t.Fatalf("message = %q", result["message"])
	}
	if auth.staged != 1 || otp.requested != 1 {
		t.Fatalf("stage=%d issue=%d", auth.staged, otp.requested)
	}
}

func TestPhoneChangeRequestRequiresValidPhone(t *testing.T) {
	for _, tc := range []struct {
		name       string
		phone      string
		wantStatus int
		wantError  string
	}{
		{"missing phone", "", http.StatusBadRequest, "Enter a valid Nigerian phone number."},
		{"invalid phone", "123", http.StatusBadRequest, "Enter a valid Nigerian phone number."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, &phoneChangeTestOTP{})
			w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.RequestHandler, "POST", "/phone-change/request", url.Values{"phone": {tc.phone}})
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			result := decodePhoneChangeResponse(t, w)
			if result["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", result["error"], tc.wantError)
			}
		})
	}
}

func TestPhoneChangeRequestMapsDuplicatePhoneToGenericResponse(t *testing.T) {
	auth := &phoneChangeTestAuth{err: services.ErrPhoneTaken}
	otp := &phoneChangeTestOTP{}
	handler := NewPhoneChangeHandler(auth, otp)
	w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.RequestHandler, "POST", "/phone-change/request", url.Values{"phone": {"08012345678"}})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	result := decodePhoneChangeResponse(t, w)
	if result["error"] != "That phone number is already in use. Please try another number." {
		t.Fatalf("error = %q", result["error"])
	}
	if otp.requested != 0 {
		t.Fatal("duplicate phone should not issue an OTP")
	}
}

func TestPhoneChangeRequestReturnsOTPIssueFailure(t *testing.T) {
	handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, &phoneChangeTestOTP{err: otp.ErrRateLimited})
	w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.RequestHandler, "POST", "/phone-change/request", url.Values{"phone": {"08012345678"}})

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	result := decodePhoneChangeResponse(t, w)
	if result["error"] != otpUserMessage(otp.ErrRateLimited) {
		t.Fatalf("error = %q", result["error"])
	}
}

func TestPhoneChangeVerifyAppliesValidCode(t *testing.T) {
	otp := &phoneChangeTestOTP{}
	handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, otp)
	farmer := &models.Farmer{ID: 42, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}

	w, _ := onboardingRequest(t, farmer, handler.VerifyHandler, "POST", "/phone-change/verify", url.Values{
		"phone": {farmer.Phone},
		"code":  {"123456"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	result := decodePhoneChangeResponse(t, w)
	if result["message"] != "Phone number updated successfully." {
		t.Fatalf("message = %q", result["message"])
	}
	if otp.verified != 1 {
		t.Fatalf("verifications = %d", otp.verified)
	}
	if otp.verifyPhone != "2348012345678" {
		t.Fatalf("verify phone = %q, want %q", otp.verifyPhone, "2348012345678")
	}
	if otp.verifyCode != "123456" {
		t.Fatalf("verify code = %q, want %q", otp.verifyCode, "123456")
	}
}

func TestPhoneChangeVerifyMapsInvalidAndExpiredOTP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"invalid", otp.ErrInvalid, http.StatusBadRequest, otpVerificationMessage(otp.ErrInvalid)},
		{"expired", otp.ErrUnavailable, http.StatusBadRequest, otpVerificationMessage(otp.ErrUnavailable)},
		{"exhausted", otp.ErrTooManyAttempts, http.StatusBadRequest, otpVerificationMessage(otp.ErrTooManyAttempts)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, &phoneChangeTestOTP{err: tc.err})
			w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.VerifyHandler, "POST", "/phone-change/verify", url.Values{
				"phone": {"08012345678"},
				"code":  {"123456"},
			})
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			result := decodePhoneChangeResponse(t, w)
			if result["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", result["error"], tc.wantError)
			}
		})
	}
}

func TestPhoneChangeVerifyRejectsWrongPhone(t *testing.T) {
	phoneChangeOTP := &phoneChangeTestOTP{err: otp.ErrUnavailable}
	handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, phoneChangeOTP)
	w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.VerifyHandler, "POST", "/phone-change/verify", url.Values{
		"phone": {"08012345678"},
		"code":  {"123456"},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	result := decodePhoneChangeResponse(t, w)
	if result["error"] != otpVerificationMessage(otp.ErrUnavailable) {
		t.Fatalf("error = %q", result["error"])
	}
}

func TestPhoneChangeCancelCancelsPendingChange(t *testing.T) {
	auth := &phoneChangeTestAuth{}
	handler := NewPhoneChangeHandler(auth, &phoneChangeTestOTP{})
	w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.CancelHandler, "POST", "/phone-change/cancel", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	result := decodePhoneChangeResponse(t, w)
	if result["message"] != "Phone change cancelled." {
		t.Fatalf("message = %q", result["message"])
	}
	if auth.cancelled != 1 {
		t.Fatalf("cancelled = %d", auth.cancelled)
	}
}

func TestPhoneChangeCancelWhenNoPendingChangeExists(t *testing.T) {
	handler := NewPhoneChangeHandler(&phoneChangeTestAuth{err: services.ErrNoPendingPhoneChange}, &phoneChangeTestOTP{})
	w, _ := onboardingRequest(t, &models.Farmer{ID: 42, SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}, PhoneVerifiedAt: sql.NullTime{Valid: true, Time: time.Now()}, HasTransactionPIN: true}, handler.CancelHandler, "POST", "/phone-change/cancel", nil)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	result := decodePhoneChangeResponse(t, w)
	if result["error"] != "No pending phone change." {
		t.Fatalf("error = %q", result["error"])
	}
}

func TestPhoneChangeHandlersRejectUnauthenticatedRequests(t *testing.T) {
	handler := NewPhoneChangeHandler(&phoneChangeTestAuth{}, &phoneChangeTestOTP{})

	tests := []struct {
		name string
		call func(t *testing.T) *httptest.ResponseRecorder
	}{
		{"request", func(t *testing.T) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/phone-change/request", strings.NewReader("phone=08012345678"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handler.RequestHandler(w, r)
			return w
		}},
		{"verify", func(t *testing.T) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/phone-change/verify", strings.NewReader("phone=08012345678&code=123456"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handler.VerifyHandler(w, r)
			return w
		}},
		{"cancel", func(t *testing.T) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/phone-change/cancel", nil)
			handler.CancelHandler(w, r)
			return w
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.call(t)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestPhoneChangeDoesNotAffectOnboardingVerification(t *testing.T) {
	o := &onboardingOTP{}
	f := &models.Farmer{ID: 15, Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Valid: true, Time: time.Now()}}
	w, _ := onboardingRequest(t, f, NewVerifyPhoneHandler(o).Handler, "POST", "/verify-phone", url.Values{
		"action": {"send_code"},
		"phone":  {"08099999999"},
	})

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/verify-phone?sent=1" || o.issues != 1 {
		t.Fatalf("onboarding verification changed: status=%d location=%q issues=%d", w.Code, w.Header().Get("Location"), o.issues)
	}
}
