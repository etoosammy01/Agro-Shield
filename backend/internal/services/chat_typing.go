package services

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ErrChatAccessDenied is returned when a user asks about a conversation they
// do not belong to. Handlers map it to 403 without revealing whether the
// conversation exists.
var ErrChatAccessDenied = errors.New("conversation access denied")

const (
	typingTTL      = 5 * time.Second
	typingNameSize = 100
)

type typingKey struct {
	conversationID int
	userID         int
}

type typingEntry struct {
	name     string
	lastSeen time.Time
}

// typingTracker keeps short-lived "X is typing" state in memory. It follows the
// same single-instance model as the existing session and presence stores: it is
// correct for one process and intentionally not a substitute for a shared store
// when the application runs on multiple instances.
type typingTracker struct {
	mu      sync.Mutex
	entries map[typingKey]typingEntry
	now     func() time.Time
}

func newTypingTracker() *typingTracker {
	return &typingTracker{
		entries: make(map[typingKey]typingEntry),
		now:     time.Now,
	}
}

func (t *typingTracker) set(conversationID, userID int, name string, typing bool) {
	if conversationID <= 0 || userID <= 0 {
		return
	}
	key := typingKey{conversationID: conversationID, userID: userID}

	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if !typing {
		delete(t.entries, key)
		t.pruneLocked(now)
		return
	}
	t.entries[key] = typingEntry{name: typingDisplayName(name), lastSeen: now}
	t.pruneLocked(now)
}

// active returns the most recently seen other member who is typing. The viewer
// is deliberately excluded so a user never sees their own typing state echoed
// back as somebody else's.
func (t *typingTracker) active(conversationID, viewerID int) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.pruneLocked(now)

	var (
		found    bool
		name     string
		lastSeen time.Time
	)
	for key, entry := range t.entries {
		if key.conversationID != conversationID || key.userID == viewerID {
			continue
		}
		if !found || entry.lastSeen.After(lastSeen) {
			found = true
			name = entry.name
			lastSeen = entry.lastSeen
		}
	}
	return name, found
}

func (t *typingTracker) pruneLocked(now time.Time) {
	for key, entry := range t.entries {
		if now.Sub(entry.lastSeen) > typingTTL {
			delete(t.entries, key)
		}
	}
}

func typingDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) <= typingNameSize {
		return name
	}
	runes := []rune(name)
	return string(runes[:typingNameSize])
}

// SetTyping records or clears a member's typing state after checking that the
// member belongs to the conversation.
func (s *ChatService) SetTyping(conversationID, userID int, name string, typing bool) error {
	if conversationID <= 0 || userID <= 0 {
		return errors.New("invalid typing request")
	}
	isMember, err := s.memberRepo.IsMember(conversationID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrChatAccessDenied
	}
	s.typing.set(conversationID, userID, name, typing)
	return nil
}

// TypingStatus reports whether another member of the conversation is typing.
// Checking membership before reading the in-memory state keeps one user's
// typing state from being observable to someone outside the conversation.
func (s *ChatService) TypingStatus(conversationID, viewerID int) (string, bool, error) {
	if conversationID <= 0 || viewerID <= 0 {
		return "", false, errors.New("invalid typing request")
	}
	isMember, err := s.memberRepo.IsMember(conversationID, viewerID)
	if err != nil {
		return "", false, err
	}
	if !isMember {
		return "", false, ErrChatAccessDenied
	}
	name, typing := s.typing.active(conversationID, viewerID)
	return name, typing, nil
}
