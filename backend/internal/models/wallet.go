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
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (w Withdrawal) MaskedAccountNumber() string {
	if len(w.AccountNumber) <= 4 {
		return "••••"
	}
	return "••••••" + w.AccountNumber[len(w.AccountNumber)-4:]
}
