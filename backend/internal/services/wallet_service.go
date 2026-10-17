package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"backend/internal/models"
	"backend/internal/repository"
)

type WalletService struct {
	repo    walletStore
	farmers farmerStore
	lookups bankLookupStore
	flutter *FlutterwaveClient

	adminIDs map[int]struct{}

	// resolver is the account-name half of the Flutterwave client, held as a
	// narrow interface so the bank fan-out can be tested without HTTP. It is
	// the same underlying client as flutter.
	resolver accountResolver

	// directory is the bank-list half of the same client, for checking a stored
	// bank name against its code. Also the same underlying client.
	directory bankDirectory

	// commissionBasisPoints is the platform's cut of marketplace sales, in
	// hundredths of a percent: 2.5% is 250. Zero means no commission.
	commissionBasisPoints int64

	// notify is the notification store, held as a narrow interface so the wallet
	// does not depend on the whole notification stack - and so a wallet test can
	// assert on what a farmer was told without one. Nil means notifications are
	// switched off, which every caller tolerates: telling somebody about their
	// money must never be what stops the money moving.
	notify notifier
}

// notifier is the one method the wallet needs from NotificationService.
type notifier interface {
	CreateNotification(userID int, title, message, notificationType string) error
}

func NewWalletService(repo walletStore, farmers farmerStore, flutter *FlutterwaveClient, lookups *repository.BankLookupRepository, adminIDs []int, commissionBasisPoints int64, notify notifier) *WalletService {
	adminSet := make(map[int]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		if id > 0 {
			adminSet[id] = struct{}{}
		}
	}
	return &WalletService{
		repo:                  repo,
		farmers:               farmers,
		lookups:               lookups,
		flutter:               flutter,
		resolver:              flutter,
		directory:             flutter,
		adminIDs:              adminSet,
		commissionBasisPoints: commissionBasisPoints,
		notify:                notify,
	}
}

// AdminIDs lists the configured wallet admins, so a new request can be put in
// front of the people who have to act on it. Sorted so the order a notification
// fan-out happens in is not left to Go's map iteration.
func (s *WalletService) AdminIDs() []int {
	ids := make([]int, 0, len(s.adminIDs))
	for id := range s.adminIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// notifyUser delivers one notification, best-effort.
//
// Every caller has already moved money or recorded a decision, so a failure to
// tell somebody about it is logged and swallowed: an admin's payout must not be
// reported as failed because a notification row could not be written.
func (s *WalletService) notifyUser(userID int, title, message, kind string) {
	if s.notify == nil || userID <= 0 {
		return
	}
	if err := s.notify.CreateNotification(userID, title, message, kind); err != nil {
		log.Printf("notify user %d (%s): %v", userID, kind, err)
	}
}

// notifyAdmins tells every configured admin that a request is waiting for them.
//
// Without this a request is only found by somebody opening the approvals page,
// which is how a payout could sit untouched for days with the farmer's balance
// already reduced.
func (s *WalletService) notifyAdmins(title, message, kind string) {
	s.notifyAdminsExcept(0, title, message, kind)
}

// notifyAdminsExcept skips one admin, the one whose own request this is.
//
// An admin who is also a member can request a payout, and the self-approval rule
// means they cannot approve it - so "waiting for you" would be false for exactly
// the person most likely to open the page and act on it.
func (s *WalletService) notifyAdminsExcept(requesterID int, title, message, kind string) {
	for _, adminID := range s.AdminIDs() {
		if adminID == requesterID {
			continue
		}
		s.notifyUser(adminID, title, message, kind)
	}
}

// accountResolver resolves an account number at one specific bank. Flutterwave
// exposes no reverse lookup, so callers wishing to find which bank a number
// belongs to must ask this repeatedly, once per candidate bank.
type accountResolver interface {
	ResolveAccount(accountNumber, bankCode string) (*ResolvedAccount, error)
}

// bankDirectory is the bank-list half of the Flutterwave client. Verification
// needs it to turn a stored bank code into the name the provider uses for that
// code, which is the only way to check a bank name against its code - resolving
// an account tells us the holder, never the bank.
type bankDirectory interface {
	ListBanks(country string) ([]Bank, error)
}

// bankLookupStore is the persistence the bank fan-out needs: a cache of which
// banks have answered for a number, plus the negative rows that stop a number
// resolving nowhere from being swept again on every attempt.
type bankLookupStore interface {
	Find(ctx context.Context, accountNumber string) ([]repository.BankLookup, error)
	HasAny(ctx context.Context, accountNumber string) (bool, error)
	FindOne(ctx context.Context, accountNumber, bankCode string) (*repository.BankLookup, error)
	SaveMatches(ctx context.Context, accountNumber string, matches []repository.BankLookup) error
	SaveMiss(ctx context.Context, accountNumber string) error
}

// walletStore is everything this service asks of the wallet repository.
//
// It is an interface rather than the repository itself so the money paths can be
// exercised without a database. A withdrawal is read, verified, and only then
// begun - and that order is the whole reason a payout with details that do not
// match never reserves funds, which is not something a test can assert while the
// repository is a struct bound to a live connection.
type walletStore interface {
	GetWallet(ctx context.Context, farmerID int) (*models.Wallet, error)
	CreateDeposit(ctx context.Context, farmerID int, reference, amount string) error
	CompleteDeposit(ctx context.Context, reference, transactionID, amount, currency string) error
	FailDeposit(ctx context.Context, reference string) error
	ListDeposits(ctx context.Context, farmerID int) ([]models.WalletDeposit, error)

	CreateWithdrawal(ctx context.Context, farmerID int, reference, amount string) error
	ListFarmerWithdrawals(ctx context.Context, farmerID int) ([]models.Withdrawal, error)
	ListWithdrawals(ctx context.Context, pendingOnly bool) ([]models.Withdrawal, error)
	GetWithdrawal(ctx context.Context, withdrawalID int64) (*models.Withdrawal, error)
	BeginWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (*models.Withdrawal, error)
	RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error
	SetTransferID(ctx context.Context, reference, transferID string) error
	CompleteTransfer(ctx context.Context, reference, transferID, status string, amount int64, currency string) (bool, error)
	GetWithdrawalByReference(ctx context.Context, reference string) (*models.Withdrawal, error)
	RecordWithdrawalVerification(ctx context.Context, reference, status, resolvedName, detail string) error
	RevertWithdrawalToPending(ctx context.Context, reference string) error

	CreateTransfer(ctx context.Context, senderID, recipientID int, reference, amount, note string) error
	ListFarmerTransfers(ctx context.Context, farmerID int) ([]models.Transfer, error)
	ListPendingTransfers(ctx context.Context) ([]models.Transfer, error)
	ApproveTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error)
	RejectTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error)

	ListTransactions(ctx context.Context, farmerID int) ([]models.WalletTransaction, error)
	PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64, commissionBasisPoints int64) (int64, error)
	PlatformRevenueTotal(ctx context.Context) (string, int, error)
}

