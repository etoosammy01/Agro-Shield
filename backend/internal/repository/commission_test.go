package repository

import "testing"

func TestSplitCommission(t *testing.T) {
	// Rates are basis points: 250 is 2.5%, 10000 would be 100%.
	tests := []struct {
		name    string
		total   int64
		rate    int64
		wantFee int64
		wantNet int64
	}{
		{name: "commission off credits full total", total: 12550, rate: 0, wantFee: 0, wantNet: 12550},
		{name: "two and a half percent", total: 10000, rate: 250, wantFee: 250, wantNet: 9750},
		{name: "ten percent", total: 45000, rate: 1000, wantFee: 4500, wantNet: 40500},
		{name: "fractional rate", total: 33333, rate: 175, wantFee: 583, wantNet: 32750},

		// The kobo fee comes from dividing by 10000, so the interesting cases
		// are the ones that land exactly on a half kobo.
		{name: "half kobo rounds up", total: 100, rate: 250, wantFee: 3, wantNet: 97},
		{name: "just under half kobo rounds down", total: 100, rate: 249, wantFee: 2, wantNet: 98},
		{name: "rounds to a whole kobo", total: 40, rate: 250, wantFee: 1, wantNet: 39},

		// Rounding must never take the seller's whole payout, and must never
		// produce a zero sale credit — the ledger rejects amount = 0.
		{name: "rounding cannot consume the whole total", total: 1, rate: 5000, wantFee: 0, wantNet: 1},
		{name: "near maximum rate still leaves a kobo", total: 1, rate: 9999, wantFee: 0, wantNet: 1},
		{name: "high rate on a small sale leaves a kobo", total: 2, rate: 9999, wantFee: 1, wantNet: 1},
		{name: "large sale at near maximum rate", total: 5000, rate: 9999, wantFee: 4999, wantNet: 1},

		{name: "non-positive total is returned untouched", total: 0, rate: 250, wantFee: 0, wantNet: 0},
		{name: "negative rate means no commission", total: 500, rate: -250, wantFee: 0, wantNet: 500},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fee, net := splitCommission(test.total, test.rate)
			if fee != test.wantFee || net != test.wantNet {
				t.Fatalf("splitCommission(%d, %d) = (%d, %d), want (%d, %d)",
					test.total, test.rate, fee, net, test.wantFee, test.wantNet)
			}
		})
	}
}

// TestSplitCommissionInvariants sweeps the range rather than spot-checking, so
// a rounding change that happens to keep the chosen examples correct still
// fails here.
func TestSplitCommissionInvariants(t *testing.T) {
	rates := []int64{0, 1, 50, 175, 250, 999, 1000, 2500, 5000, 9999}

	for total := int64(1); total <= 3000; total += 7 {
		for _, rate := range rates {
			fee, net := splitCommission(total, rate)

			if fee+net != total {
				t.Fatalf("total %d at rate %d: fee %d + net %d != total", total, rate, fee, net)
			}
			if fee < 0 || net < 0 {
				t.Fatalf("total %d at rate %d: negative split (%d, %d)", total, rate, fee, net)
			}
			// A positive sale must always produce a ledger row the schema
			// accepts, hence strictly greater than zero.
			if net <= 0 {
				t.Fatalf("total %d at rate %d: net %d is not a recordable sale credit", total, rate, net)
			}
			if rate > 0 && fee > total*rate/10000+1 {
				t.Fatalf("total %d at rate %d: fee %d exceeds the configured rate", total, rate, fee)
			}
		}
	}
}

func TestKoboToAmount(t *testing.T) {
	tests := []struct {
		kobo int64
		want string
	}{
		{kobo: 0, want: "0.00"},
		{kobo: 5, want: "0.05"},
		{kobo: 50, want: "0.50"},
		{kobo: 100, want: "1.00"},
		{kobo: 12550, want: "125.50"},
		{kobo: 999999999, want: "9999999.99"},
	}

	for _, test := range tests {
		if got := koboToAmount(test.kobo); got != test.want {
			t.Errorf("koboToAmount(%d) = %q, want %q", test.kobo, got, test.want)
		}
	}
}

// TestKoboToAmountMatchesWalletTransactionConstraint guards the property the
// ledger actually depends on: anything above zero renders as something the
// NUMERIC(12,2) amount column accepts.
func TestKoboToAmountMatchesWalletTransactionConstraint(t *testing.T) {
	for kobo := int64(1); kobo <= 500; kobo++ {
		got := koboToAmount(kobo)
		if got == "0.00" || got[0] == '-' {
			t.Fatalf("koboToAmount(%d) = %q, which the amount > 0 constraint would reject", kobo, got)
		}
	}
}

func TestFormatBasisPoints(t *testing.T) {
	tests := []struct {
		basisPoints int64
		want        string
	}{
		{basisPoints: 0, want: "0.00"},
		{basisPoints: 5, want: "0.05"},
		{basisPoints: 50, want: "0.50"},
		{basisPoints: 100, want: "1.00"},
		{basisPoints: 250, want: "2.50"},
		{basisPoints: 9999, want: "99.99"},
	}

	for _, test := range tests {
		if got := FormatBasisPoints(test.basisPoints); got != test.want {
			t.Errorf("FormatBasisPoints(%d) = %q, want %q", test.basisPoints, got, test.want)
		}
	}
}
