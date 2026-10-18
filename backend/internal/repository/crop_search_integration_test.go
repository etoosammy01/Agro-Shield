package repository

import (
	"database/sql"
	"testing"

	"backend/internal/models"
)

// Marketplace search used to record the term but render every listing, so a
// buyer saw the same products after searching. These tests exercise the SQL
// filter against a real PostgreSQL database, including the unlisted exclusion.
func TestSearchAvailableFiltersByNameLocationAndSeller(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	repo := NewCropRepository(db)

	sellerName := "Search Seller " + uniqueRef("NAME")
	if _, err := db.Exec(`UPDATE farmers SET full_name = $1 WHERE id = $2`, sellerName, farmer.ID); err != nil {
		t.Fatalf("rename test seller: %v", err)
	}

	tomatoes := createListedCrop(t, db, farmer.ID, "Ripe Tomatoes", "Kano", 100, true)
	maize := createListedCrop(t, db, farmer.ID, "Yellow Maize", "Kaduna", 50, true)
	unlisted := createListedCrop(t, db, farmer.ID, "Tomato Seeds", "Kano", 20, false)
	defer cleanupCrops(t, db, farmer.ID)

	cases := []struct {
		name      string
		term      string
		want      []int
		notWanted []int
	}{
		{name: "name match", term: "tomato", want: []int{tomatoes}, notWanted: []int{maize, unlisted}},
		{name: "location match", term: "kaduna", want: []int{maize}, notWanted: []int{tomatoes}},
		{name: "seller name match", term: sellerName, want: []int{tomatoes, maize}, notWanted: []int{unlisted}},
		{name: "no match", term: "cassava", want: nil, notWanted: []int{tomatoes, maize, unlisted}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			crops, err := repo.SearchAvailable(tc.term)
			if err != nil {
				t.Fatalf("search %q: %v", tc.term, err)
			}
			found := make(map[int]bool, len(crops))
			for _, crop := range crops {
				found[crop.ID] = true
			}
			for _, id := range tc.want {
				if !found[id] {
					t.Errorf("search %q did not return crop %d", tc.term, id)
				}
			}
			for _, id := range tc.notWanted {
				if found[id] {
					t.Errorf("search %q returned unwanted crop %d", tc.term, id)
				}
			}
			for _, crop := range crops {
				if !crop.ListedForSale || crop.Quantity <= 0 {
					t.Errorf("search %q returned a crop that is not for sale: %+v", tc.term, crop)
				}
			}
		})
	}
}

func createListedCrop(t *testing.T, db *sql.DB, farmerID int, name, location string, quantity float64, listed bool) int {
	t.Helper()

	crop := &models.Crop{
		FarmerID:         farmerID,
		Name:             name,
		Quantity:         quantity,
		Unit:             "kg",
		Location:         location,
		State:            location,
		Country:          "Nigeria",
		Latitude:         9.0,
		Longitude:        8.5,
		LocationAccuracy: 10,
		PricePerUnit:     1000,
		ListedForSale:    listed,
		ImageURL:         "/static/assets/placeholder/product-placeholder.jpg",
	}
	if err := NewCropRepository(db).Create(crop); err != nil {
		t.Fatalf("create crop %q: %v", name, err)
	}
	return crop.ID
}

func cleanupCrops(t *testing.T, db *sql.DB, farmerID int) {
	t.Helper()

	for _, stmt := range []string{
		`DELETE FROM produce_price_history WHERE crop_id IN (SELECT id FROM crops WHERE farmer_id = $1)`,
		`DELETE FROM crop_images WHERE crop_id IN (SELECT id FROM crops WHERE farmer_id = $1)`,
		`DELETE FROM crops WHERE farmer_id = $1`,
	} {
		if _, err := db.Exec(stmt, farmerID); err != nil {
			t.Logf("crop cleanup for user %d: %v", farmerID, err)
		}
	}
}
