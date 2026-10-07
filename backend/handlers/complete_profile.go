package handlers

import (
	"log"
	"net/http"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

// CompleteProfilePageData is passed to complete-profile.html.
type CompleteProfilePageData struct {
	IsBuyer bool
	Error   string
}

type CompleteProfile struct {
	auth *services.AuthService
}

func NewCompleteProfileHandler(auth *services.AuthService) *CompleteProfile {
	return &CompleteProfile{auth: auth}
}

func (h *CompleteProfile) Handler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Already completed — no need to show this again.
		if services.HasCompletedProfile(farmer) {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		if err := render.RenderTemplates(w, "complete-profile.html", CompleteProfilePageData{
			IsBuyer: farmer.IsBuyer(),
		}); err != nil {
			log.Println("render error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

	case http.MethodPost:
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			h.renderWithError(w, farmer, "Invalid form submission")
			return
		}

		photoURL, err := saveUploadedFile(r, "profile_picture", "farmers")
		if err != nil || photoURL == "" {
			h.renderWithError(w, farmer, "A profile photo is required to continue")
			return
		}

		if err := h.auth.UpdatePhoto(farmer.ID, photoURL); err != nil {
			log.Println("photo update failed:", err)
			h.renderWithError(w, farmer, "Could not save your photo — try again")
			return
		}

		// Bank details stay optional, and only apply to farmers.
		if !farmer.IsBuyer() {
			bankName := r.FormValue("bank-name")
			bankCode := r.FormValue("bank-code")
			accountName := r.FormValue("account-name")
			accountNumber := r.FormValue("account-number")
			if bankName != "" || bankCode != "" || accountName != "" || accountNumber != "" {
				if err := h.auth.UpdateBankDetails(farmer.ID, bankName, bankCode, accountName, accountNumber); err != nil {
					log.Println("bank details update failed:", err)
				}
			}
		}

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (h *CompleteProfile) renderWithError(w http.ResponseWriter, farmer *models.Farmer, message string) {
	if err := render.RenderTemplates(w, "complete-profile.html", CompleteProfilePageData{
		IsBuyer: farmer.IsBuyer(),
		Error:   message,
	}); err != nil {
		log.Println("render error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
