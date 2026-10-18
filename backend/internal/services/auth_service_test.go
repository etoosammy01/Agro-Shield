package services

import "testing"

func TestSamePhoneAcceptsTheSameNumberWrittenDifferently(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "identical", a: "08012345678", b: "08012345678", want: true},
		{name: "country code", a: "08012345678", b: "+2348012345678", want: true},
		{name: "spaces and dashes", a: "2348012345678", b: "0801 234-5678", want: true},
		{name: "different number", a: "08012345678", b: "08012345679", want: false},
		{name: "empty confirmation", a: "08012345678", b: "", want: false},
		{name: "empty account", a: "", b: "", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := samePhone(test.a, test.b); got != test.want {
				t.Fatalf("samePhone(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
			}
		})
	}
}
