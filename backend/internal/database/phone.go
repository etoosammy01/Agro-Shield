package database

import (
	"context"
	"database/sql"
	"fmt"
)

// RunPhoneMigration refuses ambiguous legacy ownership before changing any
// contacts. Invalid legacy contacts remain available for explicit correction.
func RunPhoneMigration(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := RunPhoneMigrationInTx(context.Background(), tx); err != nil {
		return err
	}
	return tx.Commit()
}

// RunPhoneMigrationInTx lets an explicit duplicate repair and canonicalization
// commit together with their audit events.
func RunPhoneMigrationInTx(ctx context.Context, tx *sql.Tx) error {
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
		return fmt.Errorf("phone standard migration: %w", err)
	}
	var collisions bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM farmers WHERE deleted_at IS NULL
 AND normalized_nigerian_phone(phone) IS NOT NULL
 GROUP BY normalized_nigerian_phone(phone) HAVING count(*)>1)`).Scan(&collisions); err != nil {
		return err
	}
	if collisions {
		return fmt.Errorf("phone standard migration: duplicate phone accounts require explicit cleanup")
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
`); err != nil {
		return fmt.Errorf("phone standard migration: %w", err)
	}
	return nil
}
