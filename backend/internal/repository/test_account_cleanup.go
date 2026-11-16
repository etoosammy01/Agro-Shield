package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"backend/internal/database"
	"backend/internal/models"
	"backend/internal/phone"
)

type TestDuplicateCleanup struct {
	KeeperID               int   `json:"keeper_id"`
	ClosedIDs              []int `json:"closed_ids"`
	DepositsRemoved        int64 `json:"test_deposits_removed"`
	LedgerRowsRemoved      int64 `json:"test_ledger_rows_removed"`
	FundingAccountsRemoved int64 `json:"test_funding_accounts_removed"`
	Committed              bool  `json:"committed"`
}

// CleanupTestDuplicates is an operator-only repair, never an HTTP account
// closure path. The caller must explicitly attest that the attached funding
// records are test data. Dry-run executes the same checks and rolls back.
func (r *AdminRepository) CleanupTestDuplicates(ctx context.Context, keeperID int, targets []int, testRecords, apply bool) (TestDuplicateCleanup, error) {
	report := TestDuplicateCleanup{KeeperID: keeperID}
	if !testRecords || keeperID <= 0 || len(targets) == 0 {
		return report, ErrAdminInvalid
	}
	targets = append([]int(nil), targets...)
	sort.Ints(targets)
	for i, id := range targets {
		if id <= 0 || id == keeperID || (i > 0 && id == targets[i-1]) {
			return report, ErrAdminProtected
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	// This maintenance operation blocks writers while ownership, provider rows
	// and the test-only attestations are checked. Normal reads keep working.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE farmers,wallets,wallet_deposits,wallet_transactions,
 wallet_virtual_accounts,wallet_withdrawals,wallet_transfers IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return report, err
	}
	access, err := r.access(ctx, tx, keeperID)
	if err != nil {
		return report, err
	}
	if access.Role != "administrator" {
		return report, ErrAdminForbidden
	}
	var keeperPhone string
	if err := tx.QueryRowContext(ctx, `SELECT phone FROM farmers WHERE id=$1 AND deleted_at IS NULL`, keeperID).Scan(&keeperPhone); err != nil {
		return report, err
	}
	canonical := phone.Normalize(keeperPhone)
	if canonical == "" {
		return report, ErrInvalidPhone
	}
	for _, id := range targets {
		var raw, role string
		var walletAdmin, balanceZero bool
		if err := tx.QueryRowContext(ctx, `SELECT f.phone,f.admin_role,f.is_admin,COALESCE(w.balance=0,true)
 FROM farmers f LEFT JOIN wallets w ON w.farmer_id=f.id WHERE f.id=$1 AND f.deleted_at IS NULL`, id).Scan(&raw, &role, &walletAdmin, &balanceZero); err != nil {
			if err == sql.ErrNoRows {
				return report, ErrAccountNotFound
			}
			return report, err
		}
		if role != "" || walletAdmin || r.configured[id] {
			return report, ErrAdminProtected
		}
		if phone.Normalize(raw) != canonical {
			return report, ErrAdminConflict
		}
		if !balanceZero {
			return report, ErrAccountHasBalance
		}
		var related bool
		if err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM wallet_transfers WHERE sender_id=$1 OR recipient_id=$1 OR approved_by=$1)
 OR EXISTS(SELECT 1 FROM wallet_withdrawals WHERE farmer_id=$1 OR approved_by=$1)
 OR EXISTS(SELECT 1 FROM orders o LEFT JOIN crops c ON c.id=o.crop_id WHERE o.buyer_id=$1 OR c.farmer_id=$1)
 OR EXISTS(SELECT 1 FROM deliveries WHERE buyer_id=$1 OR farmer_id=$1)`, id).Scan(&related); err != nil {
			return report, err
		}
		if related {
			return report, ErrAccountHasPendingRequest
		}
		for i, stmt := range []string{
			`DELETE FROM wallet_deposits WHERE wallet_id IN(SELECT id FROM wallets WHERE farmer_id=$1)`,
			`DELETE FROM wallet_transactions WHERE wallet_id IN(SELECT id FROM wallets WHERE farmer_id=$1)`,
			`DELETE FROM wallet_virtual_accounts WHERE farmer_id=$1`,
		} {
			result, err := tx.ExecContext(ctx, stmt, id)
			if err != nil {
				return report, err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return report, err
			}
			switch i {
			case 0:
				report.DepositsRemoved += count
			case 1:
				report.LedgerRowsRemoved += count
			case 2:
				report.FundingAccountsRemoved += count
			}
		}
		if err := closeAccountTx(ctx, tx, id); err != nil {
			return report, err
		}
		if err := appendAdminAudit(ctx, tx, models.AdminAuditEvent{ActorID: keeperID, Action: "user.close_test_duplicate", Resource: "farmers", TargetID: int64(id), Outcome: "committed", Reason: "owner_confirmed_test_duplicates", Before: "active duplicate; zero wallet balance", After: fmt.Sprintf("closed and anonymized; administrator %d retained; test funding removed", keeperID)}); err != nil {
			return report, err
		}
		report.ClosedIDs = append(report.ClosedIDs, id)
	}
	if err := database.RunPhoneMigrationInTx(ctx, tx); err != nil {
		return report, err
	}
	if apply {
		if err := tx.Commit(); err != nil {
			return report, err
		}
		report.Committed = true
	}
	return report, nil
}
