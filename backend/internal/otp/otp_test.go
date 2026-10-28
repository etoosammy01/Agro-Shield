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
	active   *models.OTPCode
	issued   int
	verified bool
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

func (s *mockStore) CountIssuedSince(_ context.Context, _ int, _ string, _ time.Time) (int, error) {
	return s.issued, nil
}

type mockProvider struct {
	lastPhone   string
	lastCode    string
	lastPurpose string
	sendErr     error
	verifyErr   error
}

func (p *mockProvider) Send(_ context.Context, phone, code, purpose string) error {
	p.lastPhone = phone
	p.lastCode = code
	p.lastPurpose = purpose
	return p.sendErr
}

func (p *mockProvider) Verify(_ context.Context, _, _ string) error {
	return p.verifyErr
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
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, "000000"); !errors.Is(err, ErrInvalid) {
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
