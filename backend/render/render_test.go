package render

import "testing"

// The kobo rule is the part worth pinning: a whole amount must lose its ".00"
// while any amount that actually has kobo must keep both digits. Getting the
// second half wrong silently misstates a balance the user can see.
func TestMoneyRendersWithKoboRule(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		// Whole amounts lose the trailing ".00".
		{"whole float loses decimals", float64(4000), "4,000"},
		{"whole numeric text loses decimals", "4000.00", "4,000"},
		{"whole int", 4000, "4,000"},
		{"zero float", float64(0), "0"},
		{"zero numeric text", "0.00", "0"},

		// Amounts with kobo keep exactly two digits.
		{"half naira", float64(4000.5), "4,000.50"},
		{"numeric text with kobo", "4000.50", "4,000.50"},
		{"small kobo is not truncated", "0.75", "0.75"},
		{"five kobo pads to two digits", "0.05", "0.05"},
		{"ten kobo keeps trailing zero", "4000.10", "4,000.10"},

		// Thousands separators.
		{"millions", float64(1250000), "1,250,000"},
		{"just over a thousand", "1000.00", "1,000"},
		{"exactly a thousand with kobo", "1000.99", "1,000.99"},

		// Signs survive, and a negative whole amount keeps its minus.
		{"negative whole", float64(-4000), "-4,000"},
		{"negative with kobo", "-4000.50", "-4,000.50"},

		// Unparseable input renders as nothing rather than a misleading zero.
		{"empty string", "", ""},
		{"whitespace", "   ", ""},
		{"not a number", "n/a", ""},
		{"unsupported type", true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Money(tc.value); got != tc.want {
				t.Errorf("Money(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// A displayed balance must equal what can be withdrawn, so kobo must never be
// rounded away for a value that carries it.
func TestMoneyDoesNotRoundAwayKobo(t *testing.T) {
	for _, value := range []any{"1.01", "99.99", float64(0.01)} {
		got := Money(value)
		if !containsDecimal(got) {
			t.Errorf("Money(%#v) = %q, dropped the kobo", value, got)
		}
	}
}

func containsDecimal(s string) bool {
	for _, r := range s {
		if r == '.' {
			return true
		}
	}
	return false
}
