package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"backend/internal/models"
	"backend/internal/repository"
)

type WalletService struct {
	repo     *repository.WalletRepository
	farmers  *repository.FarmerRepository
	flutter  *FlutterwaveClient
	adminIDs map[int]struct{}

	// commissionBasisPoints is the platform's cut of marketplace sales, in
	// hundredths of a percent: 2.5% is 250. Zero means no commission.
	commissionBasisPoints int64
}

func NewWalletService(repo *repository.WalletRepository, farmers *repository.FarmerRepository, flutter *FlutterwaveClient, adminIDs []int, commissionBasisPoints int64) *WalletService {
	adminSet := make(map[int]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		if id > 0 {
			adminSet[id] = struct{}{}
		}
	}
	return &WalletService{
		repo:                  repo,
		farmers:               farmers,
		flutter:               flutter,
		adminIDs:              adminSet,
		commissionBasisPoints: commissionBasisPoints,
	}
}

// ParseCommissionPercent converts a percentage such as "2.5" into basis points
// (250), the integer form the money arithmetic uses. An empty value means no
// commission, so an unset COMMISSION_PERCENT changes nothing.
//
// A rate of 100% or more is rejected: it would leave the seller with nothing,
// and the ledger can't record a zero-amount sale credit.
func ParseCommissionPercent(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: enter a percentage such as 2.5", value)
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: enter a percentage such as 2.5", value)
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: enter a percentage such as 2.5", value)
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: enter a percentage such as 2.5", value)
	}
	// Only two decimal places are meaningful — the smallest unit is 0.01%.
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: use at most two decimal places", value)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	// A "." with no digits at all, e.g. "2.", is still a valid whole number.
	centsPart := int64(0)
	if fraction != "" {
		centsPart, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: enter a percentage such as 2.5", value)
		}
	}
	basisPoints := whole*100 + centsPart
	if basisPoints >= 10000 {
		return 0, fmt.Errorf("invalid COMMISSION_PERCENT %q: commission must be below 100%%", value)
	}
	return basisPoints, nil
}

func ParseWalletAdminIDs(value string) ([]int, error) {
	seen := make(map[int]struct{})
	var ids []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid user ID in WALLET_ADMIN_USER_IDS: %q", part)
		}
		if _, exists := seen[id]; !exists {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	return ids, nil
}

func (s *WalletService) IsAdmin(farmerID int) bool {
	_, ok := s.adminIDs[farmerID]
	return ok
}

func (s *WalletService) Wallet(ctx context.Context, farmerID int) (*models.Wallet, []models.WalletTransaction, error) {
	wallet, err := s.repo.GetWallet(ctx, farmerID)
	if err != nil {
		return nil, nil, err
	}
	transactions, err := s.repo.ListTransactions(ctx, farmerID)
	if err != nil {
		return nil, nil, err
	}
	return wallet, transactions, nil
}

func (s *WalletService) Withdrawals(ctx context.Context, farmerID int) ([]models.Withdrawal, error) {
	return s.repo.ListFarmerWithdrawals(ctx, farmerID)
}

func (s *WalletService) Deposits(ctx context.Context, farmerID int) ([]models.WalletDeposit, error) {
	return s.repo.ListDeposits(ctx, farmerID)
}

func normalizeWalletAmount(amount string, wholeUnits bool) (string, int64, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return "", 0, errors.New("amount is required")
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && (len(parts[1]) == 0 || len(parts[1]) > 2) {
		return "", 0, errors.New("enter a valid positive amount with up to two decimal places")
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return "", 0, errors.New("enter a valid positive amount")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 9999999999 {
		return "", 0, errors.New("amount exceeds wallet limits")
	}
	cents := int64(0)
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return "", 0, errors.New("enter a valid positive amount")
		}
	}
	if wholeUnits && cents != 0 {
		return "", 0, errors.New("NGN withdrawals must be a whole naira amount")
	}
	totalCents := whole*100 + cents
	if totalCents <= 0 {
		return "", 0, errors.New("enter a valid positive amount")
	}
	return fmt.Sprintf("%d.%02d", whole, cents), whole, nil
}

