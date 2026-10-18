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
var ErrTransferNotFound = errors.New("transfer not found")

// ErrVirtualAccountNotFound is the repository's "no row" answer. Callers that
// are deciding whether to call the provider must tell it apart from a database
// failure: only a definite absence permits a create attempt.
var ErrVirtualAccountNotFound = errors.New("virtual account not found")

// ErrDepositReversalUnfunded means a provider reversal arrived after the money
// had already been spent or withdrawn, so the wallet cannot absorb it without
// going negative. Nothing is changed; the deposit stays successful for an
// operator to reconcile.
var ErrDepositReversalUnfunded = errors.New("wallet balance cannot cover the deposit reversal")

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

// GetStatus reads the owner's committed balance plus enough identifiers for the
// wallet page to tell whether anything changed since it rendered. It never
// writes, credits, or asks the payment provider anything.
func (r *WalletRepository) GetStatus(ctx context.Context, farmerID int) (*models.WalletStatus, error) {
	var status models.WalletStatus
	err := r.db.QueryRowContext(ctx, `
		SELECT
			w.balance::text,
			w.currency,
			(SELECT COUNT(*) FROM wallet_deposits d
				WHERE d.wallet_id = w.id AND d.status IN ('pending', 'processing')),
			COALESCE((SELECT MAX(t.id) FROM wallet_transactions t WHERE t.wallet_id = w.id), 0),
			COALESCE((SELECT MAX(d.id) FROM wallet_deposits d WHERE d.wallet_id = w.id), 0),
			COALESCE((SELECT d.status FROM wallet_deposits d
				WHERE d.wallet_id = w.id ORDER BY d.id DESC LIMIT 1), '')
		FROM wallets w
		WHERE w.farmer_id = $1`, farmerID,
	).Scan(
		&status.Balance, &status.Currency, &status.PendingDeposits,
		&status.LatestTransactionID, &status.LatestDepositID, &status.LatestDepositStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	return &status, err
}

// EnsureWallet returns the user's wallet, creating it if it is somehow missing.
//
// Registration already inserts a wallet in the same transaction as the account
// (FarmerRepository.Create) and RunMigration backfills users that predate the
// wallets table. This covers every other way a wallet can go missing — a restored
// backup, a hand-inserted row — so the owner heals on first use instead of being
// stuck on a permanent "could not load wallet".
//
// Concurrent calls are safe: farmer_id is UNIQUE, so a losing
// INSERT ... ON CONFLICT DO NOTHING writes nothing and both callers read the
// same row. An unknown farmerID fails the FK, which is correct: no wallet can
// exist for a user who does not.
func (r *WalletRepository) EnsureWallet(ctx context.Context, farmerID int) (*models.Wallet, error) {
	if farmerID <= 0 {
		return nil, ErrWalletNotFound
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO wallets (farmer_id) VALUES ($1)
		ON CONFLICT (farmer_id) DO NOTHING`, farmerID); err != nil {
		return nil, err
	}
	return r.GetWallet(ctx, farmerID)
}

// DepositProvider returns the payment provider recorded on the wallet deposit
// with this reference. ErrWalletNotFound means no wallet funding attempt uses
// that reference, which is how callers tell a wallet deposit apart from an order
// payment without relying on the reference's shape.
func (r *WalletRepository) DepositProvider(ctx context.Context, reference string) (string, error) {
	var provider string
	err := r.db.QueryRowContext(ctx, `
		SELECT provider FROM wallet_deposits WHERE reference = $1`, reference).Scan(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWalletNotFound
	}
	if err != nil {
		return "", err
	}
	return provider, nil
}

// CreateDeposit records a funding attempt before the provider is called, so an
// ambiguous provider response always has a durable row to reconcile against.
func (r *WalletRepository) CreateDeposit(ctx context.Context, farmerID int, provider, reference, amount string) error {
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("deposit provider is required")
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO wallet_deposits (wallet_id, provider, reference, amount, currency)
		SELECT id, $4, $2, $3::numeric, 'NGN' FROM wallets WHERE farmer_id = $1`,
		farmerID, reference, amount, provider,
	)
	if err != nil {
		return err
	}
	// The INSERT ... SELECT writes nothing when the user has no wallet row. That
	// used to return nil, which let the caller go on to create a payment the
	// wallet could never be credited for. Report it instead.
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrWalletNotFound
	}
	return nil
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
		SET status = 'successful', provider_transaction_id = $1, updated_at = $2,
			verified_amount = $4::numeric, verified_currency = $5
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
		SELECT d.id, d.provider, d.amount::text, d.currency, d.reference, d.status,
			COALESCE(d.verified_amount::text, ''), d.created_at
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
		if err := rows.Scan(
			&deposit.ID, &deposit.Provider, &deposit.Amount, &deposit.Currency,
			&deposit.Reference, &deposit.Status, &deposit.VerifiedAmount, &deposit.CreatedAt,
		); err != nil {
			return nil, err
		}
		deposits = append(deposits, deposit)
	}
	return deposits, rows.Err()
}

