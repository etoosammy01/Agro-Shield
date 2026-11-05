package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"backend/internal/models"
	"backend/internal/services"
	"backend/internal/session"
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

// DeleteFarmerSessions removes every session belonging to a farmer. Used when
// an account is closed: the account's own tab must not keep a working session,
// and any other signed-in device must be signed out too.
func DeleteFarmerSessions(farmerID int) {
	store.mu.Lock()
	for sessionID, sessionFarmerID := range store.sessions {
		if sessionFarmerID == farmerID {
			delete(store.sessions, sessionID)
		}
	}
	delete(store.lastSeen, farmerID)
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

// SetSessionCookie writes the session cookie using the default 24-hour lifetime.
// This remains available for code that still uses the legacy session flow.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	SetSessionCookieWithExpiry(w, r, sessionID, time.Now().Add(24*time.Hour))
}

// SetSessionCookieWithExpiry writes a session cookie using the expiry
// returned by the persistent session service.
func SetSessionCookieWithExpiry(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   RequestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

// ClearSessionCookie removes the cookie on logout / invalid session.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   RequestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
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

type farmerReader interface {
	GetByID(int) (*models.Farmer, error)
}

// RequireAuth protects a handler. It checks for a valid session cookie,
// loads the logged-in farmer, and attaches it to the request context.
// Incomplete accounts can only reach onboarding pages; those handlers enforce
// the order of the steps. Ordinary protected pages require every step.
func RequireAuth(repo farmerReader, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, ok := SessionIDFromRequest(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		farmerID, ok := getFarmerIDForSession(sessionID)
		if !ok {
			ClearSessionCookie(w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		farmer, err := repo.GetByID(farmerID)
		if err != nil || farmer == nil {
			ClearSessionCookie(w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if r.URL.Path != "/complete-profile" && r.URL.Path != "/verify-phone" && r.URL.Path != "/create-transaction-pin" && !services.HasCompletedProfile(farmer) {
			http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)
			return
		}

		MarkFarmerOnline(farmerID)
		ctx := context.WithValue(r.Context(), farmerContextKey, farmer)
		next(w, r.WithContext(ctx))
	}
}

// sessionValidator checks whether a session token is valid in persistent storage.
type sessionValidator interface {
	Validate(ctx context.Context, token string) (int, error)
}

// Authenticator combines farmer lookup with persistent session validation.
type Authenticator struct {
	repo     farmerReader
	sessions sessionValidator
}

// NewAuthenticator creates an authenticator using the farmer repository
// and persistent session service.
func NewAuthenticator(repo farmerReader, sessions sessionValidator) *Authenticator {
	return &Authenticator{
		repo:     repo,
		sessions: sessions,
	}
}

// RequireAuth protects a handler using persistent database-backed sessions.
func (a *Authenticator) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := SessionIDFromRequest(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Validate checks the hashed token against persistent session storage.
		farmerID, err := a.sessions.Validate(r.Context(), token)

		if errors.Is(err, session.ErrNotFound) {
			ClearSessionCookie(w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// A database failure is different from an invalid session.
		if err != nil {
			http.Error(w, "Unable to verify session", http.StatusInternalServerError)
			return
		}

		farmer, err := a.repo.GetByID(farmerID)
		if err != nil || farmer == nil {
			ClearSessionCookie(w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Preserve the existing onboarding restrictions.
		if r.URL.Path != "/complete-profile" &&
			r.URL.Path != "/verify-phone" &&
			r.URL.Path != "/create-transaction-pin" &&
			!services.HasCompletedProfile(farmer) {
			http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)
			return
		}

		MarkFarmerOnline(farmerID)

		// Make the authenticated farmer available to the next handler.
		ctx := context.WithValue(r.Context(), farmerContextKey, farmer)
		next(w, r.WithContext(ctx))
	}
}
