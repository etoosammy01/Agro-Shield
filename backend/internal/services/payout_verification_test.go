package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/repository"
)

// fakeBankDirectory stands in for the Flutterwave bank list, so the name-versus-
// code check can be decided by the test rather than by the network.
type fakeBankDirectory struct {
	banks []Bank
	err   error
}

func (f *fakeBankDirectory) ListBanks(country string) ([]Bank, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.banks, nil
}

// The directory the tests check against. It deliberately does not match
// bankLookupShortlist's names exactly - "Access Bank" here, "Guaranty Trust
// Bank" there - because the point of the check is the provider's own wording,
// not ours.
var testBankDirectory = []Bank{
	{Name: "Access Bank", Code: "044"},
	{Name: "Guaranty Trust Bank", Code: "058"},
	{Name: "Zenith Bank", Code: "057"},
}

// fakeFarmerStore supplies the saved payout details a verification reads.
type fakeFarmerStore struct {
	farmer *models.Farmer
	err    error
}

func (f *fakeFarmerStore) GetByID(id int) (*models.Farmer, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.farmer, nil
}

func (f *fakeFarmerStore) GetByPhone(phone string) (*models.Farmer, error) {
	return f.GetByID(0)
}

func (f *fakeFarmerStore) UpdateBankDetails(id int, bankName, bankCode, accountName, accountNumber string) error {
	return nil
}

// fakeWalletStore implements only what a refused payout is allowed to touch.
//
// Everything else is inherited from the embedded nil interface, so calling one
// panics and fails the test - which is the assertion: a payout that does not
// match must not reach the repository at all. The methods that must not be
// called are overridden explicitly so the failure names the call instead of
// surfacing as a nil dereference.
type fakeWalletStore struct {
	walletStore

	pending *models.Withdrawal

	createdWithdrawal bool
	beganWithdrawal   bool
	verdicts          []string
	verdictNames      []string
}

func (f *fakeWalletStore) GetWithdrawal(ctx context.Context, withdrawalID int64) (*models.Withdrawal, error) {
	if f.pending == nil {
		return nil, repository.ErrWithdrawalNotFound
	}
	return f.pending, nil
}

func (f *fakeWalletStore) CreateWithdrawal(ctx context.Context, farmerID int, reference, amount string) error {
	f.createdWithdrawal = true
	return nil
}

func (f *fakeWalletStore) BeginWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (*models.Withdrawal, error) {
	f.beganWithdrawal = true
	return nil, errors.New("BeginWithdrawal must not be reached for a payout that was refused")
}

func (f *fakeWalletStore) RecordWithdrawalVerification(ctx context.Context, reference, status, resolvedName, detail string) error {
	f.verdicts = append(f.verdicts, status)
	f.verdictNames = append(f.verdictNames, resolvedName)
	return nil
}

// testVerificationService builds a service with a scripted provider: the
// resolver decides which banks answer, the directory decides which bank names
// are real, and the cache decides whether the resolver is asked at all.
func testVerificationService(resolver accountResolver, directory bankDirectory, lookups bankLookupStore) *WalletService {
	return &WalletService{resolver: resolver, directory: directory, lookups: lookups}
}

func TestVerifyPayoutAccountAcceptsDetailsThatMatch(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "Adaeze Okonkwo", "0690000032")

	if verdict.Status != PayoutVerified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutVerified)
	}
	if verdict.ResolvedName != "ADAEZE OKONKWO" {
		t.Errorf("ResolvedName = %q, want the name the bank reported", verdict.ResolvedName)
	}
}

// The case the whole feature exists for: the account belongs to somebody else.
func TestVerifyPayoutAccountRejectsANameTheBankDoesNotHold(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "CHIDI NWOYE", "0690000032")

	if verdict.Status != PayoutMismatch {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutMismatch)
	}
	if verdict.ResolvedName != "ADAEZE OKONKWO" {
		t.Errorf("ResolvedName = %q, want it so the farmer is told what the bank has", verdict.ResolvedName)
	}
}

