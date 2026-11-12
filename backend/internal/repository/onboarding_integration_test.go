package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"backend/internal/database"
	"backend/internal/models"
)

func TestOnboardingMigrationPreservesExistingSteps(t *testing.T) {
	db := testDB(t)
	f := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	stored, err := repo.GetByID(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SelfieCapturedAt.Valid || stored.PhoneVerifiedAt.Valid {
		t.Fatal("existing account was automatically verified")
	}
	if err := repo.UpdateSelfie(f.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.RunOnboardingMigration(db); err != nil {
			t.Fatal(err)
		}
	}
	stored, err = repo.GetByPhone(f.Phone)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.SelfieCapturedAt.Valid || stored.PhoneVerifiedAt.Valid || stored.PhotoURL != "/static/uploads/farmers/selfie-test.jpg" {
		t.Fatal("migration did not preserve completed selfie")
	}
}

func TestPhoneVerificationAtomicConsumptionAndPhoneChange(t *testing.T) {
	db := testDB(t)
	f := createTestUser(t, db)
	farmers, otp := NewFarmerRepository(db), NewOTPRepository(db)
	ctx := context.Background()
	issue := func(purpose string) int64 {
		t.Helper()
		if err := otp.Issue(ctx, f.ID, f.Phone, purpose, "test-hash", time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		code, err := otp.LatestActive(ctx, f.ID, purpose)
		if err != nil {
			t.Fatal(err)
		}
		return code.ID
	}
	id := issue(models.OTPPurposePhoneVerification)
	if err := otp.ConsumeAndVerifyPhone(ctx, id, f.ID, f.Phone); !errors.Is(err, ErrNoActiveOTP) {
		t.Fatalf("verification allowed before selfie: %v", err)
	}
	if _, err := otp.LatestActive(ctx, f.ID, models.OTPPurposePhoneVerification); err != nil {
		t.Fatal("failed transaction consumed code")
	}
	if err := farmers.UpdateSelfie(f.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- otp.ConsumeAndVerifyPhone(ctx, id, f.ID, f.Phone) }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrNoActiveOTP) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("challenge spent %d times", successes)
	}
	stored, err := farmers.GetByID(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.PhoneVerifiedAt.Valid {
		t.Fatal("phone verification not persisted")
	}
	if err := farmers.UpdateProfileDetails(f.ID, f.FullName, f.Email, f.Location, f.LGA, f.State, f.Country); err != nil {
		t.Fatal(err)
	}
	stored, err = farmers.GetByID(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.PhoneVerifiedAt.Valid {
		t.Fatal("unchanged phone lost verification")
	}
	newPhone := f.Phone + "1"
	if err := farmers.StagePhoneChange(ctx, f.ID, newPhone); err != nil {
		t.Fatal(err)
	}
	if err := otp.Issue(ctx, f.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	changeCode, err := otp.LatestActive(ctx, f.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatal(err)
	}
	if err := otp.ConsumeAndApplyPhoneChange(ctx, changeCode.ID, f.ID, newPhone); err != nil {
		t.Fatal(err)
	}
	stored, err = farmers.GetByID(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Phone != newPhone {
		t.Fatalf("phone = %q, want %q", stored.Phone, newPhone)
	}
	if stored.PendingPhone.Valid {
		t.Fatal("phone change left a pending phone")
	}
	if !stored.PhoneVerifiedAt.Valid {
		t.Fatal("phone change did not mark the new number verified")
	}
	if _, err := otp.LatestActive(ctx, f.ID, models.OTPPurposePhoneChange); !errors.Is(err, ErrNoActiveOTP) {
		t.Fatal("phone change left active challenge")
	}
}

func TestInvalidatingFailedDeliveryPreservesNewerOTPAndIssuanceAudit(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	store := NewOTPRepository(db)
	ctx := context.Background()
	purpose := models.OTPPurposeTransfer
	for _, hash := range []string{"older-delivery-hash", "newer-resend-hash"} {
		if err := store.Issue(ctx, farmer.ID, farmer.Phone, purpose, hash, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Invalidate(ctx, farmer.ID, purpose, "older-delivery-hash"); err != nil {
		t.Fatal(err)
	}
	active, err := store.LatestActive(ctx, farmer.ID, purpose)
	if err != nil || active.CodeHash != "newer-resend-hash" {
		t.Fatalf("older delivery failure invalidated the newer challenge: %v", err)
	}
	if err := store.Invalidate(ctx, farmer.ID, purpose, "newer-resend-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LatestActive(ctx, farmer.ID, purpose); !errors.Is(err, ErrNoActiveOTP) {
		t.Fatalf("failed delivery challenge remained active: %v", err)
	}
	if count, err := store.CountIssuedSince(ctx, farmer.ID, purpose, time.Now().Add(-time.Minute)); err != nil || count != 2 {
		t.Fatalf("failed delivery erased the issuance audit: count = %d, error = %v", count, err)
	}
}

func TestPhoneVerificationRejectsExpiredExhaustedAndMismatchedChallenges(t *testing.T) {
	db := testDB(t)
	f := createTestUser(t, db)
	farmers, otp := NewFarmerRepository(db), NewOTPRepository(db)
	if err := farmers.UpdateSelfie(f.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		expires  time.Time
		attempts int
		phone    string
	}{
		{"expired", time.Now().Add(-time.Minute), 0, f.Phone},
		{"exhausted", time.Now().Add(time.Minute), 5, f.Phone},
		{"different phone", time.Now().Add(time.Minute), 0, f.Phone + "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var id int64
			err := db.QueryRow(`INSERT INTO otp_codes(farmer_id,phone,purpose,code_hash,expires_at,attempts) VALUES($1,$2,$3,'test-hash',$4,$5) RETURNING id`, f.ID, tc.phone, models.OTPPurposePhoneVerification, tc.expires, tc.attempts).Scan(&id)
			if err != nil {
				t.Fatal(err)
			}
			if err := otp.ConsumeAndVerifyPhone(context.Background(), id, f.ID, f.Phone); !errors.Is(err, ErrNoActiveOTP) {
				t.Fatalf("invalid challenge accepted: %v", err)
			}
			stored, err := farmers.GetByID(f.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.PhoneVerifiedAt.Valid {
				t.Fatal("invalid challenge verified phone")
			}
		})
	}
}
