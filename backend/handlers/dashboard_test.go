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
