package services

import (
	"context"
	"errors"
	"fmt"

	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidTransactionPIN     = errors.New("enter a transaction PIN containing exactly four digits")
	ErrTransactionPINMismatch    = errors.New("the transaction PINs do not match")
	ErrTransactionPINUnavailable = errors.New("we couldn't verify your wallet PIN right now. Please try again")
)

type transactionPINStore interface {
	SetTransactionPIN(context.Context, int, string) error
	VerifyTransactionPIN(context.Context, int, string) error
}

type TransactionPINService struct {
	repo transactionPINStore
}

func NewTransactionPINService(repo transactionPINStore) *TransactionPINService {
	return &TransactionPINService{repo: repo}
}

// Create hashes the PIN before linking it to the wallet; plaintext never
// reaches persistence. Confirmation is checked on the server as well as the form.
func (s *TransactionPINService) Create(ctx context.Context, farmerID int, pin, confirmation string) error {
	if farmerID <= 0 {
		return repository.ErrAccountNotFound
	}
	if !validTransactionPIN(pin) {
		return ErrInvalidTransactionPIN
	}
	if pin != confirmation {
		return ErrTransactionPINMismatch
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return ErrTransactionPINUnavailable
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash transaction PIN: %w", err)
	}
	return s.repo.SetTransactionPIN(ctx, farmerID, string(hash))
}

func (s *TransactionPINService) Verify(ctx context.Context, farmerID int, pin string) error {
	if farmerID <= 0 {
		return repository.ErrAccountNotFound
	}
	if !validTransactionPIN(pin) {
		return ErrInvalidTransactionPIN
	}
	if s == nil || s.repo == nil {
		return ErrTransactionPINUnavailable
	}
	err := s.repo.VerifyTransactionPIN(ctx, farmerID, pin)
	switch {
	case err == nil, errors.Is(err, repository.ErrTransactionPINNotSet),
		errors.Is(err, repository.ErrTransactionPINIncorrect), errors.Is(err, repository.ErrTransactionPINLocked),
		errors.Is(err, repository.ErrTransactionPINSetupUnavailable):
		return err
	default:
		return fmt.Errorf("%w: %v", ErrTransactionPINUnavailable, err)
	}
}

func validTransactionPIN(pin string) bool {
	if len(pin) != 4 {
		return false
	}
	for _, digit := range pin {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