// StalePendingDeposits lists funding attempts that a provider has not settled
// within the given age, oldest first, for an operator to investigate.
//
// This is a report only: it never moves money and never credits a wallet. A
// deposit is only ever settled by CompleteDeposit, from a verified provider
// confirmation, so re-running this can't double-credit anything.
func (r *WalletRepository) StalePendingDeposits(ctx context.Context, olderThan time.Duration, limit int) ([]models.WalletDeposit, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, d.provider, d.amount::text, d.currency, d.reference, d.status,
			COALESCE(d.verified_amount::text, ''), d.created_at
		FROM wallet_deposits d
		WHERE d.status IN ('pending', 'processing')
			AND d.created_at < CURRENT_TIMESTAMP - $1::interval
		ORDER BY d.created_at ASC, d.id ASC
		LIMIT $2`, fmt.Sprintf("%d seconds", int64(olderThan.Seconds())), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deposits []models.WalletDeposit
	for rows.Next() {
		var deposit models.WalletDeposit
		if err := rows.Scan(
			&deposit.ID, &deposit.Provider, &deposit.Amount, &deposit.Currency,
			&deposit.Reference, &deposit.Status, &deposit.VerifiedAmount, &deposit.CreatedAt,
		); err != nil {
			return nil, err
		}
		deposits = append(deposits, deposit)
	}
	return deposits, rows.Err()
}

// --- permanent virtual accounts --------------------------------------------
//
// A farmer has at most one permanent NGN account. It is stored separately from
// wallet_deposits because it is an account, not a payment: its tx_ref is reused
// by every incoming transfer, so it cannot be the unique key of a deposit. Each
// incoming transfer is recorded in wallet_deposits under the provider's own
// transaction id instead, which is what makes duplicate deliveries harmless.

const virtualAccountColumns = `
	id, farmer_id, provider, tx_ref, order_ref, provider_ref, account_number,
	bank_name, account_name, currency, status, identity_kind, consent_version,
	consent_at, failure_reason, last_checked_at, created_at, updated_at`

type virtualAccountScanner interface {
	Scan(dest ...any) error
}

func scanVirtualAccount(row virtualAccountScanner) (*models.WalletVirtualAccount, error) {
	var account models.WalletVirtualAccount
	var consentAt, lastChecked sql.NullTime
	err := row.Scan(
		&account.ID, &account.FarmerID, &account.Provider, &account.TxRef,
		&account.OrderRef, &account.ProviderRef, &account.AccountNumber,
		&account.BankName, &account.AccountName, &account.Currency,
		&account.Status, &account.IdentityKind, &account.ConsentVersion,
		&consentAt, &account.FailureReason, &lastChecked,
		&account.CreatedAt, &account.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVirtualAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	if consentAt.Valid {
		account.ConsentAt = consentAt.Time
	}
	if lastChecked.Valid {
		account.LastCheckedAt = lastChecked.Time
	}
	return &account, nil
}

func (r *WalletRepository) GetVirtualAccount(ctx context.Context, farmerID int) (*models.WalletVirtualAccount, error) {
	return scanVirtualAccount(r.db.QueryRowContext(ctx, `
		SELECT `+virtualAccountColumns+`
		FROM wallet_virtual_accounts WHERE farmer_id = $1`, farmerID))
}

// GetVirtualAccountByTxRef is how an incoming transfer is matched to its owner.
// The webhook's tx_ref is the account's permanent reference; it is never taken
// from the browser or from the transfer amount.
func (r *WalletRepository) GetVirtualAccountByTxRef(ctx context.Context, txRef string) (*models.WalletVirtualAccount, error) {
	return scanVirtualAccount(r.db.QueryRowContext(ctx, `
		SELECT `+virtualAccountColumns+`
		FROM wallet_virtual_accounts WHERE tx_ref = $1`, txRef))
}

// EnsureVirtualAccountAttempt creates the one row a farmer may have, or returns
// the row another caller already created. The bool reports whether this call
// inserted it. Two simultaneous requests therefore agree on one tx_ref instead
// of both asking the provider to open an account.
func (r *WalletRepository) EnsureVirtualAccountAttempt(ctx context.Context, farmerID int, provider, txRef, identityKind, consentVersion string, now time.Time) (*models.WalletVirtualAccount, bool, error) {
	account, err := scanVirtualAccount(r.db.QueryRowContext(ctx, `
		INSERT INTO wallet_virtual_accounts
			(farmer_id, provider, tx_ref, identity_kind, consent_version, consent_at, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $6)
		ON CONFLICT (farmer_id) DO NOTHING
		RETURNING `+virtualAccountColumns,
		farmerID, provider, txRef, identityKind, consentVersion, now))
	if err == nil {
		return account, true, nil
	}
	if !errors.Is(err, ErrVirtualAccountNotFound) {
		return nil, false, err
	}
	existing, err := r.GetVirtualAccount(ctx, farmerID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

// ResetVirtualAccountAttempt refreshes the consent record on a retry and puts a
// failed row back into pending. An active account is never reset: its details
// stay valid and a retry must not replace it.
func (r *WalletRepository) ResetVirtualAccountAttempt(ctx context.Context, farmerID int, identityKind, consentVersion string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_virtual_accounts
		SET status = 'pending', identity_kind = $2, consent_version = $3,
			consent_at = $4, failure_reason = '', updated_at = $4
		WHERE farmer_id = $1 AND status <> 'active'`,
		farmerID, identityKind, consentVersion, now)
	return err
}

