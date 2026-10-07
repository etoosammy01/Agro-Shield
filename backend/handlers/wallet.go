package handlers

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type WalletHandler struct {
	wallet *services.WalletService
}

func NewWalletHandler(wallet *services.WalletService) *WalletHandler {
	return &WalletHandler{wallet: wallet}
}

type WalletPageData struct {
	Wallet        *models.Wallet
	Transactions  []models.WalletTransaction
	Deposits      []models.WalletDeposit
	Withdrawals   []models.Withdrawal
	BankName      string
	BankCode      string
	AccountName   string
	AccountNumber string
	Error         string
	Success       string
	IsAdmin       bool
}

func (h *WalletHandler) Page(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	wallet, transactions, err := h.wallet.Wallet(r.Context(), farmer.ID)
	if err != nil {
		log.Printf("load wallet for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not load wallet", http.StatusInternalServerError)
		return
	}
	withdrawals, err := h.wallet.Withdrawals(r.Context(), farmer.ID)
	if err != nil {
		log.Printf("load wallet withdrawals for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not load withdrawal history", http.StatusInternalServerError)
		return
	}
	deposits, err := h.wallet.Deposits(r.Context(), farmer.ID)
	if err != nil {
		log.Printf("load wallet deposits for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not load deposit history", http.StatusInternalServerError)
		return
	}
	data := WalletPageData{
		Wallet: wallet, Transactions: transactions, Deposits: deposits, Withdrawals: withdrawals,
		BankName: farmer.BankName, BankCode: farmer.BankCode,
		AccountName: farmer.AccountName, AccountNumber: farmer.AccountNumber,
		Error: r.URL.Query().Get("error"), Success: r.URL.Query().Get("success"),
		IsAdmin: h.wallet.IsAdmin(farmer.ID),
	}
	if err := render.RenderTemplates(w, "wallet.html", data); err != nil {
		log.Printf("render wallet page for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not render wallet", http.StatusInternalServerError)
	}
}

func (h *WalletHandler) Deposit(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	result, err := h.wallet.InitiateDeposit(r.Context(), farmer, r.FormValue("amount"))
	if err != nil {
		log.Printf("wallet deposit initiation for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape("We could not confirm the payment setup because of a technical issue. Check your deposit history before retrying; if you completed a payment, do not pay again while its status is pending."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, result.CheckoutURL, http.StatusSeeOther)
}

func (h *WalletHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	if err := h.wallet.RequestWithdrawal(r.Context(), farmer.ID, r.FormValue("amount")); err != nil {
		log.Printf("wallet withdrawal request for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape("Withdrawal request submitted for admin approval."), http.StatusSeeOther)
}

func (h *WalletHandler) SaveBankDetails(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	err := h.wallet.UpdateBankDetails(
		farmer.ID,
		r.FormValue("bank_name"),
		r.FormValue("bank_code"),
		r.FormValue("account_name"),
		r.FormValue("account_number"),
	)
	if err != nil {
		log.Printf("save wallet payout details for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape("Bank payout details saved."), http.StatusSeeOther)
}

func (h *WalletHandler) Banks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	banks, err := h.wallet.Banks()
	if err != nil {
		log.Printf("load Flutterwave bank directory for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not load banks from payment provider", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, banks)
}

func (h *WalletHandler) AdminPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil || !h.wallet.IsAdmin(farmer.ID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	withdrawals, err := h.wallet.AdminWithdrawals(r.Context())
	if err != nil {
		log.Printf("load wallet withdrawals for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not load withdrawal requests", http.StatusInternalServerError)
		return
	}
	data := struct {
		Withdrawals []models.Withdrawal
		Error       string
		Success     string
	}{withdrawals, r.URL.Query().Get("error"), r.URL.Query().Get("success")}
	if err := render.RenderTemplates(w, "wallet-admin.html", data); err != nil {
		log.Printf("render wallet admin page for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not render withdrawal requests", http.StatusInternalServerError)
	}
}

func (h *WalletHandler) ApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.handleAdminAction(w, r, func(ctx context.Context, id int64, adminID int) error {
		return h.wallet.ApproveWithdrawal(ctx, id, adminID)
	}, "Payout started with Flutterwave.")
}

func (h *WalletHandler) RejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.handleAdminAction(w, r, func(ctx context.Context, id int64, adminID int) error {
		return h.wallet.RejectWithdrawal(ctx, id, adminID)
	}, "Withdrawal rejected and reserved funds returned.")
}

func (h *WalletHandler) ReconcileWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.handleAdminAction(w, r, func(ctx context.Context, id int64, adminID int) error {
		_, err := h.wallet.ReconcileWithdrawal(ctx, id, adminID)
		return err
	}, "Provider status checked. Review the withdrawal status; funds stay reserved while the payout is processing.")
}

func (h *WalletHandler) handleAdminAction(w http.ResponseWriter, r *http.Request, action func(context.Context, int64, int) error, success string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil || !h.wallet.IsAdmin(farmer.ID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("withdrawal_id")), 10, 64)
	if err != nil || id <= 0 {
		http.Redirect(w, r, "/admin/wallet-withdrawals?error="+url.QueryEscape("Invalid withdrawal request."), http.StatusSeeOther)
		return
	}
	if err := action(r.Context(), id, farmer.ID); err != nil {
		log.Printf("admin %d wallet withdrawal action: %v", farmer.ID, err)
		http.Redirect(w, r, "/admin/wallet-withdrawals?error="+url.QueryEscape("Could not confirm the payout action due to a technical issue or status change. Check the withdrawal status before retrying; funds remain reserved while a payout is processing."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/wallet-withdrawals?success="+url.QueryEscape(success), http.StatusSeeOther)
}
