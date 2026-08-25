package services

import "errors"
import "testing"

func TestIsTemporaryGeminiError(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"Error 503: high demand, Status: UNAVAILABLE", true},
		{"429 resource exhausted", true},
		{"invalid API key", false},
		{"", false},
	}

	for _, test := range tests {
		var err error
		if test.message != "" {
			err = errors.New(test.message)
		}
		if got := isTemporaryGeminiError(err); got != test.want {
			t.Fatalf("isTemporaryGeminiError(%q) = %v, want %v", test.message, got, test.want)
		}
	}
}
