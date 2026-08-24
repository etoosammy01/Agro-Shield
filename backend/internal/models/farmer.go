package models

import "time"

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
}

func (f *Farmer) IsBuyer() bool {
	return f.Role == "buyer"
}

func (f *Farmer) IsFarmer() bool {
	return f.Role != "buyer"
}
