package services

import (
	"errors"
	"testing"

	"backend/internal/repository"
	"backend/internal/testdb"
)

func TestAuthPhoneFormatsShareOneAccountWithoutChangingPassword(t *testing.T) {
	db := testdb.Open(t)
	auth := NewAuthService(repository.NewFarmerRepository(db))
	password := " synthetic password "
	f, err := auth.Register("Synthetic", "Member", "0803 123-4567", "member@example.test", password, "Community", "Kano", "Kano", "Nigeria", "farmer", "", "", "", "")
	if err != nil || f.Phone != "2348031234567" {
		t.Fatalf("canonical signup failed: %v", err)
	}
	for _, raw := range []string{"08031234567", "0803 123 4567", "+234 (803) 123-4567", "2348031234567"} {
		logged, err := auth.Login(raw, password)
		if err != nil || logged.ID != f.ID {
			t.Fatalf("formatted login failed: %v", err)
		}
		found, err := auth.FindByPhone(raw)
		if err != nil || found.ID != f.ID {
			t.Fatalf("formatted recovery lookup failed: %v", err)
		}
		if _, err := auth.Register("Synthetic", "Duplicate", raw, "duplicate@example.test", password, "Community", "Kano", "Kano", "Nigeria", "farmer", "", "", "", ""); !errors.Is(err, ErrPhoneTaken) {
			t.Fatalf("duplicate format registered: %v", err)
		}
	}
	if _, err := auth.Login(f.Phone, "synthetic password"); err == nil {
		t.Fatal("password whitespace was normalized")
	}
	if _, err := auth.Login("abc08031234567", password); err == nil {
		t.Fatal("invalid phone letters were discarded")
	}
}
