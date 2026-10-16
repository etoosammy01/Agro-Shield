package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/models"
)

// These tests exercise the wallet's financial guarantees against a real
// PostgreSQL database: that registration produces exactly one NGN wallet, that a
// verified deposit credits it exactly once even under concurrent delivery, that a
// rejected deposit leaves the balance and the ledger untouched, and that the
// existing Flutterwave payout path still behaves.
//
// They are integration, not unit: UNIQUE constraints, row locks and ON CONFLICT
// only behave as the production code needs them to inside a real transaction, so
// an in-memory stand-in would prove nothing about the money.
//
// They run only when TEST_DATABASE_URL is set and otherwise skip, so
// `go test ./...` still passes on a machine with no database. Point it at a
// throwaway database: the tests create and delete users.

// testDB opens the database named by TEST_DATABASE_URL and brings it up to the
// current schema. It skips the test when the variable is unset.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run wallet integration tests")
	}
	db, err := database.ConnectDB(dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// RunMigration is idempotent, so it is safe both for a fresh database and
	// for one an earlier run already migrated.
	if err := database.RunMigration(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}

// createTestUser registers a user through FarmerRepository.Create — the same
// path the application uses — so the wallet under test is the one real
// registration produces rather than one the test inserted itself.
//
// PhotoURL is set explicitly because the farmers table rejects a blank one, and
// the default in the schema is not applied when a value is supplied.
func createTestUser(t *testing.T, db *sql.DB) *models.Farmer {
	t.Helper()

	farmer := &models.Farmer{
		FullName:     "Test Wallet User",
		Phone:        fmt.Sprintf("234%09d%04d", rand.Intn(1_000_000_000), rand.Intn(10_000)),
		Location:     "Kano",
		State:        "Kano",
		Country:      "Nigeria",
		Role:         "farmer",
		PasswordHash: "test-password-hash",
		PhotoURL:     "/static/assets/images/un.jpeg",
	}
	if err := NewFarmerRepository(db).Create(farmer); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() { deleteTestUser(t, db, farmer.ID) })
	return farmer
}

// deleteTestUser removes everything a test created, children before parents
// because every wallet table references wallets with ON DELETE RESTRICT. A
// failure here is logged rather than raised: it runs after the test body, and it
// must not replace the assertion that actually failed with a cleanup error.
func deleteTestUser(t *testing.T, db *sql.DB, farmerID int) {
	t.Helper()

	for _, stmt := range []string{
		`DELETE FROM wallet_transactions WHERE wallet_id IN (SELECT id FROM wallets WHERE farmer_id = $1)`,
		`DELETE FROM wallet_deposits     WHERE wallet_id IN (SELECT id FROM wallets WHERE farmer_id = $1)`,
		`DELETE FROM wallet_withdrawals  WHERE farmer_id = $1`,
		`DELETE FROM wallet_transfers    WHERE sender_id = $1 OR recipient_id = $1`,
		`DELETE FROM wallets             WHERE farmer_id = $1`,
		`DELETE FROM farmers             WHERE id = $1`,
	} {
		if _, err := db.Exec(stmt, farmerID); err != nil {
			t.Logf("cleanup of user %d: %v", farmerID, err)
		}
	}
}

// uniqueRef keeps the UNIQUE columns on the wallet tables from colliding between
// tests that run one after another in the same database.
func uniqueRef(prefix string) string {
	return fmt.Sprintf("%s-TEST-%d-%04d", prefix, time.Now().UnixNano(), rand.Intn(10_000))
}

// fundWallet credits a user through the real deposit path — a pending row first,
// then a verified completion — and returns the reference it used.
func fundWallet(t *testing.T, db *sql.DB, farmerID int, amount string) string {
	t.Helper()

	repo := NewWalletRepository(db)
	ctx := context.Background()
	reference := uniqueRef("WALLET")

	if err := repo.CreateDeposit(ctx, farmerID, models.ProviderFlutterwave, reference, amount); err != nil {
		t.Fatalf("create deposit: %v", err)
	}
	if err := repo.CompleteDeposit(ctx, reference, "TX-"+reference, amount, "NGN"); err != nil {
		t.Fatalf("complete deposit: %v", err)
	}
	return reference
}

func walletBalanceOf(t *testing.T, db *sql.DB, farmerID int) string {
	t.Helper()

	var balance string
	if err := db.QueryRow(
		`SELECT balance::text FROM wallets WHERE farmer_id = $1`, farmerID,
	).Scan(&balance); err != nil {
		t.Fatalf("read wallet balance: %v", err)
	}
	return balance
}

