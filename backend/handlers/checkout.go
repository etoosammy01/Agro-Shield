package handlers

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/render"
)

type paymentOTP interface {
	Issue(context.Context, int, string, string) error
	Verify(context.Context, int, string, string) error
}

type checkoutCrops interface {
	GetCrop(int) (*models.Crop, error)
}

type checkoutWallet interface {
	PayForOrder(context.Context, int, int, float64, string, ...float64) (int64, bool, error)
	OrderForCheckoutToken(context.Context, int, string) (int64, error)
}

// Checkout is shared by both wallet order routes so neither route can debit a
// buyer without a single-use payment code.
type Checkout struct {
	crops  checkoutCrops
	wallet checkoutWallet
	otp    paymentOTP
}

func NewCheckoutHandler(crops checkoutCrops, wallet checkoutWallet, otp paymentOTP) *Checkout {
	return &Checkout{crops: crops, wallet: wallet, otp: otp}
}

type CheckoutResult struct {
	OrderID       int64
	AlreadyPlaced bool
	CropID        int
	Quantity      float64
}

type paymentVerificationPage struct {
	Crop          *models.Crop
	Quantity      float64
	Total         float64
	Phone         string
	CheckoutToken string
	ActionURL     string
	Error         string
	Success       string
}

// Pay renders the verification step and returns nil while awaiting a code.
// A result is returned only for a committed or previously completed purchase.
func (h *Checkout) Pay(w http.ResponseWriter, r *http.Request, farmer *models.Farmer) (*CheckoutResult, error) {
	if farmer == nil || !farmer.IsBuyer() {
		return nil, errors.New("Only buyer accounts can place orders.")
	}
	if r.Method != http.MethodPost {
		return nil, errors.New("Submit the payment form to continue.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		return nil, errors.New("We couldn't read that payment form. Please try again.")
	}
	cropID, err := strconv.Atoi(r.FormValue("crop_id"))
	if err != nil || cropID <= 0 {
		return nil, errors.New("Choose a valid product before paying.")
	}
	quantity, err := strconv.ParseFloat(r.FormValue("quantity"), 64)
	if err != nil || quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return nil, errors.New("Enter a valid quantity greater than zero.")
	}
	token := strings.TrimSpace(r.FormValue("checkout_token"))
	if token == "" || len(token) > 128 {
		return nil, errors.New("This page is out of date. Refresh the marketplace and try again.")
	}
	// Checking a receipt cannot move money. Allow a completed form to show its
	// original result after its OTP has been consumed, including after sellout.
	existingID, err := h.wallet.OrderForCheckoutToken(r.Context(), farmer.ID, token)
	if err != nil {
		log.Printf("check completed checkout for user %d: %v", farmer.ID, err)
		return nil, errors.New("We couldn't check this purchase. Check your orders and wallet history before trying again.")
	}
	if existingID != 0 {
		return &CheckoutResult{OrderID: existingID, AlreadyPlaced: true, CropID: cropID, Quantity: quantity}, nil
	}
	crop, err := h.crops.GetCrop(cropID)
	if err != nil {
		log.Printf("load checkout product %d: %v", cropID, err)
		return nil, errors.New("We couldn't load this product. Please try again.")
	}
	if crop == nil || !crop.ListedForSale || quantity > crop.Quantity {
		return nil, errors.New("This produce is not available in the requested quantity.")
	}
	if crop.FarmerID == farmer.ID {
		return nil, errors.New("You can't buy your own produce.")
	}
	total := quantity * crop.PricePerUnit
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return nil, errors.New("This product needs a valid price before it can be purchased.")
	}
	page := paymentVerificationPage{
		Crop: crop, Quantity: quantity, Total: total, Phone: farmer.Phone, CheckoutToken: token, ActionURL: r.URL.Path,
	}
	switch r.FormValue("action") {
	case "", "send_payment_code":
		if err := h.otp.Issue(r.Context(), farmer.ID, farmer.Phone, models.OTPPurposePayment); err != nil {
			log.Printf("send payment code for user %d: %v", farmer.ID, err)
			page.Error = otpUserMessage(err)
			status := http.StatusBadGateway
			if errors.Is(err, otp.ErrRateLimited) {
				status = http.StatusTooManyRequests
			}
			h.render(w, status, page)
			return nil, nil
		}
		page.Success = "We've sent a verification code to your phone. It expires in 5 minutes."
		if transport, ok := h.otp.(interface{ DevelopmentDelivery() bool }); ok && transport.DevelopmentDelivery() {
			page.Success = "Development mode: no SMS was sent. Your payment code is in the server log and expires in 5 minutes."
		}
		h.render(w, http.StatusOK, page)
		return nil, nil
	case "verify_payment":
		quotedPrice, err := strconv.ParseFloat(r.FormValue("unit_price"), 64)
		if err != nil || quotedPrice != crop.PricePerUnit {
			page.Error = "The price changed. Review the updated total and request a new code before paying."
			h.render(w, http.StatusConflict, page)
			return nil, nil
		}
		if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposePayment, r.FormValue("code")); err != nil {
			log.Printf("verify payment code for user %d: %v", farmer.ID, err)
			page.Error = otpVerificationMessage(err)
			h.render(w, http.StatusBadRequest, page)
			return nil, nil
		}
	default:
		return nil, errors.New("Choose Send code or Confirm payment to continue.")
	}
	orderID, reused, err := h.wallet.PayForOrder(r.Context(), farmer.ID, cropID, quantity, token, crop.PricePerUnit)
	if err != nil {
		log.Printf("pay for order for user %d: %v", farmer.ID, err)
		return nil, errors.New(userError(err, "We couldn't confirm this purchase. Check your orders and wallet history before trying again."))
	}
	return &CheckoutResult{OrderID: orderID, AlreadyPlaced: reused, CropID: cropID, Quantity: quantity}, nil
}

func (h *Checkout) render(w http.ResponseWriter, status int, page paymentVerificationPage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := render.RenderTemplates(w, "verify-payment.html", page); err != nil {
		log.Printf("render payment verification: %v", err)
	}
}