// ActivateVirtualAccount stores the provider's confirmed details. It refuses to
// overwrite a different active account, which protects the farmer from a
// late-arriving response to an old attempt changing where their money lands.
func (r *WalletRepository) ActivateVirtualAccount(ctx context.Context, farmerID int, orderRef, providerRef, accountNumber, bankName, accountName, currency string, now time.Time) error {
	current, err := r.GetVirtualAccount(ctx, farmerID)
	if err != nil {
		return err
	}
	if current.Status == models.VirtualAccountActive {
		if current.AccountNumber == accountNumber {
			return nil
		}
		return fmt.Errorf("a different virtual account is already active for this farmer")
	}
	if strings.TrimSpace(currency) == "" {
		currency = "NGN"
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE wallet_virtual_accounts
		SET status = 'active', order_ref = $2, provider_ref = $3,
			account_number = $4, bank_name = $5, account_name = $6,
			currency = $7, failure_reason = '', last_checked_at = $8, updated_at = $8
		WHERE farmer_id = $1 AND (status <> 'active' OR account_number = $4)`,
		farmerID, orderRef, providerRef, accountNumber, bankName, accountName, currency, now)
	return err
}

func (r *WalletRepository) FailVirtualAccountAttempt(ctx context.Context, farmerID int, reason string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_virtual_accounts
		SET status = 'failed', failure_reason = $2, updated_at = $3
		WHERE farmer_id = $1 AND status <> 'active'`,
		farmerID, reason, now)
	return err
}

