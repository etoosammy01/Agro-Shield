package services

import (
	"strings"
	"testing"
)

func TestProviderErrorsDoNotLeakTheResponseBody(t *testing.T) {
	body := []byte(`{"status":"error","message":"Invalid authorization key","secret":"FLWSECK-test","echo":{"account_number":"0123456789","bvn":"12345678901"}}`)
	err := providerHTTPError("flutterwave", 401, body)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Invalid authorization key") {
		t.Fatalf("expected the provider message to be kept, got %q", msg)
	}
	for _, sensitive := range []string{"FLWSECK-test", "0123456789", "12345678901", "account_number", "bvn", `{"status"`} {
		if strings.Contains(msg, sensitive) {
			t.Fatalf("provider error leaked %q: %q", sensitive, msg)
		}
	}
}

func TestProviderErrorWithoutParsedMessageOnlyReportsStatus(t *testing.T) {
	rawBody := []byte("upstream <html>failure</html> account=0123456789")
	msg := providerHTTPError("flutterwave", 502, rawBody).Error()
	if strings.Contains(msg, "upstream") || strings.Contains(msg, "0123456789") {
		t.Fatalf("provider error leaked the raw body: %q", msg)
	}
	if !strings.Contains(msg, "HTTP 502") {
		t.Fatalf("expected the HTTP status in %q", msg)
	}
}

func TestSafeProviderMessageStripsControlsAndCapsLength(t *testing.T) {
	if got := safeProviderMessage("bad\nkey\x00\tvalue"); got != "bad key value" {
		t.Fatalf("unexpected sanitized message: %q", got)
	}
	if got := safeProviderMessage("   "); got != "" {
		t.Fatalf("expected an empty message, got %q", got)
	}
	long := safeProviderMessage(strings.Repeat("a", 500))
	if len(long) != 200 {
		t.Fatalf("expected the message to be capped at 200 bytes, got %d", len(long))
	}
}

func TestSafeProviderMessageRedactsLongDigitRuns(t *testing.T) {
	got := safeProviderMessage("account 0123456789 rejected at bank 044")
	want := "account [redacted] rejected at bank 044"
	if got != want {
		t.Fatalf("safeProviderMessage() = %q, want %q", got, want)
	}

	// Provider errors may echo an account number inside the message field,
	// which is otherwise the one field we intentionally keep.
	err := providerHTTPError("flutterwave", 400, []byte(`{"status":"error","message":"invalid account 0123456789"}`))
	if strings.Contains(err.Error(), "0123456789") {
		t.Fatalf("provider error leaked a digit run: %q", err.Error())
	}
}

func TestSameMoneyUsesExactTwoDecimalComparison(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		verified float64
		want     bool
	}{
		{"equal integers", "1000", 1000, true},
		{"same amount with trailing zeros", "1000.00", 1000, true},
		{"same amount written differently", "10.5", 10.50, true},
		{"fractional kobo", "1250.75", 1250.75, true},
		{"rounds to the same kobo", "10.004", 10.004, true},
		{"one kobo short", "1000.00", 999.99, false},
		{"one kobo over", "1000.00", 1000.01, false},
		{"currency scale mismatch", "1000", 100000, false},
		{"negative cannot pass a positive charge", "1000.00", -1000, false},
		{"zero does not match a positive charge", "1000.00", 0, false},
		{"invalid expected amount", "not-a-number", 1000, false},
		{"empty expected amount", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameMoney(tc.expected, tc.verified); got != tc.want {
				t.Fatalf("sameMoney(%q, %v) = %v, want %v", tc.expected, tc.verified, got, tc.want)
			}
		})
	}
}
