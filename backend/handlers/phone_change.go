package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"backend/internal/otp"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
)

type phoneChangeAuth interface {
	RequestPhoneChange(context.Context, int, string) error
	CancelPhoneChange(context.Context, int) error
}

type phoneChangeOTP interface {
	RequestPhoneChange(context.Context, int, string) error
	VerifyPhoneChange(context.Context, int, string, string) error
}

type PhoneChange struct {
	auth phoneChangeAuth
	otp  phoneChangeOTP
}

func NewPhoneChangeHandler(auth phoneChangeAuth, otp phoneChangeOTP) *PhoneChange {
	return &PhoneChange{auth: auth, otp: otp}
}

func (h *PhoneChange) RequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Could not read the phone number. Please try again."})
		return
	}

	phone := strings.TrimSpace(r.FormValue("phone"))
	normalized := services.NormalizeNigerianPhone(phone)
	if normalized == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Enter a valid Nigerian phone number."})
		return
	}

	if err := h.auth.RequestPhoneChange(r.Context(), farmer.ID, normalized); err != nil {
		log.Printf("stage phone change for user %d: %v", farmer.ID, err)
		status, message := phoneChangeStageError(err)
		writeJSON(w, status, map[string]string{"error": message})
		return
	}

	if err := h.otp.RequestPhoneChange(r.Context(), farmer.ID, normalized); err != nil {
		log.Printf("issue phone change code for user %d: %v", farmer.ID, err)
		status := http.StatusBadGateway
		if errors.Is(err, otp.ErrRateLimited) {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{"error": otpUserMessage(err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Verification code sent. It expires in 5 minutes."})
}

func phoneChangeStageError(err error) (int, string) {
	switch {
	case errors.Is(err, services.ErrPhoneTaken):
		return http.StatusConflict, "That phone number is already in use. Please try another number."
	case errors.Is(err, services.ErrPhoneAlreadyCurrent):
		return http.StatusBadRequest, "That phone number is already your verified number."
	case errors.Is(err, repository.ErrAccountNotFound):
		return http.StatusUnauthorized, "We couldn't start the phone change. Please try again."
	default:
		return http.StatusInternalServerError, "We couldn't start the phone change. Please try again."
	}
}

func (h *PhoneChange) VerifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Could not read the verification code. Please try again."})
		return
	}

	phone := strings.TrimSpace(r.FormValue("phone"))
	code := strings.TrimSpace(r.FormValue("code"))
	normalized := services.NormalizeNigerianPhone(phone)
	if normalized == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Enter a valid Nigerian phone number."})
		return
	}
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Enter the verification code we sent to your phone."})
		return
	}

	if err := h.otp.VerifyPhoneChange(r.Context(), farmer.ID, normalized, code); err != nil {
		status := http.StatusBadRequest
		message := otpVerificationMessage(err)
		if errors.Is(err, otp.ErrRateLimited) {
			status = http.StatusTooManyRequests
		}
		if errors.Is(err, services.ErrPhoneTaken) {
			status = http.StatusConflict
			message = "That phone number is already in use. Please try another number."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Phone number updated successfully."})
}

func (h *PhoneChange) CancelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	if err := h.auth.CancelPhoneChange(r.Context(), farmer.ID); err != nil {
		status := http.StatusConflict
		message := "We couldn't cancel the phone change. Please try again."
		switch {
		case errors.Is(err, services.ErrNoPendingPhoneChange):
			status = http.StatusConflict
			message = "No pending phone change."
		case errors.Is(err, repository.ErrAccountNotFound):
			status = http.StatusUnauthorized
			message = "We couldn't cancel the phone change. Please try again."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Phone change cancelled."})
}
