package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"backend/internal/models"
)

var ErrWalletNotFound = errors.New("wallet not found")
var ErrWithdrawalNotFound = errors.New("withdrawal not found")
var ErrTransferNotFound = errors.New("transfer not found")

// Rejections that describe something the user can act on. They are sentinels so
// callers can pass them through to the page while replacing genuine
// infrastructure failures with a generic message.
var (
	ErrInsufficientBalance         = errors.New("insufficient wallet balance")
	ErrSelfTransfer                = errors.New("you cannot transfer to your own wallet")
	ErrInvalidTransferParticipants = errors.New("invalid transfer participants")

	// ErrSelfApproval means an admin tried to approve a request they made
	// themselves.
	//
	// It sits here rather than only in the service because the approving
	// transaction is where the requester's id is unambiguous: the SELECT above
	// the check has already locked the row, so no caller can slip past the rule
	// by reading the request somewhere else first. The service translates it
	// into its own sentinel for the page.
	ErrSelfApproval = errors.New("an admin cannot approve their own request")

	// ErrRequestNotPending means a request was cancelled or expired by another
	// caller between the user seeing it and acting on it. Nothing was written.
	ErrRequestNotPending = errors.New("this request is no longer pending")
)

// --- money helpers ---------------------------------------------------------
//
// Amounts are handled as integer kobo wherever they are divided or combined,
// and only turned back into text at the edge, where the NUMERIC columns want
// it. Floats never carry a value that decides who gets paid what.

// splitCommission divides totalKobo into the platform's fee and the seller's
// net. The fee is rounded half-up to the nearest kobo and the net is whatever
// remains, so fee plus net always equals the total exactly.
//
// The seller always keeps at least one kobo of a sale: rounding a very small
// order up can otherwise give the platform the whole total, and both a seller
// credited nothing and a zero-amount ledger row are worse than a fee that is a
// single kobo short of the configured rate.
func splitCommission(totalKobo, rateBasisPoints int64) (feeKobo, netKobo int64) {
	if totalKobo <= 0 || rateBasisPoints <= 0 {
		return 0, totalKobo
	}
	// +5000 makes the division by 10000 round half-up rather than truncate.
	feeKobo = (totalKobo*rateBasisPoints + 5000) / 10000
	if feeKobo >= totalKobo {
		// totalKobo is positive here, so the fee stays non-negative.
		feeKobo = totalKobo - 1
	}
	return feeKobo, totalKobo - feeKobo
}

// koboToAmount renders integer kobo as the "123.45" text the NUMERIC columns
// expect. Callers pass a value already known to be non-negative.
func koboToAmount(kobo int64) string {
	return fmt.Sprintf("%d.%02d", kobo/100, kobo%100)
}

// FormatBasisPoints renders basis points as a percentage, e.g. 250 -> "2.50".
//
// Exported because the rate is both stored on the revenue ledger and shown to
// admins, and those two readings must agree: a rate that displays as 2.50% but
// records as 2.5 would make the ledger unexplainable against the order.
func FormatBasisPoints(basisPoints int64) string {
	return fmt.Sprintf("%d.%02d", basisPoints/100, basisPoints%100)
}

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