// The regression that matters most.
//
// A dead key, a timeout or a rate limit is not an answer about the account. If
// this ever reads as a mismatch, an expired credential refuses every farmer's
// payout while telling each of them their own details are wrong.
//
// The wording of each of these is taken from what the API actually returned, so
// the classification is pinned to real messages rather than to invented ones.
func TestVerifyPayoutAccountTreatsAProviderFailureAsUnverified(t *testing.T) {
	providers := map[string]error{
		"a rejected key": errors.New(`flutterwave resolve returned 401: {"message":"Invalid authorization key"}`),
		"a timeout":      errors.New("call flutterwave resolve: context deadline exceeded"),
		"a rate limit":   errors.New("flutterwave resolve returned 429: too many requests"),
		"an unparseable answer": errors.New(
			"flutterwave resolve response is missing the account name"),
	}

	for name, providerErr := range providers {
		t.Run(name, func(t *testing.T) {
			resolver := &fakeResolver{
				holders: map[string]string{"044": "ADAEZE OKONKWO"},
				errs:    map[string]error{"044": providerErr},
			}

			verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
				VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

			if verdict.Status != PayoutUnverified {
				t.Fatalf("verdict = %+v, want %s - a provider failure says nothing about the account",
					verdict, PayoutUnverified)
			}
			if verdict.ResolvedName != "" {
				t.Errorf("ResolvedName = %q, want empty: nothing answered", verdict.ResolvedName)
			}
		})
	}
}

// The bank answered, and the answer is that this account is not there.
func TestVerifyPayoutAccountTreatsAnAccountTheBankDoesNotHoldAsMismatch(t *testing.T) {
	resolver := &fakeResolver{} // no holders: every bank answers "no such account"

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutMismatch {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutMismatch)
	}
}

// A bank name the provider spells differently is flagged, not refused.
//
// This is weaker than it looks like it should be, and deliberately so: the bank
// directory differs between the provider's test and live modes and changes when
// a bank rebrands, while the name we hold was copied from whatever list was
// current when it was saved. Read with this project's key, the directory already
// disagrees with our constants on four names. Refusing on that would hold a
// farmer's money over our own stale constant. What the transfer follows is the
// code, and the code is checked where it matters - a number that does not exist
// at it is a refusal.
func TestVerifyPayoutAccountFlagsABankNameTheProviderSpellsDifferently(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Zenith Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutUnverified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutUnverified)
	}
	// The admin has to be able to see which name the provider uses, or the flag
	// is not actionable.
	if !strings.Contains(verdict.Detail, "Access Bank") || !strings.Contains(verdict.Detail, "Zenith Bank") {
		t.Errorf("Detail = %q, want both the provider's spelling and the one on file", verdict.Detail)
	}
	if verdict.ResolvedName != "ADAEZE OKONKWO" {
		t.Errorf("ResolvedName = %q, want the holder the bank confirmed", verdict.ResolvedName)
	}
}

// A code the provider's list does not carry is flagged too, for the same reason:
// the list is mode-dependent, so an absent code may be one it simply does not
// publish, and the transfer is the authority on whether the code works.
func TestVerifyPayoutAccountFlagsABankCodeOutsideTheDirectory(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"999": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Closed Bank", "999", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutUnverified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutUnverified)
	}
	if verdict.ResolvedName != "ADAEZE OKONKWO" {
		t.Errorf("ResolvedName = %q, want the holder the bank confirmed", verdict.ResolvedName)
	}
}

// A stale bank name must not be able to mask the check that matters. The
// provider's answer about who holds the account is what blocks money, so it is
// asked first and its mismatch wins - otherwise a farmer with a stale name and
// somebody else's account number would only ever see the cosmetic flag.
func TestVerifyPayoutAccountLetsAHolderMismatchOutrankAStaleBankName(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Zenith Bank", "044", "CHIDI NWOYE", "0690000032")

	if verdict.Status != PayoutMismatch {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutMismatch)
	}
	if verdict.ResolvedName != "ADAEZE OKONKWO" {
		t.Errorf("ResolvedName = %q, want the registered holder", verdict.ResolvedName)
	}
}

// A malformed number or code must never reach the provider, and must be refused
// rather than waved through.
func TestVerifyPayoutAccountRejectsMalformedDetails(t *testing.T) {
	tests := []struct {
		name          string
		bankCode      string
		accountName   string
		accountNumber string
	}{
		{"short number", "044", "ADAEZE OKONKWO", "069000003"},
		{"non-numeric number", "044", "ADAEZE OKONKWO", "06900000ab"},
		{"missing code", "", "ADAEZE OKONKWO", "0690000032"},
		{"missing name", "044", "", "0690000032"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

			verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, newFakeLookupStore()).
				VerifyPayoutAccount(context.Background(), "Access Bank", test.bankCode, test.accountName, test.accountNumber)

			if verdict.Status != PayoutMismatch {
				t.Fatalf("verdict = %+v, want %s", verdict, PayoutMismatch)
			}
			if resolver.callCount() != 0 {
				t.Errorf("made %d resolve calls, want 0", resolver.callCount())
			}
		})
	}
}

