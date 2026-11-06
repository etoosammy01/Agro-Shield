package middleware

import (
	"testing"
	"time"
)

func cleanupPresence(t *testing.T, farmerIDs ...int) {
	t.Helper()
	t.Cleanup(func() {
		online.mu.Lock()
		defer online.mu.Unlock()
		for _, farmerID := range farmerIDs {
			delete(online.lastSeen, farmerID)
		}
	})
}

func TestFarmerPresenceTracksRecentActivity(t *testing.T) {
	const (
		activeFarmerID = 900001
		otherFarmerID  = 900002
	)
	cleanupPresence(t, activeFarmerID, otherFarmerID)

	MarkFarmerOnline(activeFarmerID)

	if !IsFarmerOnline(activeFarmerID) {
		t.Fatal("recently active farmer was not marked online")
	}
	if IsFarmerOnline(otherFarmerID) {
		t.Fatal("inactive farmer was marked online")
	}
}

func TestFarmerPresenceExpiresAfterOnlineWindow(t *testing.T) {
	const farmerID = 900003
	cleanupPresence(t, farmerID)

	online.mu.Lock()
	online.lastSeen[farmerID] = time.Now().Add(-onlineWindow - time.Second)
	online.mu.Unlock()

	if IsFarmerOnline(farmerID) {
		t.Fatal("presence did not expire after the online window")
	}
}
