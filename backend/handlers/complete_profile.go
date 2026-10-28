package handlers

import (
	"log"
	"net/http"

	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type CompleteProfilePageData struct {
	Error string
}

type selfieSaver interface {
	UpdateSelfie(int, string) error
}

type CompleteProfile struct {
	auth selfieSaver
}

func NewCompleteProfileHandler(auth selfieSaver) *CompleteProfile {
	return &CompleteProfile{auth: auth}
}

func (h *CompleteProfile) Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if farmer.SelfieCapturedAt.Valid {
		http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		h.renderPage(w, http.StatusOK, "")
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		h.fail(w, r, "We couldn't read your selfie. Please try again.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	photoURL, err := saveUploadedFile(r, "profile_picture", "farmers")
	if err != nil {
		log.Println("selfie upload failed:", err)
		h.fail(w, r, uploadErrorMessage(err))
		return
	}
	if photoURL == "" {
		h.fail(w, r, "Take a selfie before continuing.")
		return
	}
	if err := h.auth.UpdateSelfie(farmer.ID, photoURL); err != nil {
		log.Println("selfie update failed:", err)
		h.fail(w, r, "We couldn't save your selfie. Please try again.")
		return
	}
	if r.Header.Get("Accept") == "application/json" {
		writeJSON(w, http.StatusOK, map[string]string{"next": "/verify-phone"})
		return
	}
	http.Redirect(w, r, "/verify-phone", http.StatusSeeOther)
}

func (h *CompleteProfile) fail(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("Accept") == "application/json" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
		return
	}
	h.renderPage(w, http.StatusBadRequest, message)
}

func (h *CompleteProfile) renderPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := render.RenderTemplates(w, "complete-profile.html", CompleteProfilePageData{Error: message}); err != nil {
		log.Println("render selfie page:", err)
	}
}