// farmerStore is the account half of the farmer repository: reading where a
// payout would go, and writing the details a farmer saves. Narrow for the same
// reason as walletStore - a withdrawal requested against details that do not
// match must not reach the repository at all, and that is only provable with a
// stand-in for the repository.
type farmerStore interface {
	GetByID(id int) (*models.Farmer, error)
	GetByPhone(phone string) (*models.Farmer, error)
	UpdateBankDetails(id int, bankName, bankCode, accountName, accountNumber string) error
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

	// The payout details are checked before CreateWithdrawal, not after. That
	// call snapshots these same fields onto the withdrawal and holds the funds
	// in one transaction, so a mismatch found afterwards would leave money
	// reserved against an account that cannot receive it.
	farmer, err := s.farmers.GetByID(farmerID)
	if err != nil {
		return fmt.Errorf("%w: read payout details: %v", ErrWalletUnavailable, err)
	}
	if farmer == nil {
		return errors.New("invalid wallet owner")
	}

	// Nothing saved yet is not a mismatch. Let CreateWithdrawal say so in its
	// own words, which tell the farmer to save details rather than that theirs
	// are wrong.
	if !farmerHasPayoutDetails(farmer) {
		return s.repo.CreateWithdrawal(ctx, farmerID, reference, amount)
	}

	verdict := s.VerifyPayoutAccount(ctx, farmer.BankName, farmer.BankCode, farmer.AccountName, farmer.AccountNumber)
	if verdict.Status == PayoutMismatch {
		return fmt.Errorf("%w: %s", ErrPayoutDetailsMismatch, verdict.Detail)
	}

	if err := s.repo.CreateWithdrawal(ctx, farmerID, reference, amount); err != nil {
		return err
	}
	s.recordVerification(ctx, reference, verdict)
	// The requester is skipped when they are themselves an admin: the
	// self-approval rule stops them acting on it, so it is not waiting for them.
	s.notifyAdminsExcept(farmerID, "Withdrawal waiting for you",
		fmt.Sprintf("%s (#%d) asked to withdraw %s to %s. Approve or reject it on the wallet approvals page.",
			farmer.FullName, farmerID, amount, farmer.BankName),
		"wallet_withdrawal_request")
	return nil
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

// ErrPayoutDetailsMismatch means a payout was refused because its bank details
// do not describe one account: the number does not exist at that bank, or the
// account name is not the one the bank has registered there.
//
// It is deliberately not raised when verification merely could not run - see
// PayoutUnverified. A dead provider or a bad API key must never read as "your
// details are wrong", or a credential problem would look like every farmer
// having mistyped their account.
var ErrPayoutDetailsMismatch = errors.New("your bank details do not match the account at that bank")

// ErrSelfApproval means an admin tried to approve a request they made
// themselves.
//
// A payout is reviewed so that someone other than the person being paid looks at
// where the money is going. An admin who can wave their own request through is
// the one case where that review does nothing, so the approval paths refuse it
// and the page says another admin has to look instead.
var ErrSelfApproval = errors.New("you cannot approve your own request, so another admin must review it")

// ErrPayoutSubmitted means the payout reached Flutterwave but recording its
// transfer id here failed.
//
// It exists to stop a specific mistake. The row is left 'processing' with no
// saved transfer id, which is exactly the shape a payout that never started
// has - so without this the admin would be told to return it to the queue,
// reject it, and refund a farmer the provider is also paying. The wrapped error
// carries the transfer id, which is the only record of it.
var ErrPayoutSubmitted = errors.New("a payout was submitted to Flutterwave, but saving its reference failed")

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

	// Told, rather than left to be found on the approvals page. The sender's
	// balance is already reduced by this point, so a transfer nobody opens holds
	// their money for as long as nobody opens it. The sender's name is a nicety,
	// so a read that fails costs the name and not the notification.
	who := fmt.Sprintf("User #%d", senderID)
	if sender, lookupErr := s.farmers.GetByID(senderID); lookupErr == nil && sender != nil && sender.FullName != "" {
		who = fmt.Sprintf("%s (#%d)", sender.FullName, senderID)
	}
	// Skipped for the same reason as a withdrawal: an admin who sent this cannot
	// approve it, so it is not waiting for them.
	s.notifyAdminsExcept(senderID, "Transfer waiting for you",
		fmt.Sprintf("%s sent ₦%s to user #%d. Approve or reject it on the wallet approvals page.", who, amount, recipientID),
		"wallet_transfer_request")
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
	settled, err := s.repo.ApproveTransfer(ctx, transferID, adminID)
	if errors.Is(err, repository.ErrSelfApproval) {
		// Translated here so handlers depend on this package alone and both
		// approval paths report the refusal in the same words. The rule itself
		// lives in the store, where the sender's id is already locked.
		return nil, ErrSelfApproval
	}
	if err != nil {
		return nil, err
	}
	return settled, nil
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

// validateAccountNumber enforces the NUBAN shape. Shared by the save path and
// the lookup path so the two cannot drift into accepting different numbers.
func validateAccountNumber(accountNumber string) error {
	if len(accountNumber) != 10 {
		return errors.New("Nigerian bank account numbers must contain 10 digits")
	}
	for _, digit := range accountNumber {
		if digit < '0' || digit > '9' {
			return errors.New("Nigerian bank account numbers must contain 10 digits")
		}
	}
	return nil
}

// validateBankCode enforces that we hold a Flutterwave bank code rather than
// the human-readable bank name a user might have pasted in.
func validateBankCode(bankCode string) error {
	if bankCode == "" {
		return errors.New("bank code is required")
	}
	for _, digit := range bankCode {
		if digit < '0' || digit > '9' {
			return errors.New("use the Flutterwave bank code for the selected bank")
		}
	}
	return nil
}

func (s *WalletService) UpdateBankDetails(ctx context.Context, farmerID int, bankName, bankCode, accountName, accountNumber string) error {
	bankName = strings.TrimSpace(bankName)
	bankCode = strings.TrimSpace(bankCode)
	accountName = strings.TrimSpace(accountName)
	accountNumber = strings.TrimSpace(accountNumber)
	if bankName == "" || accountName == "" {
		return errors.New("bank name and account name are required")
	}
	if err := validateBankCode(bankCode); err != nil {
		return err
	}
	if err := validateAccountNumber(accountNumber); err != nil {
		return err
	}

	// If we already know whose name is on this account at this bank, the
	// submitted name has to match it. This is what stops a user overriding the
	// verified name with one of their choosing - the name saved here is what an
	// admin reads when approving a payout.
	//
	// A miss (or a provider that has never been reached for this pair) is not a
	// mismatch: the provider being down must never lock a farmer out of
	// correcting their own payout details.
	if cached, err := s.lookups.FindOne(ctx, accountNumber, bankCode); err != nil {
		if !errors.Is(err, repository.ErrNoBankLookup) {
			return fmt.Errorf("check saved account name: %w", err)
		}
	} else if !accountNamesMatch(cached.AccountName, accountName) {
		return fmt.Errorf("the name registered on this account is %q, which does not match the name entered", cached.AccountName)
	}

	return s.farmers.UpdateBankDetails(farmerID, bankName, bankCode, accountName, accountNumber)
}

// accountNamesMatch compares two account names loosely. Banks return names
// with inconsistent spacing and casing, and a user retyping a verified name
// will rarely reproduce that exactly, so an exact compare would reject honest
// saves. Case and internal whitespace are ignored; the letters must still be
// the same and in the same order.
func accountNamesMatch(a, b string) bool {
	return normalizeAccountName(a) == normalizeAccountName(b)
}

func normalizeAccountName(name string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(name))), " ")
}

