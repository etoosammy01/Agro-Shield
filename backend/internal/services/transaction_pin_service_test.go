package services

import (
	"context"
	"errors"
	"testing"

	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

type pinTestStore struct {
	savedOwner, verifiedOwner      int
	hash, verifiedPIN, expectedPIN string
	saves, verifications           int
	err                            error
	events                         *[]string
}

func (s *pinTestStore) SetTransactionPIN(_ context.Context, owner int, hash string) error {
	s.saves++
	s.savedOwner, s.hash = owner, hash
	return s.err
}

func (s *pinTestStore) VerifyTransactionPIN(_ context.Context, owner int, pin string) error {
	s.verifications++
	s.verifiedOwner, s.verifiedPIN = owner, pin
	if s.events != nil {
		*s.events = append(*s.events, "PIN")
	}
	if s.expectedPIN != "" && pin != s.expectedPIN {
		return repository.ErrTransactionPINIncorrect
	}
	return s.err
}

func TestTransactionPINCreationHashesAndLinksToWalletOwner(t *testing.T) {
	first, second := &pinTestStore{}, &pinTestStore{}
	for _, store := range []*pinTestStore{first, second} {
		if err := NewTransactionPINService(store).Create(context.Background(), 7, "0456", "0456"); err != nil {
			t.Fatal(err)
		}
		if store.savedOwner != 7 || store.hash == "0456" {
			t.Fatal("PIN was not hashed and linked to the wallet owner")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(store.hash), []byte("0456")); err != nil {
			t.Fatal("PIN lost its leading zero")
		}
		if cost, err := bcrypt.Cost([]byte(store.hash)); err != nil || cost < bcrypt.DefaultCost {
			t.Fatal("PIN hash does not meet the configured bcrypt cost")
		}
	}
	if first.hash == second.hash {
		t.Fatal("identical PINs were stored without independent salts")
	}
}

func TestTransactionPINCreationRejectsInvalidInputBeforeStorage(t *testing.T) {
	for _, tc := range []struct {
		name, pin, confirmation string
		owner                   int
		want                    error
	}{
		{"missing owner", "0456", "0456", 0, repository.ErrAccountNotFound},
		{"empty", "", "", 7, ErrInvalidTransactionPIN},
		{"short", "456", "456", 7, ErrInvalidTransactionPIN},
		{"long", "04567", "04567", 7, ErrInvalidTransactionPIN},
		{"letters", "04ab", "04ab", 7, ErrInvalidTransactionPIN},
		{"spaces", " 456", " 456", 7, ErrInvalidTransactionPIN},
		{"different confirmation", "0456", "0457", 7, ErrTransactionPINMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &pinTestStore{}
			err := NewTransactionPINService(store).Create(context.Background(), tc.owner, tc.pin, tc.confirmation)
			if !errors.Is(err, tc.want) || store.saves != 0 {
				t.Fatalf("Create() = %v, saves = %d", err, store.saves)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &pinTestStore{}
	if err := NewTransactionPINService(store).Create(ctx, 7, "0456", "0456"); !errors.Is(err, context.Canceled) || store.saves != 0 {
		t.Fatal("canceled setup reached storage")
	}
}

func TestTransactionPINVerificationUsesWalletOwnerAndPreservesLockout(t *testing.T) {
	for _, want := range []error{nil, repository.ErrTransactionPINIncorrect, repository.ErrTransactionPINLocked, repository.ErrTransactionPINNotSet} {
		store := &pinTestStore{err: want}
		err := NewTransactionPINService(store).Verify(context.Background(), 7, "0456")
		if !errors.Is(err, want) || store.verifiedOwner != 7 || store.verifiedPIN != "0456" {
			t.Fatalf("Verify() = %v, owner = %d", err, store.verifiedOwner)
		}
	}
	store := &pinTestStore{err: errors.New("database unavailable")}
	if err := NewTransactionPINService(store).Verify(context.Background(), 7, "0456"); !errors.Is(err, ErrTransactionPINUnavailable) {
		t.Fatal("storage failure was not marked as unavailable")
	}
	if err := NewTransactionPINService(nil).Verify(context.Background(), 7, "0456"); !errors.Is(err, ErrTransactionPINUnavailable) {
		t.Fatal("missing verifier did not fail closed")
	}
}
