package models

import (
	"database/sql"
	"time"
)

type Wallet struct {
	ID                int64
	FarmerID          int
	Balance           string
	Currency          string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	HasTransactionPIN bool
}

// WalletStatus is the small read-only snapshot the wallet page polls while it is
// open. It exists so a confirmed credit can move the on-screen balance and
// history without the browser ever computing or crediting anything: the server
// reports what is already committed, and the page reloads when it changes.
type WalletStatus struct {
	Balance             string
	Currency            string
	PendingDeposits     int
	LatestTransactionID int64
	LatestDepositID     int64
	LatestDepositStatus string
}

type WalletTransaction struct {
	ID        int64
	Type      string
	Amount    string
	Reference string
	CreatedAt time.Time
}

// Wallet funding providers. wallet_deposits.provider records which vendor owns a
// funding attempt, so confirmation is routed from stored data rather than from
// the shape of the reference string. Monnify is added alongside its client.
const ProviderFlutterwave = "flutterwave"

// Funding lifecycle. A deposit is only ever credited from DepositPending, and
// the terminal states are never processed again.
//
// Expired is deliberately not a synonym for failed. Failed means the payment
// provider answered and the payment did not succeed; expired means the
// reconciliation pass asked and the provider had no transaction for the
// reference at all. Only that second answer may close an attempt nobody paid.
const (
	DepositPending    = "pending"
	DepositProcessing = "processing"
	DepositSuccessful = "successful"
	DepositFailed     = "failed"
	DepositExpired    = "expired"
	DepositReversed   = "reversed"
)

type WalletDeposit struct {
	ID             int64
	Provider       string
	Amount         string
	Currency       string
	Reference      string
	Status         string
	VerifiedAmount string
	CreatedAt      time.Time
}

// Permanent virtual-account lifecycle.
//
// A single permanent account belongs to a farmer for as long as the account
// exists, so it is stored once and reused for every incoming bank transfer.
// Pending means the provider has not confirmed the account yet; the farmer may
// have an account at Flutterwave even so, which is why a retry asks the
// provider before creating anything. Failed means the provider refused the
// attempt outright. Active is the only state whose details are shown as usable.
const (
	VirtualAccountPending = "pending"
	VirtualAccountActive  = "active"
	VirtualAccountFailed  = "failed"
)

// Expired is deliberately not a synonym for failed. Failed means the payment
// provider answered and the payment did not succeed; expired means the
// reconciliation pass asked and the provider had no transaction for the
// reference at all. Only that second answer may close an attempt nobody paid.
// const (
// 	// DepositPending    = "pending"
// 	DepositSuccessful = "successful"
// 	DepositFailed     = "failed"
// 	DepositExpired    = "expired"
// )