// BankMatch is one bank that answered for an account number, along with the
// name registered on that account.
type BankMatch struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	AccountName string `json:"account_name"`
}

// bankLookupShortlist is the set of banks a lookup fans out over, in
// market-share order so the most likely match is probed first and a truncated
// sweep degrades to the banks most users actually use.
//
// A shortlist rather than the full bank directory is a deliberate trade: the
// full directory is 200+ banks, and probing all of it per lookup would burn the
// provider's rate limit long before we had thousands of users. Coverage comes
// from the cache (each number is swept once, ever) plus the manual bank search
// in the payout form, which is the fallback whenever a number resolves nowhere.
//
// Tune this from real lookup data: a bank that never answers costs a request on
// every uncached number, and a bank that is missing costs its users the
// verified-name flow.
var bankLookupShortlist = []Bank{
	{Name: "Access Bank", Code: "044"},
	{Name: "Guaranty Trust Bank", Code: "058"},
	{Name: "First Bank of Nigeria", Code: "011"},
	{Name: "United Bank for Africa", Code: "033"},
	{Name: "Zenith Bank", Code: "057"},
	{Name: "Fidelity Bank", Code: "070"},
	{Name: "Union Bank of Nigeria", Code: "032"},
	{Name: "Sterling Bank", Code: "232"},
	{Name: "Wema Bank", Code: "035"},
	{Name: "Polaris Bank", Code: "076"},
	{Name: "Stanbic IBTC Bank", Code: "221"},
	{Name: "Ecobank Nigeria", Code: "050"},
	{Name: "Keystone Bank", Code: "082"},
	{Name: "Providus Bank", Code: "101"},
	{Name: "Kuda Microfinance Bank", Code: "50211"},
	{Name: "Opay Digital Services", Code: "999992"},
	{Name: "PalmPay", Code: "999991"},
	{Name: "Moniepoint MFB", Code: "50515"},
	{Name: "9 Payment Service Bank", Code: "120001"},
	{Name: "Sparkle Microfinance Bank", Code: "51310"},
}

