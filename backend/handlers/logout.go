package handlers

import (
	"net/http"

	"backend/middleware"
)

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if sessionID, ok := middleware.SessionIDFromRequest(r); ok {
		middleware.DeleteSession(sessionID)
	}
	middleware.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