// CompleteDeposit credits the wallet for a verified payment and reports whether
// this call is the one that moved the row. The bool is what lets a caller tell
// the farmer once: the webhook and the reconciliation pass can both arrive for
// the same successful payment, and being credited twice in words after one
// credit in the ledger is alarming.
func (r *WalletRepository) CompleteDeposit(ctx context.Context, reference, transactionID, amount, currency string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
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
		return false, ErrWalletNotFound
	}
	if err != nil {
		return false, err
	}
	if status == models.PaymentSuccessful {
		if storedTransactionID.Valid && storedTransactionID.String == transactionID && storedAmount == amount && storedCurrency == currency {
			return false, nil
		}
		return false, fmt.Errorf("wallet deposit already completed with a different transaction")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE wallet_deposits
		SET status = 'successful', provider_transaction_id = $1, updated_at = $2
		WHERE reference = $3 AND status = 'pending' AND amount = $4::numeric AND currency = $5`,
		transactionID, time.Now(), reference, amount, currency,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, fmt.Errorf("verified deposit amount or currency does not match the pending deposit")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		amount, time.Now(), walletID,
	); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'deposit', $3::numeric)`,
		walletID, "deposit:"+reference, amount,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *WalletRepository) FailDeposit(ctx context.Context, reference string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_deposits SET status = 'failed', updated_at = $1
		WHERE reference = $2 AND status = 'pending'`, time.Now(), reference,
	)
	return err
}

// ListPendingDepositsOlderThan lists attempts that have waited on the provider
// past cutoff. The reconciliation pass re-checks them with the provider, and the
// admin page shows the same rows as its exception list.
//
// A limit keeps one pass bounded; the oldest are returned first, so anything
// missed by the limit is picked up by the next pass rather than starved.
func (r *WalletRepository) ListPendingDepositsOlderThan(ctx context.Context, cutoff time.Time) ([]models.PendingDeposit, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT w.farmer_id, f.full_name, d.amount::text, d.currency, d.reference, d.created_at
		FROM wallet_deposits d
		JOIN wallets w ON w.id = d.wallet_id
		JOIN farmers f ON f.id = w.farmer_id
		WHERE d.status = 'pending' AND d.created_at < $1
		ORDER BY d.created_at ASC, d.id ASC
		LIMIT 100`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deposits []models.PendingDeposit
	for rows.Next() {
		var deposit models.PendingDeposit
		if err := rows.Scan(&deposit.FarmerID, &deposit.FullName, &deposit.Amount,
			&deposit.Currency, &deposit.Reference, &deposit.CreatedAt); err != nil {
			return nil, err
		}
		deposits = append(deposits, deposit)
	}
	return deposits, rows.Err()
}

