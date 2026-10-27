package services

import "testing"

func TestOTPDevPhone(t *testing.T) {
	const want = "2349060904492"
	valid := []string{
		"09060904492", "9060904492", "2349060904492", "+2349060904492",
		"+234 906 090 4492", "002349060904492", "+2340906 0904492",
	}
	for _, in := range valid {
		if got := otpDevPhone(in); got != want {
			t.Errorf("otpDevPhone(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "0123456789", "+14155550123", "0906090449", "090609044921"} {
		if got := otpDevPhone(in); got != "" {
			t.Errorf("otpDevPhone(%q) = %q, want empty", in, got)
		}
	}
}
