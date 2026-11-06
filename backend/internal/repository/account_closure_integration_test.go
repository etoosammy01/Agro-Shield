package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"backend/internal/models"
)

// Closing an account must remove the personal profile and sign-in without
// destroying records that other people or the ledger depend on. These tests
// run against PostgreSQL because the guarantees are transactional: the
// refusals, the anonymisation, and the preserved order all rely on row locks
// and foreign keys.
func TestDeleteAccountAnonymisesAndKeepsRecords(t *testing.T) {
	db := testDB(t)
	repo := NewFarmerRepository(db)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	buyer := createTestUser(t, db)
	originalPhone := farmer.Phone

	if _, err := db.Exec(`
		UPDATE farmers
		SET email = 'close-me@example.test', full_name = 'Close Me',
			bank_name = 'Test Bank', bank_code = '999',
			account_name = 'Close Me', account_number = '0123456789'
		WHERE id = $1`, farmer.ID); err != nil {
		t.Fatalf("seed personal details: %v", err)
	}

	soldCrop := createListedCrop(t, db, farmer.ID, "Sold Maize", "Kano", 100, true)
	looseCrop := createListedCrop(t, db, farmer.ID, "Unsold Beans", "Kano", 50, true)
	t.Cleanup(func() { cleanupCrops(t, db, farmer.ID) })

	var orderID int
	if err := db.QueryRow(`
		INSERT INTO orders (buyer_id, crop_id, quantity, total_price, status)
		VALUES ($1, $2, 10, 5000, 'pending') RETURNING id`,
		buyer.ID, soldCrop).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM orders WHERE id = $1`, orderID); err != nil {
			t.Logf("order cleanup: %v", err)
		}
	})

	if err := repo.DeleteAccount(ctx, farmer.ID); err != nil {
		t.Fatalf("close account: %v", err)
	}

	// The profile row is still there for references, but the person is gone.
	closed, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("read closed account: %v", err)
	}
	if closed != nil {
		t.Fatalf("closed account still loads as active: %+v", closed)
	}

	var name, phone, hash, photo, location string
	var email, bankName, bankCode, accountName, accountNumber sql.NullString
	var deletedAt sql.NullTime
	if err := db.QueryRow(`
		SELECT full_name, phone, password_hash, photo_url, location,
			email, bank_name, bank_code, account_name, account_number, deleted_at
		FROM farmers WHERE id = $1`, farmer.ID,
	).Scan(&name, &phone, &hash, &photo, &location, &email, &bankName,
		&bankCode, &accountName, &accountNumber, &deletedAt); err != nil {
		t.Fatalf("read anonymised row: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("deleted_at was not set")
	}
	if name != "Deleted account" || location != "Deleted account" {
		t.Errorf("name/location = %q/%q, want anonymised values", name, location)
	}
	if phone == originalPhone || phone == "" {
		t.Errorf("phone = %q, want a non-reusable placeholder", phone)
	}
	if hash != "deleted" {
		t.Errorf("password_hash = %q, want 'deleted'", hash)
	}
	if photo != defaultPhotoURL {
		t.Errorf("photo_url = %q, want %q", photo, defaultPhotoURL)
	}
	for label, value := range map[string]sql.NullString{
		"email": email, "bank_name": bankName, "bank_code": bankCode,
		"account_name": accountName, "account_number": accountNumber,
	} {
		if value.Valid {
			t.Errorf("%s was not cleared: %q", label, value.String)
		}
	}

	if found, err := repo.GetByPhone(originalPhone); err != nil || found != nil {
		t.Fatalf("closed phone still loads: user=%+v err=%v", found, err)
	}

	// A listing with an order stays attached to that order but is no longer
	// for sale; a listing nobody ordered is removed.
	var listed bool
	if err := db.QueryRow(`SELECT listed_for_sale FROM crops WHERE id = $1`, soldCrop).Scan(&listed); err != nil {
		t.Fatalf("ordered crop should be kept: %v", err)
	}
	if listed {
		t.Error("ordered crop is still listed for sale after closure")
	}
	var looseExists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM crops WHERE id = $1)`, looseCrop).Scan(&looseExists); err != nil {
		t.Fatalf("check loose crop: %v", err)
	}
	if looseExists {
		t.Error("unsold crop was not removed")
	}

	var orderTotal float64
	if err := db.QueryRow(`SELECT total_price FROM orders WHERE id = $1`, orderID).Scan(&orderTotal); err != nil {
		t.Fatalf("order history was removed: %v", err)
	}
	if orderTotal != 5000 {
		t.Errorf("order total = %v, want 5000", orderTotal)
	}

	var walletRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wallets WHERE farmer_id = $1`, farmer.ID).Scan(&walletRows); err != nil {
		t.Fatalf("count wallet: %v", err)
	}
	if walletRows != 1 {
		t.Errorf("wallet rows = %d, want the ledger wallet kept", walletRows)
	}

	// A repeat request must not act again or report success.
	if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("second closure error = %v, want ErrAccountNotFound", err)
	}

	// The old number is free for a new registration; the anonymised row's
	// placeholder can never collide with a real one.
	reused := &models.Farmer{
		FullName:     "New Owner",
		Phone:        originalPhone,
		Location:     "Kano",
		State:        "Kano",
		Country:      "Nigeria",
		Role:         "farmer",
		PasswordHash: "test-password-hash",
		PhotoURL:     "/static/assets/images/un.jpeg",
	}
	if err := repo.Create(reused); err != nil {
		t.Fatalf("re-register freed phone: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM wallets WHERE farmer_id = $1`, reused.ID); err != nil {
			t.Logf("reused wallet cleanup: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM farmers WHERE id = $1`, reused.ID); err != nil {
			t.Logf("reused farmer cleanup: %v", err)
		}
	})
}