// bankLookupConcurrency caps how many banks are probed at once. Each probe is a
// separate HTTP request to Flutterwave, so this is the knob that decides how
// hard one user's lookup pushes on the provider's rate limit.
const bankLookupConcurrency = 6

// LookupAccountBanks finds which banks an account number belongs to and returns
// the registered holder's name for each.
//
// A NUBAN is unique only within a bank, not across banks, so more than one
// bank can legitimately answer and the caller must let the user choose. An
// empty result is a normal outcome, not an error: the number may not exist, or
// it may be at a bank outside the shortlist.
//
// A number is swept at most once. The first lookup probes the shortlist and
// stores what answered; every later lookup for that number - by any user - is
// served from the database without touching the provider. That is what keeps
// this affordable at scale.
func (s *WalletService) LookupAccountBanks(ctx context.Context, accountNumber string) ([]BankMatch, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	if err := validateAccountNumber(accountNumber); err != nil {
		return nil, err
	}

	cached, err := s.lookups.Find(ctx, accountNumber)
	if err != nil {
		return nil, fmt.Errorf("read cached bank lookup: %w", err)
	}
	if len(cached) > 0 {
		return bankMatchesFromLookups(cached), nil
	}
	// A recorded miss is part of the cache too: it means we already asked every
	// bank on the shortlist and none claimed this number. Re-asking would spend
	// a full sweep to reproduce a known answer.
	if swept, err := s.lookups.HasAny(ctx, accountNumber); err != nil {
		return nil, fmt.Errorf("read cached bank lookup: %w", err)
	} else if swept {
		return nil, nil
	}

	matches, complete := s.sweepBanks(ctx, accountNumber)
	if len(matches) > 0 {
		lookups := make([]repository.BankLookup, 0, len(matches))
		for _, match := range matches {
			lookups = append(lookups, repository.BankLookup{
				AccountNumber: accountNumber,
				BankCode:      match.Code,
				BankName:      match.Name,
				AccountName:   match.AccountName,
			})
		}
		if err := s.lookups.SaveMatches(ctx, accountNumber, lookups); err != nil {
			// The answer we have is still good, so a failed write costs the next
			// lookup a repeat sweep rather than failing this one.
			log.Printf("cache bank lookup for %s: %v", maskAccountNumber(accountNumber), err)
		}
		return matches, nil
	}

	// Nothing answered. Record the miss so a typo is not re-swept on every
	// attempt - but only when the sweep genuinely asked every bank.
	//
	// A cancelled request probes only part of the shortlist, and a rejected key
	// or a timeout leaves most banks unasked; caching either as a miss would tell
	// every later lookup that a real account does not exist. That is not
	// hypothetical - a dead API key once poisoned this table for every number
	// anyone tried while it was broken.
	if complete && ctx.Err() == nil {
		if err := s.lookups.SaveMiss(ctx, accountNumber); err != nil {
			log.Printf("cache empty bank lookup for %s: %v", maskAccountNumber(accountNumber), err)
		}
	}
	return nil, nil
}

// sweepBanks probes the shortlist concurrently and returns every bank that
// resolved. Results come back in shortlist order, so the most widely used bank
// is listed first.
//
// The second return reports whether every bank on the shortlist actually
// answered the question. False means at least one probe failed for an
// infrastructure reason, so an empty result is not evidence that the number
// exists nowhere - see LookupAccountBanks, which refuses to cache it as a miss.
func (s *WalletService) sweepBanks(ctx context.Context, accountNumber string) ([]BankMatch, bool) {
	type outcome struct {
		index  int
		match  *BankMatch
		failed bool
	}

	results := make(chan outcome, len(bankLookupShortlist))
	sem := make(chan struct{}, bankLookupConcurrency)

	var wg sync.WaitGroup
	for i, bank := range bankLookupShortlist {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// An explicit check, not just the select below: when a slot is free
			// and ctx is already done, both cases are ready at once and Go picks
			// one at random, so a cancelled request could still fire probes.
			if ctx.Err() != nil {
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			if ctx.Err() != nil {
				return
			}

			resolved, err := s.resolver.ResolveAccount(accountNumber, bank.Code)
			if err != nil {
				if errors.Is(err, ErrAccountNotResolved) {
					// The expected path for every bank that does not hold this
					// number: a real answer, just a negative one.
					return
				}
				// A rejected key, a timeout, a rate limit. We could not ask, so
				// the sweep is incomplete and its silence proves nothing.
				results <- outcome{index: i, failed: true}
				return
			}
			results <- outcome{index: i, match: &BankMatch{
				Name:        bank.Name,
				Code:        bank.Code,
				AccountName: resolved.AccountName,
			}}
		}()
	}

	wg.Wait()
	close(results)

	swept := make([]*BankMatch, len(bankLookupShortlist))
	complete := true
	for result := range results {
		if result.failed {
			complete = false
			continue
		}
		swept[result.index] = result.match
	}

	matches := make([]BankMatch, 0, len(bankLookupShortlist))
	for _, match := range swept {
		if match != nil {
			matches = append(matches, *match)
		}
	}
	return matches, complete
}