// WalletVirtualAccount is the farmer's permanent NGN funding account.
//
// The BVN or NIN used to open it is deliberately absent: it is sent to
// Flutterwave during setup and never stored here. IdentityKind records which
// document was used so the wallet page can describe the verification honestly,
// and ConsentAt/ConsentVersion record that the farmer agreed to the share.
type WalletVirtualAccount struct {
	ID          int64
	FarmerID    int
	Provider    string
	TxRef       string
	OrderRef    string
	ProviderRef string

	AccountNumber string
	BankName      string
	AccountName   string
	Currency      string
	Status        string

	IdentityKind   string
	ConsentVersion string
	ConsentAt      time.Time
	FailureReason  string
	LastCheckedAt  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Ready reports whether the account can be shown as a usable funding account.
func (a WalletVirtualAccount) Ready() bool {
	return a.Status == VirtualAccountActive && a.AccountNumber != ""
}

// MaskedAccountNumber is safe to use where a full number would be too much
// detail, such as a notification. The wallet page itself shows the full number
// because the farmer needs to copy it.
func (a WalletVirtualAccount) MaskedAccountNumber() string {
	if len(a.AccountNumber) <= 4 {
		return "••••"
	}
	return "••••••" + a.AccountNumber[len(a.AccountNumber)-4:]
}

// PendingVirtualAccountTransfer is an incoming bank transfer that Flutterwave
// reported but has not settled. Reconciliation re-checks it by the provider's
// own transaction id, because a static account's permanent tx_ref identifies
// the account rather than one payment.
type PendingVirtualAccountTransfer struct {
	FarmerID              int
	Reference             string
	ProviderTransactionID string
	Amount                string
	Currency              string
}

// PendingDeposit is an attempt that has been waiting on the payment provider
// longer than the reconciliation window. The admin page lists these as its
// exception list; the reconciliation pass re-checks the same rows with the
// provider rather than letting them accumulate silently.
type PendingDeposit struct {
	FarmerID  int
	FullName  string
	Amount    string
	Currency  string
	Reference string
	CreatedAt time.Time
}

// AdminAccount is one row of the admin list on the approvals page. The role
// lives on the farmer account so it can be granted and revoked from the page
// itself, without a database client or a restart.
type AdminAccount struct {
	FarmerID int
	FullName string
	Phone    string

	// FromConfig marks an admin granted by WALLET_ADMIN_USER_IDS. That grant
	// lives in the server environment, so the page can list it but must not
	// offer a remove button that would not stick.
	FromConfig bool
}

type Withdrawal struct {
	ID            int64
	FarmerID      int
	FullName      string
	Amount        string
	Currency      string
	BankName      string
	BankCode      string
	AccountName   string
	AccountNumber string
	Reference     string
	TransferID    sql.NullString
	Status        string

	// The payout-details check recorded when this withdrawal was requested and
	// again when it was approved. Empty means it was never checked - every row
	// written before this check existed, which is why the admin page must treat
	// empty as "not checked" rather than as a pass.
	//
	// NameCheckResolved holds the holder the bank reported, and is only worth
	// showing when it differs from the claimed AccountName.
	NameCheckStatus   string
	NameCheckResolved string
	NameCheckDetail   string
	NameCheckAt       sql.NullTime

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Payout-details check verdicts.//
// They live here rather than in services because they are stored on the row and
// rendered by the templates, both of which this package serves; services names
// the same three values as PayoutVerified/PayoutMismatch/PayoutUnverified, so
// there is one definition of each string.
const (
	NameCheckVerified   = "verified"
	NameCheckMismatch   = "mismatch"
	NameCheckUnverified = "unverified"
)

// Withdrawal statuses.
//
// Cancelled and expired both return the reserved money to the wallet, but they
// are recorded separately: cancelled is the farmer choosing to stop, expired is
// the ageing pass releasing a request nobody acted on. Neither is a decision an
// admin made, so neither reads as rejected.
const (
	WithdrawalPending    = "pending"
	WithdrawalProcessing = "processing"
	WithdrawalPaid       = "paid"
	WithdrawalRejected   = "rejected"
	WithdrawalFailed     = "failed"
	WithdrawalCancelled  = "cancelled"
	WithdrawalExpired    = "expired"
)

func (w Withdrawal) MaskedAccountNumber() string {
	if len(w.AccountNumber) <= 4 {
		return "••••"
	}
	return "••••••" + w.AccountNumber[len(w.AccountNumber)-4:]
}

// Wallet-to-wallet transfer statuses.
//
// A transfer is reserved from the sender the moment it is requested, and the
// recipient is only credited once an admin approves it — the same reserve-first
// shape withdrawals already use, so a mistaken or fraudulent transfer can be
// reversed without chasing money that has already left.
const (
	TransferPending   = "pending"
	TransferCompleted = "completed"
	TransferRejected  = "rejected"
	TransferCancelled = "cancelled"
	TransferExpired   = "expired"
)

type Transfer struct {
	ID             int64
	SenderID       int
	SenderName     string
	RecipientID    int
	RecipientName  string
	RecipientPhone string
	Amount         string
	Currency       string
	Note           string
	Reference      string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IsIncoming reports whether this transfer credits the given user rather than
// debiting them, so one list can present sent and received transfers together.
func (t Transfer) IsIncoming(farmerID int) bool {
	return t.RecipientID == farmerID && t.SenderID != farmerID
}
