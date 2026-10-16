package services

import "testing"

func TestNormalizeNigerianPhone(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "local trunk form", in: "08012345678", want: "2348012345678"},
		{name: "already international", in: "2348012345678", want: "2348012345678"},
		{name: "plus prefix", in: "+2348012345678", want: "2348012345678"},
		{name: "spaces and dashes", in: "0801 234-5678", want: "2348012345678"},
		{name: "bare subscriber number", in: "8012345678", want: "2348012345678"},
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   ", want: ""},
		{name: "letters only", in: "not-a-number", want: ""},
		{name: "too short trunk", in: "0801234", want: ""},
		{name: "too long trunk", in: "080123456789", want: ""},
		{name: "trunk with 234 prefix", in: "0234801234567", want: ""},
		{name: "foreign number", in: "+1 555 0100", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeNigerianPhone(test.in); got != test.want {
				t.Fatalf("NormalizeNigerianPhone(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestNormalizeNigerianPhoneIsIdempotent guards the property that matters for
// recipient lookup: a number stored in canonical form must normalize to itself,
// or a recipient could be unfindable by their own stored number.
func TestNormalizeNigerianPhoneIsIdempotent(t *testing.T) {
	for _, in := range []string{"08012345678", "+2348012345678", "8012345678", "234 801 234 5678"} {
		once := NormalizeNigerianPhone(in)
		if once == "" {
			t.Fatalf("NormalizeNigerianPhone(%q) = %q", in, once)
		}
		if twice := NormalizeNigerianPhone(once); twice != once {
			t.Fatalf("NormalizeNigerianPhone(%q) = %q but re-normalizing gives %q", in, once, twice)
		}
	}
}
