package repository

import (
	"errors"
	"testing"
)

// Saving a profile with a number another account already holds must not put the
// database's constraint text in front of the user, and must not change either
// account. The repository maps it to ErrPhoneExists for the handler to explain.
func TestUpdateProfileRejectsAnotherAccountsPhone(t *testing.T) {
	db := testDB(t)
	first := createTestUser(t, db)
	second := createTestUser(t, db)

	repo := NewFarmerRepository(db)
	err := repo.UpdateProfile(second.ID, second.FullName, first.Phone, second.Email,
		second.Location, second.LGA, second.State, second.Country)
	if !errors.Is(err, ErrPhoneExists) {
		t.Fatalf("update profile with another account's phone = %v, want ErrPhoneExists", err)
	}

	stored, err := repo.GetByID(second.ID)
	if err != nil {
		t.Fatalf("reload second user: %v", err)
	}
	if stored.Phone != second.Phone {
		t.Fatalf("phone changed to %q after a rejected update", stored.Phone)
	}
}
