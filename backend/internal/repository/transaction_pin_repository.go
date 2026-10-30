package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrTransactionPINAlreadySet       = errors.New("your wallet already has a transaction PIN")
	ErrTransactionPINSetupUnavailable = errors.New("complete selfie and phone verification before using your wallet PIN")
	ErrTransactionPINNotSet           = errors.New("create your wallet transaction PIN before sending a transfer")
	ErrTransactionPINIncorrect        = errors.New("that transaction PIN is not correct")
	ErrTransactionPINLocked           = errors.New("too many incorrect PIN attempts. Try again in 15 minutes")
)

const (
	maxTransactionPINAttempts  = 5
	transactionPINLockDuration = 15 * time.Minute
)

// lockPINOwner protects setup and verification against phone changes and account
// closure. A share lock remains compatible with transfer foreign-key checks.
func lockPINOwner(ctx context.Context, tx *sql.Tx, farmerID int) error {
	var selfie, phone sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT selfie_captured_at, phone_verified_at
		FROM farmers WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, farmerID).Scan(&selfie, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	if !selfie.Valid || !phone.Valid {
		return ErrTransactionPINSetupUnavailable
	}
	return nil
}

// SetTransactionPIN links a hash to the owner's wallet once. Locking the wallet
// prevents simultaneous setup requests from replacing each other's PIN.
func (r *WalletRepository) SetTransactionPIN(ctx context.Context, farmerID int, hash string) error {
	if _, err := bcrypt.Cost([]byte(hash)); err != nil {
		return errors.New("a valid transaction PIN hash is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPINOwner(ctx, tx, farmerID); err != nil {
		return err
	}
	var walletID int64
	var currentHash sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id, transaction_pin_hash FROM wallets
		WHERE farmer_id = $1 FOR UPDATE`, farmerID).Scan(&walletID, &currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWalletNotFound
	}
	if err != nil {
		return err
	}
	if currentHash.Valid && currentHash.String != "" {
		return ErrTransactionPINAlreadySet
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET transaction_pin_hash = $1,
		transaction_pin_created_at = CURRENT_TIMESTAMP, transaction_pin_failed_attempts = 0,
		transaction_pin_locked_until = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, hash, walletID); err != nil {
		return err
	}
	return tx.Commit()
}

// VerifyTransactionPIN checks only the sender's wallet. Failed-attempt tracking
// shares the wallet lock with verification, so concurrent guesses cannot bypass
// the five-attempt limit. Lockouts persist across sessions and server restarts.
func (r *WalletRepository) VerifyTransactionPIN(ctx context.Context, farmerID int, pin string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPINOwner(ctx, tx, farmerID); err != nil {
		return err
	}
	var walletID int64
	var hash sql.NullString
	var attempts int
	var lockedUntil sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id, transaction_pin_hash,
		transaction_pin_failed_attempts, transaction_pin_locked_until FROM wallets
		WHERE farmer_id = $1 FOR UPDATE`, farmerID).Scan(&walletID, &hash, &attempts, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWalletNotFound
	}
	if err != nil {
		return err
	}
	if !hash.Valid || hash.String == "" {
		return ErrTransactionPINNotSet
	}
	now := time.Now()
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return ErrTransactionPINLocked
	}
	if lockedUntil.Valid {
		attempts = 0
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(pin)); err != nil {
		if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return errors.New("stored wallet PIN could not be verified")
		}
		attempts++
		var nextLock sql.NullTime
		failure := ErrTransactionPINIncorrect
		if attempts >= maxTransactionPINAttempts {
			nextLock = sql.NullTime{Time: now.Add(transactionPINLockDuration), Valid: true}
			failure = ErrTransactionPINLocked
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wallets SET transaction_pin_failed_attempts = $1,
			transaction_pin_locked_until = $2 WHERE id = $3`, attempts, nextLock, walletID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return failure
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET transaction_pin_failed_attempts = 0,
		transaction_pin_locked_until = NULL WHERE id = $1`, walletID); err != nil {
		return err
	}
	return tx.Commit()
}
