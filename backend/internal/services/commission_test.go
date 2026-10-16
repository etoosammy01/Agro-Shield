package services

import "testing"

func TestParseCommissionPercent(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		// The default. COMMISSION_PERCENT is unset out of the box, and an
		// unset rate must leave the platform taking nothing.
		{name: "empty means disabled", value: "", want: 0},
		{name: "whitespace means disabled", value: "   ", want: 0},
		{name: "explicit zero is disabled", value: "0", want: 0},
		{name: "zero with decimals is disabled", value: "0.00", want: 0},

		{name: "whole number", value: "2", want: 200},
		{name: "one decimal place", value: "2.5", want: 250},
		{name: "two decimal places", value: "2.50", want: 250},
		{name: "sub one percent", value: "0.5", want: 50},
		{name: "smallest meaningful step", value: "0.01", want: 1},
		{name: "trailing point is a whole number", value: "2.", want: 200},
		{name: "just below the cap", value: "99.99", want: 9999},
		{name: "surrounding spaces are trimmed", value: " 2.5 ", want: 250},

		// A 100% rate would leave the seller with nothing, and the ledger
		// cannot record a zero-amount sale credit.
		{name: "reject one hundred percent", value: "100", wantErr: true},
		{name: "reject above one hundred percent", value: "150", wantErr: true},

		{name: "reject negative", value: "-1", wantErr: true},
		{name: "reject negative decimal", value: "-2.5", wantErr: true},
		{name: "reject explicit plus", value: "+2.5", wantErr: true},
		{name: "reject leading point", value: ".5", wantErr: true},
		{name: "reject excess precision", value: "2.555", wantErr: true},
		{name: "reject two points", value: "2.5.5", wantErr: true},
		{name: "reject exponent notation", value: "1e2", wantErr: true},
		{name: "reject comma decimal separator", value: "2,5", wantErr: true},
		{name: "reject percent sign", value: "2.5%", wantErr: true},
		{name: "reject words", value: "two", wantErr: true},
		{name: "reject internal space", value: "2 .5", wantErr: true},
		{name: "reject unrepresentable magnitude", value: "99999999999999999999", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCommissionPercent(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseCommissionPercent(%q) error = %v, wantErr %v", test.value, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("ParseCommissionPercent(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestCommissionEnabled(t *testing.T) {
	tests := []struct {
		basisPoints int64
		want        bool
	}{
		{basisPoints: 0, want: false},
		{basisPoints: 1, want: true},
		{basisPoints: 250, want: true},
		{basisPoints: 9999, want: true},
	}

	for _, test := range tests {
		service := &WalletService{commissionBasisPoints: test.basisPoints}
		if got := service.CommissionEnabled(); got != test.want {
			t.Errorf("CommissionEnabled() at %d basis points = %v, want %v",
				test.basisPoints, got, test.want)
		}
	}
}

func TestCommissionLabel(t *testing.T) {
	tests := []struct {
		basisPoints int64
		want        string
	}{
		{basisPoints: 0, want: "0.00"},
		{basisPoints: 50, want: "0.50"},
		{basisPoints: 250, want: "2.50"},
		{basisPoints: 300, want: "3.00"},
		{basisPoints: 9999, want: "99.99"},
	}

	for _, test := range tests {
		service := &WalletService{commissionBasisPoints: test.basisPoints}
		if got := service.CommissionLabel(); got != test.want {
			t.Errorf("CommissionLabel() at %d basis points = %q, want %q",
				test.basisPoints, got, test.want)
		}
	}
}

// TestParseCommissionPercentRoundTripsThroughLabel checks the rate a config
// value parses to is the rate an admin is later shown, so the number in the
// UI cannot disagree with the number dividing the money.
//
// Only canonical two-decimal spellings round-trip: "2" parses to the same 200
// basis points as "2.00" and comes back out as "2.00", which is the intended
// normalisation, not a mismatch.
func TestParseCommissionPercentRoundTripsThroughLabel(t *testing.T) {
	values := []string{"0.00", "0.01", "0.50", "1.00", "2.50", "7.75", "99.99"}

	for _, value := range values {
		basisPoints, err := ParseCommissionPercent(value)
		if err != nil {
			t.Fatalf("ParseCommissionPercent(%q): %v", value, err)
		}
		service := &WalletService{commissionBasisPoints: basisPoints}
		if got := service.CommissionLabel(); got != value {
			t.Errorf("ParseCommissionPercent(%q) then CommissionLabel() = %q", value, got)
		}
	}
}