func bankMatchesFromLookups(cached []repository.BankLookup) []BankMatch {
	matches := make([]BankMatch, 0, len(cached))
	for _, lookup := range cached {
		matches = append(matches, BankMatch{
			Name:        lookup.BankName,
			Code:        lookup.BankCode,
			AccountName: lookup.AccountName,
		})
	}
	return matches
}

// maskAccountNumber keeps a full account number out of logs. Only the last two
// digits are ever needed to correlate a support report with a record.
func maskAccountNumber(accountNumber string) string {
	if len(accountNumber) <= 2 {
		return strings.Repeat("*", len(accountNumber))
	}
	return strings.Repeat("*", len(accountNumber)-2) + accountNumber[len(accountNumber)-2:]
}

// Payout verification verdicts.
//
// Three states rather than a boolean, because "we could not ask" is a real and
// common outcome - an outage, a rate limit, an expired key - and it has to be
// told apart from "the bank says these details are wrong". Collapsing the two
// either refuses honest payouts every time the provider hiccups, or waves
// through details that nobody ever checked.
//
// The values live on models.Withdrawal, which is where they are stored and
// which the templates render from; these names are the service-side spelling of
// the same three strings, so there is one definition of each value.
const (
	// PayoutVerified: the number exists at that bank and the name registered
	// on it matches the name on the payout.
	PayoutVerified = models.NameCheckVerified

	// PayoutMismatch: the details do not describe one account. No payout should
	// proceed on this verdict.
	PayoutMismatch = models.NameCheckMismatch

	// PayoutUnverified: no answer was obtained. This says nothing about the
	// account, so on its own it is never a reason to refuse - it is a reason to
	// tell a human.
	PayoutUnverified = models.NameCheckUnverified
)

// PayoutVerification is the outcome of checking that a payout's bank name, bank
// code, account number and account name all describe one real account.
type PayoutVerification struct {
	Status string

	// ResolvedName is the holder the bank reported, when it answered. It is what
	// a mismatch quotes and what an admin compares the claimed name against.
	ResolvedName string

	// Detail is a short sentence for the log and the admin page explaining a
	// mismatch or an unverified verdict. It never carries the account number or
	// raw provider text, because it is stored and rendered.
	Detail string
}

// VerifyPayoutAccount checks that a payout's four bank fields describe one real
// account, before money is allowed to move against them.
//
// The claims are checked strongest first: the number and code must be well
// formed, and the provider must confirm that this number exists at this bank and
// carries this account name - the only one of the four checks that can prove the
// details wrong. The bank name is checked against the provider's own list last,
// and a disagreement there is flagged rather than refused; see flagStaleBankName
// for why that one is weaker.
//
// It returns a verdict rather than an error, because "could not verify" is an
// expected outcome that callers must handle differently from a mismatch.
func (s *WalletService) VerifyPayoutAccount(ctx context.Context, bankName, bankCode, accountName, accountNumber string) PayoutVerification {
	bankName = strings.TrimSpace(bankName)
	bankCode = strings.TrimSpace(bankCode)
	accountName = strings.TrimSpace(accountName)
	accountNumber = strings.TrimSpace(accountNumber)

	if err := validateAccountNumber(accountNumber); err != nil {
		return PayoutVerification{Status: PayoutMismatch, Detail: err.Error()}
	}
	if err := validateBankCode(bankCode); err != nil {
		return PayoutVerification{Status: PayoutMismatch, Detail: err.Error()}
	}
	if accountName == "" {
		return PayoutVerification{Status: PayoutMismatch, Detail: "the account name is missing"}
	}

	// The provider's own answer is checked first, because it is the only part of
	// this that can prove the details are wrong, and a verdict of "wrong" must
	// never be masked by the weaker check below.
	resolvedName, verdict, decided := s.resolvePayoutHolder(ctx, accountNumber, bankCode)
	if decided {
		return verdict
	}
	if !accountNamesMatch(resolvedName, accountName) {
		return PayoutVerification{
			Status:       PayoutMismatch,
			ResolvedName: resolvedName,
			Detail:       fmt.Sprintf("the bank has %q registered on this account", resolvedName),
		}
	}

	// The details are real. The bank name is the one remaining claim, and it is
	// only flagged, never refused - see flagStaleBankName for why.
	if flagged, ok := s.flagStaleBankName(bankCode, bankName); ok {
		flagged.ResolvedName = resolvedName
		return flagged
	}

	return PayoutVerification{Status: PayoutVerified, ResolvedName: resolvedName}
}

