package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/models"
)

var ErrPaymentNotFound = errors.New("payment not found")

// ============================================================
// PAYMENT REPOSITORY
//
// Handles payment and webhook-delivery database operations.
// ============================================================

type PaymentRepository struct {
	db *sql.DB
}

func NewPaymentRepository(db *sql.DB) *PaymentRepository {
	return &PaymentRepository{db: db}
}

// Create inserts a new payment row in "pending" status, before we even
// call the payment provider. This way, even if that call fails or times
// out, we have a record that a payment attempt was started.
func (r *PaymentRepository) Create(ctx context.Context, orderID int64, reference, amount, currency, provider string) (*models.Payment, error) {
	const q = `
		INSERT INTO payments (order_id, reference, amount, currency, status, provider)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, order_id, reference, transaction_id, amount, currency, status, provider, created_at, updated_at`

	var p models.Payment
	err := r.db.QueryRowContext(ctx, q, orderID, reference, amount, currency, models.PaymentPending, provider).Scan(
		&p.ID, &p.OrderID, &p.Reference, &p.TransactionID, &p.Amount, &p.Currency, &p.Status, &p.Provider, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PaymentRepository) GetByReference(ctx context.Context, reference string) (*models.Payment, error) {
	const q = `
		SELECT id, order_id, reference, transaction_id, amount, currency, status, provider, created_at, updated_at
		FROM payments WHERE reference = $1`

	var p models.Payment
	err := r.db.QueryRowContext(ctx, q, reference).Scan(
		&p.ID, &p.OrderID, &p.Reference, &p.TransactionID, &p.Amount, &p.Currency, &p.Status, &p.Provider, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateStatus moves a payment to a new status and records the provider's
// transaction ID.
func (r *PaymentRepository) UpdateStatus(ctx context.Context, reference, status, transactionID string) error {
	const q = `
		UPDATE payments
		SET status = $1, transaction_id = $2, updated_at = $3
		WHERE reference = $4`

	result, err := r.db.ExecContext(ctx, q, status, sql.NullString{String: transactionID, Valid: transactionID != ""}, time.Now(), reference)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrPaymentNotFound
	}
	return nil
}

// --- Webhook idempotency -------------------------------------------------

// HasWebhookBeenProcessed checks whether we've already fully handled this
// exact (transaction_id, event) pair. Call this BEFORE acting on a webhook.
func (r *PaymentRepository) HasWebhookBeenProcessed(ctx context.Context, transactionID, event string) (bool, error) {
	const q = `SELECT processed_at IS NOT NULL FROM webhook_events WHERE transaction_id = $1 AND event = $2`

	var processed bool
	err := r.db.QueryRowContext(ctx, q, transactionID, event).Scan(&processed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // never seen this event before
	}
	if err != nil {
		return false, err
	}
	return processed, nil
}

// LogWebhookEvent records that we received this webhook, before we act on
// it. Uses ON CONFLICT DO NOTHING because the provider may send the exact
// same delivery more than once (network retries) - we only need one row.
func (r *PaymentRepository) LogWebhookEvent(ctx context.Context, transactionID, event string, payload []byte) error {
	const q = `
		INSERT INTO webhook_events (transaction_id, event, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (transaction_id, event) DO NOTHING`

	_, err := r.db.ExecContext(ctx, q, transactionID, event, json.RawMessage(payload))
	return err
}

// MarkWebhookProcessed flags an event as fully handled, so a retried
// delivery of the same event short-circuits instead of reprocessing.
func (r *PaymentRepository) MarkWebhookProcessed(ctx context.Context, transactionID, event string) error {
	const q = `UPDATE webhook_events SET processed_at = $1 WHERE transaction_id = $2 AND event = $3`
	_, err := r.db.ExecContext(ctx, q, time.Now(), transactionID, event)
	return err
}
