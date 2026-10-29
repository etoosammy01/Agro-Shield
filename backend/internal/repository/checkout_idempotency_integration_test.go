package repository

import (
	"context"
	"sync"
	"testing"

	"backend/internal/models"
)

// A checkout form can be submitted twice: a double click, a browser retry, or
// the same page open in two tabs. The token rendered into that form makes the
// second submission find the first order instead of debiting the buyer again.
func TestPayForOrderIsIdempotentPerCheckoutToken(t *testing.T) {
	db := testDB(t)
	buyer := createTestUser(t, db)
	seller := createTestUser(t, db)
	crop := &models.Crop{
		FarmerID:         seller.ID,
		Name:             "Idempotency Maize",
		Quantity:         10,
		Unit:             "kg",
		Location:         "Kano",
		LGA:              "Kano",
		State:            "Kano",
		Country:          "Nigeria",
		Latitude:         12.0022,
		Longitude:        8.5919,
		LocationAccuracy: 10,
		PricePerUnit:     1000,
		ListedForSale:    true,
		ImageURL:         "/static/assets/images/Farmer1.jpeg",
	}
	if err := NewCropRepository(db).Create(crop); err != nil {
		t.Fatalf("create crop: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM orders WHERE crop_id = $1`, crop.ID)
		db.Exec(`DELETE FROM crops WHERE id = $1`, crop.ID)
	})
	fundWallet(t, db, buyer.ID, "5000.00")

	repo := NewWalletRepository(db)
	ctx := context.Background()
	token := uniqueRef("CHECKOUT")
	if _, _, err := repo.PayForOrder(ctx, buyer.ID, crop.ID, 1, uniqueRef("PRICE-CHANGE"), 0, 2000); err == nil {
		t.Fatal("checkout charged a different price from the confirmed amount")
	}
	if balance := walletBalanceOf(t, db, buyer.ID); balance != "5000.00" {
		t.Fatalf("rejected price change debited the buyer: %s", balance)
	}

	firstID, firstReused, err := repo.PayForOrder(ctx, buyer.ID, crop.ID, 1, token, 0)
	if err != nil {
		t.Fatalf("first checkout: %v", err)
	}
	secondID, secondReused, err := repo.PayForOrder(ctx, buyer.ID, crop.ID, 1, token, 0)
	if err != nil {
		t.Fatalf("repeated checkout: %v", err)
	}
	if secondID != firstID {
		t.Fatalf("repeated checkout returned order %d, want the original %d", secondID, firstID)
	}
	if firstReused {
		t.Fatal("first checkout reported an earlier order")
	}
	if !secondReused {
		t.Fatal("repeated checkout did not report the original order")
	}

	if balance := walletBalanceOf(t, db, buyer.ID); balance != "4000.00" {
		t.Fatalf("buyer balance = %s, want 4000.00 (charged once)", balance)
	}
	var orders int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE checkout_token = $1`, token).Scan(&orders); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orders != 1 {
		t.Fatalf("orders for token = %d, want 1", orders)
	}
	if receiptID, err := repo.OrderForCheckoutToken(ctx, buyer.ID, token); err != nil || receiptID != firstID {
		t.Fatalf("completed checkout receipt = %d, error = %v", receiptID, err)
	}
	if receiptID, err := repo.OrderForCheckoutToken(ctx, seller.ID, token); err != nil || receiptID != 0 {
		t.Fatalf("another user could read the buyer's receipt: id = %d, error = %v", receiptID, err)
	}
}

// The same token can arrive concurrently, which is the case a double click
// actually produces. The transaction-scoped advisory lock must make one call
// wait for the other and then reuse its order.
func TestConcurrentPayForOrderWithSameTokenChargesOnce(t *testing.T) {
	db := testDB(t)
	buyer := createTestUser(t, db)
	seller := createTestUser(t, db)
	crop := &models.Crop{
		FarmerID:         seller.ID,
		Name:             "Concurrent Maize",
		Quantity:         10,
		Unit:             "kg",
		Location:         "Kano",
		LGA:              "Kano",
		State:            "Kano",
		Country:          "Nigeria",
		Latitude:         12.0022,
		Longitude:        8.5919,
		LocationAccuracy: 10,
		PricePerUnit:     1000,
		ListedForSale:    true,
		ImageURL:         "/static/assets/images/Farmer1.jpeg",
	}
	if err := NewCropRepository(db).Create(crop); err != nil {
		t.Fatalf("create crop: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM orders WHERE crop_id = $1`, crop.ID)
		db.Exec(`DELETE FROM crops WHERE id = $1`, crop.ID)
	})
	fundWallet(t, db, buyer.ID, "5000.00")

	repo := NewWalletRepository(db)
	token := uniqueRef("CHECKOUT-RACE")

	start := make(chan struct{})
	ids := make([]int64, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], _, errs[i] = repo.PayForOrder(context.Background(), buyer.ID, crop.ID, 1, token, 0)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent checkout %d: %v", i, err)
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("concurrent checkouts returned different orders: %d and %d", ids[0], ids[1])
	}
	if balance := walletBalanceOf(t, db, buyer.ID); balance != "4000.00" {
		t.Fatalf("buyer balance = %s, want 4000.00 (charged once)", balance)
	}
}
