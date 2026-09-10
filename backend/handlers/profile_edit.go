package handlers

import (
	"log"
	"net/http"

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
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	fullName := r.FormValue("full_name")
	phone := r.FormValue("phone")
	email := r.FormValue("email")
	location := r.FormValue("location")

	if err := h.auth.UpdateProfile(farmer.ID, fullName, phone, email, location); err != nil {
		log.Println("profile update failed:", err)
	}

	photoURL, err := saveUploadedFile(r, "passport", "farmers")
	if err != nil {
		log.Println("passport photo upload failed:", err)
	} else if photoURL != "" {
		if err := h.auth.UpdatePhoto(farmer.ID, photoURL); err != nil {
			log.Println("photo update failed:", err)
		}
	}

	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}