func TestDeleteAccountRevokesAllSessions(t *testing.T) {
	db := testDB(t)
	repo := NewFarmerRepository(db)
	sessions := NewSessionRepository(db)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	otherFarmer := createTestUser(t, db)

	farmerSessions := []string{
		createSessionForTest(t, db, farmer.ID),
		createSessionForTest(t, db, farmer.ID),
	}
	otherSession := createSessionForTest(t, db, otherFarmer.ID)

	if err := repo.DeleteAccount(ctx, farmer.ID); err != nil {
		t.Fatalf("close account: %v", err)
	}

	for _, tokenHash := range farmerSessions {
		assertSessionRevoked(t, sessions, tokenHash)
	}

	// Revocation keeps the rows; it must not delete the audit records.
	var total, revoked int
	if err := db.QueryRow(`
		SELECT COUNT(*), COUNT(revoked_at)
		FROM sessions
		WHERE farmer_id = $1`, farmer.ID).Scan(&total, &revoked); err != nil {
		t.Fatalf("count closed account sessions: %v", err)
	}
	if total != len(farmerSessions) {
		t.Errorf("closed account session rows = %d, want %d", total, len(farmerSessions))
	}
	if revoked != total {
		t.Errorf("revoked sessions = %d, want all %d", revoked, total)
	}

	assertSessionValid(t, sessions, otherSession, otherFarmer.ID)
}

func TestDeleteAccountFailureDoesNotPartiallyRevokeSessions(t *testing.T) {
	db := testDB(t)
	repo := NewFarmerRepository(db)
	sessions := NewSessionRepository(db)
	ctx := context.Background()

	farmer := createTestUser(t, db)
	tokenHash := createSessionForTest(t, db, farmer.ID)

	// Claim the placeholder phone the anonymisation step writes, so closure
	// fails after the session revocation statement has run.
	var blockerID int
	if err := db.QueryRow(`
		INSERT INTO farmers (full_name, phone, password_hash, location, country)
		VALUES ('Placeholder blocker', $1, 'test-password-hash', 'Kano', 'Nigeria')
		RETURNING id`, fmt.Sprintf("deleted-%d", farmer.ID)).Scan(&blockerID); err != nil {
		t.Fatalf("create placeholder blocker: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM farmers WHERE id = $1`, blockerID); err != nil {
			t.Logf("blocker cleanup: %v", err)
		}
	})

	if err := repo.DeleteAccount(ctx, farmer.ID); err == nil {
		t.Fatal("DeleteAccount succeeded despite conflicting placeholder phone")
	}

	assertStillActive(t, repo, farmer.ID)
	assertSessionValid(t, sessions, tokenHash, farmer.ID)

	var revoked sql.NullTime
	if err := db.QueryRow(
		`SELECT revoked_at FROM sessions WHERE session_token_hash = $1`,
		tokenHash,
	).Scan(&revoked); err != nil {
		t.Fatalf("read session revocation: %v", err)
	}
	if revoked.Valid {
		t.Fatal("failed closure left the session revoked")
	}
}

