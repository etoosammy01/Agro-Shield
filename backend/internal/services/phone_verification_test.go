package services

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

type phoneOTPStore struct {
	active     *models.OTPCode
	issued     int
	verified   bool
	consumeErr error
}

func (s *phoneOTPStore) Issue(_ context.Context, farmerID int, phone, purpose, hash string, expires time.Time) error {
	s.issued++
	s.active = &models.OTPCode{ID: int64(s.issued), FarmerID: farmerID, Phone: phone, Purpose: purpose, CodeHash: hash, ExpiresAt: expires}
	return nil
}
func (s *phoneOTPStore) LatestActive(_ context.Context, farmerID int, purpose string) (*models.OTPCode, error) {
	if s.active == nil || s.active.FarmerID != farmerID || s.active.Purpose != purpose || s.active.ConsumedAt.Valid || !s.active.ExpiresAt.After(time.Now()) {
		return nil, repository.ErrNoActiveOTP
	}
	copy := *s.active
	return &copy, nil
}
func (s *phoneOTPStore) IncrementAttempts(_ context.Context, _ int64) error {
	s.active.Attempts++
	return nil
}
func (s *phoneOTPStore) Consume(_ context.Context, _ int64) error {
	if s.consumeErr != nil {
		return s.consumeErr
	}
	if s.active.ConsumedAt.Valid {
		return repository.ErrNoActiveOTP
	}
	s.active.ConsumedAt = sql.NullTime{Time: time.Now(), Valid: true}
	return nil
}
func (s *phoneOTPStore) ConsumeAndVerifyPhone(ctx context.Context, id int64, farmerID int, phone string) error {
	if s.active.FarmerID != farmerID || s.active.Phone != phone || s.active.Purpose != models.OTPPurposePhoneVerification {
		return repository.ErrNoActiveOTP
	}
	if err := s.Consume(ctx, id); err != nil {
		return err
	}
	s.verified = true
	return nil
}
func (s *phoneOTPStore) CountIssuedSince(context.Context, int, string, time.Time) (int, error) {
	return s.issued, nil
}

type phoneOTPProvider struct {
	code, phone        string
	sendErr, verifyErr error
	verifyCalls        int
}

func (p *phoneOTPProvider) Send(_ context.Context, phone, code, _ string) error {
	p.phone, p.code = phone, code
	return p.sendErr
}
func (p *phoneOTPProvider) Verify(context.Context, string, string) error {
	p.verifyCalls++
	return p.verifyErr
}

func phoneOTPFixture(t *testing.T) (*OTPService, *phoneOTPStore, *phoneOTPProvider) {
	t.Helper()
	store, provider := &phoneOTPStore{}, &phoneOTPProvider{}
	service := &OTPService{repo: store, provider: provider}
	if err := service.Issue(context.Background(), 1, "08012345678", models.OTPPurposePhoneVerification); err != nil {
		t.Fatal(err)
	}
	return service, store, provider
}

func TestPhoneVerificationStoresHashAndVerifiesOnce(t *testing.T) {
	s, store, provider := phoneOTPFixture(t)
	if provider.phone != "2348012345678" || !isWellFormedOTPCode(provider.code) {
		t.Fatal("SMS destination or code invalid")
	}
	if store.active.CodeHash == provider.code || bcrypt.CompareHashAndPassword([]byte(store.active.CodeHash), []byte(provider.code)) != nil {
		t.Fatal("OTP was not stored hashed")
	}
	if err := s.VerifyPhone(context.Background(), 1, "08012345678", provider.code); err != nil {
		t.Fatal(err)
	}
	if !store.verified || !store.active.ConsumedAt.Valid || provider.verifyCalls != 1 {
		t.Fatal("verification not persisted or provider not checked")
	}
	if err := s.VerifyPhone(context.Background(), 1, "08012345678", provider.code); !errors.Is(err, ErrOTPUnavailable) {
		t.Fatalf("replay accepted: %v", err)
	}
}

