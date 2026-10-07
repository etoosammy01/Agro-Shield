package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
)

// ============================================================
// PAYMENT SERVICE
//
// Coordinates between the payment repository (PostgreSQL) and
// the Flutterwave client (external API). Handlers only ever
// talk to this service - never to the repository or Flutterwave
// client directly.
// ============================================================

type PaymentService struct {
	repo   *repository.PaymentRepository
	fw     *FlutterwaveClient
	wallet *WalletService
}

func NewPaymentService(repo *repository.PaymentRepository, fw *FlutterwaveClient, wallet *WalletService) *PaymentService {
	return &PaymentService{repo: repo, fw: fw, wallet: wallet}
}

// --- Initiate --------------------------------------------------------

type InitiatePaymentInput struct {
	OrderID       int64
	Amount        string // e.g. "5000.00" - keep as string, never float, for money
	Currency      string // e.g. "NGN"
	CustomerEmail string
	CustomerName  string
}

type InitiatePaymentResult struct {
	CheckoutURL string
	Reference   string
}

// generateReference makes a unique tx_ref for this payment attempt.
func generateReference(orderID int64) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("ORDER-%d-%s", orderID, hex.EncodeToString(buf)), nil
}

// Initiate creates a payment record, asks Flutterwave for a checkout link,
// and returns it for the handler to send back to the frontend.
//
// NOTE: in a real flow you'd look up the order (amount, currency, customer)
// from your orders repository using OrderID, rather than trusting these
// values from the caller - otherwise a client could initiate a payment for
// less than the order actually costs.
func (s *PaymentService) Initiate(ctx context.Context, in InitiatePaymentInput) (*InitiatePaymentResult, error) {
	reference, err := generateReference(in.OrderID)
	if err != nil {
		return nil, fmt.Errorf("generate reference: %w", err)
	}

	if _, err := s.repo.Create(ctx, in.OrderID, reference, in.Amount, in.Currency, "flutterwave"); err != nil {
		return nil, fmt.Errorf("create payment record: %w", err)
	}

	checkoutURL, err := s.fw.InitializePayment(InitializePaymentRequest{
		Reference:     reference,
		Amount:        in.Amount,
		Currency:      in.Currency,
		CustomerEmail: in.CustomerEmail,
		CustomerName:  in.CustomerName,
		Title:         "Order Payment",
		Description:   fmt.Sprintf("Payment for order %d", in.OrderID),
	})
	if err != nil {
		// The payment row stays "pending" - safe to retry initiation later
		// with a fresh reference, or investigate why Flutterwave rejected it.
		return nil, fmt.Errorf("initialize payment with flutterwave: %w", err)
	}

	return &InitiatePaymentResult{CheckoutURL: checkoutURL, Reference: reference}, nil
}

// --- Callback ----------------------------------------------------------

// ErrReferenceMismatch means the tx_ref in the redirect didn't match what
// Flutterwave has on file for that transaction ID - treat as suspicious.
var ErrReferenceMismatch = fmt.Errorf("reference mismatch")

func verifiedPaymentStatus(providerStatus string) string {
	switch strings.ToLower(strings.TrimSpace(providerStatus)) {
	case "successful":
		return models.PaymentSuccessful
	case "failed", "cancelled", "canceled":
		return models.PaymentFailed
	default:
		return models.PaymentPending
	}
}

// ConfirmCallback re-verifies a transaction directly with Flutterwave
// (never trust redirect query params alone - they can be faked) and
// updates our payment record accordingly. Returns the resulting status.
func (s *PaymentService) ConfirmCallback(ctx context.Context, transactionID, txRef string) (string, error) {
	verified, err := s.fw.VerifyTransaction(transactionID)
	if err != nil {
		return "", fmt.Errorf("verify transaction: %w", err)
	}

	if verified.TxRef != txRef {
		return "", ErrReferenceMismatch
	}

	if strings.HasPrefix(txRef, "WALLET-") {
		status := verifiedPaymentStatus(verified.Status)
		if err := s.wallet.ConfirmDeposit(ctx, txRef, verified.TransactionID, verified.Amount, verified.Currency, verified.Status); err != nil {
			return "", fmt.Errorf("confirm wallet deposit: %w", err)
		}
		return status, nil
	}

	payment, err := s.repo.GetByReference(ctx, txRef)
	if err != nil {
		return "", fmt.Errorf("find payment: %w", err)
	}
	if !sameMoney(payment.Amount, verified.Amount) || !strings.EqualFold(payment.Currency, verified.Currency) {
		return "", errors.New("verified payment amount or currency does not match")
	}

	status := verifiedPaymentStatus(verified.Status)

	if err := s.repo.UpdateStatus(ctx, txRef, status, transactionID); err != nil {
		return "", fmt.Errorf("update payment status: %w", err)
	}

	return status, nil
}