// amountToCents converts an already-normalized amount ("1234.56") into integer
// kobo.
//
// Money arithmetic is done in integers rather than floats so rounding can never
// invent or lose a kobo — which matters once a single payment has to be split
// between a seller, a platform fee, and the buyer's debit and still add up.
func amountToCents(amount string) (int64, error) {
	amount = strings.TrimSpace(amount)
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("invalid amount")
	}
	// Digits only, so a sign or separator can't sneak through as a negative or
	// partially-parsed amount.
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, errors.New("invalid amount")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid amount")
	}
	cents := int64(0)
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		if len(fraction) != 2 {
			return 0, errors.New("invalid amount")
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, errors.New("invalid amount")
		}
	}
	return whole*100 + cents, nil
}

func walletReference(prefix string, farmerID int) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%s", prefix, farmerID, hex.EncodeToString(random)), nil
}

type DepositResult struct {
	CheckoutURL string
	Reference   string
}

func (s *WalletService) InitiateDeposit(ctx context.Context, farmer *models.Farmer, amount string) (*DepositResult, error) {
	if farmer == nil || farmer.ID <= 0 {
		return nil, errors.New("invalid wallet owner")
	}
	amount, _, err := normalizeWalletAmount(amount, false)
	if err != nil {
		return nil, err
	}
	reference, err := walletReference("WALLET", farmer.ID)
	if err != nil {
		return nil, fmt.Errorf("generate deposit reference: %w", err)
	}
	if err := s.repo.CreateDeposit(ctx, farmer.ID, reference, amount); err != nil {
		return nil, fmt.Errorf("create wallet deposit: %w", err)
	}
	checkoutURL, err := s.flutter.InitializePayment(InitializePaymentRequest{
		Reference:     reference,
		Amount:        amount,
		Currency:      "NGN",
		CustomerEmail: farmer.Email,
		CustomerName:  farmer.FullName,
		Title:         "Agro-Shield Wallet Deposit",
		Description:   "Wallet deposit",
	})
	if err != nil {
		// The provider may have accepted the request even if its response timed out.
		// Keep the attempt pending so a delayed webhook can still reconcile it.
		return nil, fmt.Errorf("initialize wallet deposit: %w", err)
	}
	if strings.TrimSpace(checkoutURL) == "" {
		// A missing response field is not proof the provider did not create the attempt.
		return nil, errors.New("payment provider returned no checkout URL")
	}
	return &DepositResult{CheckoutURL: checkoutURL, Reference: reference}, nil
}

func (s *WalletService) ConfirmDeposit(ctx context.Context, reference, transactionID string, amount float64, currency, status string) error {
	switch strings.ToLower(status) {
	case "failed", "cancelled", "canceled":
		return s.repo.FailDeposit(ctx, reference)
	case "successful":
	default:
		return nil
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return errors.New("invalid verified deposit amount")
	}
	cents := amount * 100
	roundedCents := math.Round(cents)
	if math.Abs(cents-roundedCents) > 0.000001 {
		return errors.New("verified deposit amount has unsupported precision")
	}
	verifiedAmount, _, err := normalizeWalletAmount(strconv.FormatFloat(roundedCents/100, 'f', 2, 64), false)
	if err != nil {
		return fmt.Errorf("invalid verified deposit amount: %w", err)
	}
	if currency != "NGN" {
		return errors.New("wallet deposits must be paid in NGN")
	}
	if transactionID == "" || !strings.HasPrefix(reference, "WALLET-") {
		return errors.New("invalid wallet deposit verification")
	}
	return s.repo.CompleteDeposit(ctx, reference, transactionID, verifiedAmount, currency)
}

// PreflightWithdrawal reports whether a withdrawal request would be accepted,
// without moving any money.
//
// The withdrawal handler calls this before consuming a one-time passcode, so a
// request that was never going to succeed — a typo'd amount, or more than the
// balance holds — doesn't burn the user's code and push them toward the
// issuance rate limit. The real check still happens, atomically, inside
// CreateWithdrawal; this one is advisory.
func (s *WalletService) PreflightWithdrawal(ctx context.Context, farmerID int, amount string) error {
	return s.preflightDebit(ctx, farmerID, amount, true)
}

// PreflightTransfer is the same advisory check for a wallet-to-wallet transfer,
// which unlike a bank payout may be any amount down to a single kobo.
func (s *WalletService) PreflightTransfer(ctx context.Context, senderID int, amount string) error {
	return s.preflightDebit(ctx, senderID, amount, false)
}

