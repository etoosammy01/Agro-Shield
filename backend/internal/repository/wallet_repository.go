package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"backend/internal/models"
)

var ErrWalletNotFound = errors.New("wallet not found")
var ErrWithdrawalNotFound = errors.New("withdrawal not found")

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) GetWallet(ctx context.Context, farmerID int) (*models.Wallet, error) {
	var wallet models.Wallet
	err := r.db.QueryRowContext(ctx, `
		SELECT id, farmer_id, balance::text, currency, created_at, updated_at
		FROM wallets WHERE farmer_id = $1`, farmerID,
	).Scan(&wallet.ID, &wallet.FarmerID, &wallet.Balance, &wallet.Currency, &wallet.CreatedAt, &wallet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	return &wallet, err
}

func (r *WalletRepository) CreateDeposit(ctx context.Context, farmerID int, reference, amount string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wallet_deposits (wallet_id, reference, amount, currency)
		SELECT id, $2, $3::numeric, 'NGN' FROM wallets WHERE farmer_id = $1`,
		farmerID, reference, amount,
	)
	return err
}

func (r *WalletRepository) CompleteDeposit(ctx context.Context, reference, transactionID, amount, currency string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var status string
	var storedAmount, storedCurrency string
	var storedTransactionID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT wallet_id, status, amount::text, currency, provider_transaction_id
		FROM wallet_deposits WHERE reference = $1 FOR UPDATE`, reference,
	).Scan(&walletID, &status, &storedAmount, &storedCurrency, &storedTransactionID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWalletNotFound
	}
	if err != nil {
		return err
	}
	if status == models.PaymentSuccessful {
		if storedTransactionID.Valid && storedTransactionID.String == transactionID && storedAmount == amount && storedCurrency == currency {
			return nil
		}
		return fmt.Errorf("wallet deposit already completed with a different transaction")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE wallet_deposits
		SET status = 'successful', provider_transaction_id = $1, updated_at = $2
		WHERE reference = $3 AND status = 'pending' AND amount = $4::numeric AND currency = $5`,
		transactionID, time.Now(), reference, amount, currency,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("verified deposit amount or currency does not match the pending deposit")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		amount, time.Now(), walletID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'deposit', $3::numeric)`,
		walletID, "deposit:"+reference, amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WalletRepository) FailDeposit(ctx context.Context, reference string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_deposits SET status = 'failed', updated_at = $1
		WHERE reference = $2 AND status = 'pending'`, time.Now(), reference,
	)
	return err
}

func (r *WalletRepository) ListDeposits(ctx context.Context, farmerID int) ([]models.WalletDeposit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.amount::text, d.currency, d.reference, d.status, d.created_at
		FROM wallet_deposits d
		JOIN wallets w ON w.id = d.wallet_id
		WHERE w.farmer_id = $1
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 50`, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deposits []models.WalletDeposit
	for rows.Next() {
		var deposit models.WalletDeposit
		if err := rows.Scan(&deposit.Amount, &deposit.Currency, &deposit.Reference, &deposit.Status, &deposit.CreatedAt); err != nil {
			return nil, err
		}
		deposits = append(deposits, deposit)
	}
	return deposits, rows.Err()
}

func (r *WalletRepository) CreateWithdrawal(ctx context.Context, farmerID int, reference, amount string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var bankName, bankCode, accountName, accountNumber string
	err = tx.QueryRowContext(ctx, `
		SELECT w.id, COALESCE(f.bank_name, ''), COALESCE(f.bank_code, ''),
			COALESCE(f.account_name, ''), COALESCE(f.account_number, '')
		FROM wallets w
		JOIN farmers f ON f.id = w.farmer_id
		WHERE w.farmer_id = $1`, farmerID,
	).Scan(&walletID, &bankName, &bankCode, &accountName, &accountNumber)
	if err != nil {
		return err
	}
	if bankName == "" || bankCode == "" || accountName == "" || accountNumber == "" {
		return fmt.Errorf("save your bank payout details before requesting a withdrawal")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance - $1::numeric, updated_at = $2
		WHERE id = $3 AND balance >= $1::numeric`, amount, time.Now(), walletID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("insufficient wallet balance")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_withdrawals
			(wallet_id, farmer_id, reference, amount, currency, bank_name, bank_code, account_name, account_number)
		VALUES ($1, $2, $3, $4::numeric, 'NGN', $5, $6, $7, $8)`,
		walletID, farmerID, reference, amount, bankName, bankCode, accountName, accountNumber,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'withdrawal_reserved', $3::numeric)`,
		walletID, "withdrawal:"+reference, amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WalletRepository) PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64) (int64, error) {
	if buyerID <= 0 || cropID <= 0 {
		return 0, fmt.Errorf("invalid buyer or crop")
	}
	if quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return 0, fmt.Errorf("quantity must be a valid positive number")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var sellerID int
	var available float64
	var listed bool
	err = tx.QueryRowContext(ctx, `
		SELECT farmer_id, quantity, listed_for_sale
		FROM crops WHERE id = $1 FOR UPDATE`, cropID,
	).Scan(&sellerID, &available, &listed)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("this produce is not available for purchase")
	}
	if err != nil {
		return 0, err
	}
	if !listed || quantity > available {
		return 0, fmt.Errorf("this produce is not available in the requested quantity")
	}
	if sellerID == buyerID {
		return 0, fmt.Errorf("you can't buy your own produce")
	}

	var buyerWalletID, sellerWalletID int64
	var total string
	err = tx.QueryRowContext(ctx, `
		SELECT buyer_wallet.id, seller_wallet.id,
			($3::numeric * crops.price_per_unit::numeric)::numeric(12,2)::text
		FROM crops
		JOIN wallets buyer_wallet ON buyer_wallet.farmer_id = $1
		JOIN wallets seller_wallet ON seller_wallet.farmer_id = crops.farmer_id
		WHERE crops.id = $2`,
		buyerID, cropID, quantity,
	).Scan(&buyerWalletID, &sellerWalletID, &total)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(total) == "" || total == "0.00" {
		return 0, fmt.Errorf("order total must be greater than zero")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance - $1::numeric, updated_at = $2
		WHERE id = $3 AND balance >= $1::numeric`, total, time.Now(), buyerWalletID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected != 1 {
		return 0, fmt.Errorf("insufficient wallet balance")
	}

	var orderID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO orders (buyer_id, crop_id, quantity, total_price, status)
		VALUES ($1, $2, $3, $4::numeric, 'completed')
		RETURNING id`, buyerID, cropID, quantity, total,
	).Scan(&orderID)
	if err != nil {
		return 0, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE crops SET quantity = quantity - $1,
			listed_for_sale = CASE WHEN quantity - $1 <= 0 THEN FALSE ELSE listed_for_sale END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND quantity >= $1`, quantity, cropID)
	if err != nil {
		return 0, err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected != 1 {
		return 0, fmt.Errorf("produce quantity changed during checkout")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		total, time.Now(), sellerWalletID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'order_payment', $3::numeric), ($4, $5, 'sale_credit', $3::numeric)`,
		buyerWalletID, fmt.Sprintf("order-payment:%d", orderID), total,
		sellerWalletID, fmt.Sprintf("sale-credit:%d", orderID),
	); err != nil {
		return 0, err
	}
	return orderID, tx.Commit()
}

func (r *WalletRepository) ListTransactions(ctx context.Context, farmerID int) ([]models.WalletTransaction, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.type, t.amount::text, t.reference, t.created_at
		FROM wallet_transactions t
		JOIN wallets w ON w.id = t.wallet_id
		WHERE w.farmer_id = $1
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT 100`, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []models.WalletTransaction
	for rows.Next() {
		var transaction models.WalletTransaction
		if err := rows.Scan(&transaction.ID, &transaction.Type, &transaction.Amount, &transaction.Reference, &transaction.CreatedAt); err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	return transactions, rows.Err()
}

func (r *WalletRepository) ListFarmerWithdrawals(ctx context.Context, farmerID int) ([]models.Withdrawal, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, amount::text, currency, reference, status, created_at, updated_at
		FROM wallet_withdrawals
		WHERE farmer_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 100`, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []models.Withdrawal
	for rows.Next() {
		var withdrawal models.Withdrawal
		if err := rows.Scan(&withdrawal.ID, &withdrawal.Amount, &withdrawal.Currency, &withdrawal.Reference, &withdrawal.Status, &withdrawal.CreatedAt, &withdrawal.UpdatedAt); err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, withdrawal)
	}
	return withdrawals, rows.Err()
}

func (r *WalletRepository) ListWithdrawals(ctx context.Context, pendingOnly bool) ([]models.Withdrawal, error) {
	query := `
		SELECT x.id, x.farmer_id, f.full_name, x.amount::text, x.currency,
			x.bank_name, x.bank_code, x.account_name, x.account_number,
			x.reference, x.provider_transfer_id, x.status, x.created_at, x.updated_at
		FROM wallet_withdrawals x
		JOIN farmers f ON f.id = x.farmer_id`
	if pendingOnly {
		query += ` WHERE x.status IN ('pending', 'processing')`
	}
	query += ` ORDER BY x.created_at ASC LIMIT 100`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []models.Withdrawal
	for rows.Next() {
		var withdrawal models.Withdrawal
		if err := rows.Scan(
			&withdrawal.ID, &withdrawal.FarmerID, &withdrawal.FullName, &withdrawal.Amount, &withdrawal.Currency,
			&withdrawal.BankName, &withdrawal.BankCode, &withdrawal.AccountName, &withdrawal.AccountNumber,
			&withdrawal.Reference, &withdrawal.TransferID, &withdrawal.Status, &withdrawal.CreatedAt, &withdrawal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, withdrawal)
	}
	return withdrawals, rows.Err()
}

func (r *WalletRepository) BeginWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (*models.Withdrawal, error) {
	var withdrawal models.Withdrawal
	err := r.db.QueryRowContext(ctx, `
		UPDATE wallet_withdrawals
		SET status = 'processing', approved_by = $1, updated_at = $2
		WHERE id = $3 AND status = 'pending'
		RETURNING id, farmer_id, amount::text, currency, bank_name, bank_code,
			account_name, account_number, reference, status`,
		adminID, time.Now(), withdrawalID,
	).Scan(
		&withdrawal.ID, &withdrawal.FarmerID, &withdrawal.Amount, &withdrawal.Currency,
		&withdrawal.BankName, &withdrawal.BankCode, &withdrawal.AccountName, &withdrawal.AccountNumber,
		&withdrawal.Reference, &withdrawal.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

func (r *WalletRepository) GetWithdrawal(ctx context.Context, withdrawalID int64) (*models.Withdrawal, error) {
	var withdrawal models.Withdrawal
	err := r.db.QueryRowContext(ctx, `
		SELECT x.id, x.farmer_id, f.full_name, x.amount::text, x.currency,
			x.bank_name, x.bank_code, x.account_name, x.account_number,
			x.reference, x.provider_transfer_id, x.status, x.created_at, x.updated_at
		FROM wallet_withdrawals x
		JOIN farmers f ON f.id = x.farmer_id
		WHERE x.id = $1`, withdrawalID,
	).Scan(
		&withdrawal.ID, &withdrawal.FarmerID, &withdrawal.FullName, &withdrawal.Amount, &withdrawal.Currency,
		&withdrawal.BankName, &withdrawal.BankCode, &withdrawal.AccountName, &withdrawal.AccountNumber,
		&withdrawal.Reference, &withdrawal.TransferID, &withdrawal.Status, &withdrawal.CreatedAt, &withdrawal.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

func (r *WalletRepository) SetTransferID(ctx context.Context, reference, transferID string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_withdrawals SET provider_transfer_id = $1, updated_at = $2
		WHERE reference = $3 AND status = 'processing'`,
		transferID, time.Now(), reference,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrWithdrawalNotFound
	}
	return nil
}

func (r *WalletRepository) RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var amount string
	err = tx.QueryRowContext(ctx, `
		SELECT wallet_id, amount::text FROM wallet_withdrawals
		WHERE id = $1 AND status = 'pending' FOR UPDATE`, withdrawalID,
	).Scan(&walletID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWithdrawalNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_withdrawals SET status = 'rejected', approved_by = $1, updated_at = $2 WHERE id = $3`,
		adminID, time.Now(), withdrawalID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		amount, time.Now(), walletID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'withdrawal_refund', $3::numeric)`,
		walletID, fmt.Sprintf("withdrawal-refund:%d", withdrawalID), amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WalletRepository) CompleteTransfer(ctx context.Context, reference, transferID, status string, amount int64, currency string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var withdrawalAmount string
	var currentStatus string
	var storedTransferID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT wallet_id, amount::text, status, provider_transfer_id
		FROM wallet_withdrawals WHERE reference = $1 FOR UPDATE`, reference,
	).Scan(&walletID, &withdrawalAmount, &currentStatus, &storedTransferID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWithdrawalNotFound
	}
	if err != nil {
		return err
	}
	if currentStatus == "paid" || currentStatus == "failed" {
		return nil
	}
	if currentStatus != "processing" || !storedTransferID.Valid || storedTransferID.String != transferID {
		return fmt.Errorf("transfer does not match a processing withdrawal")
	}

	var result sql.Result
	switch status {
	case "SUCCESSFUL":
		result, err = tx.ExecContext(ctx, `
			UPDATE wallet_withdrawals SET status = 'paid', updated_at = $1
			WHERE reference = $2 AND status = 'processing' AND amount = $3::numeric AND currency = $4`,
			time.Now(), reference, amount, currency)
	case "FAILED", "REVERSED":
		result, err = tx.ExecContext(ctx, `
			UPDATE wallet_withdrawals SET status = 'failed', updated_at = $1
			WHERE reference = $2 AND status = 'processing' AND amount = $3::numeric AND currency = $4`,
			time.Now(), reference, amount, currency)
		if err == nil {
			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return rowsErr
			}
			if affected != 1 {
				return fmt.Errorf("verified transfer amount or currency does not match withdrawal")
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
				withdrawalAmount, time.Now(), walletID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
				VALUES ($1, $2, 'withdrawal_refund', $3::numeric)`,
				walletID, "transfer-refund:"+reference, withdrawalAmount)
		}
	default:
		return fmt.Errorf("transfer has non-terminal status %q", status)
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("verified transfer amount or currency does not match withdrawal")
	}
	return tx.Commit()
}
