package middleware

import (
	"context"
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

// Presence is intentionally process-local. Persistent sessions live in the
// database, but online status is only a short-lived chat indicator.
type presenceStore struct {
	mu       sync.RWMutex
	lastSeen map[int]time.Time
}

var online = &presenceStore{
	lastSeen: make(map[int]time.Time),
}

const onlineWindow = 60 * time.Second

// MarkFarmerOnline records recent authenticated activity for presence checks.
func MarkFarmerOnline(farmerID int) {
	online.mu.Lock()
	online.lastSeen[farmerID] = time.Now()
	online.mu.Unlock()
}

// IsFarmerOnline reports whether a user has been active recently.
func IsFarmerOnline(farmerID int) bool {
	online.mu.RLock()
	lastSeen, ok := online.lastSeen[farmerID]
	online.mu.RUnlock()
	return ok && time.Since(lastSeen) <= onlineWindow
}

// SetSessionCookie writes the session cookie on login.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   RequestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
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

// FarmerFromContext retrieves the logged-in farmer attached by the
// persistent authenticator.
func FarmerFromContext(r *http.Request) (*models.Farmer, bool) {
	farmer, ok := r.Context().Value(farmerContextKey).(*models.Farmer)
	return farmer, ok
}

type farmerReader interface {
	GetByID(int) (*models.Farmer, error)
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
