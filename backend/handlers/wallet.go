package handlers

import (
	"context"
	"errors"
	"fmt"
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
	wallet       *services.WalletService
	otp          *services.OTPService
	notification *services.NotificationService
}

func NewWalletHandler(wallet *services.WalletService, otp *services.OTPService, notification *services.NotificationService) *WalletHandler {
	return &WalletHandler{wallet: wallet, otp: otp, notification: notification}
}

type WalletPageData struct {
	Wallet        *models.Wallet
	Transactions  []models.WalletTransaction
	Deposits      []models.WalletDeposit
	Withdrawals   []models.Withdrawal
	Transfers     []models.Transfer
	BankName      string
	BankCode      string
	AccountName   string
	AccountNumber string
	Error         string
	Success       string
	IsAdmin       bool
}

// WalletAdminPageData is the approvals page's payload.
//
// It is a named type rather than an inline struct because the template tests
// have to build one too: an inline struct has to be copied by hand into each
// test, and the copy silently stops matching the template the moment a field is
// added — which is a compile-clean, render-time failure.
type WalletAdminPageData struct {
	Withdrawals       []models.Withdrawal
	Transfers         []models.Transfer
	StalePending      []models.WalletDeposit
	StaleAfterHours   int
	RevenueTotal      string
	RevenueCount      int
	CommissionEnabled bool
	CommissionLabel   string
	Error             string
	Success           string
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
	transfers, err := h.wallet.Transfers(r.Context(), farmer.ID)
	if err != nil {
		log.Printf("load wallet transfers for user %d: %v", farmer.ID, err)
		http.Error(w, "Could not load transfer history", http.StatusInternalServerError)
		return
	}
	data := WalletPageData{
		Wallet: wallet, Transactions: transactions, Deposits: deposits,
		Withdrawals: withdrawals, Transfers: transfers,
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

// SendOTP issues a one-time passcode for a wallet action.
//
// The code always goes to the phone number already on the account, never one
// supplied by the browser — otherwise the second factor would prove nothing.
// Only the wallet purposes are accepted here; password reset has its own
// unauthenticated flow.
func (h *WalletHandler) SendOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	purpose := strings.TrimSpace(r.FormValue("purpose"))
	if purpose != models.OTPPurposeWithdrawal && purpose != models.OTPPurposeTransfer {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unsupported verification purpose."})
		return
	}

	if err := h.otp.Issue(r.Context(), farmer.ID, farmer.Phone, purpose); err != nil {
		log.Printf("issue %s code for user %d: %v", purpose, farmer.ID, err)
		status := http.StatusBadGateway
		if errors.Is(err, services.ErrOTPRateLimited) {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{"error": otpUserMessage(err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Verification code sent. It expires in 5 minutes.",
	})
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

	// Order matters. Everything that can be rejected cheaply is checked before
	// the passcode is spent, so a doomed request doesn't cost the user a code.
	if farmer.BankName == "" || farmer.BankCode == "" || farmer.AccountName == "" || farmer.AccountNumber == "" {
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape("Save your bank payout details before requesting a withdrawal."), http.StatusSeeOther)
		return
	}
	if err := h.wallet.PreflightWithdrawal(r.Context(), farmer.ID, r.FormValue("amount")); err != nil {
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	// No money moves without proof of control of the account's phone.
	if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposeWithdrawal, r.FormValue("code")); err != nil {
		log.Printf("verify withdrawal code for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(otpUserMessage(err)), http.StatusSeeOther)
		return
	}

	if err := h.wallet.RequestWithdrawal(r.Context(), farmer.ID, r.FormValue("amount")); err != nil {
		log.Printf("wallet withdrawal request for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape("Withdrawal request submitted for admin approval."), http.StatusSeeOther)
}

// walletUserMessage picks text that is safe to show the user.
//
// Validation errors are written to be read by a person and pass through
// unchanged; infrastructure failures are replaced, so raw driver text never
// reaches the page.
func walletUserMessage(err error) string {
	switch {
	case errors.Is(err, services.ErrWalletUnavailable),
		errors.Is(err, services.ErrTransferUnavailable):
		return "That could not be completed because of a technical issue. Check your wallet history before retrying; anything already on hold is listed there."
	default:
		return err.Error()
	}
}

// Transfer reserves money from the sender's wallet for a recipient identified
// by phone number.
//
// The money leaves the sender immediately but does not reach the recipient
// until an admin approves it, so a mistyped or fraudulent transfer can be
// rejected and refunded.
func (h *WalletHandler) Transfer(w http.ResponseWriter, r *http.Request) {
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

	// Same ordering rule as withdrawals: everything that can be rejected
	// cheaply is checked before the passcode is spent.
	recipient, err := h.wallet.ResolveTransferRecipient(r.Context(), farmer.ID, r.FormValue("recipient_phone"))
	if err != nil {
		log.Printf("resolve transfer recipient for user %d: %v", farmer.ID, err)
		h.redirectTransferError(w, r, err)
		return
	}
	if err := h.wallet.PreflightTransfer(r.Context(), farmer.ID, r.FormValue("amount")); err != nil {
		h.redirectTransferError(w, r, err)
		return
	}
	// No money moves without proof of control of the account's phone.
	if err := h.otp.Verify(r.Context(), farmer.ID, models.OTPPurposeTransfer, r.FormValue("code")); err != nil {
		log.Printf("verify transfer code for user %d: %v", farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(otpUserMessage(err)), http.StatusSeeOther)
		return
	}

	if err := h.wallet.RequestTransfer(r.Context(), farmer.ID, recipient.ID, r.FormValue("amount"), r.FormValue("note")); err != nil {
		log.Printf("wallet transfer request for user %d: %v", farmer.ID, err)
		h.redirectTransferError(w, r, err)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape(
		"Transfer submitted for admin approval. The money is on hold and will reach "+recipient.FullName+" once it is approved; you can see it below as pending."),
		http.StatusSeeOther)
}

func (h *WalletHandler) redirectTransferError(w http.ResponseWriter, r *http.Request, err error) {
	http.Redirect(w, r, "/wallet?error="+url.QueryEscape(walletUserMessage(err)), http.StatusSeeOther)
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
	transfers, err := h.wallet.AdminTransfers(r.Context())
	if err != nil {
		log.Printf("load pending wallet transfers for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not load transfer requests", http.StatusInternalServerError)
		return
	}
	revenueTotal, revenueCount, err := h.wallet.PlatformRevenue(r.Context())
	if err != nil {
		log.Printf("load platform revenue for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not load platform revenue", http.StatusInternalServerError)
		return
	}
	// Read-only operational signal: funding attempts no provider has settled.
	// Nothing here credits or expires anything.
	stalePending, err := h.wallet.StalePendingDeposits(r.Context())
	if err != nil {
		log.Printf("load stale pending deposits for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not load pending deposits", http.StatusInternalServerError)
		return
	}
	data := WalletAdminPageData{
		Withdrawals: withdrawals, Transfers: transfers,
		StalePending: stalePending, StaleAfterHours: int(h.wallet.StaleDepositAfter().Hours()),
		RevenueTotal: revenueTotal, RevenueCount: revenueCount,
		CommissionEnabled: h.wallet.CommissionEnabled(), CommissionLabel: h.wallet.CommissionLabel(),
		Error: r.URL.Query().Get("error"), Success: r.URL.Query().Get("success"),
	}
	if err := render.RenderTemplates(w, "wallet-admin.html", data); err != nil {
		log.Printf("render wallet admin page for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not render withdrawal requests", http.StatusInternalServerError)
	}
}

// adminWalletPath is the shared approvals page. Withdrawals and transfers are
// reviewed side by side there, so both sets of actions return to it to show
// their result banner.
const adminWalletPath = "/admin/wallet-withdrawals"

// adminWalletAction describes an admin-triggered change to a wallet record.
//
// Withdrawals and transfers differ only in the form field, the page they return
// to, and the wording, so the authorization and validation are shared rather
// than duplicated — a second copy is how one of the two ends up missing its
// admin check.
type adminWalletAction struct {
	idField      string
	redirectPath string
	failure      string
	success      string
	run          func(ctx context.Context, id int64, adminID int) error
}

func (h *WalletHandler) runAdminWalletAction(w http.ResponseWriter, r *http.Request, action adminWalletAction) {
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
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue(action.idField)), 10, 64)
	if err != nil || id <= 0 {
		http.Redirect(w, r, action.redirectPath+"?error="+url.QueryEscape("Invalid request."), http.StatusSeeOther)
		return
	}
	if err := action.run(r.Context(), id, farmer.ID); err != nil {
		log.Printf("admin %d %s: %v", farmer.ID, action.idField, err)
		http.Redirect(w, r, action.redirectPath+"?error="+url.QueryEscape(action.failure), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, action.redirectPath+"?success="+url.QueryEscape(action.success), http.StatusSeeOther)
}

func (h *WalletHandler) ApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not confirm the payout action due to a technical issue or status change. Check the withdrawal status before retrying; funds remain reserved while a payout is processing.",
		success:      "Payout started with Flutterwave.",
		run: func(ctx context.Context, id int64, adminID int) error {
			return h.wallet.ApproveWithdrawal(ctx, id, adminID)
		},
	})
}

func (h *WalletHandler) RejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not confirm the payout action due to a technical issue or status change. Check the withdrawal status before retrying; funds remain reserved while a payout is processing.",
		success:      "Withdrawal rejected and reserved funds returned.",
		run: func(ctx context.Context, id int64, adminID int) error {
			return h.wallet.RejectWithdrawal(ctx, id, adminID)
		},
	})
}