// flagStaleBankName reports whether the stored bank name is not the name
// Flutterwave uses for the stored code.
//
// It flags rather than refuses, deliberately, and this is the one place the
// check is weaker than it looks like it should be. The reason is that the bank
// directory is not a stable authority: it differs between Flutterwave's test and
// live modes and it changes when a bank rebrands, while the bank name we hold
// was copied from whatever list was current when the farmer saved it. Comparing
// our copy against theirs therefore says as much about the age and the mode of
// our data as about the payout.
//
// Read with the key this project holds, the directory already disagrees with the
// constants below on four names ("Guaranty Trust Bank" against "GTBank Plc",
// "Union Bank of Nigeria" against "Union Bank", "Ecobank Nigeria" against
// "Ecobank Plc", "Providus Bank" against "ProvidusBank PLC") and does not carry
// five of the codes at all. Refusing a payout over that would hold a farmer's
// money because a constant in this file is out of date.
//
// What the transfer actually follows is the code, and the code is checked
// against the provider where it matters: an account that does not exist at that
// code is a refusal, and the transfer itself is verified afterwards. So a stale
// name is surfaced for the admin to read, with the provider's spelling, and the
// payout is allowed to proceed.
//
// The second return reports whether the name was flagged; false means the name
// agrees and verification should carry on.
func (s *WalletService) flagStaleBankName(bankCode, bankName string) (PayoutVerification, bool) {
	directory, err := s.directory.ListBanks("NG")
	if err != nil {
		// The directory comes from the same provider the holder lookup needs, so
		// failing to read it is the same "could not ask". It is also cached in
		// the client for hours, so a failure here means the outage is real.
		log.Printf("verify payout: read bank directory: %v", err)
		return PayoutVerification{
			Status: PayoutUnverified,
			Detail: "the bank list could not be loaded to check the bank name",
		}, true
	}
	if len(directory) == 0 {
		// A success carrying no banks is the provider failing to answer, not a
		// statement that no bank exists. Reading it as one would tell every
		// farmer their bank is unsupported at the same moment.
		log.Printf("verify payout: the bank directory came back empty")
		return PayoutVerification{
			Status: PayoutUnverified,
			Detail: "the bank list came back empty, so the bank name could not be checked",
		}, true
	}

	for _, bank := range directory {
		if bank.Code != bankCode {
			continue
		}
		if accountNamesMatch(bank.Name, bankName) {
			return PayoutVerification{}, false
		}
		return PayoutVerification{
			Status: PayoutUnverified,
			Detail: fmt.Sprintf("the bank name saved for code %s is %q, but the provider calls it %s",
				bankCode, bankName, bank.Name),
		}, true
	}

	// A code the provider does not list. Also a flag rather than a refusal: the
	// directory is mode-dependent, so an absent code may be one this list simply
	// does not carry, and the transfer itself is the authority on whether the
	// code works.
	return PayoutVerification{
		Status: PayoutUnverified,
		Detail: fmt.Sprintf("bank code %s is not in the provider's bank list", bankCode),
	}, true
}

// resolvePayoutHolder returns the name the bank has registered for this number
// at this bank, preferring the cached answer so a payout whose details were
// already confirmed costs no provider call.
//
// The verdict return reports whether it produced a verdict instead of a name.
func (s *WalletService) resolvePayoutHolder(ctx context.Context, accountNumber, bankCode string) (string, PayoutVerification, bool) {
	cached, err := s.lookups.FindOne(ctx, accountNumber, bankCode)
	switch {
	case err == nil:
		return cached.AccountName, PayoutVerification{}, false
	case !errors.Is(err, repository.ErrNoBankLookup):
		log.Printf("verify payout: read cached account name for %s: %v", maskAccountNumber(accountNumber), err)
		return "", PayoutVerification{
			Status: PayoutUnverified,
			Detail: "the saved account name could not be read to check against",
		}, true
	}

	resolved, err := s.resolver.ResolveAccount(accountNumber, bankCode)
	if err != nil {
		if errors.Is(err, ErrAccountNotResolved) {
			// The bank answered, and the answer is that this account is not
			// there. That is a statement about the details, not a failure to
			// check them.
			return "", PayoutVerification{
				Status: PayoutMismatch,
				Detail: "no account with this number exists at that bank",
			}, true
		}
		// A rejected key, a timeout, a rate limit: we could not ask, which says
		// nothing about the account. Reading this as a mismatch would blame the
		// farmer for our own credentials.
		log.Printf("verify payout: resolve %s at bank %s: %v", maskAccountNumber(accountNumber), bankCode, err)
		return "", PayoutVerification{
			Status: PayoutUnverified,
			Detail: "the bank could not be reached to confirm the account name",
		}, true
	}
	return resolved.AccountName, PayoutVerification{}, false
}

// recordVerification stores a verdict against a withdrawal so an admin can see
// which payouts were checked and which were not.
//
// A failed write is logged, not returned: the verdict has already been acted
// on, and losing the record must not fail a payout whose details were fine.
func (s *WalletService) recordVerification(ctx context.Context, reference string, verdict PayoutVerification) {
	if err := s.repo.RecordWithdrawalVerification(ctx, reference, verdict.Status, verdict.ResolvedName, verdict.Detail); err != nil {
		log.Printf("record payout verification for %s: %v", reference, err)
	}
}