func (s *WalletService) preflightDebit(ctx context.Context, farmerID int, amount string, wholeUnits bool) error {
	if farmerID <= 0 {
		return errors.New("invalid wallet owner")
	}
	normalized, _, err := normalizeWalletAmount(amount, wholeUnits)
	if err != nil {
		return err
	}
	wallet, err := s.repo.GetWallet(ctx, farmerID)
	if err != nil {
		return fmt.Errorf("%w: read wallet: %v", ErrWalletUnavailable, err)
	}
	balance, err := amountToCents(wallet.Balance)
	if err != nil {
		return fmt.Errorf("%w: read wallet balance: %v", ErrWalletUnavailable, err)
	}
	requested, err := amountToCents(normalized)
	if err != nil {
		return err
	}
	if balance < requested {
		return errors.New("insufficient wallet balance")
	}
	return nil
}

func (s *WalletService) RequestWithdrawal(ctx context.Context, farmerID int, amount string) error {
	if farmerID <= 0 {
		return errors.New("invalid wallet owner")
	}
	amount, _, err := normalizeWalletAmount(amount, true)
	if err != nil {
		return err
	}
	reference, err := walletReference("WITHDRAW", farmerID)
	if err != nil {
		return fmt.Errorf("generate withdrawal reference: %w", err)
	}
	return s.repo.CreateWithdrawal(ctx, farmerID, reference, amount)
}

// --- wallet-to-wallet transfers -------------------------------------------

// Transfer lookup failures. These are distinct because the two cases want
// different words in the UI: a malformed number is the sender's typo, whereas
// an unknown number means no account matched.
var (
	ErrTransferRecipientInvalid  = errors.New("enter a valid Nigerian phone number for the recipient")
	ErrTransferRecipientNotFound = errors.New("no Agro-Shield account uses that phone number")

	// ErrTransferUnavailable wraps infrastructure failures. Handlers test for
	// it so a database error never reaches the page as raw internal text.
	ErrTransferUnavailable = errors.New("transfer is temporarily unavailable")
)

// ErrWalletUnavailable is the same idea for a plain balance read.
var ErrWalletUnavailable = errors.New("wallet is temporarily unavailable")

// recipientPhoneForms lists the ways one Nigerian number could have been stored
// on an account.
//
// Signup saves the phone exactly as typed, so the same person may be recorded
// as "+2348012345678", "08012345678" or "8012345678". All of those have to
// resolve to the same account or a transfer would fail for a recipient who is
// plainly registered. Order is most-canonical first so the international form
// wins if it exists.
func recipientPhoneForms(phone string) []string {
	normalized := NormalizeNigerianPhone(phone)
	if normalized == "" {
		return nil
	}
	subscriber := normalized[len("234"):]
	return []string{normalized, "0" + subscriber, subscriber}
}

// ResolveTransferRecipient turns the phone number a sender typed into the
// account that will be credited.
//
// This necessarily reveals whether a number is registered, which a
// deliberately vague error could hide — but a transfer feature where the sender
// cannot tell a typo from an unknown person is unusable, and no money moves
// without a one-time passcode and an admin approval. The lookup is therefore
// reachable only from an authenticated session.
func (s *WalletService) ResolveTransferRecipient(ctx context.Context, senderID int, phone string) (*models.Farmer, error) {
	if senderID <= 0 {
		return nil, errors.New("invalid wallet owner")
	}
	forms := recipientPhoneForms(phone)
	if len(forms) == 0 {
		return nil, ErrTransferRecipientInvalid
	}
	for _, form := range forms {
		recipient, err := s.farmers.GetByPhone(form)
		if err != nil {
			return nil, fmt.Errorf("%w: look up transfer recipient: %v", ErrTransferUnavailable, err)
		}
		if recipient == nil {
			continue
		}
		if recipient.ID == senderID {
			return nil, errors.New("you cannot transfer to your own wallet")
		}
		return recipient, nil
	}
	return nil, ErrTransferRecipientNotFound
}

// normalizeTransferNote trims and bounds the free-text note. The note is shown
// in both parties' histories, so control characters are dropped rather than
// stored.
func normalizeTransferNote(note string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, note)
	cleaned = strings.TrimSpace(cleaned)
	if len([]rune(cleaned)) > 140 {
		return "", errors.New("keep the transfer note under 140 characters")
	}
	return cleaned, nil
}

