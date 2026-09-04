-- Run this after your existing payments table migration.

CREATE INDEX IF NOT EXISTS idx_payments_order_id ON payments(order_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_transaction_id ON payments(transaction_id);

-- Every webhook delivery gets logged here BEFORE we act on it.
-- (transaction_id, event) gives us idempotency: if Flutterwave retries
-- a delivery -- which it does on anything other than a fast 200 -- we
-- recognize the duplicate and skip reprocessing it.
CREATE TABLE IF NOT EXISTS webhook_events (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id VARCHAR(100) NOT NULL,
    event          VARCHAR(50) NOT NULL,
    payload        JSONB NOT NULL,
    processed_at   TIMESTAMP,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_webhook_event UNIQUE (transaction_id, event)
);