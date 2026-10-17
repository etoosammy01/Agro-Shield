package handlers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"backend/internal/models"
	"backend/internal/services"
	"backend/render"
)

type ForgotPassword struct {
	auth *services.AuthService
	otp  *services.OTPService
}

func NewForgotPasswordHandler(auth *services.AuthService, otp *services.OTPService) *ForgotPassword {
	return &ForgotPassword{auth: auth, otp: otp}
}

type ForgotPasswordPageData struct {
	Error   string
	Success string
	// CodeSent keeps the verification-code field on screen after the first
	// step, so the user doesn't have to re-request it.
	CodeSent bool
	// Phone is echoed back into the form. Without it the field was cleared
	// after the code was requested, so the second step submitted an empty
	// number and reported the code as invalid even when it was correct.
	Phone string
}

func (h *ForgotPassword) Handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.renderPage(w, "", ForgotPasswordPageData{})

	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}
		phone := strings.TrimSpace(r.FormValue("phone"))
		// The form submits twice: once to ask for a code, once to spend it.
		if r.FormValue("action") == "send_code" {
			h.sendCode(w, r, phone)
			return
		}
		h.resetPassword(w, r, phone)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// sendCode issues a one-time passcode for the account owning this phone number.
//
// The response is the same whether or not an account exists, so this page can't
// be used to discover which phone numbers are registered.
func (h *ForgotPassword) sendCode(w http.ResponseWriter, r *http.Request, phone string) {
	if phone == "" {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: "Enter your phone number first."})
		return
	}

	const genericSent = "If that number has an account, we've sent a verification code."

	farmer, err := h.auth.FindByPhone(phone)
	if err != nil {
		log.Printf("password reset lookup: %v", err)
		h.renderPage(w, phone, ForgotPasswordPageData{Success: genericSent, CodeSent: true})
		return
	}
	if farmer == nil {
		h.renderPage(w, phone, ForgotPasswordPageData{Success: genericSent, CodeSent: true})
		return
	}

	if err := h.otp.Issue(r.Context(), farmer.ID, farmer.Phone, models.OTPPurposePasswordReset); err != nil {
		log.Printf("issue password reset code for user %d: %v", farmer.ID, err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: otpUserMessage(err), CodeSent: true})
		return
	}

	h.renderPage(w, phone, ForgotPasswordPageData{
		Success:  "We've sent a verification code to your phone. It expires in 5 minutes.",
		CodeSent: true,
	})
}

// resetPassword verifies the passcode before writing the new password. No
// password changes without a valid code — this is what closed the account
// takeover hole where a phone number alone was enough.
func (h *ForgotPassword) resetPassword(w http.ResponseWriter, r *http.Request, phone string) {
	code := strings.TrimSpace(r.FormValue("code"))
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if newPassword != confirmPassword {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: "Passwords do not match", CodeSent: true})
		return
	}
	if code == "" {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: "Enter the verification code we sent to your phone.", CodeSent: true})
		return
	}

	// Deliberately the same wording as a wrong code, so a failed reset doesn't
	// reveal whether the number is registered.
	const genericInvalid = "That verification code is not correct or has expired."

	farmer, err := h.auth.FindByPhone(phone)
	if err != nil {
		log.Printf("password reset lookup: %v", err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: genericInvalid, CodeSent: true})
		return
	}
	if farmer == nil {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: genericInvalid, CodeSent: true})
		return
	}

	if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposePasswordReset, code); err != nil {
		log.Printf("verify password reset code for user %d: %v", farmer.ID, err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: otpUserMessage(err), CodeSent: true})
		return
	}

	if err := h.auth.ResetPassword(phone, newPassword); err != nil {
		log.Printf("reset password for user %d: %v", farmer.ID, err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: err.Error(), CodeSent: true})
		return
	}

	h.renderPage(w, phone, ForgotPasswordPageData{Success: "Password updated — you can now log in."})
}

// otpUserMessage turns an OTP error into something safe to show. Only the
// errors a user can act on are surfaced; anything else would leak internals.
func otpUserMessage(err error) string {
	switch {
	case errors.Is(err, services.ErrOTPRateLimited),
		errors.Is(err, services.ErrOTPUnusablePhone):
		return err.Error()
	default:
		return "We couldn't send a verification code right now. Please try again in a moment."
	}
}

func (h *ForgotPassword) renderPage(w http.ResponseWriter, phone string, data ForgotPasswordPageData) {
	data.Phone = phone
	if err := render.RenderTemplates(w, "forgot-password.html", data); err != nil {
		log.Println("render error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
