package database

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed refunds.sql
var refundsSQL string

//go:embed financial_integration.sql
var financialIntegrationSQL string

// RunRefundMigration preserves legacy refund IDs, provider fields and history.
// It is called by the existing ordered migration runner, before escrow/audit.
func RunRefundMigration(db *sql.DB) error {
	return runIntegrationSQL(db, "refund integration", refundsSQL)
}

func RunFinancialIntegrationMigration(db *sql.DB) error {
	return runIntegrationSQL(db, "financial integration", financialIntegrationSQL)
}

func runIntegrationSQL(db *sql.DB, name, source string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(current_schema()), 60817)`); err != nil {
		return err
	}
	if _, err := tx.Exec(source); err != nil {
		return fmt.Errorf("%s migration: %w", name, err)
	}
	return tx.Commit()
}