func TestPhoneVerificationFailureDoesNotCompleteOnboarding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*phoneOTPStore, *phoneOTPProvider)
		phone  string
		want   error
	}{
		{"expired", func(s *phoneOTPStore, _ *phoneOTPProvider) { s.active.ExpiresAt = time.Now().Add(-time.Minute) }, "08012345678", ErrOTPUnavailable},
		{"different phone", func(*phoneOTPStore, *phoneOTPProvider) {}, "08012345679", ErrOTPUnavailable},
		{"wallet code", func(s *phoneOTPStore, _ *phoneOTPProvider) { s.active.Purpose = models.OTPPurposeTransfer }, "08012345678", ErrOTPUnavailable},
		{"exhausted", func(s *phoneOTPStore, _ *phoneOTPProvider) { s.active.Attempts = otpMaxAttempts }, "08012345678", ErrOTPTooManyAttempts},
		{"already consumed concurrently", func(s *phoneOTPStore, _ *phoneOTPProvider) { s.consumeErr = repository.ErrNoActiveOTP }, "08012345678", ErrOTPUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, provider := phoneOTPFixture(t)
			tc.change(store, provider)
			if err := s.VerifyPhone(context.Background(), 1, tc.phone, provider.code); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if store.verified {
				t.Fatal("failed verification completed onboarding")
			}
		})
	}
}

func TestPhoneVerificationProviderFailureDoesNotConsumeCode(t *testing.T) {
	s, store, provider := phoneOTPFixture(t)
	provider.verifyErr = errors.New("provider unavailable")
	if err := s.VerifyPhone(context.Background(), 1, "08012345678", provider.code); err == nil {
		t.Fatal("provider failure ignored")
	}
	if store.verified || store.active.ConsumedAt.Valid || store.active.Attempts != 0 {
		t.Fatal("provider outage spent the code or verified the phone")
	}
	provider.verifyErr = nil
	if err := s.VerifyPhone(context.Background(), 1, "08012345678", provider.code); err != nil {
		t.Fatal(err)
	}
}

func TestPhoneVerificationCountsWrongAttemptsAndBurnsCode(t *testing.T) {
	s, store, provider := phoneOTPFixture(t)
	wrong := "000000"
	if wrong == provider.code {
		wrong = "111111"
	}
	for i := 1; i <= otpMaxAttempts; i++ {
		want := ErrOTPInvalid
		if i == otpMaxAttempts {
			want = ErrOTPTooManyAttempts
		}
		if err := s.VerifyPhone(context.Background(), 1, "08012345678", wrong); !errors.Is(err, want) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if store.verified || store.active.Attempts != otpMaxAttempts || !store.active.ConsumedAt.Valid {
		t.Fatal("attempt cap not enforced")
	}
}

func TestPhoneVerificationResendAndIssueLimit(t *testing.T) {
	s, store, provider := phoneOTPFixture(t)
	for i := 1; i < otpMaxIssuesPerWindow; i++ {
		oldHash := store.active.CodeHash
		if err := s.Issue(context.Background(), 1, "08012345678", models.OTPPurposePhoneVerification); err != nil {
			t.Fatal(err)
		}
		if store.active.CodeHash == oldHash {
			t.Fatal("resend retained old challenge")
		}
	}
	if err := s.Issue(context.Background(), 1, "08012345678", models.OTPPurposePhoneVerification); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("issue limit ignored: %v", err)
	}
	if err := s.VerifyPhone(context.Background(), 1, "08012345678", provider.code); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingPathUsesPersistedSteps(t *testing.T) {
	done := sql.NullTime{Time: time.Now(), Valid: true}
	for _, tc := range []struct {
		farmer *models.Farmer
		want   string
	}{
		{nil, "/complete-profile"},
		{&models.Farmer{PhotoURL: "/static/uploads/old.jpg"}, "/complete-profile"},
		{&models.Farmer{SelfieCapturedAt: done}, "/verify-phone"},
		{&models.Farmer{SelfieCapturedAt: done, PhoneVerifiedAt: done}, "/dashboard"},
	} {
		if got := OnboardingPath(tc.farmer); got != tc.want {
			t.Fatalf("path=%s want=%s", got, tc.want)
		}
	}
}
