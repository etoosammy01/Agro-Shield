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