func (r *WalletRepository) MarkVirtualAccountChecked(ctx context.Context, farmerID int, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_virtual_accounts SET last_checked_at = $2 WHERE farmer_id = $1`,
		farmerID, now)
	return err
}

// ListVirtualAccountsForReconciliation rotates through active accounts, least
// recently checked first, so one pass is bounded and every account is reached.
func (r *WalletRepository) ListVirtualAccountsForReconciliation(ctx context.Context, limit int) ([]models.WalletVirtualAccount, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+virtualAccountColumns+`
		FROM wallet_virtual_accounts
		WHERE status = 'active'
		ORDER BY last_checked_at ASC NULLS FIRST, id ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []models.WalletVirtualAccount
	for rows.Next() {
		account, err := scanVirtualAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *account)
	}
	return accounts, rows.Err()
}

// ListPendingVirtualAccountDeposits lists transfers the provider reported but
// did not settle, so the pass can verify them by their own transaction id.
func (r *WalletRepository) ListPendingVirtualAccountDeposits(ctx context.Context, limit int) ([]models.PendingVirtualAccountTransfer, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT w.farmer_id, d.reference, d.provider_transaction_id, d.amount::text, d.currency
		FROM wallet_deposits d
		JOIN wallets w ON w.id = d.wallet_id
		WHERE d.provider = 'flutterwave'
			AND d.status = 'pending'
			AND d.provider_transaction_id IS NOT NULL
			AND d.reference LIKE 'VA-%'
		ORDER BY d.created_at ASC, d.id ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var transfers []models.PendingVirtualAccountTransfer
	for rows.Next() {
		var transfer models.PendingVirtualAccountTransfer
		if err := rows.Scan(&transfer.FarmerID, &transfer.Reference,
			&transfer.ProviderTransactionID, &transfer.Amount, &transfer.Currency); err != nil {
			return nil, err
		}
		transfers = append(transfers, transfer)
	}
	return transfers, rows.Err()
}

// RecordPendingVirtualAccountDeposit remembers an incoming transfer the provider
// has not settled. It never credits the wallet; it only gives reconciliation a
// transaction id to ask about later.
func (r *WalletRepository) RecordPendingVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wallet_deposits
			(wallet_id, provider, reference, provider_transaction_id, amount, currency, status)
		SELECT id, 'flutterwave', $2, $3, $4::numeric, $5, 'pending'
		FROM wallets WHERE farmer_id = $1
		ON CONFLICT (provider, provider_transaction_id) DO NOTHING`,
		farmerID, reference, providerTxnID, amount, currency)
	return err
}

// CreditVirtualAccountDeposit credits a verified incoming transfer exactly once.
//
// The provider transaction id is the idempotency key: a retried webhook, a
// concurrent delivery and the reconciliation pass all name the same payment, and
// only the call that inserts the successful row moves the balance. The amount
// moves as a NUMERIC value, never as a float.
func (r *WalletRepository) CreditVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var walletID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM wallets WHERE farmer_id = $1`, farmerID).Scan(&walletID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrWalletNotFound
	}
	if err != nil {
		return false, err
	}

	var depositID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO wallet_deposits
			(wallet_id, provider, reference, provider_transaction_id, amount, currency,
			 verified_amount, verified_currency, status)
		VALUES ($1, 'flutterwave', $2, $3, $4::numeric, $5, $4::numeric, $5, 'successful')
		ON CONFLICT (provider, provider_transaction_id) DO NOTHING
		RETURNING id`,
		walletID, reference, providerTxnID, amount, currency).Scan(&depositID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	credited := false
	if err == nil {
		credited = true
	} else {
		// The payment is already recorded. Credit only a row that was still
		// pending for this same wallet and the same verified money; anything else
		// is a conflict that a human has to look at.
		var storedDepositID, storedWalletID int64
		var storedStatus, storedAmount, storedCurrency, storedReference string
		err = tx.QueryRowContext(ctx, `
			SELECT id, wallet_id, status, amount::text, currency, reference
			FROM wallet_deposits
			WHERE provider = 'flutterwave' AND provider_transaction_id = $1
			FOR UPDATE`, providerTxnID,
		).Scan(&storedDepositID, &storedWalletID, &storedStatus, &storedAmount, &storedCurrency, &storedReference)
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("virtual account deposit disappeared during credit")
		}
		if err != nil {
			return false, err
		}
		if storedWalletID != walletID {
			return false, fmt.Errorf("provider transaction belongs to a different wallet")
		}
		if storedStatus == models.DepositSuccessful {
			if storedAmount == amount && storedCurrency == currency {
				return false, nil
			}
			return false, fmt.Errorf("provider transaction was already credited with different details")
		}
		if storedStatus != models.DepositPending && storedStatus != models.DepositProcessing {
			return false, fmt.Errorf("provider transaction cannot be credited from status %s", storedStatus)
		}
		if storedAmount != amount || storedCurrency != currency {
			return false, fmt.Errorf("verified amount or currency does not match the pending transfer")
		}
		reference = storedReference
		if _, err := tx.ExecContext(ctx, `
			UPDATE wallet_deposits
			SET status = 'successful', verified_amount = $1::numeric,
				verified_currency = $2, updated_at = $3
			WHERE id = $4`,
			amount, currency, time.Now(), storedDepositID); err != nil {
			return false, err
		}
		credited = true
	}

	if !credited {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + $1::numeric, updated_at = $2 WHERE id = $3`,
		amount, time.Now(), walletID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
		VALUES ($1, $2, 'deposit', $3::numeric)`,
		walletID, "deposit:"+reference, amount); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// FailVirtualAccountDeposit closes a pending incoming transfer the provider says
// did not succeed. No balance moves: a pending transfer was never credited.
func (r *WalletRepository) FailVirtualAccountDeposit(ctx context.Context, providerTxnID, status string, now time.Time) error {
	if status != models.DepositFailed && status != models.DepositReversed {
		return fmt.Errorf("invalid deposit failure status %q", status)
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE wallet_deposits SET status = $2, updated_at = $3
		WHERE provider = 'flutterwave' AND provider_transaction_id = $1 AND status = 'pending'`,
		providerTxnID, status, now)
	return err
}

