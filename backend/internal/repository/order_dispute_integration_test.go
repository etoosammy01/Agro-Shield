package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"backend/internal/models"
)

func createPaidOrderForDispute(t *testing.T) (*sql.DB, int, int, int64) {
	t.Helper()

	db := testDB(t)
	buyer := createTestUser(t, db)
	seller := createTestUser(t, db)

	crop := &models.Crop{
		FarmerID:         seller.ID,
		Name:             "Dispute Test Maize",
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

	// Remove dependent dispute records before deleting the order.
	t.Cleanup(func() {
		db.Exec(`
			DELETE FROM order_dispute_evidence
			WHERE dispute_id IN (
				SELECT id FROM order_disputes WHERE order_id IN (
					SELECT id FROM orders WHERE crop_id = $1
				)
			)
		`, crop.ID)

		db.Exec(`
			DELETE FROM order_dispute_events
			WHERE dispute_id IN (
				SELECT id FROM order_disputes WHERE order_id IN (
					SELECT id FROM orders WHERE crop_id = $1
				)
			)
		`, crop.ID)

		db.Exec(`
			DELETE FROM refunds
			WHERE order_id IN (
				SELECT id FROM orders WHERE crop_id = $1
			)
		`, crop.ID)

		db.Exec(`
			DELETE FROM order_disputes
			WHERE order_id IN (
				SELECT id FROM orders WHERE crop_id = $1
			)
		`, crop.ID)

		db.Exec(`DELETE FROM orders WHERE crop_id = $1`, crop.ID)
		db.Exec(`DELETE FROM crops WHERE id = $1`, crop.ID)
	})

	fundWallet(t, db, buyer.ID, "5000.00")

	orderID, _, err := NewWalletRepository(db).PayForOrder(
		context.Background(),
		buyer.ID,
		crop.ID,
		1,
		uniqueRef("DISPUTE-CHECKOUT"),
		0,
	)
	if err != nil {
		t.Fatalf("pay for test order: %v", err)
	}

	return db, buyer.ID, seller.ID, orderID
}

func newTestDispute(orderID int64, buyerID int) *models.OrderDispute {
	return &models.OrderDispute{
		OrderID:         orderID,
		BuyerID:         buyerID,
		ReasonCode:      "damaged_item",
		Description:     "The produce arrived damaged.",
		RequestedAmount: "500.00",
	}
}

func TestCreateOrderDisputeAndPreventDuplicateActive(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)

	first := newTestDispute(orderID, buyerID)

	if err := repo.Create(context.Background(), first); err != nil {
		t.Fatalf("create first dispute: %v", err)
	}

	if first.ID <= 0 {
		t.Fatal("dispute ID was not assigned")
	}
	if first.Status != models.DisputeSubmitted {
		t.Fatalf("status = %q, want submitted", first.Status)
	}
	if first.SellerID != sellerID {
		t.Fatalf("seller ID = %d, want %d", first.SellerID, sellerID)
	}

	second := newTestDispute(orderID, buyerID)
	err := repo.Create(context.Background(), second)

	if !errors.Is(err, ErrActiveDisputeExists) {
		t.Fatalf("second active dispute error = %v, want ErrActiveDisputeExists", err)
	}

	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM order_disputes WHERE order_id = $1
	`, orderID).Scan(&count)
	if err != nil {
		t.Fatalf("count disputes: %v", err)
	}
	if count != 1 {
		t.Fatalf("dispute count = %d, want 1", count)
	}

	if balance := walletBalanceOf(t, db, buyerID); balance != "4000.00" {
		t.Fatalf("buyer balance changed during dispute submission: %s", balance)
	}
}

func TestOrderDisputeEnforcesBuyerAndPaymentOwnership(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)

	// A seller cannot submit a dispute as though they were the buyer.
	err := repo.Create(context.Background(), newTestDispute(orderID, sellerID))
	if !errors.Is(err, ErrDisputeNotBuyer) {
		t.Fatalf("wrong buyer error = %v, want ErrDisputeNotBuyer", err)
	}

	// Create an unpaid order directly. It must not qualify for a refund dispute.
	var unpaidOrderID int64
	err = db.QueryRow(`
		INSERT INTO orders (buyer_id, crop_id, quantity, total_price, status)
		VALUES ($1, (SELECT crop_id FROM orders WHERE id = $2), 1, 1000, 'pending')
		RETURNING id
	`, buyerID, orderID).Scan(&unpaidOrderID)
	if err != nil {
		t.Fatalf("create unpaid order: %v", err)
	}

	dispute := newTestDispute(unpaidOrderID, buyerID)
	err = repo.Create(context.Background(), dispute)

	if !errors.Is(err, ErrDisputeNotPaid) {
		t.Fatalf("unpaid order error = %v, want ErrDisputeNotPaid", err)
	}
}

func TestOrderDisputeSellerResponseAndAdminRejection(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)

	admin := createTestUser(t, db)

	dispute := newTestDispute(orderID, buyerID)
	if err := repo.Create(context.Background(), dispute); err != nil {
		t.Fatalf("create dispute: %v", err)
	}

	// The buyer cannot respond as the seller.
	err := repo.AddSellerResponse(
		context.Background(),
		dispute.ID,
		buyerID,
		"We disagree with this complaint.",
	)
	if !errors.Is(err, ErrDisputeNotFound) {
		t.Fatalf("unauthorized seller response error = %v, want ErrDisputeNotFound", err)
	}

	// The actual seller can respond.
	err = repo.AddSellerResponse(
		context.Background(),
		dispute.ID,
		sellerID,
		"We packed the produce in good condition.",
	)
	if err != nil {
		t.Fatalf("seller response: %v", err)
	}

	// Administrator moves the dispute into review.
	err = repo.StartAdminReview(
		context.Background(),
		dispute.ID,
		admin.ID,
	)
	if err != nil {
		t.Fatalf("start admin review: %v", err)
	}

	// Administrator rejects with a recorded reason.
	reason := "The submitted evidence does not support the complaint."
	err = repo.Reject(
		context.Background(),
		dispute.ID,
		admin.ID,
		reason,
	)
	if err != nil {
		t.Fatalf("reject dispute: %v", err)
	}

	got, err := repo.GetByID(context.Background(), dispute.ID)
	if err != nil {
		t.Fatalf("get resolved dispute: %v", err)
	}
	if got == nil {
		t.Fatal("resolved dispute not found")
	}
	if got.Status != models.DisputeResolvedRejected {
		t.Fatalf("status = %q, want resolved_rejected", got.Status)
	}
	if got.ResolvedBy == nil || *got.ResolvedBy != admin.ID {
		t.Fatal("administrator decision was not recorded")
	}
	if got.ResolutionNote == nil || *got.ResolutionNote != reason {
		t.Fatal("rejection reason was not recorded")
	}

	events, err := repo.ListEvents(context.Background(), dispute.ID)
	if err != nil {
		t.Fatalf("list dispute events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4", len(events))
	}

	if events[0].EventType != "dispute_submitted" {
		t.Fatalf("first event = %q, want dispute_submitted", events[0].EventType)
	}
	if events[1].EventType != "seller_responded" {
		t.Fatalf("second event = %q, want seller_responded", events[1].EventType)
	}
	if events[2].EventType != "admin_review_started" {
		t.Fatalf("third event = %q, want admin_review_started", events[2].EventType)
	}
	if events[3].EventType != "dispute_rejected" {
		t.Fatalf("fourth event = %q, want dispute_rejected", events[3].EventType)
	}
}

func TestConcurrentDisputeSubmissionsCreateOnlyOneActiveDispute(t *testing.T) {
	db, buyerID, _, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)

	start := make(chan struct{})
	var wg sync.WaitGroup

	errs := make([]error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()
			<-start

			dispute := newTestDispute(orderID, buyerID)
			errs[i] = repo.Create(context.Background(), dispute)
		}(i)
	}

	close(start)
	wg.Wait()

	successes := 0
	duplicates := 0

	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrActiveDisputeExists):
			duplicates++
		default:
			t.Fatalf("unexpected concurrent submission error: %v", err)
		}
	}

	if successes != 1 || duplicates != 1 {
		t.Fatalf(
			"successful submissions = %d, duplicates = %d; want 1 each",
			successes,
			duplicates,
		)
	}

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM order_disputes
		WHERE order_id = $1
		  AND status IN ('submitted', 'seller_responded', 'under_review')
	`, orderID).Scan(&count); err != nil {
		t.Fatalf("count active disputes: %v", err)
	}

	if count != 1 {
		t.Fatalf("active dispute count = %d, want 1", count)
	}
}
