package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
)

type contextKey string

const farmerContextKey contextKey = "farmer"

const sessionCookieName = "agroshield_session"

// In-memory session store: sessionID -> farmerID.
// Good enough for a single-server setup. If you later scale to multiple
// server instances, swap this for a shared store (e.g. Redis).
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]int
	lastSeen map[int]time.Time
}

var store = &sessionStore{
	sessions: make(map[string]int),
	lastSeen: make(map[int]time.Time),
}

const onlineWindow = 60 * time.Second

func newSessionID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateSession creates a new session for a farmer and returns the session ID.
func CreateSession(farmerID int) string {
	id := newSessionID()
	store.mu.Lock()
	store.sessions[id] = farmerID
	store.mu.Unlock()
	return id
}

// DeleteSession removes a session (used on logout).
func DeleteSession(sessionID string) {
	store.mu.Lock()
	farmerID, exists := store.sessions[sessionID]
	delete(store.sessions, sessionID)
	if exists {
		hasAnotherSession := false
		for _, sessionFarmerID := range store.sessions {
			if sessionFarmerID == farmerID {
				hasAnotherSession = true
				break
			}
		}
		if !hasAnotherSession {
			delete(store.lastSeen, farmerID)
		}
	}
	store.mu.Unlock()
}

// MarkFarmerOnline records recent authenticated activity for presence checks.
func MarkFarmerOnline(farmerID int) {
	store.mu.Lock()
	store.lastSeen[farmerID] = time.Now()
	store.mu.Unlock()
}

// IsFarmerOnline reports whether a user has been active recently.
func IsFarmerOnline(farmerID int) bool {
	store.mu.RLock()
	lastSeen, ok := store.lastSeen[farmerID]
	store.mu.RUnlock()
	return ok && time.Since(lastSeen) <= onlineWindow
}

func getFarmerIDForSession(sessionID string) (int, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	id, ok := store.sessions[sessionID]
	return id, ok
}

// SetSessionCookie writes the session cookie on login.
func SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
}

// ClearSessionCookie removes the cookie on logout / invalid session.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// SessionIDFromRequest reads the session cookie, if present.
func SessionIDFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	return cookie.Value, true
}

// FarmerFromContext retrieves the logged-in farmer attached by RequireAuth.
func FarmerFromContext(r *http.Request) (*models.Farmer, bool) {
	farmer, ok := r.Context().Value(farmerContextKey).(*models.Farmer)
	return farmer, ok
}

// completeProfilePath is exempted from the profile-completion gate below,
// so a farmer who hasn't uploaded a real photo yet can actually reach the
// page that lets them do so, instead of being redirected back to itself.
const completeProfilePath = "/complete-profile"

// RequireAuth protects a handler. It checks for a valid session cookie,
// loads the logged-in farmer, and attaches it to the request context.
// If there's no valid session, it redirects to /login. If the session is
// valid but the farmer hasn't completed their mandatory profile photo yet,
// it redirects to /complete-profile instead of the requested page — this
// closes the gap where a farmer could otherwise type a URL like /dashboard
// directly and skip the mandatory step entirely.
func RequireAuth(repo *repository.FarmerRepository, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, ok := SessionIDFromRequest(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		farmerID, ok := getFarmerIDForSession(sessionID)
		if !ok {
			ClearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		farmer, err := repo.GetByID(farmerID)
		if err != nil || farmer == nil {
			ClearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if r.URL.Path != completeProfilePath && !services.HasCompletedProfile(farmer) {
			http.Redirect(w, r, completeProfilePath, http.StatusSeeOther)
			return
		}

		MarkFarmerOnline(farmerID)
		ctx := context.WithValue(r.Context(), farmerContextKey, farmer)
		next(w, r.WithContext(ctx))
	}
}