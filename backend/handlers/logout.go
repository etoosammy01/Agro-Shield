package handlers

import (
	"log"
	"net/http"

	"backend/internal/services"
	"backend/middleware"
)

// Logout handles user logout using persistent database-backed sessions.
type Logout struct {
	sessions *services.SessionService
}

// NewLogoutHandler creates a logout handler with its session dependency.
func NewLogoutHandler(sessions *services.SessionService) *Logout {
	return &Logout{
		sessions: sessions,
	}
}

// LogoutHandler revokes the persistent session and clears the browser cookie.
func (h *Logout) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Read the raw session token from the browser cookie.
	if token, ok := middleware.SessionIDFromRequest(r); ok {
		// Revoke the session in the database.
		if err := h.sessions.Revoke(r.Context(), token); err != nil {
			log.Printf("failed to revoke logout session: %v", err)
		}
	}

	// Always clear the browser cookie, even if database revocation fails.
	middleware.ClearSessionCookie(w, r)

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
