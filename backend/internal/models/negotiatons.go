package models

import "time"

type Negotiation struct {
	ID         int
	CropID     int
	BuyerID    int
	FarmerID   int
	Quantity   float64
	Status     string // "open", "accepted", "rejected", "expired"
	RoundCount int
	MaxRounds  int
	CreatedAt  time.Time
	ExpiresAt  time.Time

	// Populated for display only
	CropName       string
	BuyerName      string
	SellerName     string
	BuyerPhotoURL  string
	SellerPhotoURL string
}

func (n *Negotiation) TimeLeft() time.Duration {
	d := time.Until(n.ExpiresAt)
	if d < 0 {
		return 0
	}
	return d
}

func (n *Negotiation) IsExpired() bool {
	return time.Now().After(n.ExpiresAt)
}

func (n *Negotiation) RoundsLeft() int {
	left := n.MaxRounds - n.RoundCount
	if left < 0 {
		return 0
	}
	return left
}
