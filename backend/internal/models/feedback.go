package models

import "time"

type Feedback struct {
	ID int

	UserID int

	FarmerID int

	ProductID int

	Rating int

	Comment string

	CreatedAt time.Time
}
