package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// RunPhoneMigration refuses ambiguous legacy ownership before changing any
// contacts. Invalid legacy contacts remain available for explicit correction.
func RunPhoneMigration(db *sql.DB) error {
	return runPhoneMigration(db, false)
}

// RunPhoneMigrationForStartup keeps legacy ownership conflicts available for
// operator review without preventing unrelated accounts from using the app.
// Phone lookups still refuse ambiguous ownership and a temporary database guard
// prevents new collisions until the full unique index can be installed.
func RunPhoneMigrationForStartup(db *sql.DB) error {
	return runPhoneMigration(db, true)
}

func runPhoneMigration(db *sql.DB, allowLegacyDuplicates bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	deferred, err := runPhoneMigrationInTx(context.Background(), tx, allowLegacyDuplicates)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if deferred {
		log.Print("WARNING: legacy duplicate phones detected; startup completed with database-enforced phone reservations. Existing accounts and wallet records preserved; ambiguous phone sign-ins blocked. See docs/ADMIN_GOVERNANCE.md")
	}
	return nil
}

// RunPhoneMigrationInTx lets an explicit duplicate repair and canonicalization
// commit together with their audit events.
func RunPhoneMigrationInTx(ctx context.Context, tx *sql.Tx) error {
	_, err := runPhoneMigrationInTx(ctx, tx, false)
	return err
}

func runPhoneMigrationInTx(ctx context.Context, tx *sql.Tx, allowLegacyDuplicates bool) (bool, error) {
	if _, err := tx.ExecContext(ctx, `
LOCK TABLE farmers IN SHARE ROW EXCLUSIVE MODE;
CREATE OR REPLACE FUNCTION normalized_nigerian_phone(raw TEXT) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE value TEXT;
BEGIN
 value := translate(raw, E' ()-\t\n\r\f' || chr(11), '');
 IF value !~ '^\+?[0-9]+$' THEN RETURN NULL; END IF;
 IF left(value,1)='+' THEN
  IF left(value,4)<>'+234' THEN RETURN NULL; END IF;
  value := substr(value,2);
 END IF;
 IF left(value,2)='00' THEN value := substr(value,3); END IF;
 IF left(value,3)='234' THEN value := substr(value,4); END IF;
 IF left(value,1)='0' THEN value := substr(value,2); END IF;
 IF value !~ '^[789][0-9]{9}$' THEN RETURN NULL; END IF;
 RETURN '234' || value;
END $$;
`); err != nil {
		return false, fmt.Errorf("phone standard migration: %w", err)
	}
	var collisions bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM farmers WHERE deleted_at IS NULL
 AND normalized_nigerian_phone(phone) IS NOT NULL
 GROUP BY normalized_nigerian_phone(phone) HAVING count(*)>1)`).Scan(&collisions); err != nil {
		return false, err
	}
	if collisions {
		if !allowLegacyDuplicates {
			return false, fmt.Errorf("phone standard migration: duplicate phone accounts require explicit cleanup")
		}
		// The table lock above makes detection and guard installation atomic.
		// Existing contacts stay untouched; only new ownership claims are checked.
		if _, err := tx.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_farmers_phone_lookup
 ON farmers(normalized_nigerian_phone(phone)) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS farmer_phone_ownership (
 phone TEXT PRIMARY KEY,
 owner_count BIGINT NOT NULL CHECK (owner_count>=0)
);
INSERT INTO farmer_phone_ownership(phone,owner_count)
 SELECT normalized_nigerian_phone(phone),count(*) FROM farmers
 WHERE deleted_at IS NULL AND normalized_nigerian_phone(phone) IS NOT NULL
 GROUP BY normalized_nigerian_phone(phone)
 ON CONFLICT(phone) DO UPDATE SET owner_count=EXCLUDED.owner_count;
DELETE FROM farmer_phone_ownership p WHERE NOT EXISTS
 (SELECT 1 FROM farmers f WHERE f.deleted_at IS NULL
  AND normalized_nigerian_phone(f.phone)=p.phone);
CREATE OR REPLACE FUNCTION guard_legacy_phone_ownership() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE old_phone TEXT; new_phone TEXT; remaining BIGINT;
BEGIN
 IF TG_OP<>'INSERT' THEN
  IF OLD.deleted_at IS NULL THEN old_phone := normalized_nigerian_phone(OLD.phone); END IF;
 END IF;
 IF TG_OP<>'DELETE' THEN
  IF NEW.deleted_at IS NULL THEN new_phone := normalized_nigerian_phone(NEW.phone); END IF;
 END IF;
 IF old_phone IS DISTINCT FROM new_phone THEN
  IF old_phone IS NOT NULL THEN
   UPDATE farmer_phone_ownership SET owner_count=owner_count-1
    WHERE phone=old_phone RETURNING owner_count INTO remaining;
   IF NOT FOUND THEN RAISE EXCEPTION 'phone ownership reservation missing'; END IF;
   IF remaining=0 THEN DELETE FROM farmer_phone_ownership WHERE phone=old_phone; END IF;
  END IF;
  IF new_phone IS NOT NULL THEN
   -- A real primary key serializes claims at every transaction isolation level.
   -- A failed claim rolls back the release of the old phone as well.
   INSERT INTO farmer_phone_ownership(phone,owner_count) VALUES(new_phone,1);
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER farmers_legacy_phone_ownership
 BEFORE INSERT OR UPDATE OF phone,deleted_at OR DELETE ON farmers
 FOR EACH ROW EXECUTE FUNCTION guard_legacy_phone_ownership();
`); err != nil {
			return false, fmt.Errorf("phone ownership guard migration: %w", err)
		}
		return true, nil
	}
	if _, err := tx.ExecContext(ctx, `
WITH changed AS (
 UPDATE farmers SET phone=normalized_nigerian_phone(phone), updated_at=CURRENT_TIMESTAMP
 WHERE deleted_at IS NULL AND normalized_nigerian_phone(phone) IS NOT NULL
 AND phone<>normalized_nigerian_phone(phone) RETURNING id
), retired AS (
 UPDATE otp_codes SET consumed_at=CURRENT_TIMESTAMP
 WHERE farmer_id IN (SELECT id FROM changed) AND consumed_at IS NULL
)
INSERT INTO admin_audit_events(action,resource,target_id,outcome,reason,before_state,after_state)
SELECT 'system.phone_standardization','farmers',0,'committed','canonical_phone_format',
 count(*)::text || ' formatted contacts','canonical international digits'
FROM changed HAVING count(*)>0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_farmers_normalized_phone
 ON farmers(normalized_nigerian_phone(phone)) WHERE deleted_at IS NULL;
DROP TRIGGER IF EXISTS farmers_legacy_phone_ownership ON farmers;
DROP FUNCTION IF EXISTS guard_legacy_phone_ownership();
DROP TABLE IF EXISTS farmer_phone_ownership;
DROP INDEX IF EXISTS idx_farmers_phone_lookup;
`); err != nil {
		return false, fmt.Errorf("phone standard migration: %w", err)
	}
	return false, nil
}