// farmerHasPayoutDetails reports whether anything is saved to verify.
func farmerHasPayoutDetails(farmer *models.Farmer) bool {
	return strings.TrimSpace(farmer.BankName) != "" &&
		strings.TrimSpace(farmer.BankCode) != "" &&
		strings.TrimSpace(farmer.AccountName) != "" &&
		strings.TrimSpace(farmer.AccountNumber) != ""
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

	// Read the request without moving it out of 'pending'.
	//
	// Verification must run before BeginWithdrawal. That call reserves the funds
	// and flips the row to 'processing', and RejectWithdrawal only matches
	// 'pending' - so a payout refused after it would sit with the farmer's money
	// reserved and no way to release it. ReconcileWithdrawal is no help either:
	// it needs a provider transfer ID that a refused payout never obtained.
	pending, err := s.repo.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return err
	}
	// Refused before verification, which costs a provider call and a cached
	// lookup that would be spent deciding a request this admin cannot approve
	// either way.
	if pending.FarmerID == adminID {
		return ErrSelfApproval
	}
	verdict := s.VerifyPayoutAccount(ctx, pending.BankName, pending.BankCode, pending.AccountName, pending.AccountNumber)
	if verdict.Status == PayoutMismatch {
		// Recorded before returning so the admin can see why it was refused
		// when they reject it.
		s.recordVerification(ctx, pending.Reference, verdict)
		return fmt.Errorf("%w: %s. Reject this request to release the reserved funds", ErrPayoutDetailsMismatch, verdict.Detail)
	}

	withdrawal, err := s.repo.BeginWithdrawal(ctx, withdrawalID, adminID)
	if err != nil {
		return err
	}
	s.recordVerification(ctx, withdrawal.Reference, verdict)

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
		// A refusal is the provider saying no transfer exists, so the request can
		// go back to the queue and be rejected - which releases the funds that
		// BeginWithdrawal reserved. Every other failure may or may not have
		// created a transfer, so the row stays processing and reserved, where
		// ReconcileWithdrawal and an admin can deal with it.
		if errors.Is(err, ErrTransferNotCreated) {
			if revertErr := s.repo.RevertWithdrawalToPending(ctx, withdrawal.Reference); revertErr != nil {
				return fmt.Errorf("provider refused the payout (%v) and the request could not be returned for rejection: %w", err, revertErr)
			}
			return fmt.Errorf("%w: the request is back in the queue to reject", err)
		}
		return fmt.Errorf("start payout; withdrawal remains reserved for reconciliation: %w", err)
	}
	if err := s.repo.SetTransferID(ctx, withdrawal.Reference, transferID); err != nil {
		// The payout exists at the provider and its id is known only here, so
		// the row must not be returned to the queue: that would let it be
		// rejected and refunded while Flutterwave pays it out. The id goes in
		// the error because this is the only place it survives.
		return fmt.Errorf("%w: transfer %s (withdrawal %s): %v", ErrPayoutSubmitted, transferID, withdrawal.Reference, err)
	}
	verified, err := s.flutter.VerifyTransfer(transferID)
	if err != nil {
		return fmt.Errorf("payout submitted to Flutterwave but confirmation is delayed; funds remain reserved: %w", err)
	}
	if verified.ID != transferID || verified.Reference != withdrawal.Reference || verified.Amount != transferAmount || verified.Currency != withdrawal.Currency {
		return errors.New("payout submitted but provider confirmation did not match; funds remain reserved")
	}
	if verified.Status == "SUCCESSFUL" || verified.Status == "FAILED" || verified.Status == "REVERSED" {
		_, err := s.settleWithdrawal(ctx, verified.Reference, withdrawal, verified.ID, verified.Status, verified.Amount, verified.Currency)
		return err
	}
	return nil
}

// ReturnWithdrawalToQueue is the manual counterpart to the automatic revert in
// ApproveWithdrawal, for the payouts whose fate the provider never settled: a
// timeout, a 500, a rate limit. Those leave the row 'processing' with funds
// reserved and no transfer id, and nothing else can move it.
//
// The store refuses any row that has a saved transfer id, so an admin cannot
// refund a payout that is already on its way. What the store cannot know is
// whether a transfer exists that we failed to record, which is why the page
// warns the admin to check the provider using the reference first - see
// ErrPayoutSubmitted, the failure that leaves exactly that state.
func (s *WalletService) ReturnWithdrawalToQueue(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	withdrawal, err := s.repo.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return err
	}
	if withdrawal.Status != "processing" {
		return fmt.Errorf("only a payout that has not settled can be returned to the queue; this one is %s", withdrawal.Status)
	}
	return s.repo.RevertWithdrawalToPending(ctx, withdrawal.Reference)
}

