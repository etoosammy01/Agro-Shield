package models

import (
	"database/sql"
	"time"
)

type Payment struct {
	ID            int64
	OrderID       int64
	Reference     string
	TransactionID sql.NullString
	Amount        string
	Currency      string
	Status        string
	Provider      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const (
	PaymentPending    = "pending"
	PaymentSuccessful = "successful"
	PaymentFailed     = "failed"
)
