package handlers

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/services"
)

type checkoutTestCrops struct {
	crop *models.Crop
}

func (c checkoutTestCrops) GetCrop(int) (*models.Crop, error) { return c.crop, nil }

type checkoutTestWallet struct {
	paid, receiptBuyerID int
	receiptToken         string
	existingOrderID      int64
}

func (w *checkoutTestWallet) PayForOrder(_ context.Context, buyerID, cropID int, quantity float64, token string, expectedUnitPrice ...float64) (int64, bool, error) {
	w.paid++
	return 40, false, nil
}

func (w *checkoutTestWallet) OrderForCheckoutToken(_ context.Context, buyerID int, token string) (int64, error) {
	w.receiptBuyerID, w.receiptToken = buyerID, token
	return w.existingOrderID, nil
}

type checkoutTestOTP struct {
	issues, verifications int
	phone, purpose, code  string
	issueErr, verifyErr   error
}

func (o *checkoutTestOTP) Issue(_ context.Context, _ int, phone, purpose string) error {
	o.issues++
	o.phone, o.purpose = phone, purpose
	return o.issueErr
}

func (o *checkoutTestOTP) Verify(_ context.Context, _ int, purpose, code string) error {
	o.verifications++
	o.purpose, o.code = purpose, code
	return o.verifyErr
}

func checkoutTestForm(action, code string) url.Values {
	return url.Values{
		"crop_id": {"3"}, "quantity": {"2"}, "unit_price": {"1250.50"}, "checkout_token": {"checkout-token"}, "action": {action}, "code": {code},
		"phone": {"08099999999"},
	}
}