func ledgerCountOf(t *testing.T, db *sql.DB, farmerID int) int {
	t.Helper()

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM wallet_transactions
		WHERE wallet_id IN (SELECT id FROM wallets WHERE farmer_id = $1)`, farmerID,
	).Scan(&count); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	return count
}

func depositStatusOf(t *testing.T, db *sql.DB, reference string) string {
	t.Helper()

	var status string
	if err := db.QueryRow(
		`SELECT status FROM wallet_deposits WHERE reference = $1`, reference,
	).Scan(&status); err != nil {
		t.Fatalf("read deposit status: %v", err)
	}
	return status
}

func walletCountOf(t *testing.T, db *sql.DB, farmerID int) int {
	t.Helper()

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM wallets WHERE farmer_id = $1`, farmerID,
	).Scan(&count); err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	return count
}

// Registration must leave the user with exactly one wallet, in NGN, at zero, and
// owned by the account that was just created — never a second one, never another
// user's.
func TestRegistrationCreatesExactlyOneNGNWallet(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)

	wallet, err := NewWalletRepository(db).GetWallet(context.Background(), farmer.ID)
	if err != nil {
		t.Fatalf("get wallet for new user: %v", err)
	}
	if wallet.FarmerID != farmer.ID {
		t.Errorf("wallet belongs to user %d, want %d", wallet.FarmerID, farmer.ID)
	}
	if wallet.Currency != "NGN" {
		t.Errorf("wallet currency = %q, want NGN", wallet.Currency)
	}
	if wallet.Balance != "0.00" {
		t.Errorf("new wallet balance = %q, want 0.00", wallet.Balance)
	}
	if got := walletCountOf(t, db, farmer.ID); got != 1 {
		t.Errorf("user has %d wallets, want exactly 1", got)
	}
}

// Repairing a missing wallet must converge on the same single row however many
// callers ask at once, and must return the existing wallet rather than a new one.
func TestEnsureWalletIsIdempotent(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()

	// The case this covers: a wallet lost to a restored backup or a hand edit.
	if _, err := db.Exec(`DELETE FROM wallets WHERE farmer_id = $1`, farmer.ID); err != nil {
		t.Fatalf("delete wallet: %v", err)
	}

	wallet, err := repo.EnsureWallet(ctx, farmer.ID)
	if err != nil {
		t.Fatalf("ensure missing wallet: %v", err)
	}
	if _, err := repo.EnsureWallet(ctx, farmer.ID); err != nil {
		t.Fatalf("ensure existing wallet: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	ids := make([]int64, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repaired, err := repo.EnsureWallet(ctx, farmer.ID)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = repaired.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent ensure %d: %v", i, err)
		}
	}
	if got := walletCountOf(t, db, farmer.ID); got != 1 {
		t.Fatalf("user has %d wallets after concurrent repair, want exactly 1", got)
	}
	for i, id := range ids {
		if id != wallet.ID {
			t.Errorf("concurrent ensure %d returned wallet %d, want the original %d", i, id, wallet.ID)
		}
	}
}

