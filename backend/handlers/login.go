package handlers

import (
	"log"
	"net/http"

	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// LoginPageData is passed to login.html so it can show an error message or a
// one-time notice (for example after an account was closed).
type LoginPageData struct {
	Error  string
	Notice string
}

type Login struct {
	service  *services.AuthService
	sessions *services.SessionService
}

func NewLoginHandler(service *services.AuthService, sessions ...*services.SessionService) *Login {
	var backend *services.SessionService
	if len(sessions) > 0 {
		backend = sessions[0]
	}
	return &Login{
		service:  service,
		sessions: backend,
	}
}

func (h *Login) LoginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Login Page")
		data := LoginPageData{}
		if r.URL.Query().Get("deleted") == "1" {
			data.Notice = "Your account has been closed. Your order and payment history is kept for record-keeping."
		} else if r.URL.Query().Get("onboarded") == "1" {
			data.Notice = "Account setup complete. Your wallet transaction PIN is ready. Log in to continue."
		} else if r.URL.Query().Get("verified") == "1" {
			data.Notice = "Phone verified. Log in to continue."
		}
		if err := render.RenderTemplates(w, "login.html", data); err != nil {
			log.Println("Render Error")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		phone := r.FormValue("phone")
		password := r.FormValue("password")

		farmer, err := h.service.Login(phone, password)
		if err != nil {
			log.Println("login failed:", err)
			if renderErr := render.RenderTemplates(w, "login.html", LoginPageData{
				Error: "Invalid phone number or password",
			}); renderErr != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		sessionID, _, err := h.sessions.CreateForLogin(r.Context(), farmer.ID, farmer.SessionsRevokedAt.Time)
		if err != nil {
			log.Printf("create login session: %v", err)
			http.Error(w, "Unable to sign in. Please try again.", http.StatusServiceUnavailable)
			return
		}
		middleware.SetSessionCookie(w, r, sessionID)

		http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)

	default:
		http.Error(w, "Method Not Allowed!", http.StatusMethodNotAllowed)
	}
}
