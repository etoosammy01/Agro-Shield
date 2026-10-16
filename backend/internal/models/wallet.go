package models

import (
	"database/sql"
	"time"
)

type Wallet struct {
	ID        int64
	FarmerID  int
	Balance   string
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
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
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

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