func TestDeleteAccountRefusesWhileMoneyIsAttached(t *testing.T) {
	db := testDB(t)
	repo := NewFarmerRepository(db)
	ctx := context.Background()

	t.Run("balance", func(t *testing.T) {
		farmer := createTestUser(t, db)
		sessions := NewSessionRepository(db)
		tokenHash := createSessionForTest(t, db, farmer.ID)
		fundWallet(t, db, farmer.ID, "10.00")

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasBalance) {
			t.Fatalf("error = %v, want ErrAccountHasBalance", err)
		}
		assertStillActive(t, repo, farmer.ID)
		assertSessionValid(t, sessions, tokenHash, farmer.ID)
	})

	t.Run("pending deposit", func(t *testing.T) {
		farmer := createTestUser(t, db)
		sessions := NewSessionRepository(db)
		tokenHash := createSessionForTest(t, db, farmer.ID)
		if err := NewWalletRepository(db).CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, uniqueRef("CLOSE"), "5.00"); err != nil {
			t.Fatalf("create pending deposit: %v", err)
		}

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasPendingRequest) {
			t.Fatalf("error = %v, want ErrAccountHasPendingRequest", err)
		}
		assertStillActive(t, repo, farmer.ID)
		assertSessionValid(t, sessions, tokenHash, farmer.ID)
	})

	t.Run("funding account", func(t *testing.T) {
		farmer := createTestUser(t, db)
		sessions := NewSessionRepository(db)
		tokenHash := createSessionForTest(t, db, farmer.ID)
		if _, err := db.Exec(`
			INSERT INTO wallet_virtual_accounts (farmer_id, tx_ref, status)
			VALUES ($1, $2, 'pending')`, farmer.ID, uniqueRef("VA")); err != nil {
			t.Fatalf("create funding account: %v", err)
		}

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasFundingAccount) {
			t.Fatalf("error = %v, want ErrAccountHasFundingAccount", err)
		}
		assertStillActive(t, repo, farmer.ID)
		assertSessionValid(t, sessions, tokenHash, farmer.ID)
	})
}

func assertStillActive(t *testing.T, repo *FarmerRepository, farmerID int) {
	t.Helper()

	farmer, err := repo.GetByID(farmerID)
	if err != nil {
		t.Fatalf("read account after refusal: %v", err)
	}
	if farmer == nil {
		t.Fatal("account was closed despite the refusal")
	}
}

func createSessionForTest(t *testing.T, db *sql.DB, farmerID int) string {
	t.Helper()

	tokenHash := uniqueRef("SESSION")
	if err := NewSessionRepository(db).Create(
		context.Background(),
		tokenHash,
		farmerID,
		time.Now().Add(24*time.Hour),
	); err != nil {
		t.Fatalf("create session for farmer %d: %v", farmerID, err)
	}
	return tokenHash
}

func assertSessionValid(t *testing.T, repo *SessionRepository, tokenHash string, farmerID int) {
	t.Helper()

	gotFarmerID, err := repo.FindValid(context.Background(), tokenHash)
	if err != nil {
		t.Fatalf("session %q should remain valid: %v", tokenHash, err)
	}
	if gotFarmerID != farmerID {
		t.Errorf("session farmer ID = %d, want %d", gotFarmerID, farmerID)
	}
}

func assertSessionRevoked(t *testing.T, repo *SessionRepository, tokenHash string) {
	t.Helper()

	if _, err := repo.FindValid(context.Background(), tokenHash); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("session %q error = %v, want ErrSessionNotFound", tokenHash, err)
	}
}
