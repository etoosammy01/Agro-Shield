package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
)

// ============================================================
// PAYMENT HANDLER
// ============================================================

type PaymentHandler struct {
	payment paymentProcessor
	otp     paymentOTP
}

type paymentProcessor interface {
	Preflight(context.Context, services.InitiatePaymentInput) error
	Initiate(context.Context, services.InitiatePaymentInput) (*services.InitiatePaymentResult, error)
	ConfirmCallback(context.Context, string, string) (string, error)
	ProcessWebhook(context.Context, []byte, string) (bool, error)
}

func NewPaymentHandler(payment paymentProcessor, otp paymentOTP) *PaymentHandler {
	return &PaymentHandler{payment: payment, otp: otp}
}

// --- POST /orders/pay ----------------------------------------------------

type initiatePaymentRequest struct {
	OrderID int64  `json:"order_id"`
	Code    string `json:"code"`
}

func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil || !farmer.IsBuyer() {
		http.Error(w, "Only authenticated buyers can pay for orders", http.StatusForbidden)
		return
	}
	var req initiatePaymentRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID <= 0 {
		http.Error(w, "valid order_id and verification code are required", http.StatusBadRequest)
		return
	}
	input := services.InitiatePaymentInput{
		OrderID: req.OrderID,
		BuyerID: farmer.ID,
	}
	if err := h.payment.Preflight(r.Context(), input); err != nil {
		log.Printf("preflight order payment for user %d: %v", farmer.ID, err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": userError(err, "We couldn't check this order. Please try again.")})
		return
	}
	if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposePayment, req.Code); err != nil {
		log.Printf("verify order payment code for user %d: %v", farmer.ID, err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": otpVerificationMessage(err)})
		return
	}
	result, err := h.payment.Initiate(r.Context(), input)
	if err != nil {
		log.Printf("initiate payment: %v", err)
		http.Error(w, "Payment service is temporarily unavailable. No payment status was confirmed; check your payment status before retrying.", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"checkout_url": result.CheckoutURL,
		"reference":    result.Reference,
	})
}

// --- GET /payments/callback ----------------------------------------------

// Callback handles the customer being redirected back from Flutterwave's
// hosted checkout page. This is a UX convenience ONLY - the webhook below
// is the real source of truth for marking a payment complete.
func (h *PaymentHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	transactionID := r.URL.Query().Get("transaction_id")
	txRef := r.URL.Query().Get("tx_ref")

	if transactionID == "" || txRef == "" {
		http.Error(w, "missing transaction_id or tx_ref", http.StatusBadRequest)
		return
	}

	status, err := h.payment.ConfirmCallback(r.Context(), transactionID, txRef)
	if err != nil {
		if errors.Is(err, services.ErrReferenceMismatch) {
			http.Error(w, "reference mismatch", http.StatusBadRequest)
			return
		}
		log.Printf("confirm callback: %v", err)
		http.Error(w, "could not verify transaction", http.StatusBadGateway)
		return
	}

	if strings.HasPrefix(txRef, "WALLET-") {
		switch status {
		case models.PaymentFailed:
			http.Redirect(w, r, "/wallet?error="+url.QueryEscape("The provider reports this deposit failed, so it was not credited to your wallet. If your bank was charged, the bank or payment provider controls the reversal timing."), http.StatusSeeOther)
		case models.PaymentSuccessful:
			http.Redirect(w, r, "/wallet?success="+url.QueryEscape("Deposit verified and credited to your wallet."), http.StatusSeeOther)
		default:
			http.Redirect(w, r, "/wallet?success="+url.QueryEscape("Deposit is still being verified. Check deposit history and do not retry while it is pending."), http.StatusSeeOther)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    status,
		"reference": txRef,
	})
}

// --- POST /webhooks/flutterwave -------------------------------------------

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	receivedHash := r.Header.Get("verif-hash")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	_, err = h.payment.ProcessWebhook(r.Context(), body, receivedHash)
	if err != nil {
		if errors.Is(err, services.ErrUnauthorizedWebhook) {
			// Record that a delivery was rejected, but not why, and never
			// either hash: the header on this request is attacker-controlled,
			// and naming the expected value would hand over the secret.
			// Whether the header arrived at all is the one fact worth keeping,
			// and it is what separates the two causes - an absent header means
			// Flutterwave isn't sending one, a present one means what the
			// dashboard holds isn't what we hold.
			hashState := "missing"
			if receivedHash != "" {
				hashState = "present"
			}
			log.Printf("webhook rejected: verif-hash %s, from %s", hashState, r.RemoteAddr)

			// Do not reveal WHY it failed - just reject.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("process webhook: %v", err)
		// Return 500 so Flutterwave retries - we want another shot at this
		// once whatever's failing (network blip, etc) clears up.
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
