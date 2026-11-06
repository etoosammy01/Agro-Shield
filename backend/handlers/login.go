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

func NewLoginHandler(
	service *services.AuthService,
	sessions *services.SessionService,
) *Login {
	return &Login{
		service:  service,
		sessions: sessions,
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
			log.Println("Render Error:", err)
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

		// Create a persistent database-backed session after successful login.
		sessionToken, _, err := h.sessions.Create(r.Context(), farmer.ID)
		if err != nil {
			log.Printf("failed to create login session: %v", err)
			http.Error(w, "Unable to start your session. Please try again.", http.StatusInternalServerError)
			return
		}

		// Store the raw token in the browser cookie.
		// The database stores only its SHA-256 hash.
		middleware.SetSessionCookie(w, r, sessionToken)

		http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)

	default:
		http.Error(w, "Method Not Allowed!", http.StatusMethodNotAllowed)
	}
}
