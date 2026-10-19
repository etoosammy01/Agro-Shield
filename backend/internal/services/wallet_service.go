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
	"time"
	"unicode"

	"backend/internal/models"
	"backend/internal/repository"
)

// notifier is the notification store as the wallet service sees it. Keeping it
// this narrow lets a test record what was said without a database.
type notifier interface {
	CreateNotification(userID int, title, message, kind string) error
}

type WalletService struct {
	repo    walletStore
	farmers farmerStore
	lookups bankLookupStore
	flutter *FlutterwaveClient

	// notify delivers the messages that follow money movement. It may be nil: a
	// wallet still has to move money when nobody can be told about it.
	notify notifier

	adminIDs map[int]struct{}

	// configuredAdminIDs is the set from WALLET_ADMIN_USER_IDS. It is kept apart
	// from adminIDs so a refresh from the database cannot drop an operator who
	// was configured in the environment.
	configuredAdminIDs map[int]struct{}

	// adminMu guards adminIDs, which the reconciliation loop rewrites while
	// handlers read it.
	adminMu sync.RWMutex

	// resolver is the account-name half of the Flutterwave client, held as a
	// narrow interface so the bank fan-out can be tested without HTTP. It is
	// the same underlying client as flutter.
	resolver accountResolver

	// directory is the bank-list half of the same client, for checking a stored
	// bank name against its code. Also the same underlying client.
	directory bankDirectory

	// virtualAccounts is the permanent-account half of the Flutterwave client,
	// held as a narrow interface so the setup flow can be tested without HTTP.
	virtualAccounts virtualAccountProvider

	// commissionBasisPoints is the platform's cut of marketplace sales, in
	// hundredths of a percent: 2.5% is 250. Zero means no commission.
	commissionBasisPoints int64

	// staleDepositAfter is how long a funding attempt may stay unsettled before
	// it is listed for an operator to investigate. Reporting only.
	staleDepositAfter time.Duration
}

func NewWalletService(repo walletStore, farmers farmerStore, lookups bankLookupStore, flutter *FlutterwaveClient, adminIDs []int, commissionBasisPoints int64, notify notifier) *WalletService {
	configured := make(map[int]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		if id > 0 {
			configured[id] = struct{}{}
		}
	}
	adminSet := make(map[int]struct{}, len(configured))
	for id := range configured {
		adminSet[id] = struct{}{}
	}
	return &WalletService{
		repo:                  repo,
		farmers:               farmers,
		lookups:               lookups,
		flutter:               flutter,
		notify:                notify,
		resolver:              flutter,
		directory:             flutter,
		virtualAccounts:       flutter,
		adminIDs:              adminSet,
		configuredAdminIDs:    configured,
		commissionBasisPoints: commissionBasisPoints,
		staleDepositAfter:     24 * time.Hour,
	}
}

// SetStaleDepositAfter sets how long an unsettled funding attempt may wait
// before the admin report lists it. A zero or negative age keeps the 24-hour
// default, because a zero threshold would report every pending deposit.
func (s *WalletService) SetStaleDepositAfter(age time.Duration) {
	if age > 0 {
		s.staleDepositAfter = age
	}
}

// accountResolver resolves an account number at one specific bank. Flutterwave
// exposes no reverse lookup, so callers wishing to find which bank a number
// belongs to must ask this repeatedly, once per candidate bank.
type accountResolver interface {
	ResolveAccount(accountNumber, bankCode string) (*ResolvedAccount, error)
}

// virtualAccountProvider is the permanent-account half of the Flutterwave
// client. Both methods return the provider's confirmed account details, or an
// error that distinguishes a refusal/no-account answer from a failure that says
// nothing about whether an account exists.
type virtualAccountProvider interface {
	CreatePermanentVirtualAccount(req CreateVirtualAccountRequest) (*VirtualAccountDetails, error)
	GetPermanentVirtualAccount(orderRef string) (*VirtualAccountDetails, error)
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
	EnsureWallet(ctx context.Context, farmerID int) (*models.Wallet, error)
	GetStatus(ctx context.Context, farmerID int) (*models.WalletStatus, error)
	DepositProvider(ctx context.Context, reference string) (string, error)
	CreateDeposit(ctx context.Context, farmerID int, provider, reference, amount string) error
	CompleteDeposit(ctx context.Context, reference, transactionID, amount, currency string) (bool, error)
	FailDeposit(ctx context.Context, reference string) error
	ListDeposits(ctx context.Context, farmerID int) ([]models.WalletDeposit, error)
	ListPendingDepositsOlderThan(ctx context.Context, cutoff time.Time) ([]models.PendingDeposit, error)
	ExpireDeposit(ctx context.Context, reference string) error
	StalePendingDeposits(ctx context.Context, olderThan time.Duration, limit int) ([]models.WalletDeposit, error)

	GetVirtualAccount(ctx context.Context, farmerID int) (*models.WalletVirtualAccount, error)
	GetVirtualAccountByTxRef(ctx context.Context, txRef string) (*models.WalletVirtualAccount, error)
	GetVirtualAccountByAccountNumber(ctx context.Context, accountNumber string) (*models.WalletVirtualAccount, error)
	EnsureVirtualAccountAttempt(ctx context.Context, farmerID int, provider, txRef, identityKind, consentVersion string, now time.Time) (*models.WalletVirtualAccount, bool, error)
	ResetVirtualAccountAttempt(ctx context.Context, farmerID int, identityKind, consentVersion string, now time.Time) error
	ActivateVirtualAccount(ctx context.Context, farmerID int, orderRef, providerRef, accountNumber, bankName, accountName, currency string, now time.Time) error
	FailVirtualAccountAttempt(ctx context.Context, farmerID int, reason string, now time.Time) error
	MarkVirtualAccountChecked(ctx context.Context, farmerID int, now time.Time) error
	ListVirtualAccountsForReconciliation(ctx context.Context, limit int) ([]models.WalletVirtualAccount, error)
	ListPendingVirtualAccountDeposits(ctx context.Context, limit int) ([]models.PendingVirtualAccountTransfer, error)
	RecordPendingVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) error
	CreditVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) (bool, error)
	FailVirtualAccountDeposit(ctx context.Context, providerTxnID, status string, now time.Time) error
	ReverseVirtualAccountDeposit(ctx context.Context, providerTxnID, amount, currency string, now time.Time) (bool, error)

	CreateWithdrawal(ctx context.Context, farmerID int, reference, amount string) error
	ListFarmerWithdrawals(ctx context.Context, farmerID int) ([]models.Withdrawal, error)
	ListWithdrawals(ctx context.Context, pendingOnly bool) ([]models.Withdrawal, error)
	GetWithdrawal(ctx context.Context, withdrawalID int64) (*models.Withdrawal, error)
	BeginWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (*models.Withdrawal, error)
	RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error
	CancelWithdrawal(ctx context.Context, withdrawalID int64, farmerID int) error
	ExpireStaleWithdrawals(ctx context.Context, cutoff time.Time) ([]models.Withdrawal, error)
	ClaimWithdrawalReminder(ctx context.Context, withdrawalID int64, cutoff time.Time) (bool, error)
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
	CancelTransfer(ctx context.Context, transferID int64, senderID int) error
	ExpireStaleTransfers(ctx context.Context, cutoff time.Time) ([]models.Transfer, error)
	ClaimTransferReminder(ctx context.Context, transferID int64, cutoff time.Time) (bool, error)

	ListTransactions(ctx context.Context, farmerID int) ([]models.WalletTransaction, error)
	ListAdminIDs(ctx context.Context) ([]int, error)
	ListAdminAccounts(ctx context.Context) ([]models.AdminAccount, error)
	SetAdmin(ctx context.Context, farmerID int, admin bool) (bool, error)
	PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64, checkoutToken string, commissionBasisPoints int64) (int64, bool, error)
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

