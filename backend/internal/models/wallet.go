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

type WalletDeposit struct {
	Amount    string
	Currency  string
	Reference string
	Status    string
	CreatedAt time.Time
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

// Payout-details check verdicts.
//
// They live here rather than in services because they are stored on the row and
// rendered by the templates, both of which this package serves; services names
// the same three values as PayoutVerified/PayoutMismatch/PayoutUnverified, so
// there is one definition of each string.
const (
	NameCheckVerified   = "verified"
	NameCheckMismatch   = "mismatch"
	NameCheckUnverified = "unverified"
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
