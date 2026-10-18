package handlers

import (
	"errors"
	"log"
	"net/http"
	"net/url"

	"backend/internal/repository"
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
		message := "We couldn't save your profile changes. Please check the details and try again."
		if errors.Is(err, repository.ErrPhoneExists) {
			message = "That phone number is already registered to another account."
		}
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}

	photoURL, err := saveUploadedFile(r, "passport", "farmers")
	if err != nil {
		log.Println("passport photo upload failed:", err)
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Profile details were saved, but the photo could not be uploaded. "+uploadErrorMessage(err)), http.StatusSeeOther)
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

// DeleteHandler closes the signed-in user's account. The form has already
// asked the user to type their own phone number and current password; those
// are checked again here because the browser is not trusted. On success every
// session for the account is dropped and the user is sent to the login page
// with a confirmation notice. No success message is shown on failure.
func (h *ProfileEdit) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Could not read the confirmation. Please try again."), http.StatusSeeOther)
		return
	}

	err := h.auth.CloseAccount(r.Context(), farmer.ID, r.FormValue("confirm_phone"), r.FormValue("password"))
	if err != nil {
		var message string
		switch {
		case errors.Is(err, services.ErrDeletePhoneMismatch):
			message = "The phone number you typed does not match this account."
		case errors.Is(err, services.ErrDeletePasswordWrong):
			message = "That password is not correct."
		case errors.Is(err, services.ErrDeletePasswordRequired):
			message = "Enter your password to confirm."
		case errors.Is(err, repository.ErrAccountHasBalance):
			message = "Your wallet still has a balance. Withdraw it before closing your account."
		case errors.Is(err, repository.ErrAccountHasPendingRequest):
			message = "A wallet transaction is still pending. Wait for it to finish before closing your account."
		case errors.Is(err, repository.ErrAccountHasFundingAccount):
			message = "Your funding account must be closed before you can close your Agro-Shield account. Please contact support."
		case errors.Is(err, repository.ErrAccountNotFound):
			message = "This account is no longer active."
		default:
			log.Printf("close account for user %d: %v", farmer.ID, err)
			message = "We couldn't close your account right now. Please try again."
		}
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(message), http.StatusSeeOther)
		return
	}

	if sessionID, ok := middleware.SessionIDFromRequest(r); ok {
		middleware.DeleteSession(sessionID)
	}
	middleware.DeleteFarmerSessions(farmer.ID)
	middleware.ClearSessionCookie(w)
	http.Redirect(w, r, "/login?deleted=1", http.StatusSeeOther)
}