// ParseStaleDepositAfter converts WALLET_STALE_DEPOSIT_HOURS into the age at
// which an unsettled funding attempt is listed for an operator to investigate.
// An empty value means the 24-hour default.
func ParseStaleDepositAfter(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "24"
	}
	hours, err := strconv.Atoi(value)
	if err != nil || hours <= 0 {
		return 0, fmt.Errorf("invalid WALLET_STALE_DEPOSIT_HOURS %q: enter a whole number of hours greater than zero", value)
	}
	return time.Duration(hours) * time.Hour, nil
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
	s.adminMu.RLock()
	defer s.adminMu.RUnlock()

	_, ok := s.adminIDs[farmerID]
	return ok
}

// AdminIDs returns the current admin set as a sorted copy, so a caller cannot
// mutate the set the service guards with its own lock.
func (s *WalletService) AdminIDs() []int {
	s.adminMu.RLock()
	defer s.adminMu.RUnlock()
	ids := make([]int, 0, len(s.adminIDs))
	for id := range s.adminIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// RefreshAdminIDs merges the accounts flagged in the database into the admin
// set. Configured IDs are always kept: they are the recovery path when the
// table is empty or unreadable.
func (s *WalletService) RefreshAdminIDs(ctx context.Context) error {
	flagged, err := s.repo.ListAdminIDs(ctx)
	if err != nil {
		return err
	}
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	next := make(map[int]struct{}, len(s.configuredAdminIDs)+len(flagged))
	for id := range s.configuredAdminIDs {
		next[id] = struct{}{}
	}
	for _, id := range flagged {
		if id > 0 {
			next[id] = struct{}{}
		}
	}
	s.adminIDs = next
	return nil
}

// isConfiguredAdmin reports whether the role comes from the environment rather
// than the database, which is what decides whether the page may revoke it.
func (s *WalletService) isConfiguredAdmin(farmerID int) bool {
	s.adminMu.RLock()
	defer s.adminMu.RUnlock()
	_, ok := s.configuredAdminIDs[farmerID]
	return ok
}

// AdminAccounts lists the accounts allowed to review wallet requests: the ones
// flagged in the database plus any configured in the environment that have no
// row of their own. Configured grants are marked so the page does not offer a
// removal that would not stick.
func (s *WalletService) AdminAccounts(ctx context.Context) ([]models.AdminAccount, error) {
	flagged, err := s.repo.ListAdminAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]models.AdminAccount, len(flagged))
	index := make(map[int]int, len(flagged))
	for i, account := range flagged {
		accounts[i] = account
		index[account.FarmerID] = i
	}

	s.adminMu.RLock()
	configured := make([]int, 0, len(s.configuredAdminIDs))
	for id := range s.configuredAdminIDs {
		configured = append(configured, id)
	}
	s.adminMu.RUnlock()
	sort.Ints(configured)

	for _, id := range configured {
		if i, ok := index[id]; ok {
			// Already listed from the database: mark why it cannot be removed.
			accounts[i].FromConfig = true
			continue
		}
		farmer, err := s.farmers.GetByID(id)
		if err != nil {
			return nil, err
		}
		if farmer == nil {
			continue
		}
		accounts = append(accounts, models.AdminAccount{
			FarmerID:   farmer.ID,
			FullName:   farmer.FullName,
			Phone:      farmer.Phone,
			FromConfig: true,
		})
	}
	return accounts, nil
}

// SetAdmin grants or revokes the admin role. Only an admin may change it, and
// two grants are protected: an actor cannot remove themselves (which could lock
// the last admin out), and a configured grant cannot be revoked here because
// the environment would restore it on the next start.
func (s *WalletService) SetAdmin(ctx context.Context, actorID, targetID int, admin bool) error {
	if !s.IsAdmin(actorID) {
		return ErrNotAdmin
	}
	if targetID <= 0 {
		return ErrAdminNotFound
	}
	if !admin && targetID == actorID {
		return ErrAdminSelfRemoval
	}
	if !admin && s.isConfiguredAdmin(targetID) {
		return ErrAdminFromConfig
	}
	found, err := s.repo.SetAdmin(ctx, targetID, admin)
	if err != nil {
		return err
	}
	if !found {
		return ErrAdminNotFound
	}
	// Apply the change to the cached set now, before the redirect lands, so the
	// page that follows already reflects it.
	if err := s.RefreshAdminIDs(ctx); err != nil {
		log.Printf("refresh wallet admins after change: %v", err)
	}
	return nil
}

// SetAdminByPhone is the page's other entry point: an operator knows the
// account's phone number, not its internal id.
func (s *WalletService) SetAdminByPhone(ctx context.Context, actorID int, phone string, admin bool) error {
	if !s.IsAdmin(actorID) {
		return ErrNotAdmin
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ErrAdminPhoneRequired
	}
	farmer, err := s.farmers.GetByPhone(phone)
	if err != nil {
		return err
	}
	if farmer == nil {
		return ErrAdminNotFound
	}
	return s.SetAdmin(ctx, actorID, farmer.ID, admin)
}

// notifyUser sends one message to one account. Best-effort by design: the money
// has already moved, so a failed send is logged rather than returned.
func (s *WalletService) notifyUser(farmerID int, title, message, kind string) {
	if s.notify == nil || farmerID <= 0 {
		return
	}
	if err := s.notify.CreateNotification(farmerID, title, message, kind); err != nil {
		log.Printf("notify user %d (%s): %v", farmerID, kind, err)
	}
}

// notifyAdminsExcept sends one message to every admin but exceptID. The
// exception keeps the requester from being told their own request is waiting
// for them: the self-approval rule means it is not.
func (s *WalletService) notifyAdminsExcept(exceptID int, title, message, kind string) {
	for _, id := range s.adminIDsExcept(exceptID) {
		s.notifyUser(id, title, message, kind)
	}
}

