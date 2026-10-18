package services

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTypingTrackerReportsAnotherMembersLatestState(t *testing.T) {
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tracker := newTypingTracker()
	tracker.now = func() time.Time { return clock }

	tracker.set(7, 1, "Ada", true)
	clock = clock.Add(time.Second)
	tracker.set(7, 2, "Bola", true)

	// The viewer never sees their own typing state.
	if name, typing := tracker.active(7, 1); !typing || name != "Bola" {
		t.Fatalf("viewer 1 saw name=%q typing=%v, want Bola", name, typing)
	}
	// A different conversation does not leak into this one.
	tracker.set(8, 3, "Chidi", true)
	if name, typing := tracker.active(7, 3); !typing || name != "Bola" {
		t.Fatalf("conversation isolation failed: name=%q typing=%v", name, typing)
	}
	// An explicit false clears that member without affecting the other.
	tracker.set(7, 2, "Bola", false)
	if name, typing := tracker.active(7, 1); typing || name != "" {
		t.Fatalf("cleared typing state still visible: name=%q typing=%v", name, typing)
	}
}

func TestTypingTrackerExpiresSilently(t *testing.T) {
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tracker := newTypingTracker()
	tracker.now = func() time.Time { return clock }

	tracker.set(1, 10, "Ada", true)
	if _, typing := tracker.active(1, 11); !typing {
		t.Fatal("expected active typing state")
	}
	clock = clock.Add(typingTTL + time.Second)
	if name, typing := tracker.active(1, 11); typing || name != "" {
		t.Fatalf("expired state still visible: name=%q typing=%v", name, typing)
	}
	if len(tracker.entries) != 0 {
		t.Fatalf("expired entries were not pruned: %d remain", len(tracker.entries))
	}
}

func TestTypingTrackerIgnoresInvalidIDsAndTruncatesNames(t *testing.T) {
	tracker := newTypingTracker()
	tracker.set(0, 1, "Ada", true)
	tracker.set(1, 0, "Ada", true)
	if len(tracker.entries) != 0 {
		t.Fatal("invalid IDs should not create entries")
	}

	long := strings.Repeat("a", typingNameSize+50)
	tracker.set(1, 2, long, true)
	if name, typing := tracker.active(1, 3); !typing || utf8.RuneCountInString(name) != typingNameSize {
		t.Fatalf("expected a %d-rune name, got %q", typingNameSize, name)
	}

	tracker.set(1, 2, "   ", true)
	if name, typing := tracker.active(1, 3); !typing || name != "" {
		t.Fatalf("blank names should remain blank, got %q typing=%v", name, typing)
	}
}
