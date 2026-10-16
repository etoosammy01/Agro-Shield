package services

import "testing"

func TestNormalizeWalletAmount(t *testing.T) {
	tests := []struct {
		name       string
		amount     string
		wholeUnits bool
		want       string
		wantErr    bool
	}{
		{name: "decimal deposit", amount: "125.50", want: "125.50"},
		{name: "whole withdrawal", amount: "125", wholeUnits: true, want: "125.00"},
		{name: "reject fractional withdrawal", amount: "125.50", wholeUnits: true, wantErr: true},
		{name: "reject excess precision", amount: "1.001", wantErr: true},
		{name: "reject exponent notation", amount: "1e3", wantErr: true},
		{name: "reject negative", amount: "-1", wantErr: true},
		{name: "reject zero", amount: "0", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := normalizeWalletAmount(test.amount, test.wholeUnits)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeWalletAmount() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeWalletAmount() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAmountToCents(t *testing.T) {
	tests := []struct {
		name    string
		amount  string
		want    int64
		wantErr bool
	}{
		{name: "zero", amount: "0.00", want: 0},
		{name: "whole naira", amount: "125.00", want: 12500},
		{name: "with kobo", amount: "125.50", want: 12550},
		{name: "one decimal place", amount: "125.5", want: 12550},
		{name: "single kobo", amount: "0.01", want: 1},
		{name: "wallet maximum", amount: "9999999999.99", want: 999999999999},
		{name: "surrounding whitespace", amount: " 12.34 ", want: 1234},
		{name: "reject excess precision", amount: "1.001", wantErr: true},
		{name: "reject negative", amount: "-1.00", wantErr: true},
		{name: "reject explicit plus", amount: "+1.00", wantErr: true},
		{name: "reject trailing decimal point", amount: "1.", wantErr: true},
		{name: "reject leading decimal point", amount: ".50", wantErr: true},
		{name: "reject two decimal points", amount: "1.2.3", wantErr: true},
		{name: "reject empty", amount: "", wantErr: true},
		{name: "reject text", amount: "abc", wantErr: true},
		{name: "reject exponent notation", amount: "1e3", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := amountToCents(test.amount)
			if (err != nil) != test.wantErr {
				t.Fatalf("amountToCents(%q) error = %v, wantErr %v", test.amount, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("amountToCents(%q) = %d, want %d", test.amount, got, test.want)
			}
		})
	}
}

// TestAmountToCentsAgreesWithNormalizeWalletAmount pins the two parsers
// together. PreflightWithdrawal compares a normalized request against a stored
// balance using amountToCents, so if they ever disagreed a user could be told
// they have funds they cannot actually withdraw.
func TestAmountToCentsAgreesWithNormalizeWalletAmount(t *testing.T) {
	for _, in := range []string{"0.01", "1", "1.5", "125.50", "125", "9999999999", "0.99"} {
		normalized, _, err := normalizeWalletAmount(in, false)
		if err != nil {
			t.Fatalf("normalizeWalletAmount(%q) error = %v", in, err)
		}
		fromNormalized, err := amountToCents(normalized)
		if err != nil {
			t.Fatalf("amountToCents(%q) error = %v", normalized, err)
		}
		fromRaw, err := amountToCents(in)
		if err != nil {
			t.Fatalf("amountToCents(%q) error = %v", in, err)
		}
		if fromNormalized != fromRaw {
			t.Fatalf("amountToCents disagrees for %q: %d via %q, %d raw", in, fromNormalized, normalized, fromRaw)
		}
	}
}

func TestParseWalletAdminIDs(t *testing.T) {
	ids, err := ParseWalletAdminIDs("12, 34,12")
	if err != nil {
		t.Fatalf("ParseWalletAdminIDs() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != 12 || ids[1] != 34 {
		t.Fatalf("ParseWalletAdminIDs() = %v, want [12 34]", ids)
	}
	if _, err := ParseWalletAdminIDs("12,not-an-id"); err == nil {
		t.Fatal("ParseWalletAdminIDs() accepted an invalid ID")
	}
}
