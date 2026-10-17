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
	"time"

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

// refreshAdmins reloads the admin flags written in the database before a page or
// action decides whether the viewer is one. A failed read is logged and the
// cached set kept: the page must still open with the last known answer rather
// than fail because the flags could not be re-read.
func (h *WalletHandler) refreshAdmins(ctx context.Context) {
	if err := h.wallet.RefreshAdminIDs(ctx); err != nil {
		log.Printf("refresh wallet admins: %v", err)
	}
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
	PendingDeposits   []models.PendingDeposit
	Admins            []models.AdminAccount
	AgedRequests      int
	RevenueTotal      string
	RevenueCount      int
	CommissionEnabled bool
	CommissionLabel   string
	Error             string
	Success           string

	// AdminID is the viewer's own farmer id. The page uses it to withhold the
	// approve button on requests they made themselves, which the service would
	// refuse anyway - a control the admin cannot see the reason for reads as a
	// broken button.
	AdminID int

	// AdminName is the viewer's name, shown in the admin console header so a
	// shared phone always makes clear whose session is open.
	AdminName string
}

func (h *WalletHandler) Page(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.refreshAdmins(r.Context())
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
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(depositErrorMessage(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, result.CheckoutURL, http.StatusSeeOther)
}

// depositErrorMessage separates an amount the user can correct from a failure
// that happened after the attempt was already recorded.
//
// Only a rejected amount is shown verbatim in spirit. Everything else keeps the
// payment warning, because by then a deposit row may exist and a delayed
// webhook could still credit it - so "do not pay again while pending" is the
// correct advice.
func depositErrorMessage(err error) string {
	switch err.Error() {
	case "amount is required":
		return "Enter how much you would like to add."
	case "enter a valid positive amount",
		"enter a valid positive amount with up to two decimal places":
		return "Enter a valid amount using digits only, like 5000 or 5000.50."
	case "amount exceeds wallet limits":
		return "That amount is too large. Enter a smaller amount."
	default:
		return "We could not confirm the payment setup because of a technical issue. Check your deposit history before retrying; if you completed a payment, do not pay again while its status is pending."
	}
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

		// A mismatch is the one failure on this path the farmer can fix, so it
		// gets an instruction instead of the raw wrapped error. The detail is
		// kept because "which name does the bank hold?" is exactly what they need
		// to correct an account name.
		if errors.Is(err, services.ErrPayoutDetailsMismatch) {
			http.Redirect(w, r, "/wallet?error="+url.QueryEscape(
				err.Error()+". Update your bank payout details and try again."), http.StatusSeeOther)
			return
		}
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

// cancelErrorMessage names the two outcomes a user can act on - a request that
// no longer exists, and one that was already being paid - and replaces anything
// else, so a driver failure never reaches the page as raw text.
func cancelErrorMessage(err error) string {
	switch {
	case errors.Is(err, services.ErrRequestNotFound):
		return "That request could not be found. Check your wallet history."
	case errors.Is(err, services.ErrRequestNotPending):
		return "That request is no longer pending, so it can no longer be cancelled."
	default:
		return "That could not be cancelled because of a technical issue. Check your wallet history before trying again."
	}
}

// CancelWithdrawal releases the user's own pending withdrawal and returns the
// reserved money to their balance. It is a POST so a link preview or a page
// refresh cannot trigger it.
func (h *WalletHandler) CancelWithdrawal(w http.ResponseWriter, r *http.Request) {
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
	withdrawalID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("withdrawal_id")), 10, 64)
	if err != nil || withdrawalID <= 0 {
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape("Invalid request."), http.StatusSeeOther)
		return
	}
	if err := h.wallet.CancelWithdrawal(r.Context(), farmer.ID, withdrawalID); err != nil {
		log.Printf("cancel withdrawal %d for user %d: %v", withdrawalID, farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(cancelErrorMessage(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape(
		"Withdrawal request cancelled. The reserved money is back in your wallet."), http.StatusSeeOther)
}

// CancelTransfer is the transfer half of CancelWithdrawal.
func (h *WalletHandler) CancelTransfer(w http.ResponseWriter, r *http.Request) {
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
	transferID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("transfer_id")), 10, 64)
	if err != nil || transferID <= 0 {
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape("Invalid request."), http.StatusSeeOther)
		return
	}
	if err := h.wallet.CancelTransfer(r.Context(), farmer.ID, transferID); err != nil {
		log.Printf("cancel transfer %d for user %d: %v", transferID, farmer.ID, err)
		http.Redirect(w, r, "/wallet?error="+url.QueryEscape(cancelErrorMessage(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/wallet?success="+url.QueryEscape(
		"Transfer request cancelled. The money is back in your wallet."), http.StatusSeeOther)
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
		r.Context(),
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

// LookupAccount finds which banks an account number belongs to and the name
// registered on it, so the payout form can fill in a verified name instead of
// asking the user to type one.
//
// This is a POST, not a GET: the account number is a sensitive identifier, and
// a GET would put it in access logs, browser history and any Referer header
// sent from the page.
func (h *WalletHandler) LookupAccount(w http.ResponseWriter, r *http.Request) {
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
	accountNumber := r.FormValue("account_number")

	matches, err := h.wallet.LookupAccountBanks(r.Context(), accountNumber)
	if err != nil {
		// A malformed account number is the user's to fix and is worth saying so;
		// anything else is ours, and is logged rather than shown.
		log.Printf("bank lookup for user %d: %v", farmer.ID, err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if matches == nil {
		matches = []services.BankMatch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"banks": matches})
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
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.refreshAdmins(r.Context())
	if !h.wallet.IsAdmin(farmer.ID) {
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
	// The exception list is supporting information, so a failed read must not
	// take the approvals page down with it: the admin can still act on the
	// withdrawals and transfers that did load.
	pendingDeposits, err := h.wallet.PendingDepositsAwaitingProvider(r.Context())
	if err != nil {
		log.Printf("load pending deposits for admin %d: %v", farmer.ID, err)
		pendingDeposits = nil
	}
	// The admin list is supporting information as well: not being able to read
	// it must not stop the approvals themselves from being served.
	admins, err := h.wallet.AdminAccounts(r.Context())
	if err != nil {
		log.Printf("load wallet admins for admin %d: %v", farmer.ID, err)
		admins = nil
	}
	// The overview counts the same requests the reminder pass nudges, so the
	// number on the page and the notification an admin receives cannot disagree.
	agedCutoff := time.Now().Add(-services.ApprovalReminderAfter)
	agedRequests := 0
	for _, withdrawal := range withdrawals {
		if (withdrawal.Status == models.WithdrawalPending || withdrawal.Status == models.WithdrawalProcessing) &&
			withdrawal.CreatedAt.Before(agedCutoff) {
			agedRequests++
		}
	}
	for _, transfer := range transfers {
		if transfer.Status == models.TransferPending && transfer.CreatedAt.Before(agedCutoff) {
			agedRequests++
		}
	}
	data := WalletAdminPageData{
		Withdrawals: withdrawals, Transfers: transfers, PendingDeposits: pendingDeposits,
		Admins:       admins,
		AgedRequests: agedRequests,
		RevenueTotal: revenueTotal, RevenueCount: revenueCount,
		CommissionEnabled: h.wallet.CommissionEnabled(), CommissionLabel: h.wallet.CommissionLabel(),
		Error: r.URL.Query().Get("error"), Success: r.URL.Query().Get("success"),
		AdminID:   farmer.ID,
		AdminName: farmer.FullName,
	}
	if err := render.RenderTemplates(w, "wallet-admin.html", data); err != nil {
		log.Printf("render wallet admin page for admin %d: %v", farmer.ID, err)
		http.Error(w, "Could not render withdrawal requests", http.StatusInternalServerError)
	}
}

// SetWalletAdmin grants or revokes the admin role from the approvals page. The
// role is an account property, so this is the UI side of the change an operator
// would otherwise make with SQL; the first admin still comes from configuration,
// and after that the page manages who else can see it.
func (h *WalletHandler) SetWalletAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	h.refreshAdmins(r.Context())
	if !ok || farmer == nil || !h.wallet.IsAdmin(farmer.ID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	var changeErr error
	success := ""
	switch r.FormValue("action") {
	case "remove":
		id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("farmer_id")))
		if err != nil || id <= 0 {
			changeErr = services.ErrAdminNotFound
			break
		}
		changeErr = h.wallet.SetAdmin(r.Context(), farmer.ID, id, false)
		success = "Admin access removed."
	default:
		changeErr = h.wallet.SetAdminByPhone(r.Context(), farmer.ID, r.FormValue("phone"), true)
		success = "Admin access granted."
	}

	if changeErr != nil {
		log.Printf("admin %d change wallet admins: %v", farmer.ID, changeErr)
		failure := changeErr.Error()
		switch {
		case errors.Is(changeErr, services.ErrNotAdmin),
			errors.Is(changeErr, services.ErrAdminNotFound),
			errors.Is(changeErr, services.ErrAdminPhoneRequired),
			errors.Is(changeErr, services.ErrAdminSelfRemoval),
			errors.Is(changeErr, services.ErrAdminFromConfig):
		default:
			failure = "Could not change admin access. Check the server log and try again."
		}
		http.Redirect(w, r, adminWalletPath+"?error="+url.QueryEscape(failure), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, adminWalletPath+"?success="+url.QueryEscape(success), http.StatusSeeOther)
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

	// failureFor derives the banner from the error itself, for failures the admin
	// has to act on differently. Returning "" falls back to failure, which is
	// deliberately vague about cause and explicit about where the money is.
	failureFor func(error) string

	success string
	run     func(ctx context.Context, id int64, adminID int) error

	// runForm replaces run for an action that needs a second field from the
	// submitted form, which is the Flutterwave transfer id an admin copied out
	// of the provider's dashboard. The form is parsed before either is called.
	runForm func(ctx context.Context, id int64, adminID int, form url.Values) error
}

func (h *WalletHandler) runAdminWalletAction(w http.ResponseWriter, r *http.Request, action adminWalletAction) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	farmer, ok := middleware.FarmerFromContext(r)
	h.refreshAdmins(r.Context())
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
	var runErr error
	if action.runForm != nil {
		runErr = action.runForm(r.Context(), id, farmer.ID, r.PostForm)
	} else {
		runErr = action.run(r.Context(), id, farmer.ID)
	}
	if runErr != nil {
		log.Printf("admin %d %s: %v", farmer.ID, action.idField, runErr)

		failure := action.failure
		if action.failureFor != nil {
			if specific := action.failureFor(runErr); specific != "" {
				failure = specific
			}
		}
		http.Redirect(w, r, action.redirectPath+"?error="+url.QueryEscape(failure), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, action.redirectPath+"?success="+url.QueryEscape(action.success), http.StatusSeeOther)
}

// approvalFailureFor explains the refusals an admin can act on differently.
//
// The generic failure text tells them to check the status and retry, which is
// right for a status race and wrong for these: retrying changes nothing. Each
// one needs its own instruction instead. Returning "" falls back to the generic
// text.
func approvalFailureFor(err error) string {
	switch {
	case errors.Is(err, services.ErrPayoutDetailsMismatch):
		// A refused payout leaves the withdrawal pending, so the funds are still
		// reserved and Reject is what releases them.
		return "Payout blocked: " + err.Error() + "."
	case errors.Is(err, services.ErrSelfApproval):
		return "Not approved: " + err.Error() + "."
	case errors.Is(err, services.ErrPayoutSubmitted):
		// The dangerous one: the row looks like a payout that never started, but
		// money is on its way. Neither Reject nor the way back to the queue is
		// safe, and reconciling needs the transfer id this failure did not save -
		// so the admin has to bring that id back from Flutterwave first.
		return "Do not reject or return this request to the queue. " + err.Error() + ". Find that transfer in Flutterwave, record its id below, then check the provider status to settle or refund it."
	case errors.Is(err, services.ErrTransferNotCreated):
		return "Flutterwave refused the payout and the request is back in the queue. Reject it to release the reserved funds."
	}
	return ""
}

func (h *WalletHandler) ApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not confirm the payout action due to a technical issue or status change. Check the withdrawal status before retrying; funds remain reserved while a payout is processing.",
		failureFor:   approvalFailureFor,
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

// ReturnWithdrawalToQueue puts a payout that never reached Flutterwave back in
// the approval queue, so it can be rejected and the reserved funds released.
func (h *WalletHandler) ReturnWithdrawalToQueue(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not return the request to the queue. It has a saved Flutterwave transfer, so a payout may be on its way - check the provider before doing anything else. Funds remain reserved.",
		success:      "Request returned to the queue. Reject it to release the reserved funds.",
		run: func(ctx context.Context, id int64, adminID int) error {
			return h.wallet.ReturnWithdrawalToQueue(ctx, id, adminID)
		},
	})
}

// RecordTransferID takes the Flutterwave transfer id an admin found in the
// provider's dashboard and stores it against a payout that never recorded its
// own, so that payout can then be reconciled.
func (h *WalletHandler) RecordTransferID(w http.ResponseWriter, r *http.Request) {
	h.runAdminWalletAction(w, r, adminWalletAction{
		idField:      "withdrawal_id",
		redirectPath: adminWalletPath,
		failure:      "Could not record the Flutterwave transfer id. Check the number against the provider's dashboard and try again; funds remain reserved.",
		success:      "Flutterwave transfer recorded. Check the provider status to settle or refund this payout.",
		runForm: func(ctx context.Context, id int64, adminID int, form url.Values) error {
			return h.wallet.RecordProviderTransferID(ctx, id, adminID, form.Get("provider_transfer_id"))
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
		failureFor:   approvalFailureFor,
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
