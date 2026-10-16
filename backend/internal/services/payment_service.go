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
	repo    *repository.PaymentRepository
	orders  *repository.OrderRepository
	farmers *repository.FarmerRepository
	fw      *FlutterwaveClient
	wallet  *WalletService
}

func NewPaymentService(repo *repository.PaymentRepository, orders *repository.OrderRepository, farmers *repository.FarmerRepository, fw *FlutterwaveClient, wallet *WalletService) *PaymentService {
	return &PaymentService{repo: repo, orders: orders, farmers: farmers, fw: fw, wallet: wallet}
}

// --- Initiate --------------------------------------------------------

type InitiatePaymentInput struct {
	OrderID int64
	BuyerID int
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

// Initiate loads the order and customer from PostgreSQL. Financial and customer
// details are never trusted from the browser. Only eligible unpaid orders can
// be paid through this endpoint; wallet-paid completed orders are rejected.
func (s *PaymentService) Initiate(ctx context.Context, in InitiatePaymentInput) (*InitiatePaymentResult, error) {
	if in.OrderID <= 0 || in.BuyerID <= 0 {
		return nil, errors.New("valid order and buyer are required")
	}
	order, err := s.orders.GetByID(int(in.OrderID))
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	if order == nil {
		return nil, errors.New("order not found")
	}
	if order.BuyerID != in.BuyerID {
		return nil, errors.New("order does not belong to this buyer")
	}
	status := strings.ToLower(strings.TrimSpace(order.Status))
	if status != "pending" && status != "pending_payment" {
		return nil, fmt.Errorf("order is not awaiting external payment")
	}
	if order.TotalPrice <= 0 {
		return nil, errors.New("order total must be greater than zero")
	}
	buyer, err := s.farmers.GetByID(in.BuyerID)
	if err != nil {
		return nil, fmt.Errorf("load buyer: %w", err)
	}
	if buyer == nil || !buyer.IsBuyer() || strings.TrimSpace(buyer.Email) == "" {
		return nil, errors.New("a registered buyer with an email address is required")
	}
	amount := fmt.Sprintf("%.2f", order.TotalPrice)
	reference, err := generateReference(in.OrderID)
	if err != nil {
		return nil, fmt.Errorf("generate reference: %w", err)
	}
	if _, err := s.repo.Create(ctx, in.OrderID, reference, amount, "NGN", "flutterwave"); err != nil {
		return nil, fmt.Errorf("create payment record: %w", err)
	}
	checkoutURL, err := s.fw.InitializePayment(InitializePaymentRequest{
		Reference: reference, Amount: amount, Currency: "NGN",
		CustomerEmail: buyer.Email, CustomerName: buyer.FullName,
		Title: "Agro-Shield Order Payment", Description: fmt.Sprintf("Payment for order %d", in.OrderID),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize payment with Flutterwave: %w", err)
	}
	if strings.TrimSpace(checkoutURL) == "" {
		return nil, errors.New("Flutterwave returned no checkout URL")
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

	// Route on the provider recorded against the funding attempt, not on the
	// reference's shape.
	_, isWalletDeposit, err := s.wallet.IsWalletDeposit(ctx, txRef)
	if err != nil {
		return "", err
	}
	if isWalletDeposit {
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
	if status == models.PaymentSuccessful {
		if err := s.orders.MarkPaid(int(payment.OrderID)); err != nil {
			return "", fmt.Errorf("payment verified but order status update failed; reconcile payment %s: %w", txRef, err)
		}
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

	_, isWalletDeposit, err := s.wallet.IsWalletDeposit(ctx, verified.TxRef)
	if err != nil {
		return false, err
	}
	if isWalletDeposit {
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

	if status == models.PaymentSuccessful {
		if err := s.orders.MarkPaid(int(payment.OrderID)); err != nil {
			return false, fmt.Errorf("payment verified but order status update failed; reconcile payment %s: %w", verified.TxRef, err)
		}
	}

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
