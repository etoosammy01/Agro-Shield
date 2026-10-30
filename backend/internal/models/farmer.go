package models

import (
	"database/sql"
	"time"
)

type Farmer struct {
	ID                  int
	FullName            string
	Phone               string
	Email               string
	PasswordHash        string
	Location            string
	LGA, State, Country string
	Role                string // "farmer" or "buyer"
	PhotoURL            string // passport photograph
	CreatedAt           time.Time
	UpdatedAt           time.Time
	SelfieCapturedAt    sql.NullTime
	PhoneVerifiedAt     sql.NullTime
	HasTransactionPIN   bool

	BankName      string
	BankCode      string
	AccountName   string
	AccountNumber string

	// WalletNumber is the permanent Agro-Shield identifier a member can share
	// to receive wallet-to-wallet transfers. It is independent of the bank
	// account used for payouts.
	WalletNumber string
}

func (f *Farmer) IsBuyer() bool {
	return f.Role == "buyer"
}

func (f *Farmer) IsFarmer() bool {
	return f.Role != "buyer"
}
