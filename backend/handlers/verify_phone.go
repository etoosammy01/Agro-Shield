package handlers

import (
	"context"
	"log"
	"net/http"

	"backend/internal/models"
	"backend/middleware"
	"backend/render"
)

type phoneVerifier interface {
	Issue(context.Context, int, string, string) error
	VerifyPhone(context.Context, int, string, string) error
}

type VerifyPhone struct {
	otp phoneVerifier
}

type VerifyPhonePageData struct {
	Phone    string
	Error    string
	Success  string
	CodeSent bool
}

func NewVerifyPhoneHandler(otp phoneVerifier) *VerifyPhone {
	return &VerifyPhone{otp: otp}
}

func (h *VerifyPhone) Handler(w http.ResponseWriter, r *http.Request) {
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
	if !farmer.SelfieCapturedAt.Valid {
		http.Redirect(w, r, "/complete-profile", http.StatusSeeOther)
		return
	}
	if farmer.PhoneVerifiedAt.Valid {
		h.finish(w, r, farmer)
		return
	}
	page := VerifyPhonePageData{Phone: farmer.Phone}
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("sent") == "1" {
			page.CodeSent = true
			page.Success = "We've sent a verification code to your phone. It expires in 5 minutes."
			if transport, ok := h.otp.(interface{ DevelopmentDelivery() bool }); ok && transport.DevelopmentDelivery() {
				page.Success = "Development mode: no SMS was sent. Your verification code is in the server log and expires in 5 minutes."
			}
		}
		h.renderPage(w, http.StatusOK, page)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		page.Error = "We couldn't read that form. Please try again."
		h.renderPage(w, http.StatusBadRequest, page)
		return
	}
	switch r.FormValue("action") {
	case "send_code":
		if err := h.otp.Issue(r.Context(), farmer.ID, farmer.Phone, models.OTPPurposePhoneVerification); err != nil {
			log.Printf("send phone verification for user %d: %v", farmer.ID, err)
			page.Error = otpUserMessage(err)
			h.renderPage(w, http.StatusBadRequest, page)
			return
		}
		http.Redirect(w, r, "/verify-phone?sent=1", http.StatusSeeOther)
	case "verify_code":
		if err := h.otp.VerifyPhone(r.Context(), farmer.ID, farmer.Phone, r.FormValue("code")); err != nil {
			log.Printf("verify phone for user %d: %v", farmer.ID, err)
			page.CodeSent = true
			page.Error = otpVerificationMessage(err)
			h.renderPage(w, http.StatusBadRequest, page)
			return
		}
		h.finish(w, r, farmer)
	default:
		page.Error = "Choose Send code or Verify to continue."
		h.renderPage(w, http.StatusBadRequest, page)
	}
}

func (h *VerifyPhone) finish(w http.ResponseWriter, r *http.Request, farmer *models.Farmer) {
	if !farmer.HasTransactionPIN {
		http.Redirect(w, r, "/create-transaction-pin", http.StatusSeeOther)
		return
	}
	if sessionID, ok := middleware.SessionIDFromRequest(r); ok {
		middleware.DeleteSession(sessionID)
	}
	middleware.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/login?verified=1", http.StatusSeeOther)
}

func (h *VerifyPhone) renderPage(w http.ResponseWriter, status int, page VerifyPhonePageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := render.RenderTemplates(w, "verify-phone.html", page); err != nil {
		log.Println("render phone verification page:", err)
	}
}
