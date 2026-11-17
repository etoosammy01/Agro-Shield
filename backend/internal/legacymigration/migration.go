// Package legacymigration imports the observed normalized users/products schema
// into an empty current application schema. It does not run payment providers.
package legacymigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const Version = "normalized-users-products-v1"

type Options struct {
	Source, Target      string
	ExpectedFingerprint string
}

type Issue struct {
	Check string `json:"check"`
	Rows  int64  `json:"rows"`
}

type Table struct {
	Source string `json:"source"`
	Target string `json:"target,omitempty"`
	Rows   int64  `json:"rows"`
	Digest string `json:"digest"`
}

type Report struct {
	Version            string           `json:"version"`
	Source             string           `json:"source_schema"`
	Target             string           `json:"target_schema"`
	Fingerprint        string           `json:"source_fingerprint"`
	Tables             []Table          `json:"tables"`
	Issues             []Issue          `json:"issues"`
	WalletBalanceTotal string           `json:"wallet_balance_total"`
	WalletNumbers      map[string]int64 `json:"wallet_number_mapping,omitempty"`
	Copied             map[string]int64 `json:"copied,omitempty"`
	Checks             map[string]bool  `json:"verification,omitempty"`
	AlreadyApplied     bool             `json:"already_applied"`
	Committed          bool             `json:"committed"`
}

var ErrUnsafeMigration = errors.New("migration preflight did not pass")

func names(o Options) error {
	if o.Source == "" || o.Target == "" || o.Source == o.Target || strings.HasPrefix(o.Source, "pg_") || strings.HasPrefix(o.Target, "pg_") {
		return fmt.Errorf("distinct, explicit source and target schemas are required")
	}
	return nil
}

func sourceSQL(query string, o Options) string {
	query = strings.ReplaceAll(query, "{{wallet_number}}", canonicalWalletNumberSQL)
	return strings.NewReplacer("{{source}}", pgx.Identifier{o.Source}.Sanitize(), "{{target}}", pgx.Identifier{o.Target}.Sanitize()).Replace(query)
}

// Preserve identifiers accepted by the current application. Legacy identifiers
// are recorded exactly in bookkeeping, never stripped into a transfer address.
const canonicalWalletNumberSQL = `CASE WHEN w.wallet_number ~ '^[0-9]{10}$' THEN w.wallet_number ELSE '8'||lpad(u.id::text,9,'0') END`

func safeError(stage string, err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		return fmt.Errorf("%s failed (SQLSTATE %s; record details suppressed)", stage, pgerr.Code)
	}
	return fmt.Errorf("%s failed (connection or transaction unavailable; details suppressed)", stage)
}

