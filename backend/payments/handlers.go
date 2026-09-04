package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type PaymentHandlers struct {
	DB *sql.DB
	FW *FlutterwaveClient
}

// generateReference makes a unique tx_ref for this payment attempt.
// Prefixing with the order ID makes references easy to eyeball in logs.
func generateReference(orderID int64) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("ORDER-%d-%s", orderID, hex.EncodeToString(buf)), nil
}

// --- POST /orders/{order_id}/pay ----------------------------------------

type initiatePaymentRequest struct {
	OrderID       int64  `json:"order_id"`
	Amount        string `json:"amount"`   // e.g. "5000.00" - keep as string, never float, for money
	Currency      string `json:"currency"` // e.g. "NGN"
	CustomerEmail string `json:"customer_email"`
	CustomerName  string `json:"customer_name"`
}

type initiatePaymentResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Reference   string `json:"reference"`
}

// InitiatePayment creates a payment record, asks Flutterwave for a
// checkout link, and hands that link back to the frontend.
//
// NOTE: in a real flow you'd look up the order (amount, currency, customer)
// from your orders table using order_id, rather than trusting these values
// from the request body - otherwise a client could initiate a payment for
// less than the order actually costs. Wire that lookup in once your orders
// package exists; the shape below assumes it's already been validated.
func (h *PaymentHandlers) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	var req initiatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	reference, err := generateReference(req.OrderID)
	if err != nil {
		log.Printf("generate reference: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	_, err = CreatePayment(ctx, h.DB, req.OrderID, reference, req.Amount, req.Currency, "flutterwave")
	if err != nil {
		log.Printf("create payment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	checkoutURL, err := h.FW.InitializePayment(InitializePaymentRequest{
		Reference:     reference,
		Amount:        req.Amount,
		Currency:      req.Currency,
		CustomerEmail: req.CustomerEmail,
		CustomerName:  req.CustomerName,
		Title:         "Order Payment",
		Description:   fmt.Sprintf("Payment for order %d", req.OrderID),
	})
	if err != nil {
		log.Printf("initialize payment with flutterwave: %v", err)
		// The payment row stays "pending" - safe to retry initiation later
		// with a fresh reference, or investigate why Flutterwave rejected it.
		http.Error(w, "could not initialize payment", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, initiatePaymentResponse{
		CheckoutURL: checkoutURL,
		Reference:   reference,
	})
}

// --- GET /payments/callback ----------------------------------------------

// Callback handles the customer being redirected back from Flutterwave's
// hosted checkout page. This is a UX convenience ONLY - it tells the
// customer's browser what happened. It must NEVER be the thing that marks
// an order paid, because redirect query params can be faked by anyone who
// knows the URL shape. We re-verify server-side here regardless, but the
// webhook handler below is the actual source of truth.
func (h *PaymentHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	transactionID := r.URL.Query().Get("transaction_id")
	txRef := r.URL.Query().Get("tx_ref")

	if transactionID == "" || txRef == "" {
		http.Error(w, "missing transaction_id or tx_ref", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	verified, err := h.FW.VerifyTransaction(transactionID)
	if err != nil {
		log.Printf("verify transaction %s: %v", transactionID, err)
		http.Error(w, "could not verify transaction", http.StatusBadGateway)
		return
	}

	if verified.TxRef != txRef {
		// The tx_ref in the URL doesn't match what Flutterwave has on file
		// for this transaction ID - treat as suspicious, don't update anything.
		http.Error(w, "reference mismatch", http.StatusBadRequest)
		return
	}

	status := PaymentFailed
	if verified.Status == "successful" {
		status = PaymentSuccessful
	}

	if err := UpdatePaymentStatus(ctx, h.DB, txRef, status, transactionID); err != nil {
		log.Printf("update payment status: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    status,
		"reference": txRef,
	})
}

// --- POST /webhooks/flutterwave -------------------------------------------

type flutterwaveWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ID     int64  `json:"id"`
		TxRef  string `json:"tx_ref"`
		Status string `json:"status"`
	} `json:"data"`
}

// Webhook is the actual source of truth for payment completion.
// It: (1) verifies the signature, (2) logs the delivery for idempotency
// and auditing, (3) re-verifies the transaction directly with Flutterwave
// (never trust the webhook body's claimed status alone - always confirm
// with a second API call), (4) updates our records.
func (h *PaymentHandlers) Webhook(w http.ResponseWriter, r *http.Request) {
	receivedHash := r.Header.Get("verif-hash")
	if !h.FW.VerifyWebhookSignature(receivedHash) {
		// Do not reveal WHY it failed - just reject.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	var payload flutterwaveWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	transactionID := fmt.Sprintf("%d", payload.Data.ID)
	ctx := r.Context()

	// Log the delivery before doing anything else. If we crash after this
	// point, the row still exists for us to investigate or replay.
	if err := LogWebhookEvent(ctx, h.DB, transactionID, payload.Event, body); err != nil {
		log.Printf("log webhook event: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	alreadyProcessed, err := HasWebhookBeenProcessed(ctx, h.DB, transactionID, payload.Event)
	if err != nil {
		log.Printf("check webhook processed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if alreadyProcessed {
		// Flutterwave retried a delivery we've already handled. Acknowledge
		// with 200 so it stops retrying, but don't touch the payment again.
		w.WriteHeader(http.StatusOK)
		return
	}

	// Re-verify with Flutterwave directly rather than trusting payload.Data.Status.
	verified, err := h.FW.VerifyTransaction(transactionID)
	if err != nil {
		log.Printf("verify transaction %s: %v", transactionID, err)
		// Return 500 so Flutterwave retries - we want another shot at this
		// once whatever's failing (network blip, etc) clears up.
		http.Error(w, "could not verify transaction", http.StatusInternalServerError)
		return
	}

	status := PaymentFailed
	if verified.Status == "successful" {
		status = PaymentSuccessful
	}

	if err := UpdatePaymentStatus(ctx, h.DB, verified.TxRef, status, transactionID); err != nil {
		log.Printf("update payment status: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// TODO: once your orders package exists, also transition the order
	// itself (e.g. orders.MarkPaid(ctx, db, orderID)) and trigger fulfillment
	// here - this is the one place in the whole system safe to do that from.

	if err := MarkWebhookProcessed(ctx, h.DB, transactionID, payload.Event); err != nil {
		log.Printf("mark webhook processed: %v", err)
		// Not fatal to the response - the payment update already succeeded.
		// Worst case, a retried webhook re-verifies and re-updates to the
		// same status, which is harmless.
	}

	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
