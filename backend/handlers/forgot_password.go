package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"backend/internal/models"
	"backend/internal/services"
	"backend/render"
)

// forgotPasswordAuth is the slice of AuthService the password-reset flow
// needs. A small interface keeps the handler testable with fakes.
type forgotPasswordAuth interface {
	FindByPhone(phone string) (*models.Farmer, error)
	ResetPassword(phone, newPassword string) error
}

// forgotPasswordOTP is the slice of the OTP service the password-reset flow
// needs.
type forgotPasswordOTP interface {
	Issue(ctx context.Context, farmerID int, phone, purpose string) error
	Verify(ctx context.Context, farmerID int, purpose, code string) error
}

type ForgotPassword struct {
	auth forgotPasswordAuth
	otp  forgotPasswordOTP
}

func NewForgotPasswordHandler(auth forgotPasswordAuth, otpService forgotPasswordOTP) *ForgotPassword {
	return &ForgotPassword{auth: auth, otp: otpService}
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

// sentCodeMessage is the only success-style response this page gives. It is
// worded identically for registered and unregistered numbers, and it is also
// used when issuing or delivering the code fails, so the response can never be
// used to discover whether a phone number has an account.
const sentCodeMessage = "If that number has an account, we've sent a verification code. It expires in 5 minutes."

// sendCode issues a one-time passcode for the account owning this phone number.
//
// The response is the same whether or not an account exists, and the same when
// issuing or delivering the code fails, so this page can't be used to discover
// which phone numbers are registered.
func (h *ForgotPassword) sendCode(w http.ResponseWriter, r *http.Request, phone string) {
	if phone == "" {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: "Enter your phone number first."})
		return
	}

	farmer, err := h.auth.FindByPhone(phone)
	if err != nil {
		log.Printf("password reset lookup: %v", err)
		h.renderPage(w, phone, ForgotPasswordPageData{Success: sentCodeMessage, CodeSent: true})
		return
	}
	if farmer == nil {
		h.renderPage(w, phone, ForgotPasswordPageData{Success: sentCodeMessage, CodeSent: true})
		return
	}

	if err := h.otp.Issue(r.Context(), farmer.ID, farmer.Phone, models.OTPPurposePasswordReset); err != nil {
		// Rate limits, delivery failures, and unusable numbers are logged but
		// deliberately not shown: any visible difference would reveal that the
		// phone number belongs to an account.
		log.Printf("issue password reset code for user %d: %v", farmer.ID, err)
	}

	h.renderPage(w, phone, ForgotPasswordPageData{Success: sentCodeMessage, CodeSent: true})
}

// invalidCodeMessage is the single failure response for every code and lookup
// outcome on this page: an unknown number, a wrong code, an expired or already
// used code, too many attempts, and infrastructure errors. One wording keeps
// the page from revealing which phone numbers are registered.
const invalidCodeMessage = "That verification code is not correct or has expired."

// resetFailedMessage covers every password-write failure that is not a
// user-fixable validation error: account lookups, database, and hashing
// failures all share this one response.
const resetFailedMessage = "We couldn't reset that password right now. Please try again."

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

	farmer, err := h.auth.FindByPhone(phone)
	if err != nil {
		log.Printf("password reset lookup: %v", err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: invalidCodeMessage, CodeSent: true})
		return
	}
	if farmer == nil {
		h.renderPage(w, phone, ForgotPasswordPageData{Error: invalidCodeMessage, CodeSent: true})
		return
	}

	if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposePasswordReset, code); err != nil {
		// Wrong, expired, used, exhausted, and infrastructure failures all get
		// the same wording, so a failed reset doesn't reveal whether the
		// number is registered.
		log.Printf("verify password reset code for user %d: %v", farmer.ID, err)
		h.renderPage(w, phone, ForgotPasswordPageData{Error: invalidCodeMessage, CodeSent: true})
		return
	}

	if err := h.auth.ResetPassword(phone, newPassword); err != nil {
		log.Printf("reset password for user %d: %v", farmer.ID, err)
		// Password rules are fixable by the user, so they keep their specific
		// wording. Every other failure (account lookup, database, hashing)
		// gets one generic response so the page cannot probe account state.
		message := resetFailedMessage
		if errors.Is(err, services.ErrPasswordTooShort) {
			message = err.Error()
		}
		h.renderPage(w, phone, ForgotPasswordPageData{Error: message, CodeSent: true})
		return
	}

	h.renderPage(w, phone, ForgotPasswordPageData{Success: "Password updated — you can now log in."})
}

func (h *ForgotPassword) renderPage(w http.ResponseWriter, phone string, data ForgotPasswordPageData) {
	data.Phone = phone
	if err := render.RenderTemplates(w, "forgot-password.html", data); err != nil {
		log.Println("render error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
