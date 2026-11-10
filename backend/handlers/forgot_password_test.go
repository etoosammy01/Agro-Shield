package handlers

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/services"
)

// A wrong or expired code must be described as such. The old mapping reused
// the "couldn't send a code" text, which was wrong and hid what to do next.
func TestResetCodeMessageDescribesVerificationFailure(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{otp.ErrInvalid, "That verification code is not correct. Check it and try again."},
		{otp.ErrUnavailable, "That verification code has expired or was already used. Request a new one."},
		{otp.ErrTooManyAttempts, "Too many incorrect attempts. Request a new verification code."},
		{otp.ErrRateLimited, "You have requested too many codes. Please wait a few minutes and try again."},
		{errors.New("SQLSTATE 08006 connection failure"), "We couldn't verify that code right now. Please try again in a moment."},
	}
	for _, tc := range cases {
		if got := otpVerificationMessage(tc.err); got != tc.want {
			t.Errorf("otpVerificationMessage(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// The password-reset page must use one message per outcome class. These
// golden strings keep the anti-enumeration wording from drifting.
func TestPasswordResetCanonicalMessages(t *testing.T) {
	const wantSent = "If that number has an account, we've sent a verification code. It expires in 5 minutes."
	const wantInvalid = "That verification code is not correct or has expired."

	if sentCodeMessage != wantSent {
		t.Errorf("sentCodeMessage = %q, want %q", sentCodeMessage, wantSent)
	}
	if invalidCodeMessage != wantInvalid {
		t.Errorf("invalidCodeMessage = %q, want %q", invalidCodeMessage, wantInvalid)
	}
}

// fakeForgotAuth imitates AuthService for password-reset handler tests.
type fakeForgotAuth struct {
	farmer    *models.Farmer
	lookupErr error
	resetErr  error

	lookupCalls  int
	resetCalls   int
	resetPhone   string
	resetNewPass string
}

func (f *fakeForgotAuth) FindByPhone(phone string) (*models.Farmer, error) {
	f.lookupCalls++
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	return f.farmer, nil
}

func (f *fakeForgotAuth) ResetPassword(phone, newPassword string) error {
	f.resetCalls++
	f.resetPhone = phone
	f.resetNewPass = newPassword
	return f.resetErr
}

// fakeForgotOTP imitates the OTP service for password-reset handler tests.
type fakeForgotOTP struct {
	issueErr  error
	verifyErr error

	issueCalls  int
	issueUserID int
	issuePhone  string
	issuePurp   string

	verifyCalls  int
	verifyUserID int
	verifyPurp   string
	verifyCode   string
}

func (f *fakeForgotOTP) Issue(_ context.Context, farmerID int, phone, purpose string) error {
	f.issueCalls++
	f.issueUserID = farmerID
	f.issuePhone = phone
	f.issuePurp = purpose
	return f.issueErr
}

func (f *fakeForgotOTP) Verify(_ context.Context, farmerID int, purpose, code string) error {
	f.verifyCalls++
	f.verifyUserID = farmerID
	f.verifyPurp = purpose
	f.verifyCode = code
	return f.verifyErr
}

func postForgotPassword(t *testing.T, h *ForgotPassword, form url.Values) (*httptest.ResponseRecorder, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.Handler(recorder, request)

	return recorder, recorder.Body.String()
}

func sendCodeForm(phone string) url.Values {
	return url.Values{"phone": {phone}, "action": {"send_code"}}
}

func resetForm(phone, code, password string) url.Values {
	return url.Values{
		"phone":            {phone},
		"code":             {code},
		"new_password":     {password},
		"confirm_password": {password},
	}
}

func registeredForgotAuth() *fakeForgotAuth {
	return &fakeForgotAuth{farmer: &models.Farmer{ID: 7, Phone: "+2348030000001"}}
}

func TestSendCodeResponseIsIdenticalForRegisteredAndUnknownPhones(t *testing.T) {
	registered := NewForgotPasswordHandler(registeredForgotAuth(), &fakeForgotOTP{})
	unknown := NewForgotPasswordHandler(&fakeForgotAuth{}, &fakeForgotOTP{})

	form := sendCodeForm("08030000001")
	knownRec, knownBody := postForgotPassword(t, registered, form)
	unknownRec, unknownBody := postForgotPassword(t, unknown, form)

	if knownRec.Code != http.StatusOK || unknownRec.Code != http.StatusOK {
		t.Errorf("status codes: registered = %d, unknown = %d, want both %d", knownRec.Code, unknownRec.Code, http.StatusOK)
	}
	if knownBody != unknownBody {
		t.Error("send-code responses differ between registered and unknown phones; the difference reveals registered numbers")
	}
}

func TestSendCodeOTPFailuresMatchUnknownPhoneResponse(t *testing.T) {
	unknown := NewForgotPasswordHandler(&fakeForgotAuth{}, &fakeForgotOTP{})
	unknownRec, unknownBody := postForgotPassword(t, unknown, sendCodeForm("08030000001"))

	cases := []struct {
		name     string
		issueErr error
	}{
		{"rate limited", otp.ErrRateLimited},
		{"unusable phone", otp.ErrUnusablePhone},
		{"delivery disabled", otp.ErrDeliveryDisabled},
		{"provider failure", errors.New("sms gateway unreachable")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewForgotPasswordHandler(registeredForgotAuth(), &fakeForgotOTP{issueErr: tc.issueErr})
			rec, body := postForgotPassword(t, handler, sendCodeForm("08030000001"))

			if rec.Code != unknownRec.Code {
				t.Errorf("status = %d, want %d", rec.Code, unknownRec.Code)
			}
			if body != unknownBody {
				t.Error("OTP failure response differs from the unknown-phone response; the difference reveals registered numbers")
			}
		})
	}
}

func TestSendCodeStillIssuesPasswordResetCode(t *testing.T) {
	auth := registeredForgotAuth()
	codes := &fakeForgotOTP{}
	handler := NewForgotPasswordHandler(auth, codes)

	postForgotPassword(t, handler, sendCodeForm("08030000001"))

	if codes.issueCalls != 1 {
		t.Fatalf("otp issues = %d, want 1", codes.issueCalls)
	}
	if codes.issueUserID != 7 {
		t.Errorf("issue farmer ID = %d, want 7", codes.issueUserID)
	}
	if codes.issuePhone != "+2348030000001" {
		t.Errorf("issue phone = %q, want the account phone", codes.issuePhone)
	}
	if codes.issuePurp != models.OTPPurposePasswordReset {
		t.Errorf("issue purpose = %q, want %q", codes.issuePurp, models.OTPPurposePasswordReset)
	}
}

func TestResetPasswordFailuresDoNotRevealRegistration(t *testing.T) {
	unknown := NewForgotPasswordHandler(&fakeForgotAuth{}, &fakeForgotOTP{})
	baselineRec, baselineBody := postForgotPassword(t, unknown, resetForm("08030000001", "123456", "newpassword1"))

	cases := []struct {
		name  string
		auth  *fakeForgotAuth
		codes *fakeForgotOTP
	}{
		{"unknown phone", &fakeForgotAuth{}, &fakeForgotOTP{}},
		{"wrong code", registeredForgotAuth(), &fakeForgotOTP{verifyErr: otp.ErrInvalid}},
		{"expired or used code", registeredForgotAuth(), &fakeForgotOTP{verifyErr: otp.ErrUnavailable}},
		{"too many attempts", registeredForgotAuth(), &fakeForgotOTP{verifyErr: otp.ErrTooManyAttempts}},
		{"verification error", registeredForgotAuth(), &fakeForgotOTP{verifyErr: errors.New("database unavailable")}},
		{"lookup failure", &fakeForgotAuth{lookupErr: errors.New("database down")}, &fakeForgotOTP{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewForgotPasswordHandler(tc.auth, tc.codes)
			rec, body := postForgotPassword(t, handler, resetForm("08030000001", "123456", "newpassword1"))

			if rec.Code != baselineRec.Code {
				t.Errorf("status = %d, want %d", rec.Code, baselineRec.Code)
			}
			if body != baselineBody {
				t.Errorf("%s: reset failure response differs from the unknown-phone response", tc.name)
			}
		})
	}
}

func TestResetPasswordWithValidCodeResetsPassword(t *testing.T) {
	auth := registeredForgotAuth()
	codes := &fakeForgotOTP{}
	handler := NewForgotPasswordHandler(auth, codes)

	rec, body := postForgotPassword(t, handler, resetForm("08030000001", "123456", "newpassword1"))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(body, "Password updated") {
		t.Error("success page not shown after a valid code")
	}
	if codes.verifyCalls != 1 {
		t.Errorf("otp verifications = %d, want 1", codes.verifyCalls)
	}
	if codes.verifyUserID != 7 {
		t.Errorf("verify farmer ID = %d, want 7", codes.verifyUserID)
	}
	if codes.verifyPurp != models.OTPPurposePasswordReset {
		t.Errorf("verify purpose = %q, want %q", codes.verifyPurp, models.OTPPurposePasswordReset)
	}
	if codes.verifyCode != "123456" {
		t.Errorf("verified code = %q, want %q", codes.verifyCode, "123456")
	}
	if auth.resetCalls != 1 {
		t.Errorf("password resets = %d, want 1", auth.resetCalls)
	}
	if auth.resetPhone != "08030000001" {
		t.Errorf("reset phone = %q, want %q", auth.resetPhone, "08030000001")
	}
	if auth.resetNewPass != "newpassword1" {
		t.Errorf("reset password = %q, want the submitted new password", auth.resetNewPass)
	}
}

// Service-level failures while writing the new password must share one
// generic response, whether the account vanished, the database failed, or
// anything else went wrong. Internal error text must never reach the page.
func TestResetPasswordServiceFailuresShareOneGenericResponse(t *testing.T) {
	cases := []struct {
		name     string
		resetErr error
	}{
		{"account not found", errors.New("account not found")},
		{"database failure", errors.New("SQLSTATE 08006 connection failure")},
		{"unexpected failure", errors.New("something else went wrong")},
	}

	baseline := ""
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewForgotPasswordHandler(
				&fakeForgotAuth{farmer: &models.Farmer{ID: 7}, resetErr: tc.resetErr},
				&fakeForgotOTP{},
			)

			rec, raw := postForgotPassword(t, handler, resetForm("08030000001", "123456", "newpassword1"))
			// The template HTML-escapes messages, so unescape before matching
			// text that contains an apostrophe.
			body := html.UnescapeString(raw)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if !strings.Contains(body, resetFailedMessage) {
				t.Error("generic reset-failure message not shown")
			}
			if strings.Contains(body, tc.resetErr.Error()) {
				t.Errorf("response leaks the internal error %q", tc.resetErr.Error())
			}
			if baseline == "" {
				baseline = body
			} else if body != baseline {
				t.Error("service failure responses differ between error types")
			}
		})
	}
}

