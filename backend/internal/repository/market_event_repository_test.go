package repository

import (
	"testing"
	"time"
)

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
