package otp

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

const (
	phoneChangeInput     = "08012345678"
	phoneChangeCanonical = "2348012345678"
	phoneChangeOther     = "08098765432"
)

func newPhoneChangeFixture(pending string) (*Service, *mockStore, *mockProvider) {
	store := &mockStore{pendingPhone: pending}
	provider := &mockProvider{}
	return NewService(store, provider), store, provider
}

func TestRequestPhoneChangeIssuesOnlyForPendingNumber(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
	ctx := context.Background()

	if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
		t.Fatalf("RequestPhoneChange failed: %v", err)
	}
	if provider.lastPurpose != models.OTPPurposePhoneChange {
		t.Fatalf("purpose = %q, want %q", provider.lastPurpose, models.OTPPurposePhoneChange)
	}
	if len(provider.lastCode) != 6 {
		t.Fatalf("code = %q, want six digits", provider.lastCode)
	}
	if store.active == nil || store.active.Phone != phoneChangeCanonical {
		t.Fatalf("challenge phone = %#v, want %q", store.active, phoneChangeCanonical)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.active.CodeHash), []byte(provider.lastCode)); err != nil {
		t.Fatalf("stored hash does not match issued code: %v", err)
	}
	if until := time.Until(store.active.ExpiresAt); until < 4*time.Minute || until > 6*time.Minute {
		t.Fatalf("expiry = %v, want about five minutes", until)
	}
}

func TestRequestPhoneChangeRejectsInvalidPhoneAndFarmer(t *testing.T) {
	for _, test := range []struct {
		name      string
		farmerID  int
		phone     string
		wantError error
	}{
		{"invalid phone", 10, "123", ErrUnusablePhone},
		{"zero farmer", 0, phoneChangeInput, nil},
		{"negative farmer", -1, phoneChangeInput, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
			err := svc.RequestPhoneChange(context.Background(), test.farmerID, test.phone)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if test.wantError == nil && err == nil {
				t.Fatal("invalid farmer was accepted")
			}
			if store.issued != 0 || provider.lastCode != "" {
				t.Fatal("invalid request issued or delivered a code")
			}
		})
	}
}

func TestRequestPhoneChangeRejectsUnstagedPhone(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture("2348012345679")
	err := svc.RequestPhoneChange(context.Background(), 10, phoneChangeInput)
	if !errors.Is(err, ErrPhoneChangeNotPending) {
		t.Fatalf("error = %v, want ErrPhoneChangeNotPending", err)
	}
	if store.issued != 0 || provider.lastCode != "" {
		t.Fatal("unstaged phone received a code")
	}
}

func TestVerifyPhoneChangeSuccess(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
	ctx := context.Background()

	if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPhoneChange(ctx, 10, "+234 801-234-5678", provider.lastCode); err != nil {
		t.Fatalf("VerifyPhoneChange failed: %v", err)
	}
	if !store.applied {
		t.Fatal("phone change was not applied")
	}
	if !store.active.ConsumedAt.Valid {
		t.Fatal("OTP was not consumed")
	}
}

func TestVerifyPhoneChangeRejectsIncorrectExpiredExhaustedAndReplayedCodes(t *testing.T) {
	t.Run("incorrect", func(t *testing.T) {
		svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatal(err)
		}
		wrong := "000000"
		if provider.lastCode == wrong {
			wrong = "000001"
		}
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, wrong); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
		if store.applied || store.active.ConsumedAt.Valid {
			t.Fatal("incorrect code changed the phone or consumed the OTP")
		}
		if store.active.Attempts != 1 {
			t.Fatalf("attempts = %d, want 1", store.active.Attempts)
		}
	})

	t.Run("expired", func(t *testing.T) {
		svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatal(err)
		}
		store.active.ExpiresAt = time.Now().Add(-time.Minute)
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
		if store.applied {
			t.Fatal("expired code changed the phone")
		}
	})

	t.Run("exhausted", func(t *testing.T) {
		svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatal(err)
		}
		store.active.Attempts = 5
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("error = %v, want ErrTooManyAttempts", err)
		}
		if !store.active.ConsumedAt.Valid {
			t.Fatal("exhausted OTP was not burned")
		}
		if store.applied {
			t.Fatal("exhausted code changed the phone")
		}
	})

	t.Run("replayed", func(t *testing.T) {
		svc, _, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatal(err)
		}
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode); err != nil {
			t.Fatal(err)
		}
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("replay error = %v, want ErrUnavailable", err)
		}
	})
}

func TestVerifyPhoneChangeRejectsWrongPhone(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
	ctx := context.Background()
	if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeOther, provider.lastCode); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if store.applied {
		t.Fatal("wrong phone applied a change")
	}
}

func TestPhoneChangeAndOnboardingCodesCannotCrossPurposes(t *testing.T) {
	t.Run("phone change code rejected by onboarding", func(t *testing.T) {
		svc, _, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatal(err)
		}
		if err := svc.VerifyPhone(ctx, 10, phoneChangeCanonical, provider.lastCode); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("onboarding code rejected by phone change", func(t *testing.T) {
		svc, _, provider := newPhoneChangeFixture(phoneChangeCanonical)
		ctx := context.Background()
		if err := svc.Issue(ctx, 10, phoneChangeCanonical, models.OTPPurposePhoneVerification); err != nil {
			t.Fatal(err)
		}
		if err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})
}

func TestVerifyPhoneChangePropagatesDuplicatePhoneError(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture(phoneChangeCanonical)
	ctx := context.Background()
	if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
		t.Fatal(err)
	}
	store.applyErr = repository.ErrPhoneExists
	err := svc.VerifyPhoneChange(ctx, 10, phoneChangeCanonical, provider.lastCode)
	if !errors.Is(err, repository.ErrPhoneExists) {
		t.Fatalf("error = %v, want ErrPhoneExists", err)
	}
	if store.applied {
		t.Fatal("duplicate phone was reported as applied")
	}
}

func TestOnboardingVerificationStillUsesConsumeAndVerifyPhone(t *testing.T) {
	svc, store, provider := newPhoneChangeFixture("")
	ctx := context.Background()
	if err := svc.Issue(ctx, 10, phoneChangeCanonical, models.OTPPurposePhoneVerification); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPhone(ctx, 10, phoneChangeCanonical, provider.lastCode); err != nil {
		t.Fatalf("VerifyPhone failed: %v", err)
	}
	if !store.verified || store.applied {
		t.Fatalf("verified=%v applied=%v, want onboarding path only", store.verified, store.applied)
	}
}

func TestRequestPhoneChangeEnforcesRateLimit(t *testing.T) {
	svc, store, _ := newPhoneChangeFixture(phoneChangeCanonical)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); err != nil {
			t.Fatalf("issue %d failed: %v", i+1, err)
		}
	}
	if err := svc.RequestPhoneChange(ctx, 10, phoneChangeInput); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("fourth issue error = %v, want ErrRateLimited", err)
	}
	if store.issued != 3 {
		t.Fatalf("issued = %d, want 3", store.issued)
	}
}