func (h *WalletHandler) ReconcileWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not confirm the payout action due to a technical issue or status change. Check the withdrawal status before retrying; funds remain reserved while a payout is processing.",
		success:      "Provider status checked. Review the withdrawal status; funds stay reserved while the payout is processing.",
		run: func(ctx context.Context, id int64, adminID int) error {
			_, err := h.wallet.ReconcileWithdrawal(ctx, id, adminID)
			return err
		},
	})
}

// RepairWallet recreates a missing wallet for an existing account.
//
// This is a repair, not a funding operation: it creates an empty NGN wallet and
// never touches a balance. It exists for accounts that predate the wallets table
// or were restored from a backup, which would otherwise be stuck on a 500 with no
// way back. The service logs the operator and the target account.
func (h *WalletHandler) RepairWallet(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "user_id",
		redirectPath: adminWalletPath,
		failure:      "Could not repair that wallet. Check the user id and whether the account exists; no balance was changed.",
		success:      "Wallet repair completed. The account now has an empty NGN wallet.",
		run: func(ctx context.Context, id int64, adminID int) error {
			_, err := h.wallet.RepairWallet(ctx, int(id), adminID)
			return err
		},
	})
}

// notifyTransfer is best-effort. The money has already moved by the time it is
// called, so a notification failure is logged rather than surfaced as an error
// the admin would read as "the transfer did not happen".
func (h *WalletHandler) notifyTransfer(userID int, title, message, kind string) {
	if h.notification == nil {
		return
	}
	if err := h.notification.CreateNotification(userID, title, message, kind); err != nil {
		log.Printf("notify user %d (%s): %v", userID, kind, err)
	}
}