// ReverseVirtualAccountDeposit unwinds a transfer the provider has reversed.
//
// A pending transfer is simply marked reversed. A credited one is debited only
// when the wallet still holds the money; otherwise nothing is changed and the
// caller is told, because forcing the debit would violate the wallet's
// non-negative balance rule and hide a real shortfall.
func (r *WalletRepository) ReverseVirtualAccountDeposit(ctx context.Context, providerTxnID, amount, currency string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var depositID, walletID int64
	var status, storedAmount, storedCurrency, reference string
	err = tx.QueryRowContext(ctx, `
		SELECT id, wallet_id, status, amount::text, currency, reference
		FROM wallet_deposits
		WHERE provider = 'flutterwave' AND provider_transaction_id = $1
		FOR UPDATE`, providerTxnID,
	).Scan(&depositID, &walletID, &status, &storedAmount, &storedCurrency, &reference)
	if errors.Is(err, sql.ErrNoRows) {
		// Nothing was recorded, so there is no funded balance to unwind.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status == models.DepositReversed {
		return false, nil
	}
	if status == models.DepositSuccessful {
		if storedAmount != amount || !strings.EqualFold(storedCurrency, currency) {
			return false, fmt.Errorf("reversal details do not match the credited transfer")
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE wallets SET balance = balance - $1::numeric, updated_at = $2
			WHERE id = $3 AND balance >= $1::numeric`,
			amount, now, walletID)
		if err != nil {
			return false, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if affected != 1 {
			return false, ErrDepositReversalUnfunded
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_transactions (wallet_id, reference, type, amount)
			VALUES ($1, $2, 'deposit_reversal', $3::numeric)`,
			walletID, "deposit-reversal:"+providerTxnID, amount); err != nil {
			return false, err
		}
	} else if status != models.DepositPending && status != models.DepositProcessing &&
		status != models.DepositFailed && status != models.DepositExpired {
		return false, fmt.Errorf("deposit cannot be reversed from status %s", status)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_deposits SET status = 'reversed', updated_at = $2 WHERE id = $1`,
		depositID, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