// RequestTransfer reserves amount from the sender and records a transfer to
// recipientID for admin approval.
//
// The recipient is passed as an already-resolved account ID so the handler can
// validate it — and let the user fix a mistyped number — before a passcode is
// spent.
func (s *WalletService) RequestTransfer(ctx context.Context, senderID, recipientID int, amount, note string) error {
	if senderID <= 0 || recipientID <= 0 {
		return errors.New("invalid transfer participants")
	}
	amount, _, err := normalizeWalletAmount(amount, false)
	if err != nil {
		return err
	}
	note, err = normalizeTransferNote(note)
	if err != nil {
		return err
	}
	reference, err := walletReference("TRANSFER", senderID)
	if err != nil {
		return fmt.Errorf("%w: generate transfer reference: %v", ErrTransferUnavailable, err)
	}
	if err := s.repo.CreateTransfer(ctx, senderID, recipientID, reference, amount, note); err != nil {
		return wrapTransferError(err)
	}
	return nil
}

// wrapTransferError keeps the repository's own user-facing rejections intact
// and marks everything else as an infrastructure failure, so a driver error
// cannot reach the page as raw internal text.
func wrapTransferError(err error) error {
	if errors.Is(err, repository.ErrInsufficientBalance) ||
		errors.Is(err, repository.ErrSelfTransfer) ||
		errors.Is(err, repository.ErrInvalidTransferParticipants) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrTransferUnavailable, err)
}

func (s *WalletService) Transfers(ctx context.Context, farmerID int) ([]models.Transfer, error) {
	return s.repo.ListFarmerTransfers(ctx, farmerID)
}

func (s *WalletService) AdminTransfers(ctx context.Context) ([]models.Transfer, error) {
	return s.repo.ListPendingTransfers(ctx)
}

// ApproveTransfer credits the recipient. It returns the settled transfer so the
// caller can notify them.
func (s *WalletService) ApproveTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error) {
	if !s.IsAdmin(adminID) {
		return nil, errors.New("admin access required")
	}
	return s.repo.ApproveTransfer(ctx, transferID, adminID)
}

// RejectTransfer returns the reserved funds to the sender.
func (s *WalletService) RejectTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error) {
	if !s.IsAdmin(adminID) {
		return nil, errors.New("admin access required")
	}
	return s.repo.RejectTransfer(ctx, transferID, adminID)
}

// PayForOrder settles a marketplace purchase. The commission rate comes from
// this service rather than the caller, so the two purchase entry points need no
// knowledge of it.
func (s *WalletService) PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64) (int64, error) {
	return s.repo.PayForOrder(ctx, buyerID, cropID, quantity, s.commissionBasisPoints)
}

// CommissionBasisPoints exposes the configured rate for display.
func (s *WalletService) CommissionBasisPoints() int64 {
	return s.commissionBasisPoints
}

// CommissionEnabled reports whether a commission is actually being taken.
func (s *WalletService) CommissionEnabled() bool {
	return s.commissionBasisPoints > 0
}

// CommissionLabel renders the configured rate as a percentage, e.g. "2.50".
//
// It formats through the repository's formatter rather than repeating the
// format, so the rate an admin is shown is produced by the same code that
// writes the rate the ledger records.
func (s *WalletService) CommissionLabel() string {
	return repository.FormatBasisPoints(s.commissionBasisPoints)
}

// PlatformRevenue returns the commission collected so far and the number of
// sales it came from.
func (s *WalletService) PlatformRevenue(ctx context.Context) (string, int, error) {
	return s.repo.PlatformRevenueTotal(ctx)
}

func (s *WalletService) UpdateBankDetails(farmerID int, bankName, bankCode, accountName, accountNumber string) error {
	bankName = strings.TrimSpace(bankName)
	bankCode = strings.TrimSpace(bankCode)
	accountName = strings.TrimSpace(accountName)
	accountNumber = strings.TrimSpace(accountNumber)
	if bankName == "" || bankCode == "" || accountName == "" {
		return errors.New("bank name, bank code, and account name are required")
	}
	for _, digit := range bankCode {
		if digit < '0' || digit > '9' {
			return errors.New("use the Flutterwave bank code for the selected bank")
		}
	}
	if len(accountNumber) != 10 {
		return errors.New("Nigerian bank account numbers must contain 10 digits")
	}
	for _, digit := range accountNumber {
		if digit < '0' || digit > '9' {
			return errors.New("Nigerian bank account numbers must contain 10 digits")
		}
	}
	return s.farmers.UpdateBankDetails(farmerID, bankName, bankCode, accountName, accountNumber)
}

func (s *WalletService) Banks() ([]Bank, error) {
	return s.flutter.ListBanks("NG")
}

