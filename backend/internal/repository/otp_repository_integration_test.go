package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"backend/internal/models"
)

func setPendingPhone(t *testing.T, db *sql.DB, farmerID int, phone string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE farmers SET pending_phone = $1 WHERE id = $2`, phone, farmerID); err != nil {
		t.Fatalf("set pending phone: %v", err)
	}
}

func loadFarmerPhoneState(t *testing.T, db *sql.DB, farmerID int) (string, sql.NullString, sql.NullTime) {
	t.Helper()
	var phone string
	var pending sql.NullString
	var verified sql.NullTime
	if err := db.QueryRow(`SELECT phone, pending_phone, phone_verified_at
		FROM farmers WHERE id = $1`, farmerID).Scan(&phone, &pending, &verified); err != nil {
		t.Fatalf("load farmer phone state: %v", err)
	}
	return phone, pending, verified
}

func otpConsumedAt(t *testing.T, db *sql.DB, id int64) sql.NullTime {
	t.Helper()
	var consumed sql.NullTime
	if err := db.QueryRow(`SELECT consumed_at FROM otp_codes WHERE id = $1`, id).Scan(&consumed); err != nil {
		t.Fatalf("load OTP consumed_at: %v", err)
	}
	return consumed
}

func TestConsumeAndApplyPhoneChangeSuccess(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := farmers.UpdateSelfie(farmer.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatalf("update selfie: %v", err)
	}
	setPendingPhone(t, db, farmer.ID, newPhone)
	if err := otp.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
	code, err := otp.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatalf("load active phone-change OTP: %v", err)
	}

	if err := otp.ConsumeAndApplyPhoneChange(ctx, code.ID, farmer.ID, newPhone); err != nil {
		t.Fatalf("apply phone change: %v", err)
	}

	phone, pending, verified := loadFarmerPhoneState(t, db, farmer.ID)
	if phone != newPhone {
		t.Fatalf("phone = %q, want %q", phone, newPhone)
	}
	if pending.Valid {
		t.Fatalf("pending_phone = %q, want NULL", pending.String)
	}
	if !verified.Valid {
		t.Fatal("phone_verified_at is NULL")
	}
	if consumed := otpConsumedAt(t, db, code.ID); !consumed.Valid {
		t.Fatal("OTP consumed_at is NULL")
	}
}

func TestConsumeAndApplyPhoneChangeRejectsWrongPurpose(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := farmers.UpdateSelfie(farmer.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatalf("update selfie: %v", err)
	}
	setPendingPhone(t, db, farmer.ID, newPhone)
	if err := otp.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneVerification, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue verification OTP: %v", err)
	}
	code, err := otp.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneVerification)
	if err != nil {
		t.Fatalf("load active verification OTP: %v", err)
	}

	err = otp.ConsumeAndApplyPhoneChange(ctx, code.ID, farmer.ID, newPhone)
	if !errors.Is(err, ErrNoActiveOTP) {
		t.Fatalf("ConsumeAndApplyPhoneChange = %v, want ErrNoActiveOTP", err)
	}

	phone, pending, verified := loadFarmerPhoneState(t, db, farmer.ID)
	if phone != farmer.Phone {
		t.Fatalf("phone = %q, want unchanged %q", phone, farmer.Phone)
	}
	if !pending.Valid || pending.String != newPhone {
		t.Fatalf("pending_phone = %#v, want %q", pending, newPhone)
	}
	if verified.Valid {
		t.Fatalf("phone_verified_at = %v, want NULL", verified.Time)
	}
	if consumed := otpConsumedAt(t, db, code.ID); consumed.Valid {
		t.Fatal("OTP was consumed by a failed attempt")
	}
}

func TestConsumeAndApplyPhoneChangeRejectsExpiredOTP(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"

	if err := farmers.UpdateSelfie(farmer.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatalf("update selfie: %v", err)
	}
	setPendingPhone(t, db, farmer.ID, newPhone)
	var codeID int64
	if err := db.QueryRow(`
		INSERT INTO otp_codes(farmer_id, phone, purpose, code_hash, expires_at)
		VALUES($1, $2, $3, 'test-hash', $4) RETURNING id`,
		farmer.ID, newPhone, models.OTPPurposePhoneChange, time.Now().Add(-time.Minute),
	).Scan(&codeID); err != nil {
		t.Fatalf("insert expired OTP: %v", err)
	}

	err := otp.ConsumeAndApplyPhoneChange(ctx, codeID, farmer.ID, newPhone)
	if !errors.Is(err, ErrNoActiveOTP) {
		t.Fatalf("ConsumeAndApplyPhoneChange = %v, want ErrNoActiveOTP", err)
	}

	phone, pending, verified := loadFarmerPhoneState(t, db, farmer.ID)
	if phone != farmer.Phone {
		t.Fatalf("phone = %q, want unchanged %q", phone, farmer.Phone)
	}
	if !pending.Valid || pending.String != newPhone {
		t.Fatalf("pending_phone = %#v, want %q", pending, newPhone)
	}
	if verified.Valid {
		t.Fatalf("phone_verified_at = %v, want NULL", verified.Time)
	}
	if consumed := otpConsumedAt(t, db, codeID); consumed.Valid {
		t.Fatal("expired OTP was consumed")
	}
}

func TestConsumeAndApplyPhoneChangeRejectsPhoneAlreadyOwned(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	owner := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()

	if err := farmers.UpdateSelfie(farmer.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatalf("update selfie: %v", err)
	}
	setPendingPhone(t, db, farmer.ID, owner.Phone)
	if err := otp.Issue(ctx, farmer.ID, owner.Phone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
	code, err := otp.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatalf("load active phone-change OTP: %v", err)
	}

	err = otp.ConsumeAndApplyPhoneChange(ctx, code.ID, farmer.ID, owner.Phone)
	if !errors.Is(err, ErrPhoneExists) {
		t.Fatalf("ConsumeAndApplyPhoneChange = %v, want ErrPhoneExists", err)
	}

	phone, pending, _ := loadFarmerPhoneState(t, db, farmer.ID)
	if phone != farmer.Phone {
		t.Fatalf("first phone = %q, want unchanged %q", phone, farmer.Phone)
	}
	if !pending.Valid || pending.String != owner.Phone {
		t.Fatalf("first pending_phone = %#v, want %q", pending, owner.Phone)
	}
	ownerPhone, _, _ := loadFarmerPhoneState(t, db, owner.ID)
	if ownerPhone != owner.Phone {
		t.Fatalf("second phone = %q, want unchanged %q", ownerPhone, owner.Phone)
	}
	if consumed := otpConsumedAt(t, db, code.ID); consumed.Valid {
		t.Fatal("OTP was consumed by a rejected change")
	}
}

func TestConsumeAndApplyPhoneChangeRejectsWrongPendingPhone(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()
	newPhone := farmer.Phone + "1"
	differentPhone := newPhone + "1"

	if err := farmers.UpdateSelfie(farmer.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
		t.Fatalf("update selfie: %v", err)
	}
	setPendingPhone(t, db, farmer.ID, newPhone)
	if err := otp.Issue(ctx, farmer.ID, newPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("issue phone-change OTP: %v", err)
	}
	code, err := otp.LatestActive(ctx, farmer.ID, models.OTPPurposePhoneChange)
	if err != nil {
		t.Fatalf("load active phone-change OTP: %v", err)
	}

	err = otp.ConsumeAndApplyPhoneChange(ctx, code.ID, farmer.ID, differentPhone)
	if !errors.Is(err, ErrNoActiveOTP) {
		t.Fatalf("ConsumeAndApplyPhoneChange = %v, want ErrNoActiveOTP", err)
	}

	phone, pending, verified := loadFarmerPhoneState(t, db, farmer.ID)
	if phone != farmer.Phone {
		t.Fatalf("phone = %q, want unchanged %q", phone, farmer.Phone)
	}
	if !pending.Valid || pending.String != newPhone {
		t.Fatalf("pending_phone = %#v, want %q", pending, newPhone)
	}
	if verified.Valid {
		t.Fatalf("phone_verified_at = %v, want NULL", verified.Time)
	}
	if consumed := otpConsumedAt(t, db, code.ID); consumed.Valid {
		t.Fatal("OTP was consumed by a rejected change")
	}
}

func TestConsumeAndApplyPhoneChangeConcurrentPhoneClaim(t *testing.T) {
	db := testDB(t)
	first := createTestUser(t, db)
	second := createTestUser(t, db)
	farmers := NewFarmerRepository(db)
	otp := NewOTPRepository(db)
	ctx := context.Background()
	sharedPhone := first.Phone + "1"

	for _, f := range []*models.Farmer{first, second} {
		if err := farmers.UpdateSelfie(f.ID, "/static/uploads/farmers/selfie-test.jpg"); err != nil {
			t.Fatalf("update selfie for farmer %d: %v", f.ID, err)
		}
		setPendingPhone(t, db, f.ID, sharedPhone)
	}
	issue := func(f *models.Farmer) int64 {
		t.Helper()
		if err := otp.Issue(ctx, f.ID, sharedPhone, models.OTPPurposePhoneChange, "test-hash", time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("issue phone-change OTP for farmer %d: %v", f.ID, err)
		}
		code, err := otp.LatestActive(ctx, f.ID, models.OTPPurposePhoneChange)
		if err != nil {
			t.Fatalf("load active phone-change OTP for farmer %d: %v", f.ID, err)
		}
		return code.ID
	}
	firstCode := issue(first)
	secondCode := issue(second)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- otp.ConsumeAndApplyPhoneChange(ctx, firstCode, first.ID, sharedPhone)
	}()
	go func() {
		defer wg.Done()
		errs <- otp.ConsumeAndApplyPhoneChange(ctx, secondCode, second.ID, sharedPhone)
	}()
	wg.Wait()
	close(errs)

	successes, phoneExists := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrPhoneExists):
			phoneExists++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful claims = %d, want 1", successes)
	}
	if phoneExists != 1 {
		t.Fatalf("ErrPhoneExists results = %d, want 1", phoneExists)
	}

	firstPhone, firstPending, firstVerified := loadFarmerPhoneState(t, db, first.ID)
	secondPhone, secondPending, secondVerified := loadFarmerPhoneState(t, db, second.ID)
	winner := ""
	switch {
	case firstPhone == sharedPhone && secondPhone == sharedPhone:
		t.Fatal("both farmers claimed the shared phone")
	case firstPhone == sharedPhone:
		winner = "first"
	case secondPhone == sharedPhone:
		winner = "second"
	default:
		t.Fatal("neither farmer claimed the shared phone")
	}

	if winner == "first" {
		if firstPending.Valid {
			t.Fatalf("winner pending_phone = %q, want NULL", firstPending.String)
		}
		if !firstVerified.Valid {
			t.Fatal("winner phone_verified_at is NULL")
		}
		if consumed := otpConsumedAt(t, db, firstCode); !consumed.Valid {
			t.Fatal("winner OTP was not consumed")
		}
		if secondPhone != second.Phone {
			t.Fatalf("loser phone = %q, want unchanged %q", secondPhone, second.Phone)
		}
		if !secondPending.Valid || secondPending.String != sharedPhone {
			t.Fatalf("loser pending_phone = %#v, want %q", secondPending, sharedPhone)
		}
		if secondVerified.Valid {
			t.Fatalf("loser phone_verified_at = %v, want NULL", secondVerified.Time)
		}
		if consumed := otpConsumedAt(t, db, secondCode); consumed.Valid {
			t.Fatal("loser OTP was consumed")
		}
		return
	}

	if secondPending.Valid {
		t.Fatalf("winner pending_phone = %q, want NULL", secondPending.String)
	}
	if !secondVerified.Valid {
		t.Fatal("winner phone_verified_at is NULL")
	}
	if consumed := otpConsumedAt(t, db, secondCode); !consumed.Valid {
		t.Fatal("winner OTP was not consumed")
	}
	if firstPhone != first.Phone {
		t.Fatalf("loser phone = %q, want unchanged %q", firstPhone, first.Phone)
	}
	if !firstPending.Valid || firstPending.String != sharedPhone {
		t.Fatalf("loser pending_phone = %#v, want %q", firstPending, sharedPhone)
	}
	if firstVerified.Valid {
		t.Fatalf("loser phone_verified_at = %v, want NULL", firstVerified.Time)
	}
	if consumed := otpConsumedAt(t, db, firstCode); consumed.Valid {
		t.Fatal("loser OTP was consumed")
	}
}
