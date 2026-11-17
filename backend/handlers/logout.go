package handlers

import (
	"log"
	"net/http"

	"backend/internal/services"
	"backend/middleware"
)

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if err := middleware.EndSession(w, r); err != nil {
		http.Error(w, "Could not sign out. Please try again.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Logout does not require a valid session: an expired cookie must still be
// clearable, and a live token must be revoked across all application instances.
func PersistentLogoutHandler(sessions *services.SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := middleware.SessionIDFromRequest(r); ok {
			if err := sessions.Revoke(r.Context(), token); err != nil {
				log.Printf("revoke logout session: %v", err)
				http.Error(w, "Could not sign out. Please try again.", http.StatusServiceUnavailable)
				return
			}
		}
		middleware.ClearSessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}