// adminIDsExcept copies the admin set without exceptID, so the notification
// fan-out never holds the admin lock while it talks to the store.
func (s *WalletService) adminIDsExcept(exceptID int) []int {
	s.adminMu.RLock()
	defer s.adminMu.RUnlock()
	ids := make([]int, 0, len(s.adminIDs))
	for id := range s.adminIDs {
		if id != exceptID {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func (s *WalletService) Wallet(ctx context.Context, farmerID int) (*models.Wallet, []models.WalletTransaction, error) {
	wallet, err := s.repo.GetWallet(ctx, farmerID)
	if errors.Is(err, repository.ErrWalletNotFound) {
		// Every account is meant to own a wallet — FarmerRepository.Create inserts
		// one inside the registration transaction. If one is somehow missing,
		// rebuild it for the authenticated owner here rather than leaving them on a
		// permanent error with no way back. This is the owner's own wallet, so it is
		// not creation on behalf of an arbitrary user.
		wallet, err = s.repo.EnsureWallet(ctx, farmerID)
	}
	if err != nil {
		return nil, nil, err
	}
	transactions, err := s.repo.ListTransactions(ctx, farmerID)
	if err != nil {
		return nil, nil, err
	}
	return wallet, transactions, nil
}

// Status returns the committed wallet snapshot used for lightweight refreshes.
// Unlike Wallet it never creates a missing wallet: a status check must not
// change state, and the page that polls it has already loaded the wallet.
func (s *WalletService) Status(ctx context.Context, farmerID int) (*models.WalletStatus, error) {
	if farmerID <= 0 {
		return nil, repository.ErrWalletNotFound
	}
	return s.repo.GetStatus(ctx, farmerID)
}

// RepairWallet rebuilds a user's wallet if their account has none. Admin only.
//
// This is deliberately an explicit operation rather than something the funding
// path does silently: creating a wallet during an arbitrary funding request would
// let a request invent an account's financial state.
func (s *WalletService) RepairWallet(ctx context.Context, farmerID, adminID int) (*models.Wallet, error) {
	if !s.IsAdmin(adminID) {
		return nil, errors.New("admin access required")
	}
	wallet, err := s.repo.EnsureWallet(ctx, farmerID)
	if err != nil {
		return nil, err
	}
	log.Printf("wallet repair: admin %d ensured a wallet for user %d", adminID, farmerID)
	return wallet, nil
}

// IsWalletDeposit reports whether the reference belongs to a wallet funding
// attempt, and which provider owns it. Routing reads the recorded provider rather
// than the shape of the reference string.
func (s *WalletService) IsWalletDeposit(ctx context.Context, reference string) (provider string, found bool, err error) {
	provider, err = s.repo.DepositProvider(ctx, reference)
	if errors.Is(err, repository.ErrWalletNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: read deposit provider: %v", ErrWalletUnavailable, err)
	}
	return provider, true, nil
}

// StalePendingDeposits is a read-only operational report for an admin: funding
// attempts a provider has not settled within the configured age. It never moves money.
func (s *WalletService) StalePendingDeposits(ctx context.Context) ([]models.WalletDeposit, error) {
	return s.repo.StalePendingDeposits(ctx, s.staleDepositAfter, 100)
}

// StaleDepositAfter exposes the configured reporting threshold for display.
func (s *WalletService) StaleDepositAfter() time.Duration {
	return s.staleDepositAfter
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
	if err := s.repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, amount); err != nil {
		return nil, fmt.Errorf("create wallet deposit: %w", err)
	}
	accountName := virtualAccountName(farmer.FullName)
	if accountName == "" {
		accountName = "AgroShield"
	}
	checkoutURL, err := s.flutter.InitializePayment(InitializePaymentRequest{
		Reference:      reference,
		Amount:         amount,
		Currency:       "NGN",
		CustomerEmail:  farmer.Email,
		CustomerName:   farmer.FullName,
		Title:          accountName,
		Description:    accountName,
		PaymentOptions: "card",
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
	_, err := s.confirmDeposit(ctx, reference, transactionID, amount, currency, status)
	return err
}

// confirmDeposit is ConfirmDeposit with the transition reported. The webhook and
// the reconciliation pass can both settle the same payment; only the call that
// actually moved the row should tell the farmer, or one deposit reads as two.
func (s *WalletService) confirmDeposit(ctx context.Context, reference, transactionID string, amount float64, currency, status string) (bool, error) {
	switch strings.ToLower(status) {
	case "failed", "cancelled", "canceled":
		return false, s.repo.FailDeposit(ctx, reference)
	case "successful":
	default:
		return false, nil
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return false, errors.New("invalid verified deposit amount")
	}
	cents := amount * 100
	roundedCents := math.Round(cents)
	if math.Abs(cents-roundedCents) > 0.000001 {
		return false, errors.New("verified deposit amount has unsupported precision")
	}
	verifiedAmount, _, err := normalizeWalletAmount(strconv.FormatFloat(roundedCents/100, 'f', 2, 64), false)
	if err != nil {
		return false, fmt.Errorf("invalid verified deposit amount: %w", err)
	}
	if currency != "NGN" {
		return false, errors.New("wallet deposits must be paid in NGN")
	}
	if transactionID == "" {
		return false, errors.New("invalid wallet deposit verification")
	}
	// The recorded provider is authoritative, not the reference's shape: a
	// reference that belongs to no wallet funding attempt must never be credited.
	if _, err := s.repo.DepositProvider(ctx, reference); errors.Is(err, repository.ErrWalletNotFound) {
		return false, errors.New("invalid wallet deposit verification")
	} else if err != nil {
		return false, fmt.Errorf("%w: read deposit provider: %v", ErrWalletUnavailable, err)
	}
	return s.repo.CompleteDeposit(ctx, reference, transactionID, verifiedAmount, currency)
}

// --- permanent virtual accounts --------------------------------------------
//
// A farmer's permanent account is opened once and then receives ordinary bank
// transfers. Its tx_ref identifies the account, not a payment, so incoming
// transfers are keyed on the provider's transaction id instead. That split is
// what keeps a permanent account from turning every transfer into a duplicate
// of the one before it.

const (
	// VirtualAccountConsentVersion names the exact consent text a farmer agreed
	// to. Storing it means a later change to the wording can be told apart from
	// consent collected under this one.
	VirtualAccountConsentVersion = "2026-10-v1"

	// virtualAccountRetryAfter is how long a setup with no provider answer is
	// left alone before a retry asks the provider again. It is long enough for a
	// slow provider call to finish and short enough to be useful.
	virtualAccountRetryAfter = 2 * time.Minute

	// virtualAccountReconcileBatch bounds one reconciliation pass. Accounts are
	// rotated by last_checked_at, so a larger number of farmers is reached over
	// successive passes rather than in one burst.
	virtualAccountReconcileBatch = 25
)

var (
	// ErrVirtualAccountConsentRequired is returned before any identity
	// information is sent: the farmer has to agree to the share explicitly.
	ErrVirtualAccountConsentRequired = errors.New("tick the consent box before your identity information is shared with Flutterwave")

	// ErrVirtualAccountIdentityInvalid covers anything that is not an
	// 11-digit BVN or NIN. The number itself is never echoed back.
	ErrVirtualAccountIdentityInvalid = errors.New("enter a valid 11-digit BVN or NIN")

	ErrVirtualAccountEmailRequired = errors.New("add an email address to your profile before creating a funding account")
	ErrVirtualAccountNameRequired  = errors.New("add your full name to your profile before creating a funding account")

	// ErrVirtualAccountRejected means Flutterwave refused the request outright.
	// The provider's own diagnostic is logged, not shown, so a raw response body
	// cannot reach the page.
	ErrVirtualAccountRejected = errors.New("Flutterwave could not open a funding account with those details. Check your BVN or NIN and try again")

	// ErrVirtualAccountPending means the provider has not confirmed whether an
	// account exists. Retrying is safe because the same tx_ref is reused.
	ErrVirtualAccountPending = errors.New("your funding account is still being set up")

	// ErrVirtualAccountUnavailable means the account could not be saved, or the
	// provider did not permit the request. It is deliberately vague: whether a
	// merchant key is wrong or static accounts are not enabled is not something a
	// farmer can act on, and saying "check your BVN" would blame the wrong party.
	ErrVirtualAccountUnavailable = errors.New("funding accounts are temporarily unavailable. Your existing wallet still works; please try again later")
)

// VirtualAccountSetup is the browser-supplied half of the setup request. Names,
// email and phone are never taken from it; they come from the authenticated
// farmer, so a session cannot open an account in somebody else's name.
type VirtualAccountSetup struct {
	IdentityKind   string
	IdentityNumber string
	Consent        bool
}

const virtualAccountNamePrefix = "AgroShield-"

// virtualAccountName is the name Flutterwave displays when this funding account
// is resolved by another bank. Flutterwave's narration field controls that
// name, so it must identify both Agro-Shield and the account holder.
func virtualAccountName(fullName string) string {
	fullName = strings.Join(strings.Fields(fullName), " ")
	if fullName == "" {
		return ""
	}
	return virtualAccountNamePrefix + fullName
}

func isWalletAccountNumber(number string) bool {
	if len(number) != 10 {
		return false
	}
	for _, digit := range number {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validVirtualAccountEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return false
	}
	return strings.Contains(email[at+1:], ".")
}

func normalizeVirtualAccountIdentity(kind, number string) (string, string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "bvn" && kind != "nin" {
		return "", "", ErrVirtualAccountIdentityInvalid
	}
	number = strings.ReplaceAll(strings.TrimSpace(number), " ", "")
	if len(number) != 11 {
		return "", "", ErrVirtualAccountIdentityInvalid
	}
	for _, digit := range number {
		if digit < '0' || digit > '9' {
			return "", "", ErrVirtualAccountIdentityInvalid
		}
	}
	return kind, number, nil
}

func splitFarmerName(fullName string) (first, last string) {
	fields := strings.Fields(fullName)
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return fields[0], ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

// verifiedAmountKobo converts a provider amount to integer kobo, rejecting
// anything that is not an exact number of kobo. Once this returns, every amount
// decision is integer arithmetic.
func verifiedAmountKobo(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return 0, errors.New("invalid verified deposit amount")
	}
	cents := amount * 100
	rounded := math.Round(cents)
	if math.Abs(cents-rounded) > 0.000001 {
		return 0, errors.New("verified deposit amount has unsupported precision")
	}
	if rounded > math.MaxInt64 {
		return 0, errors.New("verified deposit amount is too large")
	}
	return int64(rounded), nil
}

func koboAmount(kobo int64) string {
	return fmt.Sprintf("%d.%02d", kobo/100, kobo%100)
}

// VirtualAccount returns the farmer's permanent funding account, or nil when
// none has been created. A missing row is an ordinary state, not a failure.
func (s *WalletService) VirtualAccount(ctx context.Context, farmerID int) (*models.WalletVirtualAccount, error) {
	account, err := s.repo.GetVirtualAccount(ctx, farmerID)
	if errors.Is(err, repository.ErrVirtualAccountNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (s *WalletService) activateVirtualAccount(ctx context.Context, farmerID int, details *VirtualAccountDetails, now time.Time) error {
	if details == nil {
		return fmt.Errorf("%w: provider returned no account details", ErrVirtualAccountPending)
	}
	accountNumber := strings.TrimSpace(details.AccountNumber)
	if !isWalletAccountNumber(accountNumber) {
		return fmt.Errorf("%w: provider returned an unusable account number", ErrVirtualAccountPending)
	}
	currency := strings.ToUpper(strings.TrimSpace(details.Currency))
	if currency == "" {
		currency = "NGN"
	}
	if currency != "NGN" {
		return errors.New("virtual accounts must be funded in NGN")
	}
	if err := s.repo.ActivateVirtualAccount(ctx, farmerID, details.OrderRef, details.ProviderRef,
		accountNumber, details.BankName, details.AccountName, currency, now); err != nil {
		return fmt.Errorf("%w: %v", ErrVirtualAccountUnavailable, err)
	}
	return nil
}

// SetupVirtualAccount opens the farmer's permanent NGN account, or returns the
// one already open.
//
// The identity document is passed straight to Flutterwave and is not stored,
// logged or echoed. A nil error with a pending account is an honest "not
// confirmed yet": the provider may still have created it, and a retry looks it
// up by the same reference before creating anything else.
func (s *WalletService) SetupVirtualAccount(ctx context.Context, farmer *models.Farmer, setup VirtualAccountSetup) (*models.WalletVirtualAccount, error) {
	if farmer == nil || farmer.ID <= 0 {
		return nil, errors.New("invalid wallet owner")
	}
	if !setup.Consent {
		return nil, ErrVirtualAccountConsentRequired
	}
	identityKind, identityNumber, err := normalizeVirtualAccountIdentity(setup.IdentityKind, setup.IdentityNumber)
	if err != nil {
		return nil, err
	}
	if !validVirtualAccountEmail(farmer.Email) {
		return nil, ErrVirtualAccountEmailRequired
	}
	first, last := splitFarmerName(farmer.FullName)
	if first == "" {
		return nil, ErrVirtualAccountNameRequired
	}
	accountName := virtualAccountName(farmer.FullName)

	now := time.Now()
	txRef := fmt.Sprintf("AGRO-VA-%d", farmer.ID)
	account, created, err := s.repo.EnsureVirtualAccountAttempt(ctx, farmer.ID,
		models.ProviderFlutterwave, txRef, identityKind, VirtualAccountConsentVersion, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVirtualAccountUnavailable, err)
	}
	if account.Ready() {
		return account, nil
	}
	// A concurrent or very recent attempt may still be in flight. Not calling
	// the provider again is what prevents two accounts being opened.
	if !created && account.Status == models.VirtualAccountPending &&
		now.Sub(account.UpdatedAt) < virtualAccountRetryAfter {
		return account, nil
	}

	lookupRef := strings.TrimSpace(account.OrderRef)
	if lookupRef == "" {
		lookupRef = strings.TrimSpace(account.TxRef)
	}
	// A row this call just inserted has no earlier attempt behind it, so there is
	// nothing to look up. A failed row is also final evidence that no account was
	// created, so a retry may create directly. For every other row, ask the
	// provider whether an earlier attempt already opened the account before
	// creating a second one.
	lookupable := !created && account.Status != models.VirtualAccountFailed
	if lookupable && lookupRef != "" {
		existing, lookupErr := s.virtualAccounts.GetPermanentVirtualAccount(lookupRef)
		switch {
		case lookupErr == nil && existing != nil:
			// An earlier attempt did open the account; adopt it instead of
			// asking for a second one.
			if err := s.activateVirtualAccount(ctx, farmer.ID, existing, now); err != nil {
				return nil, err
			}
			return s.repo.GetVirtualAccount(ctx, farmer.ID)
		case errors.Is(lookupErr, ErrVirtualAccountNotFound):
			// The provider has no account for this reference. Creating with the
			// same reference is safe.
		default:
			// We could not ask. Do not create another account on an unanswered
			// question; the farmer can retry shortly.
			log.Printf("look up virtual account for user %d: %v", farmer.ID, lookupErr)
			return account, nil
		}
	}

	if err := s.repo.ResetVirtualAccountAttempt(ctx, farmer.ID, identityKind, VirtualAccountConsentVersion, now); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVirtualAccountUnavailable, err)
	}

	details, err := s.virtualAccounts.CreatePermanentVirtualAccount(CreateVirtualAccountRequest{
		Email:          farmer.Email,
		TxRef:          txRef,
		Phone:          farmer.Phone,
		FirstName:      first,
		LastName:       last,
		Narration:      accountName,
		IdentityKind:   identityKind,
		IdentityNumber: identityNumber,
	})
	if err != nil {
		if errors.Is(err, ErrVirtualAccountAccessDenied) {
			// Credentials or merchant permissions, not the farmer's details. Do not
			// tell them to check a BVN that the provider never evaluated.
			log.Printf("create virtual account for user %d not permitted: %v", farmer.ID, err)
			_ = s.repo.FailVirtualAccountAttempt(ctx, farmer.ID, "Flutterwave did not permit the account request", now)
			return nil, ErrVirtualAccountUnavailable
		}
		if errors.Is(err, ErrVirtualAccountRefused) {
			// A refusal proves no account was opened, so the row can leave the
			// pending state and a later attempt can try again.
			log.Printf("create virtual account for user %d refused: %v", farmer.ID, err)
			_ = s.repo.FailVirtualAccountAttempt(ctx, farmer.ID, "Flutterwave refused the account request", now)
			return nil, ErrVirtualAccountRejected
		}
		// An unreadable or unanswered call may still have opened an account. Keep
		// the attempt pending; the next retry asks before creating.
		log.Printf("create virtual account for user %d: %v", farmer.ID, err)
		return s.repo.GetVirtualAccount(ctx, farmer.ID)
	}
	if details == nil || !isWalletAccountNumber(strings.TrimSpace(details.AccountNumber)) {
		log.Printf("create virtual account for user %d returned incomplete details", farmer.ID)
		return s.repo.GetVirtualAccount(ctx, farmer.ID)
	}
	// The provider may omit account_name even though the narration above is the
	// name it will show when another bank resolves this account.
	if strings.TrimSpace(details.AccountName) == "" {
		details.AccountName = accountName
	}
	if err := s.activateVirtualAccount(ctx, farmer.ID, details, now); err != nil {
		return nil, err
	}
	return s.repo.GetVirtualAccount(ctx, farmer.ID)
}

// RefreshVirtualAccountStatus asks the provider about an account this server has
// not confirmed. It never creates one and never treats an unanswered question as
// evidence of absence.
func (s *WalletService) RefreshVirtualAccountStatus(ctx context.Context, farmerID int) (*models.WalletVirtualAccount, error) {
	account, err := s.repo.GetVirtualAccount(ctx, farmerID)
	if errors.Is(err, repository.ErrVirtualAccountNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if account.Ready() {
		return account, nil
	}
	lookupRef := strings.TrimSpace(account.OrderRef)
	if lookupRef == "" {
		lookupRef = strings.TrimSpace(account.TxRef)
	}
	if lookupRef == "" {
		return account, nil
	}
	now := time.Now()
	details, lookupErr := s.virtualAccounts.GetPermanentVirtualAccount(lookupRef)
	if errors.Is(lookupErr, ErrVirtualAccountNotFound) {
		_ = s.repo.MarkVirtualAccountChecked(ctx, farmerID, now)
		return account, nil
	}
	if lookupErr != nil {
		log.Printf("refresh virtual account for user %d: %v", farmerID, lookupErr)
		return account, nil
	}
	if err := s.activateVirtualAccount(ctx, farmerID, details, now); err != nil {
		return nil, err
	}
	return s.repo.GetVirtualAccount(ctx, farmerID)
}

// IsVirtualAccountTransfer reports whether a verified tx_ref belongs to a
// permanent funding account. The answer comes from stored data, never from the
// shape of the reference.
func (s *WalletService) IsVirtualAccountTransfer(ctx context.Context, txRef string) (bool, error) {
	txRef = strings.TrimSpace(txRef)
	if txRef == "" {
		return false, nil
	}
	_, err := s.repo.GetVirtualAccountByTxRef(ctx, txRef)
	if errors.Is(err, repository.ErrVirtualAccountNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SettleVirtualAccountTransfer credits or records one verified incoming bank
// transfer. The webhook supplies the claimed reference; the verified transaction
// supplies the amount, currency, status and transaction id. Only a successful
// verification ever moves money.
//
// The bool reports whether the provider's answer is terminal. A pending answer
// is deliberately not terminal: the provider will be asked again.
func (s *WalletService) SettleVirtualAccountTransfer(ctx context.Context, verified *VerifiedTransaction, webhookTxRef string) (bool, error) {
	if verified == nil {
		return false, errors.New("missing verified virtual account transfer")
	}
	webhookTxRef = strings.TrimSpace(webhookTxRef)
	if webhookTxRef == "" || webhookTxRef != verified.TxRef {
		return false, ErrReferenceMismatch
	}
	account, err := s.repo.GetVirtualAccountByTxRef(ctx, verified.TxRef)
	if errors.Is(err, repository.ErrVirtualAccountNotFound) {
		return false, errors.New("verified transfer does not belong to a funding account")
	}
	if err != nil {
		return false, err
	}
	if account.Status != models.VirtualAccountActive || account.AccountNumber == "" {
		return false, errors.New("funding account is not active")
	}
	currency := strings.ToUpper(strings.TrimSpace(verified.Currency))
	if currency != "NGN" {
		return false, errors.New("wallet deposits must be paid in NGN")
	}
	amountKobo, err := verifiedAmountKobo(verified.Amount)
	if err != nil {
		return false, err
	}
	amount := koboAmount(amountKobo)
	reference := fmt.Sprintf("VA-%d-%s", account.FarmerID, verified.TransactionID)
	status := strings.ToLower(strings.TrimSpace(verified.Status))

	switch status {
	case "successful", "success":
		credited, err := s.repo.CreditVirtualAccountDeposit(ctx, account.FarmerID, reference,
			verified.TransactionID, amount, currency)
		if err != nil {
			return false, err
		}
		if credited {
			s.notifyUser(account.FarmerID, "Wallet funded",
				fmt.Sprintf("₦%s was added to your wallet from a bank transfer.", amount),
				"wallet_deposit_successful")
		}
		return true, nil
	case "pending", "processing", "new", "incomplete":
		if err := s.repo.RecordPendingVirtualAccountDeposit(ctx, account.FarmerID, reference,
			verified.TransactionID, amount, currency); err != nil {
			return false, err
		}
		return false, nil
	case "failed", "cancelled", "canceled":
		if err := s.repo.FailVirtualAccountDeposit(ctx, verified.TransactionID, models.DepositFailed, time.Now()); err != nil {
			return false, err
		}
		return true, nil
	case "reversed", "refunded":
		reversed, err := s.repo.ReverseVirtualAccountDeposit(ctx, verified.TransactionID, amount, currency, time.Now())
		if errors.Is(err, repository.ErrDepositReversalUnfunded) {
			// The money has already left the wallet. Forcing the debit would break
			// the non-negative balance guarantee, so the deposit stays as it is and
			// an operator has to reconcile it.
			log.Printf("virtual account transfer %s reversed but wallet %d cannot cover it",
				verified.TransactionID, account.FarmerID)
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if reversed {
			s.notifyUser(account.FarmerID, "Deposit reversed",
				fmt.Sprintf("₦%s added from a bank transfer was reversed. Your wallet balance has been adjusted.", amount),
				"wallet_deposit_reversed")
		}
		return true, nil
	default:
		// Unknown provider states are left for the next delivery or the
		// reconciliation pass; guessing could credit a payment that later fails.
		return false, nil
	}
}

// ReconcileVirtualAccountTransfers is the backstop for transfers whose webhook
// never arrived. It first re-checks transfers the provider already reported but
// did not settle, then rotates through active accounts and asks for the latest
// transaction on each permanent reference.
//
// The second half is deliberately best-effort: the provider's reference lookup
// answers with a transaction, not a list, so a transfer older than the latest
// one can only be recovered from its own webhook or transaction id. Nothing here
// ever expires an account or moves money that was not verified successful, and
// the credit path is idempotent by provider transaction id.
func (s *WalletService) ReconcileVirtualAccountTransfers(ctx context.Context) error {
	pending, err := s.repo.ListPendingVirtualAccountDeposits(ctx, virtualAccountReconcileBatch)
	if err != nil {
		return fmt.Errorf("list pending virtual account transfers: %w", err)
	}
	for _, transfer := range pending {
		verified, err := s.flutter.VerifyTransaction(transfer.ProviderTransactionID)
		if err != nil {
			log.Printf("verify pending virtual account transfer %s: %v", transfer.ProviderTransactionID, err)
			continue
		}
		if _, err := s.SettleVirtualAccountTransfer(ctx, verified, verified.TxRef); err != nil {
			log.Printf("settle pending virtual account transfer %s: %v", transfer.ProviderTransactionID, err)
		}
	}

	accounts, err := s.repo.ListVirtualAccountsForReconciliation(ctx, virtualAccountReconcileBatch)
	if err != nil {
		return fmt.Errorf("list virtual accounts for reconciliation: %w", err)
	}
	for _, account := range accounts {
		verified, err := s.flutter.VerifyTransactionByReference(account.TxRef)
		if err != nil {
			if !errors.Is(err, ErrTransactionNotFound) {
				log.Printf("verify virtual account %d: %v", account.FarmerID, err)
			}
			if markErr := s.repo.MarkVirtualAccountChecked(ctx, account.FarmerID, time.Now()); markErr != nil {
				log.Printf("mark virtual account %d checked: %v", account.FarmerID, markErr)
			}
			continue
		}
		if verified.TxRef != account.TxRef {
			log.Printf("virtual account %d verified against reference %s; skipping", account.FarmerID, verified.TxRef)
			continue
		}
		if _, err := s.SettleVirtualAccountTransfer(ctx, verified, verified.TxRef); err != nil {
			log.Printf("settle virtual account %d: %v", account.FarmerID, err)
		}
		if err := s.repo.MarkVirtualAccountChecked(ctx, account.FarmerID, time.Now()); err != nil {
			log.Printf("mark virtual account %d checked: %v", account.FarmerID, err)
		}
	}
	return nil
}

// --- pending requests -------------------------------------------------------
//
// Every non-terminal state needs an owner and a way out. The webhook remains
// the source of truth for a deposit; the pass below is the backstop for the one
// that never arrives, and it never closes an attempt the provider still knows
// about. Requests are treated differently because one side of a payout may be a
// provider transfer, which no local timer is allowed to call back.

const (
	// depositVerifyAfter is how long a deposit is left to the webhook before the
	// pass starts asking the provider directly.
	depositVerifyAfter = 15 * time.Minute

	// depositExpireAfter is the longest an attempt stays pending after the
	// provider has said it has no transaction for the reference.
	depositExpireAfter = 24 * time.Hour

	// requestExpireAfter bounds how long a request can hold a user's money with
	// no decision, for requests that never reached the provider.
	requestExpireAfter = 30 * 24 * time.Hour

	// approvalReminderAfter is when admins are nudged about an aged request.
	approvalReminderAfter = 4 * time.Hour

	// DefaultReconciliationInterval matches the deposit verification window, so
	// a stuck deposit is re-checked promptly without hammering the provider.
	DefaultReconciliationInterval = 15 * time.Minute
)

// ApprovalReminderAfter is approvalReminderAfter for callers that report the
// same ageing on a page: the overview and the reminder pass must count the same
// requests as "waiting too long".
const ApprovalReminderAfter = approvalReminderAfter

// Deposit reconciliation failures the page and the pass both need to name.
var (
	ErrRequestNotFound   = errors.New("that request could not be found")
	ErrRequestNotPending = errors.New("that request is no longer pending, so it cannot be cancelled")
)

// Refusals the admin page shows as the reason an admin change did not happen.
var (
	ErrNotAdmin           = errors.New("only a wallet admin can change admin access")
	ErrAdminNotFound      = errors.New("no account was found for that number")
	ErrAdminPhoneRequired = errors.New("enter the phone number of the account")
	ErrAdminSelfRemoval   = errors.New("another admin has to remove you")
	ErrAdminFromConfig    = errors.New("this admin is set by WALLET_ADMIN_USER_IDS and has to be removed there")
)

// PendingDepositsAwaitingProvider is the admin page's exception list: attempts
// that have waited longer than the normal window. It is a read of the same rows
// the reconciliation pass is working through, not a separate queue.
func (s *WalletService) PendingDepositsAwaitingProvider(ctx context.Context) ([]models.PendingDeposit, error) {
	return s.repo.ListPendingDepositsOlderThan(ctx, time.Now().Add(-depositVerifyAfter))
}

// ReconcilePendingDeposits is the backstop for a deposit whose webhook never
// arrived. It asks the provider by reference, because the numeric transaction id
// is only known once a payment exists.
//
// The rules that matter here:
//   - a provider answer of "successful" credits the wallet, using the amount the
//     provider reports and the same validation the webhook uses;
//   - a provider answer of "failed" closes the attempt as failed;
//   - "no such transaction" may close it as expired, but only after the full
//     window, so a checkout still in progress is never cut off;
//   - every other failure - timeout, bad key, 5xx, unreadable body - says nothing
//     about the payment and leaves the attempt pending for the next pass.
func (s *WalletService) ReconcilePendingDeposits(ctx context.Context) error {
	deposits, err := s.repo.ListPendingDepositsOlderThan(ctx, time.Now().Add(-depositVerifyAfter))
	if err != nil {
		return fmt.Errorf("list pending deposits: %w", err)
	}
	for _, deposit := range deposits {
		verified, err := s.flutter.VerifyTransactionByReference(deposit.Reference)
		if errors.Is(err, ErrTransactionNotFound) {
			if time.Since(deposit.CreatedAt) < depositExpireAfter {
				// The provider has nothing yet, but the attempt is still inside
				// the window in which a slow checkout could complete.
				continue
			}
			if err := s.repo.ExpireDeposit(ctx, deposit.Reference); err != nil {
				if !errors.Is(err, repository.ErrRequestNotPending) {
					log.Printf("expire deposit %s: %v", deposit.Reference, err)
				}
				continue
			}
			s.notifyUser(deposit.FarmerID, "Deposit expired",
				fmt.Sprintf("We did not receive a payment for your ₦%s deposit and the payment provider has no record of it, so the request has been closed. No money was added. If your bank shows a charge, contact support with reference %s.",
					deposit.Amount, deposit.Reference),
				"wallet_deposit_expired")
			continue
		}
		if err != nil {
			// We could not ask. Silence is not evidence, so leave it pending.
			log.Printf("verify pending deposit %s: %v", deposit.Reference, err)
			continue
		}
		if verified.TxRef != deposit.Reference {
			log.Printf("deposit %s verified against reference %s; leaving it pending", deposit.Reference, verified.TxRef)
			continue
		}
		settled, err := s.confirmDeposit(ctx, deposit.Reference, verified.TransactionID,
			verified.Amount, verified.Currency, verified.Status)
		if err != nil {
			log.Printf("confirm pending deposit %s: %v", deposit.Reference, err)
			continue
		}
		if settled {
			s.notifyUser(deposit.FarmerID, "Deposit received",
				fmt.Sprintf("₦%s was added to your wallet.", deposit.Amount),
				"wallet_deposit_successful")
		}
	}
	return nil
}

// ExpireStaleRequests releases requests that never reached the provider and have
// held a user's money past the ageing window. It reports how many were released.
//
// The store decides what is safe to release, not this method: only pending rows
// without a provider transfer qualify. Anything that may have been paid stays
// put for an admin, where the ageing reminder surfaces it.
func (s *WalletService) ExpireStaleRequests(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-requestExpireAfter)

	withdrawals, err := s.repo.ExpireStaleWithdrawals(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("expire pending withdrawals: %w", err)
	}
	for _, withdrawal := range withdrawals {
		s.notifyUser(withdrawal.FarmerID, "Withdrawal request expired",
			fmt.Sprintf("Your ₦%s withdrawal request expired without approval, so the money is back in your wallet. You can request it again.",
				withdrawal.Amount),
			"wallet_withdrawal_expired")
	}

	transfers, err := s.repo.ExpireStaleTransfers(ctx, cutoff)
	if err != nil {
		return len(withdrawals), fmt.Errorf("expire pending transfers: %w", err)
	}
	for _, transfer := range transfers {
		s.notifyUser(transfer.SenderID, "Transfer request expired",
			fmt.Sprintf("Your ₦%s transfer request expired without approval, so the money is back in your wallet.",
				transfer.Amount),
			"wallet_transfer_expired")
	}
	return len(withdrawals) + len(transfers), nil
}

// RemindAgedApprovals nudges admins once about each request that has waited too
// long. The store claims the reminder before it is sent, so a restart or a second
// instance cannot send it again.
func (s *WalletService) RemindAgedApprovals(ctx context.Context) error {
	cutoff := time.Now().Add(-approvalReminderAfter)

	withdrawals, err := s.repo.ListWithdrawals(ctx, true)
	if err != nil {
		return fmt.Errorf("list aged withdrawals: %w", err)
	}
	for _, withdrawal := range withdrawals {
		if withdrawal.Status != models.WithdrawalPending && withdrawal.Status != models.WithdrawalProcessing {
			continue
		}
		if withdrawal.CreatedAt.After(cutoff) {
			continue
		}
		claimed, err := s.repo.ClaimWithdrawalReminder(ctx, withdrawal.ID, cutoff)
		if err != nil {
			log.Printf("claim withdrawal reminder %d: %v", withdrawal.ID, err)
			continue
		}
		if !claimed {
			continue
		}
		state := "waiting for approval"
		if withdrawal.Status == models.WithdrawalProcessing {
			state = "still processing at the payment provider"
		}
		// The requester is skipped for the same reason as the original request:
		// when they are an admin, the self-approval rule means it is not waiting
		// for them.
		s.notifyAdminsExcept(withdrawal.FarmerID, "Withdrawal still waiting",
			fmt.Sprintf("%s (#%d) asked to withdraw ₦%s, and it has been %s since %s. Review it on the wallet approvals page.",
				withdrawal.FullName, withdrawal.FarmerID, withdrawal.Amount, state,
				withdrawal.CreatedAt.Format("02 Jan 15:04")),
			"wallet_withdrawal_reminder")
	}

	transfers, err := s.repo.ListPendingTransfers(ctx)
	if err != nil {
		return fmt.Errorf("list aged transfers: %w", err)
	}
	for _, transfer := range transfers {
		if transfer.CreatedAt.After(cutoff) {
			continue
		}
		claimed, err := s.repo.ClaimTransferReminder(ctx, transfer.ID, cutoff)
		if err != nil {
			log.Printf("claim transfer reminder %d: %v", transfer.ID, err)
			continue
		}
		if !claimed {
			continue
		}
		s.notifyAdminsExcept(transfer.SenderID, "Transfer still waiting",
			fmt.Sprintf("%s sent ₦%s to %s, and it has been waiting for approval since %s. Review it on the wallet approvals page.",
				transfer.SenderName, transfer.Amount, transfer.RecipientName,
				transfer.CreatedAt.Format("02 Jan 15:04")),
			"wallet_transfer_reminder")
	}
	return nil
}

// RunReconciliation runs the pending-request pass until ctx is cancelled. It
// runs once at startup as well, so a server restart does not delay the requests
// that aged while it was down by a whole interval.
func (s *WalletService) RunReconciliation(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultReconciliationInterval
	}
	s.reconciliationPass(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconciliationPass(ctx)
		}
	}
}

func (s *WalletService) reconciliationPass(ctx context.Context) {
	// Pick up admins flagged in the database, so the reminder fan-out reaches
	// them and the approvals link appears for them without a restart.
	if err := s.RefreshAdminIDs(ctx); err != nil {
		log.Printf("refresh wallet admins: %v", err)
	}
	if err := s.ReconcilePendingDeposits(ctx); err != nil {
		log.Printf("reconcile pending deposits: %v", err)
	}
	if err := s.ReconcileVirtualAccountTransfers(ctx); err != nil {
		log.Printf("reconcile virtual account transfers: %v", err)
	}
	if _, err := s.ExpireStaleRequests(ctx); err != nil {
		log.Printf("expire stale wallet requests: %v", err)
	}
	if err := s.RemindAgedApprovals(ctx); err != nil {
		log.Printf("remind aged wallet approvals: %v", err)
	}
}

// CancelWithdrawal lets the farmer release their own pending request and get
// the reservation back. Once an admin has begun paying it, the store refuses.
func (s *WalletService) CancelWithdrawal(ctx context.Context, farmerID int, withdrawalID int64) error {
	if farmerID <= 0 || withdrawalID <= 0 {
		return ErrRequestNotFound
	}
	err := s.repo.CancelWithdrawal(ctx, withdrawalID, farmerID)
	switch {
	case errors.Is(err, repository.ErrWithdrawalNotFound):
		return ErrRequestNotFound
	case errors.Is(err, repository.ErrRequestNotPending):
		return ErrRequestNotPending
	case err != nil:
		return fmt.Errorf("%w: %v", ErrWalletUnavailable, err)
	}
	s.notifyUser(farmerID, "Withdrawal request cancelled",
		"The withdrawal request was cancelled and the reserved money is back in your wallet.",
		"wallet_withdrawal_cancelled")
	return nil
}

// CancelTransfer is the transfer half of CancelWithdrawal.
func (s *WalletService) CancelTransfer(ctx context.Context, senderID int, transferID int64) error {
	if senderID <= 0 || transferID <= 0 {
		return ErrRequestNotFound
	}
	err := s.repo.CancelTransfer(ctx, transferID, senderID)
	switch {
	case errors.Is(err, repository.ErrTransferNotFound):
		return ErrRequestNotFound
	case errors.Is(err, repository.ErrRequestNotPending):
		return ErrRequestNotPending
	case err != nil:
		return fmt.Errorf("%w: %v", ErrTransferUnavailable, err)
	}
	s.notifyUser(senderID, "Transfer request cancelled",
		"The transfer request was cancelled and the money is back in your wallet.",
		"wallet_transfer_cancelled")
	return nil
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
	ErrTransferRecipientInvalid  = errors.New("enter a valid Nigerian phone number or Agro-Shield account number")
	ErrTransferRecipientNotFound = errors.New("no Agro-Shield account uses that phone or account number")

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

// ResolveTransferRecipient turns the phone number or Agro-Shield account number
// a sender typed into the account that will be credited.
//
// This necessarily reveals whether a number is registered, which a
// deliberately vague error could hide — but a transfer feature where the sender
// cannot tell a typo from an unknown person is unusable, and no money moves
// without a one-time passcode and an admin approval. The lookup is therefore
// reachable only from an authenticated session.
//
// A ten-digit value is checked as a virtual account number first and then as a
// phone number. Flutterwave account numbers and Nigerian subscriber numbers can
// both be ten digits, so the account number wins when it exists; an eleven-digit
// local phone or a 234-prefixed international phone skips the account lookup.
func (s *WalletService) ResolveTransferRecipient(ctx context.Context, senderID int, identifier string) (*models.Farmer, error) {
	if senderID <= 0 {
		return nil, errors.New("invalid wallet owner")
	}

	identifier = strings.TrimSpace(identifier)
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, identifier)

	if len(digits) == 10 {
		account, err := s.repo.GetVirtualAccountByAccountNumber(ctx, digits)
		switch {
		case err == nil:
			recipient, lookupErr := s.farmers.GetByID(account.FarmerID)
			if lookupErr != nil {
				return nil, fmt.Errorf("%w: look up transfer recipient: %v", ErrTransferUnavailable, lookupErr)
			}
			if recipient != nil {
				return transferRecipient(senderID, recipient)
			}
		case !errors.Is(err, repository.ErrVirtualAccountNotFound):
			return nil, fmt.Errorf("%w: look up transfer account number: %v", ErrTransferUnavailable, err)
		}
	}

	forms := recipientPhoneForms(identifier)
	if len(forms) == 0 {
		// A ten-digit value is a valid Agro-Shield account-number shape even
		// when it is not also a valid local phone form. If the account lookup
		// above found nothing, that is an unknown recipient rather than a typo.
		if len(digits) == 10 {
			return nil, ErrTransferRecipientNotFound
		}
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
		return transferRecipient(senderID, recipient)
	}
	return nil, ErrTransferRecipientNotFound
}

func transferRecipient(senderID int, recipient *models.Farmer) (*models.Farmer, error) {
	if recipient.ID == senderID {
		return nil, errors.New("you cannot transfer to your own wallet")
	}
	return recipient, nil
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
func (s *WalletService) PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64, checkoutToken string) (int64, bool, error) {
	return s.repo.PayForOrder(ctx, buyerID, cropID, quantity, checkoutToken, s.commissionBasisPoints)
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

// ResolvePayoutAccount confirms the holder of an account at one selected bank.
//
// This is the payout flow's primary path. The full Flutterwave directory gives
// the form a bank code, so one direct resolve is enough: there is no need to
// guess at a shortlist and no bank outside that shortlist becomes unsupported.
// LookupAccountBanks remains for callers that only have a number. The payout
// form uses it to suggest a bank when the account number is entered first, then
// sends the chosen code to ResolvePayoutAccount.
func (s *WalletService) ResolvePayoutAccount(ctx context.Context, accountNumber, bankCode string) (string, error) {
	accountNumber = strings.TrimSpace(accountNumber)
	bankCode = strings.TrimSpace(bankCode)
	if err := validateAccountNumber(accountNumber); err != nil {
		return "", err
	}
	if err := validateBankCode(bankCode); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	resolved, err := s.resolver.ResolveAccount(accountNumber, bankCode)
	if err != nil {
		return "", err
	}
	if resolved == nil || strings.TrimSpace(resolved.AccountName) == "" {
		return "", errors.New("the bank returned no account name")
	}
	return strings.TrimSpace(resolved.AccountName), nil
}

// bankLookupShortlist is the set of banks a lookup fans out over, in
// market-share order so the most likely match is probed first and a truncated
// sweep degrades to the banks most users actually use.
//
// A shortlist rather than the full bank directory is a deliberate trade for
// callers that have no bank code: the full directory is 200+ banks, and probing
// all of it per lookup would burn the provider's rate limit. The payout form
// uses this sweep only to suggest a bank; once a code is chosen it calls
// ResolvePayoutAccount directly, so every provider in the directory is
// supported.
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