func checkoutTestRequest(path string, form url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func testCheckout(wallet *checkoutTestWallet, codes *checkoutTestOTP) *Checkout {
	return NewCheckoutHandler(checkoutTestCrops{crop: &models.Crop{
		ID: 3, FarmerID: 2, Name: "Maize", Unit: "kg", Quantity: 10, PricePerUnit: 1250.50, ListedForSale: true,
	}}, wallet, codes)
}

func checkoutTestBuyer() *models.Farmer {
	return &models.Farmer{ID: 1, Role: "buyer", Phone: "08012345678", SelfieCapturedAt: sql.NullTime{Valid: true}, PhoneVerifiedAt: sql.NullTime{Valid: true}}
}

func TestWalletPurchaseRequiresConfirmationBeforeCharging(t *testing.T) {
	wallet, codes := &checkoutTestWallet{}, &checkoutTestOTP{}
	checkout := testCheckout(wallet, codes)
	buyer := checkoutTestBuyer()
	w := httptest.NewRecorder()
	result, err := checkout.Pay(w, checkoutTestRequest("/marketplace", checkoutTestForm("", "")), buyer)
	if err != nil || result != nil || w.Code != http.StatusOK || wallet.paid != 0 {
		t.Fatalf("initial purchase charged the wallet: result=%v err=%v status=%d paid=%d", result, err, w.Code, wallet.paid)
	}
	if codes.issues != 1 || codes.phone != buyer.Phone || codes.purpose != models.OTPPurposePayment {
		t.Fatal("payment code was not sent to the registered phone with its own purpose")
	}
	for _, text := range []string{"₦2,501", `name="checkout_token" value="checkout-token"`, `name="action" value="verify_payment"`, `autocomplete="one-time-code"`} {
		if !strings.Contains(w.Body.String(), text) {
			t.Errorf("payment confirmation missing %s", text)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("payment confirmation may be cached")
	}
	w = httptest.NewRecorder()
	result, err = checkout.Pay(w, checkoutTestRequest("/marketplace", checkoutTestForm("verify_payment", "123456")), buyer)
	if err != nil || result == nil || result.OrderID != 40 || wallet.paid != 1 || codes.verifications != 1 || codes.code != "123456" {
		t.Fatalf("verified payment did not complete exactly once: result=%v err=%v paid=%d", result, err, wallet.paid)
	}
}

func TestWalletPurchaseNeverChargesAfterOTPFailure(t *testing.T) {
	for _, verificationError := range []error{otp.ErrInvalid, otp.ErrUnavailable, otp.ErrTooManyAttempts, errors.New("OTP.dev unavailable")} {
		wallet := &checkoutTestWallet{}
		codes := &checkoutTestOTP{verifyErr: verificationError}
		w := httptest.NewRecorder()
		result, err := testCheckout(wallet, codes).Pay(w, checkoutTestRequest("/marketplace", checkoutTestForm("verify_payment", "123456")), &models.Farmer{ID: 1, Role: "buyer"})
		if err != nil || result != nil || wallet.paid != 0 || w.Code != http.StatusBadRequest || !strings.Contains(html.UnescapeString(w.Body.String()), otpVerificationMessage(verificationError)) {
			t.Fatalf("OTP failure allowed a charge: result=%v err=%v status=%d paid=%d", result, err, w.Code, wallet.paid)
		}
	}
}

func TestWalletPurchaseNeverChargesAfterSMSFailure(t *testing.T) {
	wallet, codes := &checkoutTestWallet{}, &checkoutTestOTP{issueErr: errors.New("SMS unavailable")}
	w := httptest.NewRecorder()
	result, err := testCheckout(wallet, codes).Pay(w, checkoutTestRequest("/marketplace", checkoutTestForm("", "")), &models.Farmer{ID: 1, Role: "buyer"})
	if err != nil || result != nil || w.Code != http.StatusBadGateway || wallet.paid != 0 || !strings.Contains(html.UnescapeString(w.Body.String()), otpUserMessage(codes.issueErr)) {
		t.Fatal("SMS failure allowed a charge or was reported as a successful delivery")
	}
}

func TestWalletPurchaseRequiresReviewIfPriceChangesDuringVerification(t *testing.T) {
	wallet, codes := &checkoutTestWallet{}, &checkoutTestOTP{}
	form := checkoutTestForm("verify_payment", "123456")
	form.Set("unit_price", "1000")
	w := httptest.NewRecorder()
	result, err := testCheckout(wallet, codes).Pay(w, checkoutTestRequest("/marketplace", form), checkoutTestBuyer())
	if err != nil || result != nil || w.Code != http.StatusConflict || codes.verifications != 0 || wallet.paid != 0 || !strings.Contains(w.Body.String(), "The price changed.") {
		t.Fatal("a changed price was charged without review or consumed the code")
	}
}

func TestCompletedWalletPurchaseCanBeConfirmedWithoutReusingOTP(t *testing.T) {
	wallet, codes := &checkoutTestWallet{existingOrderID: 20}, &checkoutTestOTP{verifyErr: otp.ErrUnavailable}
	checkout := NewCheckoutHandler(checkoutTestCrops{}, wallet, codes)
	result, err := checkout.Pay(httptest.NewRecorder(), checkoutTestRequest("/marketplace", checkoutTestForm("verify_payment", "123456")), &models.Farmer{ID: 1, Role: "buyer"})
	if err != nil || result == nil || !result.AlreadyPlaced || result.OrderID != 20 || wallet.paid != 0 || codes.verifications != 0 || wallet.receiptBuyerID != 1 || wallet.receiptToken != "checkout-token" {
		t.Fatal("completed payment retry could not read its buyer's receipt without another charge")
	}
}

func TestWalletPurchaseRejectsInvalidDetailsBeforeSendingOTP(t *testing.T) {
	for _, invalid := range []url.Values{
		{"quantity": {"NaN"}}, {"quantity": {"100"}}, {"quantity": {"0"}}, {"crop_id": {"0"}}, {"checkout_token": {""}}, {"action": {"skip_verification"}},
	} {
		form := checkoutTestForm("", "")
		for key, value := range invalid {
			form[key] = value
		}
		wallet, codes := &checkoutTestWallet{}, &checkoutTestOTP{}
		result, err := testCheckout(wallet, codes).Pay(httptest.NewRecorder(), checkoutTestRequest("/marketplace", form), &models.Farmer{ID: 1, Role: "buyer"})
		if err == nil || result != nil || wallet.paid != 0 || codes.issues != 0 || codes.verifications != 0 {
			t.Fatalf("invalid checkout details reached OTP or payment: %v", invalid)
		}
	}
}

func TestAlternateOrderRouteAlsoRequiresPaymentCode(t *testing.T) {
	wallet, codes := &checkoutTestWallet{}, &checkoutTestOTP{verifyErr: otp.ErrInvalid}
	checkout := testCheckout(wallet, codes)
	buyer := checkoutTestBuyer()
	handler := NewOrderHandler(nil, checkout)
	w, _ := onboardingRequest(t, buyer, handler.Place, "POST", "/orders/place", checkoutTestForm("verify_payment", "123456"))
	if w.Code != http.StatusBadRequest || wallet.paid != 0 || codes.verifications != 1 {
		t.Fatal("alternate order route allowed payment without a valid code")
	}
}

type checkoutTestPayment struct {
	paymentProcessor
	initiated    int
	preflightErr error
}

func (p *checkoutTestPayment) Preflight(context.Context, services.InitiatePaymentInput) error {
	return p.preflightErr
}
func (p *checkoutTestPayment) Initiate(_ context.Context, in services.InitiatePaymentInput) (*services.InitiatePaymentResult, error) {
	p.initiated++
	return &services.InitiatePaymentResult{CheckoutURL: "https://checkout.example.test", Reference: "ORDER-test"}, nil
}

func TestExternalOrderPaymentRequiresOTPAndBuyerAuthorization(t *testing.T) {
	for _, test := range []struct {
		name                                           string
		otpErr, preflightErr                           error
		wantStatus, wantInitiations, wantVerifications int
	}{
		{"valid", nil, nil, http.StatusOK, 1, 1},
		{"wrong code", otp.ErrInvalid, nil, http.StatusBadRequest, 0, 1},
		{"provider outage", errors.New("provider unavailable"), nil, http.StatusBadRequest, 0, 1},
		{"wrong order owner", nil, errors.New("order does not belong to this buyer"), http.StatusBadRequest, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			processor := &checkoutTestPayment{preflightErr: test.preflightErr}
			codes := &checkoutTestOTP{verifyErr: test.otpErr}
			handler := NewPaymentHandler(processor, codes)
			buyer := checkoutTestBuyer()
			w, _ := onboardingRequest(t, buyer, func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("Content-Type", "application/json")
				r.Body = io.NopCloser(strings.NewReader(`{"order_id":3,"code":"123456"}`))
				handler.InitiatePayment(w, r)
			}, "POST", "/orders/pay", nil)
			if w.Code != test.wantStatus || processor.initiated != test.wantInitiations || codes.verifications != test.wantVerifications || (codes.verifications > 0 && codes.purpose != models.OTPPurposePayment) {
				t.Fatalf("external payment bypassed verification: status=%d initiated=%d verified=%d", w.Code, processor.initiated, codes.verifications)
			}
		})
	}
}

func TestWalletOTPSendUsesAccountPhoneAndRejectsNonWalletPurposes(t *testing.T) {
	for _, purpose := range []string{models.OTPPurposePayment, models.OTPPurposeTransfer, models.OTPPurposeWithdrawal, models.OTPPurposePhoneVerification, models.OTPPurposePasswordReset, "unsupported"} {
		codes := &checkoutTestOTP{}
		handler := NewWalletHandler(nil, codes, nil)
		buyer := checkoutTestBuyer()
		w, _ := onboardingRequest(t, buyer, handler.SendOTP, "POST", "/wallet/otp/send", url.Values{"purpose": {purpose}, "phone": {"08099999999"}})
		allowed := purpose == models.OTPPurposePayment || purpose == models.OTPPurposeWithdrawal || purpose == models.OTPPurposeTransfer
		if allowed {
			if w.Code != http.StatusOK || codes.issues != 1 || codes.phone != buyer.Phone || codes.purpose != purpose {
				t.Fatalf("wallet OTP used browser destination or wrong purpose: %s", purpose)
			}
		} else if w.Code != http.StatusBadRequest || codes.issues != 0 {
			t.Fatalf("wallet endpoint allowed unrelated OTP purpose %s", purpose)
		}
	}
}