// A name already confirmed for this number at this bank is reused, so a payout
// whose details were checked when they were saved costs nothing to check again.
func TestVerifyPayoutAccountReusesTheCachedName(t *testing.T) {
	cached := newFakeLookupStore()
	cached.rows = []repository.BankLookup{{
		AccountNumber: "0690000032",
		BankCode:      "044",
		BankName:      "Access Bank",
		AccountName:   "ADAEZE OKONKWO",
	}}
	resolver := &fakeResolver{holders: map[string]string{"044": "SOMEBODY ELSE"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, cached).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutVerified {
		t.Fatalf("verdict = %+v, want %s from the cached name", verdict, PayoutVerified)
	}
	if resolver.callCount() != 0 {
		t.Errorf("made %d resolve calls, want 0 - the cache already holds this pair", resolver.callCount())
	}
}

// A cache that cannot be read is not a reason to accuse anyone: the check is
// skipped and reported as unverified.
func TestVerifyPayoutAccountTreatsAnUnreadableCacheAsUnverified(t *testing.T) {
	lookups := newFakeLookupStore()
	lookups.findOneErr = errors.New("database is down")
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{banks: testBankDirectory}, lookups).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutUnverified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutUnverified)
	}
}

// An unreadable bank list must read as "could not check", never as "wrong bank".
func TestVerifyPayoutAccountTreatsAnUnreadableDirectoryAsUnverified(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	directory := &fakeBankDirectory{err: errors.New("flutterwave bank list returned 401")}

	verdict := testVerificationService(resolver, directory, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutUnverified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutUnverified)
	}
}

// An empty bank list is the provider failing to answer, not proof that the
// farmer's bank does not exist - so it must not refuse the payout either.
func TestVerifyPayoutAccountTreatsAnEmptyDirectoryAsUnverified(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}

	verdict := testVerificationService(resolver, &fakeBankDirectory{}, newFakeLookupStore()).
		VerifyPayoutAccount(context.Background(), "Access Bank", "044", "ADAEZE OKONKWO", "0690000032")

	if verdict.Status != PayoutUnverified {
		t.Fatalf("verdict = %+v, want %s", verdict, PayoutUnverified)
	}
}

// --- the request path -----------------------------------------------------

func testRequestService(store *fakeWalletStore, farmer *models.Farmer, resolver accountResolver) *WalletService {
	return &WalletService{
		repo:      store,
		farmers:   &fakeFarmerStore{farmer: farmer},
		resolver:  resolver,
		directory: &fakeBankDirectory{banks: testBankDirectory},
		lookups:   newFakeLookupStore(),
	}
}

// A withdrawal whose details do not match must be refused before the repository
// is touched: CreateWithdrawal reserves the funds and snapshots these very
// fields in one transaction, so a check after it would leave money held against
// an account the farmer cannot be paid at.
func TestRequestWithdrawalRefusesAMismatchWithoutReservingFunds(t *testing.T) {
	store := &fakeWalletStore{}
	service := testRequestService(store,
		&models.Farmer{ID: 7, BankName: "Access Bank", BankCode: "044",
			AccountName: "CHIDI NWOYE", AccountNumber: "0690000032"},
		&fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}})

	err := service.RequestWithdrawal(context.Background(), 7, "5000")

	if !errors.Is(err, ErrPayoutDetailsMismatch) {
		t.Fatalf("RequestWithdrawal() error = %v, want %v", err, ErrPayoutDetailsMismatch)
	}
	if store.createdWithdrawal {
		t.Fatal("RequestWithdrawal() wrote a withdrawal for details that do not match")
	}
}

// Details that are fine must still be payable, and the verdict has to be kept so
// an admin can see the payout was checked.
func TestRequestWithdrawalRecordsAVerifiedVerdict(t *testing.T) {
	store := &fakeWalletStore{}
	service := testRequestService(store,
		&models.Farmer{ID: 7, BankName: "Access Bank", BankCode: "044",
			AccountName: "Adaeze Okonkwo", AccountNumber: "0690000032"},
		&fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}})

	if err := service.RequestWithdrawal(context.Background(), 7, "5000"); err != nil {
		t.Fatalf("RequestWithdrawal() error = %v", err)
	}
	if !store.createdWithdrawal {
		t.Fatal("RequestWithdrawal() did not write the withdrawal")
	}
	if len(store.verdicts) != 1 || store.verdicts[0] != PayoutVerified {
		t.Fatalf("recorded verdicts = %v, want one %s", store.verdicts, PayoutVerified)
	}
}

