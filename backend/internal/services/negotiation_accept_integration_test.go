package services

import (
	"database/sql"
	"fmt"
	"math/rand"
	"testing"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/testdb"
)

// These tests run the real negotiation service against PostgreSQL because the
// behaviour under test — claiming an offer only while it is still pending, then
// putting the accepted deal in the cart exactly once — depends on the database's
// conditional UPDATE and on the repository SQL. An in-memory fake would not
// prove it.
//
// They skip unless TEST_DATABASE_URL points at a throwaway database.

func negotiationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.Open(t)
}

func negotiationTestUser(t *testing.T, db *sql.DB, role string) *models.Farmer {
	t.Helper()

	farmer := &models.Farmer{
		FullName:     "Negotiation Test " + role,
		Phone:        fmt.Sprintf("234%09d%04d", rand.Intn(1_000_000_000), rand.Intn(10_000)),
		Location:     "Kano",
		State:        "Kano",
		Country:      "Nigeria",
		Role:         role,
		PasswordHash: "test-password-hash",
		PhotoURL:     "/static/assets/images/un.jpeg",
	}
	if err := repository.NewFarmerRepository(db).Create(farmer); err != nil {
		t.Fatalf("create %s: %v", role, err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM cart_items WHERE buyer_id = $1`, farmer.ID)
		db.Exec(`DELETE FROM negotiation_messages WHERE sender_id = $1`, farmer.ID)
		db.Exec(`DELETE FROM negotiations WHERE buyer_id = $1 OR farmer_id = $1`, farmer.ID)
		db.Exec(`DELETE FROM crops WHERE farmer_id = $1`, farmer.ID)
		db.Exec(`DELETE FROM wallets WHERE farmer_id = $1`, farmer.ID)
		db.Exec(`DELETE FROM farmers WHERE id = $1`, farmer.ID)
	})
	return farmer
}

func negotiationTestCrop(t *testing.T, db *sql.DB, farmerID int) *models.Crop {
	t.Helper()

	crop := &models.Crop{
		FarmerID:         farmerID,
		Name:             "Test Maize",
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
	if err := repository.NewCropRepository(db).Create(crop); err != nil {
		t.Fatalf("create crop: %v", err)
	}
	return crop
}

func newNegotiationTestService(db *sql.DB) (*NegotiationService, *repository.NegotiationMessageRepository, *repository.CartRepository) {
	negotiationRepo := repository.NewNegotiationRepository(db)
	messageRepo := repository.NewNegotiationMessageRepository(db)
	cropRepo := repository.NewCropRepository(db)
	cartRepo := repository.NewCartRepository(db)
	cartService := NewCartService(cartRepo, cropRepo, nil)
	service := NewNegotiationService(
		negotiationRepo,
		messageRepo,
		cropRepo,
		cartService,
		nil,
		nil,
	)
	return service, messageRepo, cartRepo
}

// A farmer accepting an offer used to leave the negotiation marked accepted but
// the buyer's cart empty: the service passed the stale in-memory negotiation
// (status "open") to AddFromNegotiation, which refuses anything not accepted.
func TestAcceptNegotiationAddsTheDealToTheCart(t *testing.T) {
	db := negotiationTestDB(t)
	seller := negotiationTestUser(t, db, "farmer")
	buyer := negotiationTestUser(t, db, "buyer")
	crop := negotiationTestCrop(t, db, seller.ID)

	service, messageRepo, cartRepo := newNegotiationTestService(db)

	negotiation, err := service.StartNegotiation(buyer.ID, crop.ID, 5, 800, "Can you do 800?")
	if err != nil {
		t.Fatalf("start negotiation: %v", err)
	}

	thread, err := messageRepo.ListByNegotiation(negotiation.ID)
	if err != nil {
		t.Fatalf("load negotiation messages: %v", err)
	}
	if len(thread) != 1 || thread[0].MessageType != "offer" {
		t.Fatalf("expected one offer message, got %#v", thread)
	}
	offerID := thread[0].ID

	if err := service.Accept(negotiation.ID, offerID, seller.ID); err != nil {
		t.Fatalf("accept offer: %v", err)
	}

	stored, err := service.repo.GetByID(negotiation.ID)
	if err != nil {
		t.Fatalf("reload negotiation: %v", err)
	}
	if stored.Status != "accepted" {
		t.Fatalf("negotiation status = %q, want accepted", stored.Status)
	}

	offer, err := messageRepo.GetByID(offerID)
	if err != nil {
		t.Fatalf("reload offer: %v", err)
	}
	if offer.OfferStatus != "accepted" {
		t.Fatalf("offer status = %q, want accepted", offer.OfferStatus)
	}

	items, err := cartRepo.ListByBuyer(buyer.ID)
	if err != nil {
		t.Fatalf("load cart: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("cart items = %d, want 1", len(items))
	}
	item := items[0]
	if item.NegotiationID != negotiation.ID {
		t.Fatalf("cart negotiation id = %d, want %d", item.NegotiationID, negotiation.ID)
	}
	if item.Quantity != 5 {
		t.Fatalf("cart quantity = %g, want 5", item.Quantity)
	}
	if item.PricePerUnit != 800 {
		t.Fatalf("cart price = %g, want 800", item.PricePerUnit)
	}
	if item.TotalPrice != 4000 {
		t.Fatalf("cart total = %g, want 4000", item.TotalPrice)
	}
}

// Accepting twice (double click, repeated request) must not add the same deal
// to the cart twice.
func TestAcceptNegotiationTwiceAddsOneCartItem(t *testing.T) {
	db := negotiationTestDB(t)
	seller := negotiationTestUser(t, db, "farmer")
	buyer := negotiationTestUser(t, db, "buyer")
	crop := negotiationTestCrop(t, db, seller.ID)

	service, messageRepo, cartRepo := newNegotiationTestService(db)

	negotiation, err := service.StartNegotiation(buyer.ID, crop.ID, 5, 800, "Can you do 800?")
	if err != nil {
		t.Fatalf("start negotiation: %v", err)
	}
	thread, err := messageRepo.ListByNegotiation(negotiation.ID)
	if err != nil || len(thread) != 1 {
		t.Fatalf("load offer: %v (messages: %d)", err, len(thread))
	}
	offerID := thread[0].ID

	if err := service.Accept(negotiation.ID, offerID, seller.ID); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if err := service.Accept(negotiation.ID, offerID, seller.ID); err == nil {
		t.Fatal("second accept succeeded, want an error")
	}

	items, err := cartRepo.ListByBuyer(buyer.ID)
	if err != nil {
		t.Fatalf("load cart: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("cart items after duplicate accept = %d, want 1", len(items))
	}
}
