package models

import "testing"

func TestProduceDemandSummaryFields(t *testing.T) {
	s := ProduceDemandSummary{Views: 10, CartAdds: 4, Orders: 2, Negotiations: 3}
	if got := s.Views + s.CartAdds + s.Orders + s.Negotiations; got != 19 {
		t.Fatalf("unexpected activity total: %d", got)
	}
}
