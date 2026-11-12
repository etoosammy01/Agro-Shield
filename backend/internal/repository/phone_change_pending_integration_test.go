package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"backend/internal/models"
)

func TestStagePhoneChangeStagesPendingNumber(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	otpRepo := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := repo.StagePhoneChange(ctx, farmer.ID, newPhone); err != nil {
		t.Fatalf("stage phone change: %v", err)
	}
	if err := repo.StagePhoneChange(ctx, farmer.ID, farmer.Phone); !errors.Is(err, ErrPhoneAlreadyCurrent) {
		t.Fatalf("stage current phone error = %v, want ErrPhoneAlreadyCurrent", err)
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if stored.Phone != farmer.Phone {
		t.Fatalf("verified phone = %q, want unchanged %q", stored.Phone, farmer.Phone)
	}
	if !stored.PendingPhone.Valid || stored.PendingPhone.String != newPhone {
		t.Fatalf("pending_phone = %#v, want %q", stored.PendingPhone, newPhone)
	}
	if err := otpRepo.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
}

func TestStagePhoneChangeRejectsOwnedNumber(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	owner := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := repo.StagePhoneChange(ctx, farmer.ID, newPhone); err != nil {
		t.Fatalf("stage phone change: %v", err)
	}
	if err := repo.StagePhoneChange(ctx, farmer.ID, owner.Phone); !errors.Is(err, ErrPhoneExists) {
		t.Fatalf("stage owned phone error = %v, want ErrPhoneExists", err)
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if !stored.PendingPhone.Valid || stored.PendingPhone.String != newPhone {
		t.Fatalf("pending_phone = %#v, want unchanged %q", stored.PendingPhone, newPhone)
	}
	if stored.Phone != farmer.Phone {
		t.Fatalf("verified phone = %q, want unchanged %q", stored.Phone, farmer.Phone)
	}
}

func TestStagePhoneChangeReplacesPendingNumberAndInvalidatesOtp(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	otpRepo := NewOTPRepository(db)
	ctx := context.Background()
	firstPhone := farmer.Phone + "1"
	secondPhone := farmer.Phone + "2"

	if err := repo.StagePhoneChange(ctx, farmer.ID, firstPhone); err != nil {
		t.Fatalf("stage first phone change: %v", err)
	}
	if err := otpRepo.Issue(ctx, farmer.ID, firstPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue first phone-change OTP: %v", err)
	}
	code, err := otpRepo.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatalf("load first phone-change OTP: %v", err)
	}
	if err := repo.StagePhoneChange(ctx, farmer.ID, secondPhone); err != nil {
		t.Fatalf("replace pending phone change: %v", err)
	}
	var consumed sql.NullTime
	if err := db.QueryRow(`SELECT consumed_at FROM otp_codes WHERE id = $1`, code.ID).Scan(&consumed); err != nil {
		t.Fatalf("load consumed OTP: %v", err)
	}
	if !consumed.Valid {
		t.Fatal("replacing pending phone did not consume the previous OTP")
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if !stored.PendingPhone.Valid || stored.PendingPhone.String != secondPhone {
		t.Fatalf("pending_phone = %#v, want %q", stored.PendingPhone, secondPhone)
	}
	if stored.Phone != farmer.Phone {
		t.Fatalf("verified phone = %q, want unchanged %q", stored.Phone, farmer.Phone)
	}
}

func TestStagePhoneChangeRestagesSameNumberKeepsOtpActive(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	otpRepo := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := repo.StagePhoneChange(ctx, farmer.ID, newPhone); err != nil {
		t.Fatalf("stage phone change: %v", err)
	}
	if err := otpRepo.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
	code, err := otpRepo.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatalf("load phone-change OTP: %v", err)
	}
	if err := repo.StagePhoneChange(ctx, farmer.ID, newPhone); err != nil {
		t.Fatalf("re-stage same pending phone: %v", err)
	}
	var consumed sql.NullTime
	if err := db.QueryRow(`SELECT consumed_at FROM otp_codes WHERE id = $1`, code.ID).Scan(&consumed); err != nil {
		t.Fatalf("load OTP: %v", err)
	}
	if consumed.Valid {
		t.Fatal("re-staging the same pending phone consumed the active OTP")
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if !stored.PendingPhone.Valid || stored.PendingPhone.String != newPhone {
		t.Fatalf("pending_phone = %#v, want %q", stored.PendingPhone, newPhone)
	}
}

func TestCancelPhoneChangeClearsPendingAndInvalidatesOtp(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	otpRepo := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := repo.StagePhoneChange(ctx, farmer.ID, newPhone); err != nil {
		t.Fatalf("stage phone change: %v", err)
	}
	if err := otpRepo.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
	if err := repo.CancelPhoneChange(ctx, farmer.ID); err != nil {
		t.Fatalf("cancel phone change: %v", err)
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if stored.PendingPhone.Valid || stored.PendingPhone.String != "" {
		t.Fatalf("pending_phone = %#v, want NULL", stored.PendingPhone)
	}
	var consumed sql.NullTime
	if err := db.QueryRow(`SELECT consumed_at FROM otp_codes WHERE farmer_id = $1 AND purpose = $2`, farmer.ID, models.OTPPurposePhoneChange).Scan(&consumed); err != nil {
		t.Fatalf("load OTP: %v", err)
	}
	if !consumed.Valid {
		t.Fatal("canceling phone change did not invalidate the OTP")
	}
	if err := repo.CancelPhoneChange(ctx, farmer.ID); !errors.Is(err, ErrNoPendingPhoneChange) {
		t.Fatalf("cancel without pending error = %v, want ErrNoPendingPhoneChange", err)
	}
}

func TestUpdateProfileDetailsDoesNotChangePhone(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewFarmerRepository(db)
	if err := repo.UpdateProfileDetails(
		farmer.ID,
		"Updated Name",
		"updated@example.com",
		"Community",
		"LGA",
		"Kano",
		"Nigeria",
	); err != nil {
		t.Fatalf("update profile details: %v", err)
	}
	stored, err := repo.GetByID(farmer.ID)
	if err != nil {
		t.Fatalf("load farmer: %v", err)
	}
	if stored.FullName != "Updated Name" || stored.Email != "updated@example.com" {
		t.Fatalf("profile details not updated: %+v", stored)
	}
	if stored.Phone != farmer.Phone {
		t.Fatalf("verified phone = %q, want unchanged %q", stored.Phone, farmer.Phone)
	}
	if stored.PendingPhone.Valid {
		t.Fatalf("profile update changed pending_phone = %q", stored.PendingPhone.String)
	}
}
