package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"

	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type transactionPINCreator interface {
	Create(context.Context, int, string, string) error
}

type TransactionPINSetup struct {
	pins transactionPINCreator
}

type TransactionPINPageData struct {
	Error        string
	WalletNumber string
}

func NewTransactionPINHandler(pins transactionPINCreator) *TransactionPINSetup {
	return &TransactionPINSetup{pins: pins}
}

func (h *TransactionPINSetup) Handler(w http.ResponseWriter, r *http.Request) {
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
	if !farmer.SelfieCapturedAt.Valid || !farmer.PhoneVerifiedAt.Valid {
		http.Redirect(w, r, services.OnboardingPath(farmer), http.StatusSeeOther)
		return
	}
	if farmer.HasTransactionPIN {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		h.renderPage(w, http.StatusOK, farmer.WalletNumber, "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		h.renderPage(w, http.StatusBadRequest, farmer.WalletNumber, "We couldn't read that form. Please try again.")
		return
	}
	err := h.pins.Create(r.Context(), farmer.ID, r.FormValue("pin"), r.FormValue("confirm_pin"))
	switch {
	case err == nil, errors.Is(err, repository.ErrTransactionPINAlreadySet):
		h.finish(w, r)
	case errors.Is(err, services.ErrInvalidTransactionPIN), errors.Is(err, services.ErrTransactionPINMismatch):
		h.renderPage(w, http.StatusBadRequest, farmer.WalletNumber, err.Error())
	case errors.Is(err, repository.ErrTransactionPINSetupUnavailable):
		http.Redirect(w, r, "/verify-phone", http.StatusSeeOther)
	default:
		log.Printf("create transaction PIN for user %d: %v", farmer.ID, err)
		h.renderPage(w, http.StatusInternalServerError, farmer.WalletNumber, "We couldn't save your transaction PIN. Please try again.")
	}
}

func (h *TransactionPINSetup) finish(w http.ResponseWriter, r *http.Request) {
	if sessionID, ok := middleware.SessionIDFromRequest(r); ok {
		middleware.DeleteSession(sessionID)
	}
	middleware.ClearSessionCookie(w)
	http.Redirect(w, r, "/login?onboarded=1", http.StatusSeeOther)
}

func (h *TransactionPINSetup) renderPage(w http.ResponseWriter, status int, walletNumber, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := render.RenderTemplates(w, "create-transaction-pin.html", TransactionPINPageData{Error: message, WalletNumber: walletNumber}); err != nil {
		log.Println("render transaction PIN page:", err)
	}
}
