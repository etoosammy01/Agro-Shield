package models

import "time"

type Feedback struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	FarmerID  int       `json:"farmer_id"`
	ProductID int       `json:"product_id"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}
