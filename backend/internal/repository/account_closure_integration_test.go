package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

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

func TestDeleteAccountRefusesWhileMoneyIsAttached(t *testing.T) {
	db := testDB(t)
	repo := NewFarmerRepository(db)
	ctx := context.Background()

	t.Run("balance", func(t *testing.T) {
		farmer := createTestUser(t, db)
		fundWallet(t, db, farmer.ID, "10.00")

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasBalance) {
			t.Fatalf("error = %v, want ErrAccountHasBalance", err)
		}
		assertStillActive(t, repo, farmer.ID)
	})

	t.Run("pending deposit", func(t *testing.T) {
		farmer := createTestUser(t, db)
		if err := NewWalletRepository(db).CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, uniqueRef("CLOSE"), "5.00"); err != nil {
			t.Fatalf("create pending deposit: %v", err)
		}

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasPendingRequest) {
			t.Fatalf("error = %v, want ErrAccountHasPendingRequest", err)
		}
		assertStillActive(t, repo, farmer.ID)
	})

	t.Run("funding account", func(t *testing.T) {
		farmer := createTestUser(t, db)
		if _, err := db.Exec(`
			INSERT INTO wallet_virtual_accounts (farmer_id, tx_ref, status)
			VALUES ($1, $2, 'pending')`, farmer.ID, uniqueRef("VA")); err != nil {
			t.Fatalf("create funding account: %v", err)
		}

		if err := repo.DeleteAccount(ctx, farmer.ID); !errors.Is(err, ErrAccountHasFundingAccount) {
			t.Fatalf("error = %v, want ErrAccountHasFundingAccount", err)
		}
		assertStillActive(t, repo, farmer.ID)
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
