package handlers

import (
	"testing"
	"time"

	"backend/internal/models"
)

func TestFarmerPrioritiesUrgentFirstAndLimited(t *testing.T) {
	data := DashboardData{ProduceInStorage: 10, ListingsActive: 0}
	crops := []models.Crop{{Quantity: 10, CreatedAt: time.Now().Add(-8 * 24 * time.Hour)}}
	diagnoses := []models.Diagnosis{{Result: "Severe disease detected"}}
	negotiations := []models.Negotiation{{Status: "open", ExpiresAt: time.Now().Add(time.Hour)}}

	priorities := farmerPriorities(data, crops, diagnoses, negotiations)
	if len(priorities) != 3 {
		t.Fatalf("expected three priorities, got %d", len(priorities))
	}
	if priorities[0].Tone != "danger" {
		t.Fatalf("expected urgent priority first, got %q", priorities[0].Tone)
	}
}

func TestFarmerPrioritiesOnTrack(t *testing.T) {
	data := DashboardData{ProduceInStorage: 10, ListingsActive: 2, Revenue: 100}
	priorities := farmerPriorities(data, nil, nil, nil)
	if len(priorities) != 1 || priorities[0].Tone != "success" {
		t.Fatalf("expected on-track priority, got %#v", priorities)
	}
}

func TestFarmHealthUrgentDiagnosis(t *testing.T) {
	data := DashboardData{ProduceInStorage: 10, ListingsActive: 1, Revenue: 100}
	_, score, status := farmHealth(data, []models.Diagnosis{{Description: "Urgent treatment required"}})
	if score != 62 || status != "Fair" {
		t.Fatalf("expected urgent diagnosis score 62/Fair, got %d/%q", score, status)
	}
}

func TestBuyerPriorities(t *testing.T) {
	priority := buyerPriorities(DashboardData{})[0]
	if priority.Href != "/marketplace" || priority.Tone != "info" {
		t.Fatalf("expected new buyer marketplace action, got %#v", priority)
	}
	priority = buyerPriorities(DashboardData{PurchasesMade: 1})[0]
	if priority.Tone != "success" {
		t.Fatalf("expected returning buyer on-track state, got %#v", priority)
	}
}

func TestPrioritizeDemandSummariesLimitsAndRanksActions(t *testing.T) {
	items := []models.ProduceDemandSummary{
		{ProduceType: "Healthy", Orders: 2},
		{ProduceType: "No activity"},
		{ProduceType: "Price alert", PriceDifferencePercent: 20},
		{ProduceType: "Cart interest", CartAdds: 2},
	}
	got := prioritizeDemandSummaries(items, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 insights, got %d", len(got))
	}
	if got[0].ProduceType != "Price alert" || got[1].ProduceType != "Cart interest" || got[2].ProduceType != "No activity" {
		t.Fatalf("unexpected insight order: %#v", got)
	}
}

func TestCartSummary(t *testing.T) {
	count, total := cartSummary([]models.CartItem{{TotalPrice: 1250}, {TotalPrice: 750}})
	if count != 2 || total != 2000 {
		t.Fatalf("got count=%d total=%v", count, total)
	}
}
