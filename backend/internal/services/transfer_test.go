package services

import (
	"strings"
	"testing"
)

func TestRecipientPhoneForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "local trunk form",
			in:   "08012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "international form",
			in:   "+2348012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "already normalized",
			in:   "2348012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "spaced",
			in:   "0801 234 5678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{name: "invalid returns nothing", in: "12345", want: nil},
		{name: "empty returns nothing", in: "", want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := recipientPhoneForms(test.in)
			if len(got) != len(test.want) {
				t.Fatalf("recipientPhoneForms(%q) = %v, want %v", test.in, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("recipientPhoneForms(%q) = %v, want %v", test.in, got, test.want)
				}
			}
		})
	}
}

func TestNormalizeTransferNote(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain text", in: "Payment for maize", want: "Payment for maize"},
		{name: "trimmed", in: "  seed money  ", want: "seed money"},
		{name: "empty", in: "", want: ""},
		{name: "newline becomes space", in: "first\nsecond", want: "first second"},
		{name: "tab becomes space", in: "first\tsecond", want: "first second"},
		{name: "control characters dropped", in: "a\x00b\x07c", want: "abc"},
		{name: "at the limit", in: strings.Repeat("a", 140), want: strings.Repeat("a", 140)},
		{name: "past the limit", in: strings.Repeat("a", 141), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTransferNote(test.in)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeTransferNote() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeTransferNote(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestNormalizeTransferNoteCountsRunes guards against rejecting a note by byte
// length, which would cut multilingual notes short of the stated 140 characters.
func TestNormalizeTransferNoteCountsRunes(t *testing.T) {
	note := strings.Repeat("₦", 140) // 3 bytes each, 140 characters
	got, err := normalizeTransferNote(note)
	if err != nil {
		t.Fatalf("normalizeTransferNote() rejected 140 multi-byte characters: %v", err)
	}
	if got != note {
		t.Fatal("normalizeTransferNote() altered a note within the limit")
	}
}