// RecordProviderTransferID stores the Flutterwave transfer id an admin found
// for a payout whose own id was never saved, so that it can be settled.
//
// It exists for one state, the one ErrPayoutSubmitted describes: CreateTransfer
// succeeded but SetTransferID did not. The row is 'processing' with the farmer's
// money reserved, no transfer id, and no way to resolve it in either direction -
// CompleteTransfer and ReconcileWithdrawal both match on a stored id, so the
// webhook cannot settle it either. A transfer that failed would never be
// refunded, and one that succeeded would never close. The only record of the id
// is Flutterwave's dashboard, so a human has to bring it back.
//
// The id is confirmed with the provider before it is stored. An id that does not
// describe this withdrawal would give a later settlement the authority to close
// this row on some other payout's outcome - and settlement is what refunds.
func (s *WalletService) RecordProviderTransferID(ctx context.Context, withdrawalID int64, adminID int, transferID string) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	transferID = strings.TrimSpace(transferID)
	id, err := strconv.ParseInt(transferID, 10, 64)
	if err != nil || id <= 0 {
		return errors.New("enter the Flutterwave transfer id, which is the number shown for that transfer")
	}
	withdrawal, err := s.repo.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return err
	}
	if withdrawal.Status != "processing" {
		return fmt.Errorf("only a payout that is still processing can have its transfer recorded; this one is %s", withdrawal.Status)
	}
	if withdrawal.TransferID.Valid {
		return errors.New("this request already has its Flutterwave transfer recorded")
	}
	_, amount, err := normalizeWalletAmount(withdrawal.Amount, true)
	if err != nil {
		return fmt.Errorf("invalid stored withdrawal amount: %w", err)
	}
	verified, err := s.flutter.VerifyTransfer(transferID)
	if err != nil {
		return fmt.Errorf("could not confirm transfer %s with Flutterwave: %w", transferID, err)
	}
	if verified.ID != transferID || verified.Reference != withdrawal.Reference ||
		verified.Amount != amount || verified.Currency != withdrawal.Currency {
		return fmt.Errorf("transfer %s is not this payout; check the reference %s and try again", transferID, withdrawal.Reference)
	}
	return s.repo.SetTransferID(ctx, withdrawal.Reference, transferID)
}

// settleWithdrawal makes the single transition that either closes a successful
// payout or refunds a failed one, and tells the farmer which of the two it was.
//
// All three places the provider's answer arrives - the approval call itself, an
// admin pressing "check provider status", and the webhook - funnel through here
// so the farmer's message cannot differ depending on which one got there first.
//
// request is the withdrawal behind the reference, used only to say what
// happened. It may be nil: the webhook can settle a payout whose request row it
// could not read, and losing a notification is a far smaller harm than refusing
// to settle the money.
//
// The store reports whether it actually moved the row. A webhook retry, or an
// admin reconciling a payout the webhook already settled, arrives here with the
// row already closed - and telling the farmer their money moved a second time
// would be worse than saying nothing.
func (s *WalletService) settleWithdrawal(ctx context.Context, reference string, request *models.Withdrawal, transferID, status string, amount int64, currency string) (bool, error) {
	settled, err := s.repo.CompleteTransfer(ctx, reference, transferID, status, amount, currency)
	if err != nil {
		return false, err
	}
	if settled && request != nil {
		s.notifyWithdrawalOutcome(request, status)
	}
	return settled, nil
}

// notifyWithdrawalOutcome tells the farmer what became of their payout. Both
// outcomes are money they can no longer see in their wallet, so neither is
// allowed to be silent: a paid payout and a refunded one look identical from
// the wallet page, and only one of them means the money is gone.
func (s *WalletService) notifyWithdrawalOutcome(withdrawal *models.Withdrawal, status string) {
	switch status {
	case "SUCCESSFUL":
		s.notifyUser(withdrawal.FarmerID, "Withdrawal paid",
			fmt.Sprintf("Your withdrawal of ₦%s has been sent to %s %s.",
				withdrawal.Amount, withdrawal.BankName, withdrawal.MaskedAccountNumber()),
			"wallet_withdrawal_paid")
	case "FAILED", "REVERSED":
		s.notifyUser(withdrawal.FarmerID, "Withdrawal returned to your wallet",
			fmt.Sprintf("We could not pay out your ₦%s, so it has been refunded to your wallet. Check that your bank details are correct, then request it again.",
				withdrawal.Amount),
			"wallet_withdrawal_refunded")
	}
}

func (s *WalletService) RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	// Read before rejecting, while the row is still there to read. A failed read
	// costs the notification and nothing else - rejecting is what releases the
	// farmer's money, so it must not be held up by the message about it.
	withdrawal, readErr := s.repo.GetWithdrawal(ctx, withdrawalID)
	if readErr != nil {
		log.Printf("reject withdrawal %d: read for notification: %v", withdrawalID, readErr)
		withdrawal = nil
	}
	if err := s.repo.RejectWithdrawal(ctx, withdrawalID, adminID); err != nil {
		return err
	}
	if withdrawal != nil {
		s.notifyUser(withdrawal.FarmerID, "Withdrawal not paid",
			fmt.Sprintf("Your withdrawal of ₦%s was not approved, so the money is back in your wallet. Check that your bank details are correct, then request it again.",
				withdrawal.Amount),
			"wallet_withdrawal_rejected")
	}
	return nil
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
		if _, err := s.settleWithdrawal(ctx, verified.Reference, withdrawal, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
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
	// The farmer is told what became of the payout, which needs the request this
	// transfer belongs to. Not being able to read it costs that message and
	// nothing else: closing or refunding the payout is what matters here, and a
	// settlement refused over a lookup would leave the money in limbo.
	withdrawal, lookupErr := s.repo.GetWithdrawalByReference(ctx, verified.Reference)
	if lookupErr != nil {
		log.Printf("settle transfer %s for %s: read withdrawal for notification: %v", verified.ID, verified.Reference, lookupErr)
		withdrawal = nil
	}
	if _, err := s.settleWithdrawal(ctx, verified.Reference, withdrawal, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
		return false, err
	}
	return true, nil
}