// Plan reads a consistent source snapshot and returns counts, a fingerprint,
// and all preflight problems. It neither creates tables nor copies records.
func Plan(ctx context.Context, db *sql.DB, o Options) (Report, error) {
	if err := names(o); err != nil {
		return Report{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Report{}, safeError("preflight transaction", err)
	}
	defer tx.Rollback()
	return plan(ctx, tx, o)
}

func plan(ctx context.Context, tx *sql.Tx, o Options) (Report, error) {
	r := Report{Version: Version, Source: o.Source, Target: o.Target, Tables: []Table{}, Issues: []Issue{}}
	rows, err := tx.QueryContext(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=$1`, o.Source)
	if err != nil {
		return r, safeError("source schema inspection", err)
	}
	columns := map[string]map[string]bool{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			return r, safeError("source columns", err)
		}
		if columns[table] == nil {
			columns[table] = map[string]bool{}
		}
		columns[table][column] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, safeError("source columns", err)
	}
	for table, required := range requiredColumns {
		if table == "payments" && columns[table] == nil {
			continue // Some normalized deployments have no separate payment table.
		}
		for _, column := range required {
			if !columns[table][column] {
				r.Issues = append(r.Issues, Issue{"missing source column: " + table + "." + column, 1})
			}
		}
	}
	if len(r.Issues) > 0 {
		sort.Slice(r.Issues, func(i, j int) bool { return r.Issues[i].Check < r.Issues[j].Check })
		return r, nil
	}
	queries := []string{}
	for _, table := range inventoryTables {
		if columns[table] == nil {
			continue
		}
		queries = append(queries, fmt.Sprintf(`SELECT '%s' AS name,count(*) AS n,md5(COALESCE(string_agg(to_jsonb(s)::text,E'\n' ORDER BY to_jsonb(s)::text),'')) AS digest FROM %s s`, table, pgx.Identifier{o.Source, table}.Sanitize()))
	}
	rows, err = tx.QueryContext(ctx, strings.Join(queries, " UNION ALL "))
	if err != nil {
		return r, safeError("source inventory", err)
	}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Source, &t.Rows, &t.Digest); err != nil {
			rows.Close()
			return r, safeError("source inventory", err)
		}
		for _, s := range copies {
			if s.source == t.Source {
				t.Target = s.target
				break
			}
		}
		r.Tables = append(r.Tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, safeError("source inventory", err)
	}
	sort.Slice(r.Tables, func(i, j int) bool { return r.Tables[i].Source < r.Tables[j].Source })
	encoded, _ := json.Marshal(r.Tables)
	hash := sha256.Sum256(encoded)
	r.Fingerprint = hex.EncodeToString(hash[:])
	if err := tx.QueryRowContext(ctx, sourceSQL(`SELECT COALESCE(sum(balance),0)::text FROM {{source}}.wallets`, o)).Scan(&r.WalletBalanceTotal); err != nil {
		return r, safeError("wallet total", err)
	}
	var preserved, generated int64
	if err := tx.QueryRowContext(ctx, sourceSQL(`SELECT count(*) FILTER (WHERE wallet_number ~ '^[0-9]{10}$'),count(*) FILTER (WHERE wallet_number IS NULL OR wallet_number !~ '^[0-9]{10}$') FROM {{source}}.wallets`, o)).Scan(&preserved, &generated); err != nil {
		return r, safeError("wallet-number mapping counts", err)
	}
	r.WalletNumbers = map[string]int64{"preserved": preserved, "generated": generated}
	var marker bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, pgx.Identifier{o.Target, "legacy_import_runs"}.Sanitize()).Scan(&marker); err != nil {
		return r, safeError("migration marker", err)
	}
	if marker {
		var fingerprint string
		err := tx.QueryRowContext(ctx, sourceSQL(`SELECT fingerprint FROM {{target}}.legacy_import_runs WHERE version=$1 AND source_schema=$2`, o), Version, o.Source).Scan(&fingerprint)
		if err == nil {
			r.AlreadyApplied = true
			r.Committed = true
			if fingerprint != r.Fingerprint {
				r.Issues = append(r.Issues, Issue{"source changed after the committed import; incremental reimport is unsupported", 1})
			}
			return r, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return r, safeError("migration marker", err)
		}
	}
	targetRows, err := tx.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' AND table_name NOT IN ('legacy_import_runs','legacy_import_mappings','legacy_wallet_numbers') ORDER BY table_name`, o.Target)
	if err != nil {
		return r, safeError("destination inspection", err)
	}
	targetNames := []string{}
	targetQueries := []string{}
	for targetRows.Next() {
		var table string
		if err := targetRows.Scan(&table); err != nil {
			targetRows.Close()
			return r, safeError("destination inspection", err)
		}
		targetQueries = append(targetQueries, fmt.Sprintf("SELECT %d AS table_index,count(*) FROM %s", len(targetNames), pgx.Identifier{o.Target, table}.Sanitize()))
		targetNames = append(targetNames, table)
	}
	err = targetRows.Err()
	targetRows.Close()
	if err != nil {
		return r, safeError("destination inspection", err)
	}
	if len(targetQueries) > 0 {
		counts, err := tx.QueryContext(ctx, strings.Join(targetQueries, " UNION ALL "))
		if err != nil {
			return r, safeError("destination counts", err)
		}
		for counts.Next() {
			var index int
			var n int64
			if err := counts.Scan(&index, &n); err != nil {
				counts.Close()
				return r, safeError("destination counts", err)
			}
			if n > 0 {
				r.Issues = append(r.Issues, Issue{"destination is not empty: " + targetNames[index], n})
			}
		}
		err = counts.Err()
		counts.Close()
		if err != nil {
			return r, safeError("destination counts", err)
		}
	}
	checks := []string{}
	checksToRun := append([]checkSpec{}, preflightChecks...)
	if columns["payments"] != nil {
		checksToRun = append(checksToRun, paymentPreflight)
	}
	for _, check := range checksToRun {
		checks = append(checks, "SELECT '"+check.name+"' AS name,count(*) FROM ("+sourceSQL(check.query, o)+") violations")
	}
	rows, err = tx.QueryContext(ctx, strings.Join(checks, " UNION ALL "))
	if err != nil {
		return r, safeError("data preflight", err)
	}
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.Check, &issue.Rows); err != nil {
			rows.Close()
			return r, safeError("data preflight", err)
		}
		if issue.Rows > 0 {
			r.Issues = append(r.Issues, issue)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, safeError("data preflight", err)
	}
	return r, nil
}

