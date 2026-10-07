package handlers

import (
	"log"
	"net/http"
	"net/url"

	"backend/internal/services"
	"backend/middleware"
)

type ProfileEdit struct {
	auth *services.AuthService
}

func NewProfileEditHandler(auth *services.AuthService) *ProfileEdit {
	return &ProfileEdit{auth: auth}
}

func (h *ProfileEdit) Handler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Println("form parse error:", err)
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Could not read profile changes. Please try again."), http.StatusSeeOther)
		return
	}

	fullName := r.FormValue("full_name")
	phone := r.FormValue("phone")
	email := r.FormValue("email")
	community := r.FormValue("community")
	lga := r.FormValue("lga")
	state := r.FormValue("state")
	country := r.FormValue("country")

	if err := h.auth.UpdateProfile(farmer.ID, fullName, phone, email, community, lga, state, country); err != nil {
		log.Println("profile update failed:", err)
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	photoURL, err := saveUploadedFile(r, "passport", "farmers")
	if err != nil {
		log.Println("passport photo upload failed:", err)
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Profile details were saved, but the photo could not be uploaded."), http.StatusSeeOther)
		return
	} else if photoURL != "" {
		if err := h.auth.UpdatePhoto(farmer.ID, photoURL); err != nil {
			log.Println("photo update failed:", err)
			http.Redirect(w, r, "/profile?error="+url.QueryEscape("Profile details were saved, but the photo could not be saved."), http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/profile?success="+url.QueryEscape("Profile details saved."), http.StatusSeeOther)
}