func (s *WalletService) AdminWithdrawals(ctx context.Context) ([]models.Withdrawal, error) {
	return s.repo.ListWithdrawals(ctx, true)
}

func (s *WalletService) ApproveWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	withdrawal, err := s.repo.BeginWithdrawal(ctx, withdrawalID, adminID)
	if err != nil {
		return err
	}
	_, transferAmount, err := normalizeWalletAmount(withdrawal.Amount, true)
	if err != nil {
		return fmt.Errorf("invalid stored withdrawal amount: %w", err)
	}
	transferID, err := s.flutter.CreateTransfer(CreateTransferRequest{
		BankCode:      withdrawal.BankCode,
		AccountNumber: withdrawal.AccountNumber,
		AccountName:   withdrawal.AccountName,
		Amount:        transferAmount,
		Currency:      withdrawal.Currency,
		Reference:     withdrawal.Reference,
	})
	if err != nil {
		return fmt.Errorf("start payout; withdrawal remains reserved for reconciliation: %w", err)
	}
	if err := s.repo.SetTransferID(ctx, withdrawal.Reference, transferID); err != nil {
		return fmt.Errorf("save payout reference %s for reconciliation: %w", withdrawal.Reference, err)
	}
	verified, err := s.flutter.VerifyTransfer(transferID)
	if err != nil {
		return fmt.Errorf("payout submitted to Flutterwave but confirmation is delayed; funds remain reserved: %w", err)
	}
	if verified.ID != transferID || verified.Reference != withdrawal.Reference || verified.Amount != transferAmount || verified.Currency != withdrawal.Currency {
		return errors.New("payout submitted but provider confirmation did not match; funds remain reserved")
	}
	if verified.Status == "SUCCESSFUL" || verified.Status == "FAILED" || verified.Status == "REVERSED" {
		return s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency)
	}
	return nil
}

func (s *WalletService) RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	return s.repo.RejectWithdrawal(ctx, withdrawalID, adminID)
}

func (s *WalletService) ReconcileWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (string, error) {
	if !s.IsAdmin(adminID) {
		return "", errors.New("admin access required")
	}
	withdrawal, err := s.repo.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return "", err
	}
	if withdrawal.Status != "processing" {
		return "", errors.New("only processing withdrawals can be reconciled")
	}
	if !withdrawal.TransferID.Valid {
		return "", fmt.Errorf("no provider transfer ID is saved; check Flutterwave using reference %s and do not approve again", withdrawal.Reference)
	}
	verified, err := s.flutter.VerifyTransfer(withdrawal.TransferID.String)
	if err != nil {
		return "", fmt.Errorf("verify provider transfer: %w", err)
	}
	_, amount, err := normalizeWalletAmount(withdrawal.Amount, true)
	if err != nil {
		return "", fmt.Errorf("invalid stored withdrawal amount: %w", err)
	}
	if verified.ID != withdrawal.TransferID.String || verified.Reference != withdrawal.Reference ||
		verified.Amount != amount || verified.Currency != withdrawal.Currency {
		return "", errors.New("provider transfer details do not match; funds remain reserved")
	}
	switch verified.Status {
	case "SUCCESSFUL", "FAILED", "REVERSED":
		if err := s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
			return "", err
		}
	}
	return verified.Status, nil
}

func (s *WalletService) ProcessTransferWebhook(ctx context.Context, body []byte, receivedHash string) (bool, error) {
	if !s.flutter.VerifyWebhookSignature(receivedHash) {
		return false, ErrUnauthorizedWebhook
	}
	var payload struct {
		Event string `json:"event"`
		Data  struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Ref    string `json:"reference"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("invalid transfer webhook payload: %w", err)
	}
	if payload.Data.ID <= 0 || !strings.HasPrefix(payload.Data.Ref, "WITHDRAW-") {
		return false, errors.New("invalid transfer webhook identifiers")
	}
	verified, err := s.flutter.VerifyTransfer(strconv.FormatInt(payload.Data.ID, 10))
	if err != nil {
		return false, fmt.Errorf("verify transfer: %w", err)
	}
	if verified.ID != strconv.FormatInt(payload.Data.ID, 10) || verified.Reference != payload.Data.Ref || verified.Currency != "NGN" {
		return false, errors.New("transfer verification does not match webhook")
	}
	if verified.Status != "SUCCESSFUL" && verified.Status != "FAILED" && verified.Status != "REVERSED" {
		return false, nil
	}
	if err := s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
		return false, err
	}
	return true, nil
}
