package database

import (
	"database/sql"
	"fmt"
)

// RunAdminMigration is part of the existing ordered startup migration runner.
// New timestamps use TIMESTAMPTZ; no historical timestamps are reinterpreted.
func RunAdminMigration(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
ALTER TABLE farmers ADD COLUMN IF NOT EXISTS admin_role TEXT NOT NULL DEFAULT '' CHECK (admin_role IN ('', 'moderator', 'administrator'));
ALTER TABLE farmers ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;
ALTER TABLE farmers ADD COLUMN IF NOT EXISTS sessions_revoked_at TIMESTAMPTZ;
ALTER TABLE farmers ADD COLUMN IF NOT EXISTS admin_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE crops ADD COLUMN IF NOT EXISTS moderation_blocked BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE crops ADD COLUMN IF NOT EXISTS moderation_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS admin_audit_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id INTEGER,
 action TEXT NOT NULL,
 resource TEXT NOT NULL,
 target_id BIGINT NOT NULL,
 outcome TEXT NOT NULL CHECK (outcome IN ('committed','attempted','returned','failed')),
 reason TEXT NOT NULL DEFAULT '',
 before_state TEXT NOT NULL DEFAULT '',
 after_state TEXT NOT NULL DEFAULT '',
 request_id TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS idx_admin_audit_target ON admin_audit_events(resource, target_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_admin_audit_actor ON admin_audit_events(actor_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_audit_request_stage
 ON admin_audit_events(request_id, (outcome='attempted')) WHERE request_id <> '';

CREATE OR REPLACE FUNCTION reject_admin_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'admin audit records are append-only';
END $$;
CREATE OR REPLACE TRIGGER admin_audit_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON admin_audit_events
 FOR EACH STATEMENT EXECUTE FUNCTION reject_admin_audit_mutation();

-- Record actual financial transitions inside the existing financial transaction.
-- Actor is attributed only for the decision transition, never guessed for a
-- provider settlement or automated expiry. HTTP attempts record their own actor.
CREATE OR REPLACE FUNCTION audit_wallet_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE decision_actor INTEGER;
BEGIN
 IF OLD.status IS DISTINCT FROM NEW.status THEN
  IF NEW.status IN ('processing', 'rejected', 'completed') THEN
   decision_actor := NEW.approved_by;
  END IF;
  INSERT INTO admin_audit_events(actor_id, action, resource, target_id, outcome, before_state, after_state)
  VALUES (decision_actor, 'wallet.status', TG_TABLE_NAME, NEW.id, 'committed', OLD.status, NEW.status);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER audit_withdrawal_status AFTER UPDATE ON wallet_withdrawals
 FOR EACH ROW EXECUTE FUNCTION audit_wallet_decision();
CREATE OR REPLACE TRIGGER audit_transfer_status AFTER UPDATE ON wallet_transfers
 FOR EACH ROW EXECUTE FUNCTION audit_wallet_decision();
`)
	if err != nil {
		return fmt.Errorf("admin governance migration: %w", err)
	}
	return tx.Commit()
}