// A funding attempt for an account with no wallet row must fail loudly. It used
// to write nothing and return no error, so the provider was still called and the
// payment could never be credited — money taken, wallet never funded.
func TestCreateDepositWithoutWalletIsRejected(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)

	if _, err := db.Exec(`DELETE FROM wallets WHERE farmer_id = $1`, farmer.ID); err != nil {
		t.Fatalf("delete wallet: %v", err)
	}

	reference := uniqueRef("WALLET")
	err := NewWalletRepository(db).CreateDeposit(context.Background(), farmer.ID, models.ProviderFlutterwave, reference, "1000.00")
	if !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("deposit without a wallet = %v, want ErrWalletNotFound", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wallet_deposits WHERE reference = $1`, reference).Scan(&count); err != nil {
		t.Fatalf("count deposits: %v", err)
	}
	if count != 0 {
		t.Errorf("wrote %d deposit rows for a user with no wallet, want 0", count)
	}
}

// A deposit delivered more than once — a retried webhook, a refreshed callback —
// must credit once and leave one ledger row. Money is never created twice.
func TestCompleteDepositCreditsExactlyOnce(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	reference := uniqueRef("WALLET")

	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, "5000.00"); err != nil {
		t.Fatalf("create deposit: %v", err)
	}
	if err := repo.CompleteDeposit(ctx, reference, "TX-1", "5000.00", "NGN"); err != nil {
		t.Fatalf("complete deposit: %v", err)
	}
	if err := repo.CompleteDeposit(ctx, reference, "TX-1", "5000.00", "NGN"); err != nil {
		t.Fatalf("replayed completion must be a no-op, got %v", err)
	}

	if balance := walletBalanceOf(t, db, farmer.ID); balance != "5000.00" {
		t.Errorf("balance after replay = %q, want 5000.00", balance)
	}
	if rows := ledgerCountOf(t, db, farmer.ID); rows != 1 {
		t.Errorf("ledger has %d rows after replay, want 1", rows)
	}
	if status := depositStatusOf(t, db, reference); status != models.DepositSuccessful {
		t.Errorf("deposit status = %q, want %q", status, models.DepositSuccessful)
	}
}

// Two providers can deliver the same completion at the same instant. The row
// lock in CompleteDeposit, not any application-level flag, is what must make
// exactly one of them credit.
func TestConcurrentCompleteDepositCreditsOnce(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	reference := uniqueRef("WALLET")

	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, "2500.00"); err != nil {
		t.Fatalf("create deposit: %v", err)
	}

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = repo.CompleteDeposit(ctx, reference, "TX-RACE", "2500.00", "NGN")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent completion %d: %v", i, err)
		}
	}
	if balance := walletBalanceOf(t, db, farmer.ID); balance != "2500.00" {
		t.Errorf("balance after concurrent completion = %q, want 2500.00", balance)
	}
	if rows := ledgerCountOf(t, db, farmer.ID); rows != 1 {
		t.Errorf("ledger has %d rows after concurrent completion, want 1", rows)
	}
}

// A provider that reports a different amount or currency than the one the user
// asked to pay must never be credited silently. The deposit stays pending for an
// operator to resolve.
func TestCompleteDepositRejectsMismatchedAmountOrCurrency(t *testing.T) {
	cases := []struct {
		name     string
		verified string
		currency string
	}{
		{name: "amount", verified: "4000.00", currency: "NGN"},
		{name: "currency", verified: "5000.00", currency: "USD"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			farmer := createTestUser(t, db)
			repo := NewWalletRepository(db)
			ctx := context.Background()
			reference := uniqueRef("WALLET")

			if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, "5000.00"); err != nil {
				t.Fatalf("create deposit: %v", err)
			}
			if err := repo.CompleteDeposit(ctx, reference, "TX-MISMATCH", tc.verified, tc.currency); err == nil {
				t.Fatal("mismatched deposit completion succeeded, want an error")
			}

			if balance := walletBalanceOf(t, db, farmer.ID); balance != "0.00" {
				t.Errorf("balance after rejected completion = %q, want 0.00", balance)
			}
			if rows := ledgerCountOf(t, db, farmer.ID); rows != 0 {
				t.Errorf("ledger has %d rows after rejected completion, want 0", rows)
			}
			if status := depositStatusOf(t, db, reference); status != models.DepositPending {
				t.Errorf("deposit status = %q, want %q — it must not be cleared by a mismatch", status, models.DepositPending)
			}
		})
	}
}

// The balance change, the deposit's new status and the ledger row share one
// transaction. If the ledger write fails, none of them may survive: a balance
// that moved without a ledger row is money nobody can account for.
func TestCompleteDepositRollsBackWhenLedgerWriteFails(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()
	reference := uniqueRef("WALLET")

	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, "5000.00"); err != nil {
		t.Fatalf("create deposit: %v", err)
	}

	// Claim the ledger reference the credit is about to use, so the INSERT
	// inside CompleteDeposit hits the UNIQUE constraint and the transaction has
	// to roll back.
	if _, err := db.Exec(`
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		SELECT id, $2, 'deposit', 1.00 FROM wallets WHERE farmer_id = $1`,
		farmer.ID, "deposit:"+reference,
	); err != nil {
		t.Fatalf("pre-claim ledger reference: %v", err)
	}

	if err := repo.CompleteDeposit(ctx, reference, "TX-ROLLBACK", "5000.00", "NGN"); err == nil {
		t.Fatal("completion succeeded despite a conflicting ledger row, want an error")
	}

	if balance := walletBalanceOf(t, db, farmer.ID); balance != "0.00" {
		t.Errorf("balance after rolled-back completion = %q, want 0.00", balance)
	}
	if status := depositStatusOf(t, db, reference); status != models.DepositPending {
		t.Errorf("deposit status after rollback = %q, want %q", status, models.DepositPending)
	}
}

// Migrations run on every start, so one must never reset a balance or add a
// second wallet to an account that already has one.
func TestMigrationPreservesExistingBalances(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	reference := fundWallet(t, db, farmer.ID, "1234.56")

	before, err := NewWalletRepository(db).GetWallet(context.Background(), farmer.ID)
	if err != nil {
		t.Fatalf("get wallet before migration: %v", err)
	}

	if err := database.RunMigration(db); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}

	after, err := NewWalletRepository(db).GetWallet(context.Background(), farmer.ID)
	if err != nil {
		t.Fatalf("get wallet after migration: %v", err)
	}
	if after.ID != before.ID {
		t.Errorf("migration replaced wallet %d with %d", before.ID, after.ID)
	}
	if after.Balance != before.Balance {
		t.Errorf("migration changed balance from %q to %q", before.Balance, after.Balance)
	}
	if after.Balance != "1234.56" {
		t.Errorf("balance after migration = %q, want 1234.56", after.Balance)
	}
	if got := walletCountOf(t, db, farmer.ID); got != 1 {
		t.Errorf("user has %d wallets after migration, want exactly 1", got)
	}

	// The deposit the balance came from survives too, and reads as Flutterwave's.
	var provider string
	if err := db.QueryRow(
		`SELECT provider FROM wallet_deposits WHERE reference = $1`, reference,
	).Scan(&provider); err != nil {
		t.Fatalf("read deposit provider after migration: %v", err)
	}
	if provider != models.ProviderFlutterwave {
		t.Errorf("deposit provider after migration = %q, want %q", provider, models.ProviderFlutterwave)
	}
}

// Flutterwave remains the payout provider. A withdrawal reserves the money
// immediately and must refuse one the balance cannot cover, so a payout can
// never start against funds that are not there.
func TestWithdrawalReservesAndRejectsInsufficientBalance(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()

	if err := NewFarmerRepository(db).UpdateBankDetails(
		farmer.ID, "Test Bank", "999", "Test Wallet User", "0123456789",
	); err != nil {
		t.Fatalf("save bank details: %v", err)
	}

	if err := repo.CreateWithdrawal(ctx, farmer.ID, uniqueRef("WITHDRAW"), "100.00"); !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("withdrawal against an empty wallet = %v, want ErrInsufficientBalance", err)
	}

	fundWallet(t, db, farmer.ID, "5000.00")

	reference := uniqueRef("WITHDRAW")
	if err := repo.CreateWithdrawal(ctx, farmer.ID, reference, "1000.00"); err != nil {
		t.Fatalf("withdraw against a funded wallet: %v", err)
	}
	if balance := walletBalanceOf(t, db, farmer.ID); balance != "4000.00" {
		t.Errorf("balance after reserving a withdrawal = %q, want 4000.00", balance)
	}

	var status, amount string
	if err := db.QueryRow(
		`SELECT status, amount::text FROM wallet_withdrawals WHERE reference = $1`, reference,
	).Scan(&status, &amount); err != nil {
		t.Fatalf("read withdrawal: %v", err)
	}
	if status != "pending" {
		t.Errorf("withdrawal status = %q, want pending — nothing is paid without approval", status)
	}
	if amount != "1000.00" {
		t.Errorf("withdrawal amount = %q, want 1000.00", amount)
	}

	var reserved int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM wallet_transactions
		WHERE reference = $1 AND type = 'withdrawal_reserved'`, "withdrawal:"+reference,
	).Scan(&reserved); err != nil {
		t.Fatalf("count reserve ledger rows: %v", err)
	}
	if reserved != 1 {
		t.Errorf("reserve ledger rows = %d, want 1", reserved)
	}
}

