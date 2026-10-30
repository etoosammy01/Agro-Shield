package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/models"

	"golang.org/x/crypto/bcrypt"
)

func verifiedPINOwner(t *testing.T, db *sql.DB) *models.Farmer {
	t.Helper()
	f := createTestUser(t, db)
	if _, err := db.Exec(`UPDATE farmers SET selfie_captured_at = CURRENT_TIMESTAMP,
		phone_verified_at = CURRENT_TIMESTAMP WHERE id = $1`, f.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func testPINHash(t *testing.T, pin string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func TestWalletPINPersistsOnWalletAndSurvivesMigration(t *testing.T) {
	db := testDB(t)
	f := verifiedPINOwner(t, db)
	repo := NewWalletRepository(db)
	hash := testPINHash(t, "0456")
	ctx := context.Background()
	if err := repo.SetTransactionPIN(ctx, f.ID, hash); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.RunOnboardingMigration(db); err != nil {
			t.Fatal(err)
		}
	}
	var storedHash string
	var created sql.NullTime
	if err := db.QueryRow(`SELECT transaction_pin_hash, transaction_pin_created_at FROM wallets WHERE farmer_id = $1`, f.ID).Scan(&storedHash, &created); err != nil || storedHash != hash || !created.Valid {
		t.Fatalf("wallet PIN was not preserved: %v", err)
	}
	wallet, err := repo.GetWallet(ctx, f.ID)
	if err != nil || !wallet.HasTransactionPIN {
		t.Fatal("wallet does not report its PIN")
	}
	farmers := NewFarmerRepository(db)
	for _, read := range []func() (*models.Farmer, error){
		func() (*models.Farmer, error) { return farmers.GetByID(f.ID) },
		func() (*models.Farmer, error) { return farmers.GetByPhone(f.Phone) },
		func() (*models.Farmer, error) { return farmers.GetByWalletNumber(f.WalletNumber) },
	} {
		stored, err := read()
		if err != nil || stored == nil || !stored.HasTransactionPIN {
			t.Fatalf("onboarding did not find the linked wallet PIN: %v", err)
		}
	}
	if err := repo.SetTransactionPIN(ctx, f.ID, testPINHash(t, "9999")); !errors.Is(err, ErrTransactionPINAlreadySet) {
		t.Fatal("setup overwrote an existing PIN")
	}
	if err := repo.VerifyTransactionPIN(ctx, f.ID, "0456"); err != nil {
		t.Fatal("original PIN could not be verified")
	}
	other := verifiedPINOwner(t, db)
	if err := repo.VerifyTransactionPIN(ctx, other.ID, "0456"); !errors.Is(err, ErrTransactionPINNotSet) {
		t.Fatal("another wallet's PIN authorized the owner")
	}
}

func TestWalletPINSetupRequiresVerificationAndCreatesOnlyOnce(t *testing.T) {
	db := testDB(t)
	f := createTestUser(t, db)
	repo, ctx := NewWalletRepository(db), context.Background()
	hash := testPINHash(t, "0456")
	if err := repo.SetTransactionPIN(ctx, f.ID, hash); !errors.Is(err, ErrTransactionPINSetupUnavailable) {
		t.Fatal("PIN setup allowed before phone verification")
	}
	if _, err := db.Exec(`UPDATE farmers SET selfie_captured_at = CURRENT_TIMESTAMP, phone_verified_at = CURRENT_TIMESTAMP WHERE id = $1`, f.ID); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.SetTransactionPIN(ctx, f.ID, hash) }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrTransactionPINAlreadySet) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("PIN was created %d times", successes)
	}
}

func TestWalletPINLockoutPersistsAndExpires(t *testing.T) {
	db := testDB(t)
	f := verifiedPINOwner(t, db)
	repo, ctx := NewWalletRepository(db), context.Background()
	if err := repo.SetTransactionPIN(ctx, f.ID, testPINHash(t, "0456")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		want := ErrTransactionPINIncorrect
		if i == 5 {
			want = ErrTransactionPINLocked
		}
		if err := repo.VerifyTransactionPIN(ctx, f.ID, "9999"); !errors.Is(err, want) {
			t.Fatalf("attempt %d = %v", i, err)
		}
	}
	if err := NewWalletRepository(db).VerifyTransactionPIN(ctx, f.ID, "0456"); !errors.Is(err, ErrTransactionPINLocked) {
		t.Fatal("a new session or correct PIN bypassed the active lockout")
	}
	if _, err := db.Exec(`UPDATE wallets SET transaction_pin_locked_until = $1 WHERE farmer_id = $2`, time.Now().Add(-time.Minute), f.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyTransactionPIN(ctx, f.ID, "0456"); err != nil {
		t.Fatal("correct PIN rejected after lockout expired")
	}
	var attempts int
	var locked sql.NullTime
	if err := db.QueryRow(`SELECT transaction_pin_failed_attempts, transaction_pin_locked_until FROM wallets WHERE farmer_id = $1`, f.ID).Scan(&attempts, &locked); err != nil || attempts != 0 || locked.Valid {
		t.Fatal("successful verification did not clear expired lockout")
	}
}

func TestConcurrentWalletPINGuessesCannotBypassLockout(t *testing.T) {
	db := testDB(t)
	f := verifiedPINOwner(t, db)
	repo, ctx := NewWalletRepository(db), context.Background()
	if err := repo.SetTransactionPIN(ctx, f.ID, testPINHash(t, "0456")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.VerifyTransactionPIN(ctx, f.ID, "9999") }()
	}
	wg.Wait()
	close(results)
	incorrect, locked := 0, 0
	for err := range results {
		switch {
		case errors.Is(err, ErrTransactionPINIncorrect):
			incorrect++
		case errors.Is(err, ErrTransactionPINLocked):
			locked++
		default:
			t.Fatalf("concurrent PIN check = %v", err)
		}
	}
	if incorrect != 4 || locked != 4 {
		t.Fatalf("incorrect = %d, locked = %d", incorrect, locked)
	}
	if err := repo.VerifyTransactionPIN(ctx, f.ID, "0456"); !errors.Is(err, ErrTransactionPINLocked) {
		t.Fatal("concurrent guesses did not lock the wallet PIN")
	}
}

func TestAccountClosureRemovesWalletPIN(t *testing.T) {
	db := testDB(t)
	f := verifiedPINOwner(t, db)
	ctx := context.Background()
	if err := NewWalletRepository(db).SetTransactionPIN(ctx, f.ID, testPINHash(t, "0456")); err != nil {
		t.Fatal(err)
	}
	if err := NewFarmerRepository(db).DeleteAccount(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	var hash sql.NullString
	if err := db.QueryRow(`SELECT transaction_pin_hash FROM wallets WHERE farmer_id = $1`, f.ID).Scan(&hash); err != nil || hash.Valid {
		t.Fatal("closed wallet retained its PIN")
	}
}
