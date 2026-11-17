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

-- New live sessions have an unambiguous expiry and the revision authenticated
-- at login. Historical naive timestamps are retained, without guessing their
-- timezone; old tokens without the new expiry require a fresh sign-in.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS expires_at_utc TIMESTAMPTZ;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS authentication_revision TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_sessions_expiry_utc ON sessions(expires_at_utc);

-- Invalidate an open review when any displayed listing detail changes, including
-- seller edits and stock changes made by checkout. Timestamps alone do not make
-- a review stale. Keep this in the database so every write path participates.
CREATE OR REPLACE FUNCTION advance_listing_review_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(OLD.farmer_id, OLD.name, OLD.location, OLD.quantity, OLD.unit,
        OLD.price_per_unit, OLD.listed_for_sale, OLD.moderation_blocked)
    IS DISTINCT FROM
    ROW(NEW.farmer_id, NEW.name, NEW.location, NEW.quantity, NEW.unit,
        NEW.price_per_unit, NEW.listed_for_sale, NEW.moderation_blocked) THEN
  NEW.moderation_version := OLD.moderation_version + 1;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER listing_review_version BEFORE UPDATE ON crops
 FOR EACH ROW EXECUTE FUNCTION advance_listing_review_version();

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

-- Member 5 retains the dispute event model and business transitions. Mirror
-- committed events into the governance log in the SAME transaction, without
-- copying complaint text, evidence keys, or other private free-form material.
CREATE OR REPLACE TRIGGER dispute_history_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON order_dispute_events
 FOR EACH STATEMENT EXECUTE FUNCTION reject_admin_audit_mutation();

CREATE OR REPLACE FUNCTION audit_dispute_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE audit_action TEXT;
BEGIN
 IF NEW.actor_role NOT IN ('admin', 'wallet') THEN
  RETURN NEW;
 END IF;
 IF NEW.actor_role = 'admin' AND NEW.actor_id IS NULL THEN
  RAISE EXCEPTION 'administrative dispute events require an actor';
 END IF;
 audit_action := CASE NEW.event_type
  WHEN 'admin_review_started' THEN 'dispute.review'
  WHEN 'dispute_rejected' THEN 'dispute.reject'
  WHEN 'refund_approved' THEN 'dispute.refund_approve'
  ELSE 'dispute.' || NEW.event_type
 END;
 INSERT INTO admin_audit_events(actor_id,action,resource,target_id,outcome,
  reason,before_state,after_state)
 VALUES (NEW.actor_id,audit_action,'order_disputes',NEW.dispute_id,'committed',
  NEW.event_type,COALESCE(NEW.from_status,''),COALESCE(NEW.to_status,''));
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER audit_dispute_history AFTER INSERT ON order_dispute_events
 FOR EACH ROW EXECUTE FUNCTION audit_dispute_event();

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