// --- Webhook -------------------------------------------------------------

// ErrUnauthorizedWebhook means the verif-hash header didn't match.
var ErrUnauthorizedWebhook = fmt.Errorf("unauthorized webhook")

// ProcessWebhook is the actual source of truth for payment completion.
// It: (1) verifies the signature, (2) logs the delivery for idempotency
// and auditing, (3) re-verifies the transaction directly with Flutterwave
// (never trust the webhook body's claimed status alone), (4) updates our
// records. Returns (alreadyProcessed, error) so the handler knows whether
// any real work happened.
func (s *PaymentService) ProcessWebhook(ctx context.Context, body []byte, receivedHash string) (alreadyProcessed bool, err error) {
	if !s.fw.VerifyWebhookSignature(receivedHash) {
		return false, ErrUnauthorizedWebhook
	}

	var payload struct {
		Event string `json:"event"`
		Data  struct {
			ID        int64  `json:"id"`
			TxRef     string `json:"tx_ref"`
			Reference string `json:"reference"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(payload.Event) == "" || payload.Data.ID <= 0 {
		return false, errors.New("webhook is missing a valid event or transaction ID")
	}

	transactionID := fmt.Sprintf("%d", payload.Data.ID)

	// Log the delivery before doing anything else. If we crash after this
	// point, the row still exists for us to investigate or replay.
	if err := s.repo.LogWebhookEvent(ctx, transactionID, payload.Event, body); err != nil {
		return false, fmt.Errorf("log webhook event: %w", err)
	}

	processed, err := s.repo.HasWebhookBeenProcessed(ctx, transactionID, payload.Event)
	if err != nil {
		return false, fmt.Errorf("check webhook processed: %w", err)
	}
	if processed {
		// A retried delivery of one we've already handled - nothing to do.
		return true, nil
	}

	if strings.HasPrefix(strings.ToLower(payload.Event), "transfer.") {
		terminal, err := s.wallet.ProcessTransferWebhook(ctx, body, receivedHash)
		if err != nil {
			return false, fmt.Errorf("process wallet transfer webhook: %w", err)
		}
		if !terminal {
			return false, nil
		}
		if err := s.repo.MarkWebhookProcessed(ctx, transactionID, payload.Event); err != nil {
			return false, fmt.Errorf("mark transfer webhook processed: %w", err)
		}
		return false, nil
	}

	// Re-verify with Flutterwave directly rather than trusting payload.Data.Status.
	verified, err := s.fw.VerifyTransaction(transactionID)
	if err != nil {
		return false, fmt.Errorf("verify transaction: %w", err)
	}

	if strings.HasPrefix(verified.TxRef, "WALLET-") {
		if payload.Data.TxRef != verified.TxRef {
			return false, ErrReferenceMismatch
		}
		if err := s.wallet.ConfirmDeposit(ctx, verified.TxRef, verified.TransactionID, verified.Amount, verified.Currency, verified.Status); err != nil {
			return false, fmt.Errorf("confirm wallet deposit: %w", err)
		}
		if verifiedPaymentStatus(verified.Status) == models.PaymentPending {
			return false, nil
		}
		if err := s.repo.MarkWebhookProcessed(ctx, transactionID, payload.Event); err != nil {
			return false, fmt.Errorf("mark wallet deposit webhook processed: %w", err)
		}
		return false, nil
	}

	payment, err := s.repo.GetByReference(ctx, verified.TxRef)
	if err != nil {
		return false, fmt.Errorf("find payment: %w", err)
	}
	if payload.Data.TxRef != verified.TxRef || !sameMoney(payment.Amount, verified.Amount) || !strings.EqualFold(payment.Currency, verified.Currency) {
		return false, errors.New("verified payment details do not match the recorded payment")
	}

	status := verifiedPaymentStatus(verified.Status)

	if err := s.repo.UpdateStatus(ctx, verified.TxRef, status, transactionID); err != nil {
		return false, fmt.Errorf("update payment status: %w", err)
	}
	if status == models.PaymentPending {
		return false, nil
	}

	// TODO: once order-status transitions are wired up, also mark the
	// order itself paid here and trigger fulfillment - this is the one
	// place in the system safe to do that from.

	if err := s.repo.MarkWebhookProcessed(ctx, transactionID, payload.Event); err != nil {
		// Not fatal - the payment update already succeeded. Worst case, a
		// retried webhook re-verifies and re-updates to the same status.
		return false, fmt.Errorf("mark webhook processed (non-fatal): %w", err)
	}

	return false, nil
}

func sameMoney(expected string, verified float64) bool {
	expectedAmount, err := strconv.ParseFloat(expected, 64)
	return err == nil && strconv.FormatFloat(expectedAmount, 'f', 2, 64) == strconv.FormatFloat(verified, 'f', 2, 64)
}