// Apply requires an initialized empty destination. Data, explicit identity
// mappings, verification, the migration marker, and its audit event commit
// together. Locks keep the source snapshot stable while the import runs.
func Apply(ctx context.Context, db *sql.DB, o Options) (Report, error) {
	initial, err := Plan(ctx, db, o)
	if err != nil {
		return initial, err
	}
	if len(initial.Issues) > 0 {
		return initial, ErrUnsafeMigration
	}
	if initial.AlreadyApplied {
		return initial, nil
	}
	if o.ExpectedFingerprint == "" {
		initial.Issues = append(initial.Issues, Issue{"a reviewed source fingerprint is required", 1})
		return initial, ErrUnsafeMigration
	}
	if o.ExpectedFingerprint != initial.Fingerprint {
		initial.Issues = append(initial.Issues, Issue{"source fingerprint changed since review", 1})
		return initial, ErrUnsafeMigration
	}
	if err := initializeBookkeeping(ctx, db, o); err != nil {
		return initial, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return initial, safeError("import transaction", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='120s'`); err != nil {
		return initial, safeError("import timeouts", err)
	}
	if _, err := tx.ExecContext(ctx, sourceSQL(`LOCK TABLE {{target}}.legacy_import_runs IN EXCLUSIVE MODE`, o)); err != nil {
		return initial, safeError("import serialization", err)
	}
	// Exclude concurrent application writes until verification and commit. The
	// import still requires a maintenance window so schema DDL cannot race it.
	locks := []string{}
	for _, table := range append(append([]string{}, identityTables...), "lga_neighbors", "sessions", "otp_codes", "admin_audit_events", "legacy_import_mappings", "legacy_wallet_numbers") {
		locks = append(locks, pgx.Identifier{o.Target, table}.Sanitize())
	}
	if _, err := tx.ExecContext(ctx, "LOCK TABLE "+strings.Join(locks, ",")+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return initial, safeError("destination write exclusion", err)
	}
	locks = nil
	for _, table := range initial.Tables {
		locks = append(locks, pgx.Identifier{o.Source, table.Source}.Sanitize())
	}
	if _, err := tx.ExecContext(ctx, "LOCK TABLE "+strings.Join(locks, ",")+" IN SHARE MODE"); err != nil {
		return initial, safeError("source snapshot lock", err)
	}
	r, err := plan(ctx, tx, o)
	if err != nil {
		return r, err
	}
	if r.AlreadyApplied {
		return r, nil
	}
	if len(r.Issues) > 0 || r.Fingerprint != initial.Fingerprint {
		return r, ErrUnsafeMigration
	}
	if _, err := tx.ExecContext(ctx, sourceSQL(mappingSQL, o)); err != nil {
		return r, safeError("explicit identity mappings", err)
	}
	r.Copied = map[string]int64{}
	available := map[string]bool{}
	for _, table := range r.Tables {
		available[table.Source] = true
	}
	for _, copy := range copies {
		if !available[copy.source] {
			continue
		}
		result, err := tx.ExecContext(ctx, sourceSQL(copy.query, o))
		if err != nil {
			return r, safeError("copy "+copy.source+" to "+copy.target, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return r, safeError("copy row count", err)
		}
		r.Copied[copy.target] += n
	}
	for _, table := range identityTables {
		name := pgx.Identifier{o.Target, table}.Sanitize()
		if _, err := tx.ExecContext(ctx, "SELECT setval(pg_get_serial_sequence($1,'id'),COALESCE(max(id),1),max(id) IS NOT NULL) FROM "+name, name); err != nil {
			return r, safeError("identity sequence "+table, err)
		}
	}
	r.Checks = map[string]bool{}
	checksToRun := append([]checkSpec{}, verificationChecks...)
	if available["payments"] {
		checksToRun = append(checksToRun, paymentVerification)
	}
	for _, check := range checksToRun {
		var bad int64
		if err := tx.QueryRowContext(ctx, sourceSQL("SELECT count(*) FROM ("+check.query+") bad", o)).Scan(&bad); err != nil {
			return r, safeError("verify "+check.name, err)
		}
		r.Checks[check.name] = bad == 0
		if bad > 0 {
			return r, fmt.Errorf("migration verification failed: %s (%d records)", check.name, bad)
		}
	}
	for _, copy := range copies {
		if !available[copy.source] {
			continue
		}
		var sourceCount, targetCount int64
		query := "SELECT (SELECT count(*) FROM " + pgx.Identifier{o.Source, copy.source}.Sanitize() + " " + copy.sourceFilter + "),(SELECT count(*) FROM " + pgx.Identifier{o.Target, copy.target}.Sanitize() + ")"
		if err := tx.QueryRowContext(ctx, query).Scan(&sourceCount, &targetCount); err != nil {
			return r, safeError("verify row counts", err)
		}
		if sourceCount != targetCount {
			return r, fmt.Errorf("migration row count mismatch: %s to %s", copy.source, copy.target)
		}
	}
	counts, _ := json.Marshal(r.Copied)
	var runID int64
	if err := tx.QueryRowContext(ctx, sourceSQL(`INSERT INTO {{target}}.legacy_import_runs(version,source_schema,fingerprint,counts) VALUES($1,$2,$3,$4) RETURNING id`, o), Version, o.Source, r.Fingerprint, string(counts)).Scan(&runID); err != nil {
		return r, safeError("migration completion record", err)
	}
	if _, err := tx.ExecContext(ctx, sourceSQL(`INSERT INTO {{target}}.admin_audit_events(action,resource,target_id,outcome,reason,before_state,after_state) VALUES('system.legacy_import','migration',$1,'committed','schema_upgrade','empty application schema',$2)`, o), runID, string(counts)); err != nil {
		return r, safeError("migration audit record", err)
	}
	if err := tx.Commit(); err != nil {
		return r, safeError("import commit", err)
	}
	r.Committed = true
	return r, nil
}

func initializeBookkeeping(ctx context.Context, db *sql.DB, o Options) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return safeError("bookkeeping transaction", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return safeError("bookkeeping timeout", err)
	}
	// CREATE IF NOT EXISTS alone can race while inserting PostgreSQL catalog
	// entries. Serialize the small DDL transaction before taking the import
	// snapshot, so simultaneous commands can both observe a committed marker.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),6007)`, o.Target); err != nil {
		return safeError("bookkeeping serialization", err)
	}
	if _, err := tx.ExecContext(ctx, sourceSQL(metadataSQL, o)); err != nil {
		return safeError("migration bookkeeping initialization", err)
	}
	if err := tx.Commit(); err != nil {
		return safeError("bookkeeping commit", err)
	}
	return nil
}

const metadataSQL = `
CREATE TABLE IF NOT EXISTS {{target}}.legacy_import_runs (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 version TEXT NOT NULL,source_schema TEXT NOT NULL,fingerprint TEXT NOT NULL,
 counts JSONB NOT NULL,committed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(version,source_schema)
);
CREATE TABLE IF NOT EXISTS {{target}}.legacy_import_mappings (
 source_table TEXT NOT NULL,source_id BIGINT NOT NULL,target_table TEXT NOT NULL,target_id BIGINT NOT NULL,
 PRIMARY KEY(source_table,source_id),UNIQUE(source_table,target_id)
);
CREATE TABLE IF NOT EXISTS {{target}}.legacy_wallet_numbers (
 source_wallet_id BIGINT PRIMARY KEY,source_user_id BIGINT NOT NULL UNIQUE,
 target_user_id BIGINT NOT NULL UNIQUE,legacy_number TEXT,
 wallet_number VARCHAR(10) NOT NULL UNIQUE CHECK (wallet_number ~ '^[0-9]{10}$'),
 conversion TEXT NOT NULL CHECK (conversion IN ('preserved','generated'))
);`

const mappingSQL = `
INSERT INTO {{target}}.legacy_import_mappings SELECT 'users',id,'farmers',id FROM {{source}}.users;
INSERT INTO {{target}}.legacy_import_mappings SELECT 'products',id,'crops',id FROM {{source}}.products;
INSERT INTO {{target}}.legacy_import_mappings SELECT 'wallets',id,'wallets',id FROM {{source}}.wallets;
INSERT INTO {{target}}.legacy_import_mappings SELECT 'orders',id,'orders',id FROM {{source}}.orders;
INSERT INTO {{target}}.legacy_import_mappings SELECT 'order_items',id,'orders',order_id FROM {{source}}.order_items;
INSERT INTO {{target}}.legacy_wallet_numbers(source_wallet_id,source_user_id,target_user_id,legacy_number,wallet_number,conversion)
 SELECT w.id,u.id,m.target_id,w.wallet_number,{{wallet_number}},
 CASE WHEN w.wallet_number ~ '^[0-9]{10}$' THEN 'preserved' ELSE 'generated' END
 FROM {{source}}.users u JOIN {{source}}.wallets w ON w.owner_type='user' AND w.owner_id=u.id
 JOIN {{target}}.legacy_import_mappings m ON m.source_table='users' AND m.source_id=u.id;
`
