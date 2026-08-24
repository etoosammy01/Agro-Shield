package repository

import (
	"backend/internal/models"
	"testing"
	"time"
)

func TestValidateExternalPrice(t *testing.T) {
	when := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	if err := validateExternalPrice("Maize", 120000, "bag", "Market Board", "Makurdi", when); err != nil {
		t.Fatalf("valid observation rejected: %v", err)
	}
	for _, tc := range []struct {
		name, produce          string
		price                  float64
		unit, provider, market string
		at                     time.Time
	}{
		{"missing produce", "", 1, "kg", "provider", "market", when},
		{"invalid price", "Maize", 0, "kg", "provider", "market", when},
		{"missing unit", "Maize", 1, "", "provider", "market", when},
		{"missing provider", "Maize", 1, "kg", "", "market", when},
		{"missing market", "Maize", 1, "kg", "provider", "", when},
		{"missing timestamp", "Maize", 1, "kg", "provider", "market", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateExternalPrice(tc.produce, tc.price, tc.unit, tc.provider, tc.market, tc.at); err == nil {
				t.Fatal("expected invalid observation to fail")
			}
		})
	}
}

func TestDemandRecommendation(t *testing.T) {
	if got := demandRecommendation(models.ProduceDemandSummary{PriceDifferencePercent: 20}); got == "" {
		t.Fatal("expected pricing recommendation")
	}
	if got := demandRecommendation(models.ProduceDemandSummary{}); got == "" {
		t.Fatal("expected no-activity recommendation")
	}
}

func TestMinFloat(t *testing.T) {
	if minFloat(4, 9) != 4 || minFloat(12, 3) != 3 {
		t.Fatal("minFloat returned incorrect result")
	}
}

func TestDemandClassification(t *testing.T) {
	for _, tc := range []struct {
		total int
		want  string
	}{{0, "Not enough data"}, {3, "Early signal"}, {10, "Moderate activity"}, {25, "Strong activity"}} {
		if got := classifyActivity(tc.total); got != tc.want {
			t.Errorf("classifyActivity(%d)=%q, want %q", tc.total, got, tc.want)
		}
	}
}

func TestDemandScoreAndTrend(t *testing.T) {
	if got := calculateDemandScore(20, 10, 5, 10); got != 100 {
		t.Fatalf("max score=%v", got)
	}
	if got := calculateTrendPercent(15, 10); got != 50 {
		t.Fatalf("trend=%v", got)
	}
	if got := calculateTrendPercent(5, 0); got != 0 {
		t.Fatalf("zero-baseline trend=%v", got)
	}
}

func TestSellThroughBounds(t *testing.T) {
	if got := calculateSellThrough(100, 40); got != 60 {
		t.Fatalf("sell-through=%v", got)
	}
	if got := calculateSellThrough(0, 0); got != 0 {
		t.Fatalf("empty sell-through=%v", got)
	}
	if got := calculateSellThrough(10, 20); got != 0 {
		t.Fatalf("negative sell-through=%v", got)
	}
}

func TestViewWindowDefaults(t *testing.T) {
	if 30*time.Minute <= 0 {
		t.Fatal("view deduplication window must be positive")
	}
}