// A password that breaks a rule the user can fix keeps its specific, useful
// wording instead of the generic failure message.
func TestResetPasswordShortPasswordKeepsValidationMessage(t *testing.T) {
	handler := NewForgotPasswordHandler(
		&fakeForgotAuth{farmer: &models.Farmer{ID: 7}, resetErr: services.ErrPasswordTooShort},
		&fakeForgotOTP{},
	)

	rec, raw := postForgotPassword(t, handler, resetForm("08030000001", "123456", "short"))
	body := html.UnescapeString(raw)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(body, "password must be at least 8 characters") {
		t.Error("short-password validation message not shown")
	}
	if strings.Contains(body, resetFailedMessage) {
		t.Error("short-password error should keep its specific wording")
	}
}

func TestResetPasswordValidationIsUniformAndPreserved(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{
			name: "mismatched passwords",
			form: url.Values{
				"phone":            {"08030000001"},
				"code":             {"123456"},
				"new_password":     {"aaaaaaaa"},
				"confirm_password": {"bbbbbbbb"},
			},
			want: "Passwords do not match",
		},
		{
			name: "missing code",
			form: url.Values{
				"phone":            {"08030000001"},
				"code":             {""},
				"new_password":     {"newpassword1"},
				"confirm_password": {"newpassword1"},
			},
			want: "Enter the verification code",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registered := NewForgotPasswordHandler(registeredForgotAuth(), &fakeForgotOTP{})
			unknown := NewForgotPasswordHandler(&fakeForgotAuth{}, &fakeForgotOTP{})

			knownRec, knownBody := postForgotPassword(t, registered, tc.form)
			unknownRec, unknownBody := postForgotPassword(t, unknown, tc.form)

			if !strings.Contains(knownBody, tc.want) {
				t.Errorf("registered response missing validation message %q", tc.want)
			}
			if knownRec.Code != unknownRec.Code || knownBody != unknownBody {
				t.Error("validation responses differ between registered and unknown phones")
			}
		})
	}
}