func (h *WalletHandler) ApproveTransfer(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "transfer_id",
		redirectPath: adminWalletPath,
		failure:      "Could not approve the transfer due to a technical issue or a status change. Check the transfer status before retrying; a transfer that is still pending has not been credited.",
		success:      "Transfer approved and the recipient has been credited.",
		run: func(ctx context.Context, id int64, adminID int) error {
			settled, err := h.wallet.ApproveTransfer(ctx, id, adminID)
			if err != nil {
				return err
			}
			h.notifyTransfer(
				settled.RecipientID,
				"Transfer received",
				fmt.Sprintf("%s sent you ₦%s. It is now in your wallet.", settled.SenderName, settled.Amount),
				"wallet_transfer_received",
			)
			return nil
		},
	})
}

func (h *WalletHandler) RejectTransfer(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "transfer_id",
		redirectPath: adminWalletPath,
		failure:      "Could not reject the transfer due to a technical issue or a status change. Check the transfer status before retrying; a transfer that is still pending has not been refunded.",
		success:      "Transfer rejected and the reserved funds returned to the sender.",
		run: func(ctx context.Context, id int64, adminID int) error {
			rejected, err := h.wallet.RejectTransfer(ctx, id, adminID)
			if err != nil {
				return err
			}
			h.notifyTransfer(
				rejected.SenderID,
				"Transfer not approved",
				fmt.Sprintf("Your transfer of ₦%s to %s was not approved, and the money is back in your wallet.", rejected.Amount, rejected.RecipientName),
				"wallet_transfer_rejected",
			)
			return nil
		},
	})
}