// The deposit's provider is stored data, and an unknown reference is how a
// caller tells a wallet funding attempt apart from an order payment. The
// provider's transaction id is unique per provider rather than globally, because
// two vendors are not guaranteed to avoid colliding ids.
func TestDepositRecordsProviderAndScopesTransactionIDUniqueness(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()

	first := uniqueRef("WALLET")
	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, first, "100.00"); err != nil {
		t.Fatalf("create deposit: %v", err)
	}
	provider, err := repo.DepositProvider(ctx, first)
	if err != nil {
		t.Fatalf("read deposit provider: %v", err)
	}
	if provider != models.ProviderFlutterwave {
		t.Errorf("deposit provider = %q, want %q", provider, models.ProviderFlutterwave)
	}
	if _, err := repo.DepositProvider(ctx, uniqueRef("ORDER")); !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("unknown reference = %v, want ErrWalletNotFound", err)
	}

	// "provider-b" stands in for any second funding vendor; no such client
	// exists yet. It only has to be a different provider name.
	second := uniqueRef("WALLET")
	if err := repo.CreateDeposit(ctx, farmer.ID, "provider-b", second, "100.00"); err != nil {
		t.Fatalf("create deposit for a second provider: %v", err)
	}

	// The same provider transaction id from two different providers is allowed.
	for _, reference := range []string{first, second} {
		if _, err := db.Exec(
			`UPDATE wallet_deposits SET provider_transaction_id = 'SHARED-ID' WHERE reference = $1`, reference,
		); err != nil {
			t.Fatalf("assign shared transaction id to %s: %v", reference, err)
		}
	}

	// The same provider transaction id from the same provider is not.
	third := uniqueRef("WALLET")
	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, third, "100.00"); err != nil {
		t.Fatalf("create third deposit: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE wallet_deposits SET provider_transaction_id = 'SHARED-ID' WHERE reference = $1`, third,
	); err == nil {
		t.Error("a provider transaction id reused within one provider was accepted, want a unique violation")
	}
}
