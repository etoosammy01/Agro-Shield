package otp

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

type mockStore struct {
	active       *models.OTPCode
	issued       int
	verified     bool
	pendingPhone string
	pendingErr   error
	applyErr     error
	applied      bool
}

func (s *mockStore) Issue(_ context.Context, farmerID int, phone, purpose, hash string, expires time.Time) error {
	s.issued++
	s.active = &models.OTPCode{
		ID:        int64(s.issued),
		FarmerID:  farmerID,
		Phone:     phone,
		Purpose:   purpose,
		CodeHash:  hash,
		ExpiresAt: expires,
	}
	return nil
}

func (s *mockStore) Invalidate(ctx context.Context, farmerID int, purpose, hash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.active != nil && s.active.FarmerID == farmerID && s.active.Purpose == purpose && s.active.CodeHash == hash {
		s.active.ConsumedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}
	return nil
}

func (s *mockStore) LatestActive(_ context.Context, farmerID int, purpose string) (*models.OTPCode, error) {
	if s.active == nil || s.active.FarmerID != farmerID || s.active.Purpose != purpose || s.active.ConsumedAt.Valid || !s.active.ExpiresAt.After(time.Now()) {
		return nil, repository.ErrNoActiveOTP
	}
	copy := *s.active
	return &copy, nil
}

func (s *mockStore) IncrementAttempts(_ context.Context, _ int64) error {
	s.active.Attempts++
	return nil
}

func (s *mockStore) Consume(_ context.Context, _ int64) error {
	if s.active == nil || s.active.ConsumedAt.Valid {
		return repository.ErrNoActiveOTP
	}
	s.active.ConsumedAt = sql.NullTime{Time: time.Now(), Valid: true}
	return nil
}

func (s *mockStore) ConsumeAndVerifyPhone(ctx context.Context, id int64, farmerID int, phone string) error {
	if s.active == nil || s.active.FarmerID != farmerID || s.active.Phone != phone {
		return repository.ErrNoActiveOTP
	}
	if err := s.Consume(ctx, id); err != nil {
		return err
	}
	s.verified = true
	return nil
}

func (s *mockStore) ConsumeAndApplyPhoneChange(ctx context.Context, id int64, farmerID int, phone string) error {
	if s.applyErr != nil {
		return s.applyErr
	}
	if s.active == nil || s.active.FarmerID != farmerID || s.active.Phone != phone {
		return repository.ErrNoActiveOTP
	}
	if err := s.Consume(ctx, id); err != nil {
		return err
	}
	s.applied = true
	return nil
}

func (s *mockStore) PendingPhone(_ context.Context, _ int) (string, error) {
	return s.pendingPhone, s.pendingErr
}

func (s *mockStore) CountIssuedSince(_ context.Context, _ int, _ string, _ time.Time) (int, error) {
	return s.issued, nil
}

type mockProvider struct {
	lastPhone   string
	lastCode    string
	lastPurpose string
	sendErr     error
}

func (p *mockProvider) Send(_ context.Context, phone, code, purpose string) error {
	p.lastPhone = phone
	p.lastCode = code
	p.lastPurpose = purpose
	return p.sendErr
}

func TestOTPServiceIssueAndVerify(t *testing.T) {
	store := &mockStore{}
	provider := &mockProvider{}
	svc := NewService(store, provider)

	ctx := context.Background()
	phone := "08012345678"

	if err := svc.Issue(ctx, 1, phone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	if provider.lastCode == "" || len(provider.lastCode) != 6 {
		t.Fatalf("unexpected code issued: %q", provider.lastCode)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(store.active.CodeHash), []byte(provider.lastCode)); err != nil {
		t.Fatalf("stored hash does not match issued code: %v", err)
	}

	// Verify with wrong code
	wrongCode := "000000"
	if provider.lastCode == wrongCode {
		wrongCode = "000001"
	}
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, wrongCode); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got: %v", err)
	}

	// Verify with correct code
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, provider.lastCode); err != nil {
		t.Fatalf("expected verification success, got: %v", err)
	}

	if !store.active.ConsumedAt.Valid {
		t.Fatal("expected code to be marked consumed")
	}
}

func TestVerifyPhone(t *testing.T) {
	store := &mockStore{}
	provider := &mockProvider{}
	svc := NewService(store, provider)

	ctx := context.Background()
	phone := "08012345678"

	if err := svc.Issue(ctx, 10, phone, models.OTPPurposePhoneVerification); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	if err := svc.VerifyPhone(ctx, 10, phone, provider.lastCode); err != nil {
		t.Fatalf("VerifyPhone failed: %v", err)
	}

	if !store.verified {
		t.Fatal("expected phone to be marked verified")
	}
}

func TestPaymentCodesAreLimitedToTheirAccountAndPurpose(t *testing.T) {
	store, provider := &mockStore{}, &mockProvider{}
	service := NewService(store, provider)
	ctx := context.Background()
	if err := service.Issue(ctx, 10, "08012345678", models.OTPPurposePayment); err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{models.OTPPurposePhoneVerification, models.OTPPurposeWithdrawal, models.OTPPurposeTransfer, models.OTPPurposePasswordReset} {
		if err := service.Verify(ctx, 10, purpose, provider.lastCode); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("payment code approved purpose %s: %v", purpose, err)
		}
	}
	if err := service.Verify(ctx, 11, models.OTPPurposePayment, provider.lastCode); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("payment code approved another account: %v", err)
	}
	store.active.ExpiresAt = time.Now().Add(-time.Minute)
	if err := service.Verify(ctx, 10, models.OTPPurposePayment, provider.lastCode); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expired payment code was accepted: %v", err)
	}
}

type otpSendFunc func(context.Context, string, string, string) error

func (send otpSendFunc) Send(ctx context.Context, phone, code, purpose string) error {
	return send(ctx, phone, code, purpose)
}

func TestFailedDeliveryCleanupSurvivesRequestCancellation(t *testing.T) {
	store := &mockStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(store, otpSendFunc(func(ctx context.Context, _, _, _ string) error {
		cancel()
		return ctx.Err()
	}))
	if err := service.Issue(ctx, 10, "08012345678", models.OTPPurposePayment); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delivery = %v", err)
	}
	if store.active == nil || !store.active.ConsumedAt.Valid {
		t.Fatal("canceled delivery left an undelivered challenge active")
	}
}

func TestFailedDeliveryDoesNotInvalidateNewerResend(t *testing.T) {
	store := &mockStore{}
	newCode := "123456"
	newHash, err := bcrypt.GenerateFromPassword([]byte(newCode), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, otpSendFunc(func(ctx context.Context, phone, code, purpose string) error {
		if err := store.Issue(ctx, 10, "08012345678", purpose, string(newHash), time.Now().Add(time.Minute)); err != nil {
			return err
		}
		return errors.New("earlier delivery failed")
	}))
	ctx := context.Background()
	if err := service.Issue(ctx, 10, "08012345678", models.OTPPurposePayment); err == nil {
		t.Fatal("failed delivery reported success")
	}
	if store.active.ConsumedAt.Valid || store.active.CodeHash != string(newHash) {
		t.Fatal("older delivery failure invalidated the newer resend")
	}
	if err := service.Verify(ctx, 10, models.OTPPurposePayment, newCode); err != nil {
		t.Fatalf("newer resend could not be verified: %v", err)
	}
}