// A provider we cannot reach must not stop the money. The payout proceeds and is
// flagged, which is the whole reason the verdict has three states rather than two.
func TestRequestWithdrawalProceedsWhenTheProviderCouldNotBeReached(t *testing.T) {
	store := &fakeWalletStore{}
	service := testRequestService(store,
		&models.Farmer{ID: 7, BankName: "Access Bank", BankCode: "044",
			AccountName: "ADAEZE OKONKWO", AccountNumber: "0690000032"},
		&fakeResolver{errs: map[string]error{
			"044": errors.New(`flutterwave resolve returned 401: {"message":"Invalid authorization key"}`),
		}})

	if err := service.RequestWithdrawal(context.Background(), 7, "5000"); err != nil {
		t.Fatalf("RequestWithdrawal() error = %v, want the payout to proceed while unverified", err)
	}
	if !store.createdWithdrawal {
		t.Fatal("RequestWithdrawal() did not write the withdrawal")
	}
	if len(store.verdicts) != 1 || store.verdicts[0] != PayoutUnverified {
		t.Fatalf("recorded verdicts = %v, want one %s", store.verdicts, PayoutUnverified)
	}
}

// Nothing saved yet is not a mismatch. The repository's own message tells the
// farmer to save their details, and refusing here would instead tell them the
// details they have not entered are wrong.
func TestRequestWithdrawalLetsTheRepositoryExplainMissingDetails(t *testing.T) {
	store := &fakeWalletStore{}
	service := testRequestService(store, &models.Farmer{ID: 7}, &fakeResolver{})

	if err := service.RequestWithdrawal(context.Background(), 7, "5000"); err != nil {
		t.Fatalf("RequestWithdrawal() error = %v", err)
	}
	if !store.createdWithdrawal {
		t.Fatal("RequestWithdrawal() did not defer to the repository for the missing-details message")
	}
}

// --- the approval path ----------------------------------------------------

// The trap this ordering exists for: BeginWithdrawal flips the row to
// 'processing', and RejectWithdrawal only matches 'pending'. A payout refused
// after that point would hold the farmer's money with no way to release it, so
// the refusal has to happen while the row is still pending.
func TestApproveWithdrawalRefusesAMismatchBeforeReservingFunds(t *testing.T) {
	store := &fakeWalletStore{pending: &models.Withdrawal{
		ID: 3, FarmerID: 7, Reference: "WITHDRAW-7-1", Amount: "5000", Currency: "NGN",
		BankName: "Access Bank", BankCode: "044",
		AccountName: "CHIDI NWOYE", AccountNumber: "0690000032",
		Status: "pending",
	}}
	service := &WalletService{
		repo:      store,
		resolver:  &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}},
		directory: &fakeBankDirectory{banks: testBankDirectory},
		lookups:   newFakeLookupStore(),
		adminIDs:  map[int]struct{}{1: {}},
	}

	err := service.ApproveWithdrawal(context.Background(), 3, 1)

	if !errors.Is(err, ErrPayoutDetailsMismatch) {
		t.Fatalf("ApproveWithdrawal() error = %v, want %v", err, ErrPayoutDetailsMismatch)
	}
	if store.beganWithdrawal {
		t.Fatal("ApproveWithdrawal() began the payout for details that do not match")
	}
	if len(store.verdicts) != 1 || store.verdicts[0] != PayoutMismatch {
		t.Fatalf("recorded verdicts = %v, want one %s so the admin sees why it was refused",
			store.verdicts, PayoutMismatch)
	}
}

// The refusal has to reach the admin in a form that tells them what to do: the
// row is still pending, so rejecting it is what returns the reserved funds.
func TestApproveWithdrawalTellsTheAdminHowToReleaseTheFunds(t *testing.T) {
	store := &fakeWalletStore{pending: &models.Withdrawal{
		ID: 3, FarmerID: 7, Reference: "WITHDRAW-7-1", Amount: "5000", Currency: "NGN",
		BankName: "Access Bank", BankCode: "044",
		AccountName: "CHIDI NWOYE", AccountNumber: "0690000032",
		Status: "pending",
	}}
	service := &WalletService{
		repo:      store,
		resolver:  &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}},
		directory: &fakeBankDirectory{banks: testBankDirectory},
		lookups:   newFakeLookupStore(),
		adminIDs:  map[int]struct{}{1: {}},
	}

	err := service.ApproveWithdrawal(context.Background(), 3, 1)
	if err == nil {
		t.Fatal("ApproveWithdrawal() approved a payout whose details do not match")
	}
	if !strings.Contains(err.Error(), "Reject") {
		t.Errorf("error = %q, want it to tell the admin that rejecting releases the funds", err)
	}
	if !strings.Contains(err.Error(), "ADAEZE OKONKWO") {
		t.Errorf("error = %q, want it to name the holder the bank has", err)
	}
}