// ExpireDeposit closes an attempt the provider has confirmed it has no
// transaction for.
//
// It only matches a pending row with no provider transaction: a webhook that
// lands at the same moment wins, because CompleteDeposit has already moved the
// row out of pending. Refunding is not needed here - a pending deposit never
// credited the wallet, so closing it moves no money.
func (r *WalletRepository) ExpireDeposit(ctx context.Context, reference string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_deposits SET status = 'expired', updated_at = $1
		WHERE reference = $2 AND status = 'pending' AND provider_transaction_id IS NULL`,
		time.Now(), reference,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRequestNotPending
	}
	return nil
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
		return ErrInsufficientBalance
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

// PayForOrder settles a marketplace sale: the buyer is debited the full order
// total, the seller is credited that total less commission, and the commission
// is recorded in platform_revenue.
//
// commissionBasisPoints is the platform rate in hundredths of a percent, so
// 2.5% is 250. Zero disables commission and the seller receives the whole
// total — the behaviour before COMMISSION_PERCENT is configured.
//
// The arithmetic is done in integer kobo. A single payment is now split three
// ways and the parts have to add up exactly, so the seller's share is derived
// by subtracting the fee from the total rather than being rounded separately.
// That makes buyer debit == seller credit + fee true by construction.
func (r *WalletRepository) PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64, commissionBasisPoints int64) (int64, error) {
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

	// The total is rounded to whole kobo in SQL and returned as an integer, so
	// no float carries the amount that decides who gets paid what.
	var buyerWalletID, sellerWalletID int64
	var totalKobo int64
	err = tx.QueryRowContext(ctx, `
		SELECT buyer_wallet.id, seller_wallet.id,
			(($3::numeric * crops.price_per_unit::numeric)::numeric(12,2) * 100)::bigint
		FROM crops
		JOIN wallets buyer_wallet ON buyer_wallet.farmer_id = $1
		JOIN wallets seller_wallet ON seller_wallet.farmer_id = crops.farmer_id
		WHERE crops.id = $2`,
		buyerID, cropID, quantity,
	).Scan(&buyerWalletID, &sellerWalletID, &totalKobo)
	if err != nil {
		return 0, err
	}
	if totalKobo <= 0 {
		return 0, fmt.Errorf("order total must be greater than zero")
	}

	feeKobo, netKobo := splitCommission(totalKobo, commissionBasisPoints)
	total := koboToAmount(totalKobo)
	net := koboToAmount(netKobo)
	fee := koboToAmount(feeKobo)

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
		return 0, ErrInsufficientBalance
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

	// The seller's ledger records what actually landed, not the order total.
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		net, time.Now(), sellerWalletID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'order_payment', $3::numeric), ($4, $5, 'sale_credit', $6::numeric)`,
		buyerWalletID, fmt.Sprintf("order-payment:%d", orderID), total,
		sellerWalletID, fmt.Sprintf("sale-credit:%d", orderID), net,
	); err != nil {
		return 0, err
	}

	// With commission off there is no row at all, so a zero rate leaves no
	// trace and this table stays a record of money that actually moved.
	if feeKobo > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO platform_revenue (order_id, reference, gross_amount, amount, currency, rate_percent)
			VALUES ($1, $2, $3::numeric, $4::numeric, 'NGN', $5::numeric)`,
			orderID, fmt.Sprintf("commission:%d", orderID), total, fee,
			FormatBasisPoints(commissionBasisPoints),
		); err != nil {
			return 0, err
		}
	}
	return orderID, tx.Commit()
}

// PlatformRevenueTotal returns the commission collected so far and how many
// sales it came from.
func (r *WalletRepository) PlatformRevenueTotal(ctx context.Context) (string, int, error) {
	var total string
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)::numeric(12,2)::text, COUNT(*)
		FROM platform_revenue`).Scan(&total, &count)
	if err != nil {
		return "", 0, err
	}
	return total, count, nil
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
			x.reference, x.provider_transfer_id, x.status,
			x.name_check_status, x.name_check_resolved, x.name_check_detail, x.name_check_at,
			x.created_at, x.updated_at
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
			&withdrawal.Reference, &withdrawal.TransferID, &withdrawal.Status,
			&withdrawal.NameCheckStatus, &withdrawal.NameCheckResolved, &withdrawal.NameCheckDetail, &withdrawal.NameCheckAt,
			&withdrawal.CreatedAt, &withdrawal.UpdatedAt,
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
			x.reference, x.provider_transfer_id, x.status,
			x.name_check_status, x.name_check_resolved, x.name_check_detail, x.name_check_at,
			x.created_at, x.updated_at
		FROM wallet_withdrawals x
		JOIN farmers f ON f.id = x.farmer_id
		WHERE x.id = $1`, withdrawalID,
	).Scan(
		&withdrawal.ID, &withdrawal.FarmerID, &withdrawal.FullName, &withdrawal.Amount, &withdrawal.Currency,
		&withdrawal.BankName, &withdrawal.BankCode, &withdrawal.AccountName, &withdrawal.AccountNumber,
		&withdrawal.Reference, &withdrawal.TransferID, &withdrawal.Status,
		&withdrawal.NameCheckStatus, &withdrawal.NameCheckResolved, &withdrawal.NameCheckDetail, &withdrawal.NameCheckAt,
		&withdrawal.CreatedAt, &withdrawal.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

// RecordWithdrawalVerification stores the payout-details verdict against the
// withdrawal it was reached for.
//
// It is keyed by reference because that is the one identifier both callers hold:
// the request path generates it before the row exists, and it is UNIQUE, so it
// names exactly one withdrawal. A verdict for a withdrawal that has since been
// deleted matches nothing and is reported as such rather than silently dropped.
//
// A later verdict replaces an earlier one. That is deliberate: the approval-time
// check is the one made moments before money moves, so it should be what the
// admin page shows.
func (r *WalletRepository) RecordWithdrawalVerification(ctx context.Context, reference, status, resolvedName, detail string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_withdrawals
		SET name_check_status = $1, name_check_resolved = $2,
			name_check_detail = $3, name_check_at = $4
		WHERE reference = $5`,
		status, resolvedName, detail, time.Now(), reference,
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

