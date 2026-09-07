package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"backend/internal/services"
)

// ============================================================
// PAYMENT HANDLER
// ============================================================

type PaymentHandler struct {
	payment *services.PaymentService
}

func NewPaymentHandler(payment *services.PaymentService) *PaymentHandler {
	return &PaymentHandler{payment: payment}
}

// --- POST /orders/pay ----------------------------------------------------

type initiatePaymentRequest struct {
	OrderID       int64  `json:"order_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	CustomerEmail string `json:"customer_email"`
	CustomerName  string `json:"customer_name"`
}

func (h *PaymentHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	var req initiatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.payment.Initiate(r.Context(), services.InitiatePaymentInput{
		OrderID:       req.OrderID,
		Amount:        req.Amount,
		Currency:      req.Currency,
		CustomerEmail: req.CustomerEmail,
		CustomerName:  req.CustomerName,
	})
	if err != nil {
		log.Printf("initiate payment: %v", err)
		http.Error(w, "could not initiate payment", http.StatusBadGateway)
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

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    status,
		"reference": txRef,
	})
}

// --- POST /webhooks/flutterwave -------------------------------------------

func (h *PaymentHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	receivedHash := r.Header.Get("verif-hash")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	_, err = h.payment.ProcessWebhook(r.Context(), body, receivedHash)
	if err != nil {
		if errors.Is(err, services.ErrUnauthorizedWebhook) {
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