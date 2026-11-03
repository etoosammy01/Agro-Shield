package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"backend/internal/database"
	"backend/internal/models"
)

func TestConcurrentDistinctCreditsPreserveBalance(t *testing.T) {
	db := testDB(t)
	user := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	const workers = 12
	refs := make([]string, workers)
	for i := range refs {
		refs[i] = uniqueRef("DISTINCT")
		if err := repo.CreateDeposit(ctx, user.ID, models.ProviderFlutterwave, refs[i], "100.00"); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Add(1)
		go func(ref string) {
			defer wg.Done()
			<-start
			_, err := repo.CompleteDeposit(ctx, ref, "tx-"+ref, "100.00", "NGN")
			results <- err
		}(ref)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	if got := walletBalanceOf(t, db, user.ID); got != "1200.00" {
		t.Fatalf("balance %s", got)
	}
	if got := ledgerCountOf(t, db, user.ID); got != workers {
		t.Fatalf("ledger rows %d", got)
	}
}

func TestConcurrentDebitsEnforceFundsAndOwnership(t *testing.T) {
	db := testDB(t)
	user, stranger := createTestUser(t, db), createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	if err := NewFarmerRepository(db).UpdateBankDetails(user.ID, "Test Bank", "999", "Test Wallet User", "0123456789"); err != nil {
		t.Fatal(err)
	}
	fundWallet(t, db, user.ID, "500.00")
	const workers = 12
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- repo.CreateWithdrawal(ctx, user.ID, fmt.Sprintf("reserve-%d-%d", user.ID, i), "100.00")
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInsufficientBalance) {
			t.Error(err)
		}
	}
	if successes != 5 {
		t.Fatalf("successful debits %d, want 5", successes)
	}
	if got := walletBalanceOf(t, db, user.ID); got != "0.00" {
		t.Fatal(got)
	}
	rows, err := repo.ListFarmerWithdrawals(ctx, user.ID)
	if err != nil || len(rows) != 5 {
		t.Fatalf("withdrawals %d: %v", len(rows), err)
	}
	if err := repo.CancelWithdrawal(ctx, rows[0].ID, stranger.ID); err == nil {
		t.Fatal("stranger cancelled withdrawal")
	}
	if got := walletBalanceOf(t, db, user.ID); got != "0.00" {
		t.Fatal("unauthorized cancellation moved funds")
	}
	transactions, err := repo.ListTransactions(ctx, stranger.ID)
	if err != nil || len(transactions) != 0 {
		t.Fatal("stranger transaction list leaked owner records")
	}
}

func TestDepositRollbackCanRetryAfterConstraintConflict(t *testing.T) {
	db := testDB(t)
	user := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	ref := uniqueRef("RETRY")
	if err := repo.CreateDeposit(ctx, user.ID, models.ProviderFlutterwave, ref, "100.00"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO wallet_transactions(wallet_id, reference, type, amount) SELECT id, $2, 'deposit', 1 FROM wallets WHERE farmer_id=$1`, user.ID, "deposit:"+ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CompleteDeposit(ctx, ref, "retry-tx", "100.00", "NGN"); err == nil {
		t.Fatal("expected ledger constraint failure")
	}
	if got := walletBalanceOf(t, db, user.ID); got != "0.00" {
		t.Fatal("partial credit survived")
	}
	if got := depositStatusOf(t, db, ref); got != models.DepositPending {
		t.Fatal("failed operation changed status")
	}
	if _, err := db.Exec(`DELETE FROM wallet_transactions WHERE reference=$1`, "deposit:"+ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CompleteDeposit(ctx, ref, "retry-tx", "100.00", "NGN"); err != nil {
		t.Fatal(err)
	}
	if got := walletBalanceOf(t, db, user.ID); got != "100.00" {
		t.Fatal(got)
	}
	if got := ledgerCountOf(t, db, user.ID); got != 1 {
		t.Fatal(got)
	}
}

func TestMigrationRepeatPreservesConstraintsAndIndexes(t *testing.T) {
	db := testDB(t)
	snapshot := func() string {
		t.Helper()
		var result string
		err := db.QueryRow(`SELECT string_agg(def, E'\n' ORDER BY def) FROM (
   SELECT tablename || ':' || indexdef AS def FROM pg_indexes WHERE schemaname=current_schema()
   UNION ALL
   SELECT conrelid::regclass::text || ':' || pg_get_constraintdef(oid) FROM pg_constraint WHERE connamespace=current_schema()::regnamespace
  ) inventory`).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	for i := 0; i < 2; i++ {
		if err := database.RunMigration(db); err != nil {
			t.Fatal(err)
		}
	}
	if after := snapshot(); before != after {
		t.Fatal("repeat migration changed index/constraint definitions")
	}
}

// This fixture represents the supported pre-name-check payout schema. It is
// intentionally narrower than claiming compatibility with every legacy schema.
func TestMigrationRestoresSupportedLegacyWithdrawalColumns(t *testing.T) {
	db := testDB(t)
	user := createTestUser(t, db)
	fundWallet(t, db, user.ID, "123.45")
	if _, err := db.Exec(`ALTER TABLE wallet_withdrawals DROP COLUMN name_check_status, DROP COLUMN name_check_resolved, DROP COLUMN name_check_detail, DROP COLUMN name_check_at, DROP COLUMN reminded_at`); err != nil {
		t.Fatal(err)
	}
	if err := database.RunMigration(db); err != nil {
		t.Fatal(err)
	}
	var columns int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='wallet_withdrawals' AND column_name IN ('name_check_status','name_check_resolved','name_check_detail','name_check_at','reminded_at')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 5 {
		t.Fatalf("restored columns %d, want 5", columns)
	}
	if got := walletBalanceOf(t, db, user.ID); got != "123.45" {
		t.Fatal("upgrade changed balance")
	}
}

func TestOnboardingMigrationRollsBackOnConstraintFailure(t *testing.T) {
	db := testDB(t)
	user := createTestUser(t, db)
	if _, err := db.Exec(`ALTER TABLE farmers DROP COLUMN phone_verified_at;
  ALTER TABLE otp_codes DROP CONSTRAINT otp_codes_purpose_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO otp_codes(farmer_id,phone,purpose,code_hash,expires_at) VALUES($1,'synthetic','unsupported_fixture','synthetic-hash',CURRENT_TIMESTAMP + interval '1 minute')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.RunOnboardingMigration(db); err == nil {
		t.Fatal("expected invalid legacy purpose to reject migration")
	}
	var columns int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='farmers' AND column_name='phone_verified_at'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatal("failed migration left a partially added column")
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM otp_codes WHERE farmer_id=$1 AND purpose='unsupported_fixture'`, user.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatal("failed migration removed legacy data")
	}
	if _, err := db.Exec(`DELETE FROM otp_codes WHERE farmer_id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.RunOnboardingMigration(db); err != nil {
		t.Fatalf("migration could not retry after fixture repair: %v", err)
	}
}