// RevertWithdrawalToPending returns a withdrawal to the approval queue after a
// payout failed to start, so an admin can reject it and release the reserved
// funds.
//
// BeginWithdrawal is the point of no return: it flips the row to 'processing'
// and RejectWithdrawal only matches 'pending', so a payout that fails before the
// provider is reached would otherwise sit with the farmer's money reserved and
// nothing able to release it. This is the way back.
//
// It is deliberately narrow. Only a row that is still 'processing' AND has no
// saved provider transfer id can be returned. If a transfer id was stored, a
// real transfer may exist at Flutterwave, and moving the row back to 'pending'
// would let it be rejected and refunded while the provider also paid it out -
// the one way this path could lose money twice over. Refusing is the safe
// answer: that row stays reserved for ReconcileWithdrawal and a human reading
// the provider's dashboard.
func (r *WalletRepository) RevertWithdrawalToPending(ctx context.Context, reference string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_withdrawals
		SET status = 'pending', approved_by = NULL, updated_at = $1
		WHERE reference = $2 AND status = 'processing' AND provider_transfer_id IS NULL`,
		time.Now(), reference,
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

// CancelWithdrawal releases a request the farmer withdrew themselves. The
// matched farmer_id is what stops one user cancelling another's request; a row
// someone else owns reads as not found rather than as forbidden, which also
// avoids confirming that it exists.
//
// Only a pending row is matched. Once BeginWithdrawal has moved it to
// processing, an admin has started paying it and the money may already be on its
// way - releasing the reservation then could pay it twice.
func (r *WalletRepository) CancelWithdrawal(ctx context.Context, withdrawalID int64, farmerID int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var walletID int64
	var amount, status string
	err = tx.QueryRowContext(ctx, `
		SELECT wallet_id, amount::text, status FROM wallet_withdrawals
		WHERE id = $1 AND farmer_id = $2 FOR UPDATE`, withdrawalID, farmerID,
	).Scan(&walletID, &amount, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWithdrawalNotFound
	}
	if err != nil {
		return err
	}
	if status != models.WithdrawalPending {
		return ErrRequestNotPending
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_withdrawals SET status = 'cancelled', updated_at = $1 WHERE id = $2`,
		time.Now(), withdrawalID,
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
		walletID, fmt.Sprintf("withdrawal-cancel:%d", withdrawalID), amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ExpireStaleWithdrawals releases requests nobody decided on inside the ageing
// window and returns what was released, so the caller can tell each farmer.
//
// The provider_transfer_id IS NULL guard mirrors RevertWithdrawalToPending: a
// row carrying a saved transfer may describe a payout Flutterwave has already
// made, and releasing it would pay the farmer a second time. Those rows stay for
// an admin; the ageing reminder is what surfaces them.
func (r *WalletRepository) ExpireStaleWithdrawals(ctx context.Context, cutoff time.Time) ([]models.Withdrawal, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, farmer_id, wallet_id, amount::text
		FROM wallet_withdrawals
		WHERE status = 'pending' AND provider_transfer_id IS NULL AND created_at < $1
		ORDER BY created_at ASC, id ASC
		LIMIT 100
		FOR UPDATE`, cutoff)
	if err != nil {
		return nil, err
	}

	type staleWithdrawal struct {
		id, farmerID, walletID int64
		amount                 string
	}
	var stale []staleWithdrawal
	for rows.Next() {
		var row staleWithdrawal
		if err := rows.Scan(&row.id, &row.farmerID, &row.walletID, &row.amount); err != nil {
			rows.Close()
			return nil, err
		}
		stale = append(stale, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	expired := make([]models.Withdrawal, 0, len(stale))
	for _, row := range stale {
		if _, err := tx.ExecContext(ctx, `
			UPDATE wallet_withdrawals SET status = 'expired', updated_at = $1 WHERE id = $2`,
			time.Now(), row.id,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
			row.amount, time.Now(), row.walletID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
			VALUES ($1, $2, 'withdrawal_refund', $3::numeric)`,
			row.walletID, fmt.Sprintf("withdrawal-expired:%d", row.id), row.amount,
		); err != nil {
			return nil, err
		}
		expired = append(expired, models.Withdrawal{
			ID:       row.id,
			FarmerID: int(row.farmerID),
			Amount:   row.amount,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return expired, nil
}

// ClaimWithdrawalReminder records that admins have been nudged about one aged
// request. The update only matches a row that has not been reminded about yet,
// so two application instances running the pass cannot both send the nudge.
func (r *WalletRepository) ClaimWithdrawalReminder(ctx context.Context, withdrawalID int64, cutoff time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_withdrawals SET reminded_at = $1
		WHERE id = $2 AND status IN ('pending','processing')
			AND reminded_at IS NULL AND created_at < $3`,
		time.Now(), withdrawalID, cutoff,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// CompleteTransfer settles a payout on the provider's word, refunding the wallet
// when the transfer failed or was reversed.
//
// The bool reports whether this call is the one that moved the row out of
// 'processing'. It is a claim about the transition, not about the argument: a
// second call for an already-settled row writes nothing and returns false, which
// is what lets a caller tell the farmer once - webhooks are retried, and being
// told twice that your money came back reads as a second refund.
func (r *WalletRepository) CompleteTransfer(ctx context.Context, reference, transferID, status string, amount int64, currency string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
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
		return false, ErrWithdrawalNotFound
	}
	if err != nil {
		return false, err
	}
	if currentStatus == "paid" || currentStatus == "failed" {
		return false, nil
	}
	if currentStatus != "processing" || !storedTransferID.Valid || storedTransferID.String != transferID {
		return false, fmt.Errorf("transfer does not match a processing withdrawal")
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
				return false, rowsErr
			}
			if affected != 1 {
				return false, fmt.Errorf("verified transfer amount or currency does not match withdrawal")
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
		return false, fmt.Errorf("transfer has non-terminal status %q", status)
	}
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, fmt.Errorf("verified transfer amount or currency does not match withdrawal")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// GetWithdrawalByReference reads one withdrawal by its reference, which is how
// the webhook path knows whose payout it just settled - the payload carries the
// reference and nothing else that names a farmer.
func (r *WalletRepository) GetWithdrawalByReference(ctx context.Context, reference string) (*models.Withdrawal, error) {
	var withdrawal models.Withdrawal
	err := r.db.QueryRowContext(ctx, `
		SELECT x.id, x.farmer_id, f.full_name, x.amount::text, x.currency,
			x.bank_name, x.bank_code, x.account_name, x.account_number,
			x.reference, x.provider_transfer_id, x.status,
			x.name_check_status, x.name_check_resolved, x.name_check_detail, x.name_check_at,
			x.created_at, x.updated_at
		FROM wallet_withdrawals x
		JOIN farmers f ON f.id = x.farmer_id
		WHERE x.reference = $1`, reference,
	).Scan(
		&withdrawal.ID, &withdrawal.FarmerID, &withdrawal.FullName, &withdrawal.Amount, &withdrawal.Currency,
		&withdrawal.BankName, &withdrawal.BankCode, &withdrawal.AccountName, &withdrawal.AccountNumber,
		&withdrawal.Reference, &withdrawal.TransferID, &withdrawal.Status,
		&withdrawal.NameCheckStatus, &withdrawal.NameCheckResolved, &withdrawal.NameCheckDetail, &withdrawal.NameCheckAt,
		&withdrawal.CreatedAt, &withdrawal.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWithdrawalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

// --- wallet-to-wallet transfers -------------------------------------------
//
// A transfer mirrors a withdrawal: the sender's balance is reduced when the
// transfer is requested, and the recipient is credited only when an admin
// approves it. Rejection puts the money back. Nothing here talks to the payment
// provider — both sides are internal ledger movements.

// transferColumns is shared by the two listings so a field added to one cannot
// drift out of step with the other's scan.
const transferColumns = `
	SELECT t.id, t.sender_id, sender.full_name, t.recipient_id, recipient.full_name,
		t.recipient_phone, t.amount::text, t.currency, t.note, t.reference, t.status,
		t.created_at, t.updated_at
	FROM wallet_transfers t
	JOIN farmers sender ON sender.id = t.sender_id
	JOIN farmers recipient ON recipient.id = t.recipient_id`

func scanTransfers(rows *sql.Rows) ([]models.Transfer, error) {
	defer rows.Close()
	var transfers []models.Transfer
	for rows.Next() {
		var transfer models.Transfer
		if err := rows.Scan(
			&transfer.ID, &transfer.SenderID, &transfer.SenderName,
			&transfer.RecipientID, &transfer.RecipientName, &transfer.RecipientPhone,
			&transfer.Amount, &transfer.Currency, &transfer.Note, &transfer.Reference,
			&transfer.Status, &transfer.CreatedAt, &transfer.UpdatedAt,
		); err != nil {
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	return transfers, rows.Err()
}

// CreateTransfer reserves amount from the sender and records a pending
// transfer to recipientID.
//
// The recipient is identified by account ID here, not by the phone number the
// user typed: resolving a phone to an account is the service's job, and doing
// it inside this transaction would mean re-implementing phone normalization in
// SQL. The recipient's stored phone is copied onto the row for the record.
func (r *WalletRepository) CreateTransfer(ctx context.Context, senderID, recipientID int, reference, amount, note string) error {
	if senderID <= 0 || recipientID <= 0 {
		return ErrInvalidTransferParticipants
	}
	if senderID == recipientID {
		return ErrSelfTransfer
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock the sender's wallet: the balance check and the debit must not be
	// separated by another request spending the same money.
	var senderWalletID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM wallets WHERE farmer_id = $1 FOR UPDATE`, senderID,
	).Scan(&senderWalletID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWalletNotFound
	}
	if err != nil {
		return err
	}

	var recipientWalletID int64
	var recipientPhone string
	err = tx.QueryRowContext(ctx, `
		SELECT w.id, f.phone
		FROM wallets w
		JOIN farmers f ON f.id = w.farmer_id
		WHERE w.farmer_id = $1`, recipientID,
	).Scan(&recipientWalletID, &recipientPhone)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWalletNotFound
	}
	if err != nil {
		return err
	}
	// Belt and braces: the service already refuses a self-transfer, but the
	// ledger would be nonsense if one ever got through.
	if recipientWalletID == senderWalletID {
		return ErrSelfTransfer
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance - $1::numeric, updated_at = $2
		WHERE id = $3 AND balance >= $1::numeric`, amount, time.Now(), senderWalletID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrInsufficientBalance
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transfers
			(sender_wallet_id, sender_id, recipient_wallet_id, recipient_id, recipient_phone, reference, amount, currency, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, 'NGN', $8)`,
		senderWalletID, senderID, recipientWalletID, recipientID, recipientPhone, reference, amount, note,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'transfer_out', $3::numeric)`,
		senderWalletID, "transfer:"+reference, amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WalletRepository) ListFarmerTransfers(ctx context.Context, farmerID int) ([]models.Transfer, error) {
	rows, err := r.db.QueryContext(ctx, transferColumns+`
		WHERE t.sender_id = $1 OR t.recipient_id = $1
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT 100`, farmerID)
	if err != nil {
		return nil, err
	}
	return scanTransfers(rows)
}

func (r *WalletRepository) ListPendingTransfers(ctx context.Context) ([]models.Transfer, error) {
	rows, err := r.db.QueryContext(ctx, transferColumns+`
		WHERE t.status = 'pending'
		ORDER BY t.created_at ASC, t.id ASC
		LIMIT 100`)
	if err != nil {
		return nil, err
	}
	return scanTransfers(rows)
}

// ListAdminIDs reads the accounts allowed to review wallet requests. The flag is
// on the account so granting that role does not require a config change and a
// restart; the service caches the answer and re-reads it before serving an
// admin page or action.
func (r *WalletRepository) ListAdminIDs(ctx context.Context) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM farmers WHERE is_admin ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListAdminAccounts reads the accounts flagged as admins, with the name and
// phone the approvals page shows beside them.
func (r *WalletRepository) ListAdminAccounts(ctx context.Context) ([]models.AdminAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, full_name, phone
		FROM farmers
		WHERE is_admin
		ORDER BY full_name ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.AdminAccount
	for rows.Next() {
		var account models.AdminAccount
		if err := rows.Scan(&account.FarmerID, &account.FullName, &account.Phone); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// SetAdmin writes the admin flag on one account. It reports whether the account
// exists, so the page can say "no such account" rather than claim a change that
// did not happen.
func (r *WalletRepository) SetAdmin(ctx context.Context, farmerID int, admin bool) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE farmers
		SET is_admin = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, admin, farmerID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ApproveTransfer credits the recipient and settles a pending transfer.
//
// It returns the settled transfer so the caller can tell the recipient without
// a second read.
func (r *WalletRepository) ApproveTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var transfer models.Transfer
	var senderWalletID, recipientWalletID int64
	err = tx.QueryRowContext(ctx, `
		SELECT t.id, t.sender_wallet_id, t.sender_id, sender.full_name,
			t.recipient_wallet_id, t.recipient_id,
			t.recipient_phone, t.amount::text, t.currency, t.note, t.reference, t.status
		FROM wallet_transfers t
		JOIN farmers sender ON sender.id = t.sender_id
		WHERE t.id = $1 FOR UPDATE OF t`, transferID,
	).Scan(
		&transfer.ID, &senderWalletID, &transfer.SenderID, &transfer.SenderName,
		&recipientWalletID, &transfer.RecipientID,
		&transfer.RecipientPhone, &transfer.Amount, &transfer.Currency, &transfer.Note,
		&transfer.Reference, &transfer.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	if transfer.Status != models.TransferPending {
		return nil, fmt.Errorf("this transfer has already been %s", transfer.Status)
	}
	// The sender must not be the approver. Checked before the UPDATE below so a
	// refused self-approval leaves the transfer untouched and still pending.
	if transfer.SenderID == adminID {
		return nil, ErrSelfApproval
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transfers SET status = 'completed', approved_by = $1, updated_at = $2
		WHERE id = $3`, adminID, time.Now(), transferID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		transfer.Amount, time.Now(), recipientWalletID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'transfer_in', $3::numeric)`,
		recipientWalletID, "transfer-in:"+transfer.Reference, transfer.Amount,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	transfer.Status = models.TransferCompleted
	return &transfer, nil
}

// RejectTransfer returns the reserved funds to the sender. It returns the
// rejected transfer so the caller can notify the sender.
func (r *WalletRepository) RejectTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var transfer models.Transfer
	var senderWalletID int64
	err = tx.QueryRowContext(ctx, `
		SELECT t.id, t.sender_wallet_id, t.sender_id, sender.full_name,
			t.recipient_id, recipient.full_name, t.recipient_phone,
			t.amount::text, t.currency, t.note, t.reference, t.status
		FROM wallet_transfers t
		JOIN farmers sender ON sender.id = t.sender_id
		JOIN farmers recipient ON recipient.id = t.recipient_id
		WHERE t.id = $1 FOR UPDATE OF t`, transferID,
	).Scan(
		&transfer.ID, &senderWalletID, &transfer.SenderID, &transfer.SenderName,
		&transfer.RecipientID, &transfer.RecipientName, &transfer.RecipientPhone,
		&transfer.Amount, &transfer.Currency, &transfer.Note,
		&transfer.Reference, &transfer.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	if transfer.Status != models.TransferPending {
		return nil, fmt.Errorf("this transfer has already been %s", transfer.Status)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transfers SET status = 'rejected', approved_by = $1, updated_at = $2
		WHERE id = $3`, adminID, time.Now(), transferID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		transfer.Amount, time.Now(), senderWalletID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'transfer_refund', $3::numeric)`,
		senderWalletID, "transfer-refund:"+transfer.Reference, transfer.Amount,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	transfer.Status = models.TransferRejected
	return &transfer, nil
}

// CancelTransfer releases a transfer the sender no longer wants, returning the
// reserved funds to the sender's wallet.
//
// The matched sender_id is the ownership check; someone else's transfer reads as
// not found. Only a pending transfer can be cancelled: once approved the
// recipient has been credited, and there is nothing left to release.
func (r *WalletRepository) CancelTransfer(ctx context.Context, transferID int64, senderID int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var senderWalletID int64
	var amount, status string
	err = tx.QueryRowContext(ctx, `
		SELECT sender_wallet_id, amount::text, status FROM wallet_transfers
		WHERE id = $1 AND sender_id = $2 FOR UPDATE`, transferID, senderID,
	).Scan(&senderWalletID, &amount, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTransferNotFound
	}
	if err != nil {
		return err
	}
	if status != models.TransferPending {
		return ErrRequestNotPending
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_transfers SET status = 'cancelled', updated_at = $1 WHERE id = $2`,
		time.Now(), transferID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		amount, time.Now(), senderWalletID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'transfer_refund', $3::numeric)`,
		senderWalletID, fmt.Sprintf("transfer-cancel:%d", transferID), amount,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ExpireStaleTransfers is the transfer half of ExpireStaleWithdrawals. A
// transfer holds the sender's money and never reaches a payment provider, so a
// pending row past the ageing window is safe to release and refund.
func (r *WalletRepository) ExpireStaleTransfers(ctx context.Context, cutoff time.Time) ([]models.Transfer, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, sender_id, sender_wallet_id, amount::text
		FROM wallet_transfers
		WHERE status = 'pending' AND created_at < $1
		ORDER BY created_at ASC, id ASC
		LIMIT 100
		FOR UPDATE`, cutoff)
	if err != nil {
		return nil, err
	}

	type staleTransfer struct {
		id, senderID, senderWalletID int64
		amount                       string
	}
	var stale []staleTransfer
	for rows.Next() {
		var row staleTransfer
		if err := rows.Scan(&row.id, &row.senderID, &row.senderWalletID, &row.amount); err != nil {
			rows.Close()
			return nil, err
		}
		stale = append(stale, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	expired := make([]models.Transfer, 0, len(stale))
	for _, row := range stale {
		if _, err := tx.ExecContext(ctx, `
			UPDATE wallet_transfers SET status = 'expired', updated_at = $1 WHERE id = $2`,
			time.Now(), row.id,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
			row.amount, time.Now(), row.senderWalletID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
			VALUES ($1, $2, 'transfer_refund', $3::numeric)`,
			row.senderWalletID, fmt.Sprintf("transfer-expired:%d", row.id), row.amount,
		); err != nil {
			return nil, err
		}
		expired = append(expired, models.Transfer{
			ID:       row.id,
			SenderID: int(row.senderID),
			Amount:   row.amount,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return expired, nil
}

// ClaimTransferReminder is the transfer half of ClaimWithdrawalReminder.
func (r *WalletRepository) ClaimTransferReminder(ctx context.Context, transferID int64, cutoff time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE wallet_transfers SET reminded_at = $1
		WHERE id = $2 AND status = 'pending'
			AND reminded_at IS NULL AND created_at < $3`,
		time.Now(), transferID, cutoff,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}
