package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
)

// fakeVirtualAccountStore keeps the virtual-account state in memory. Only the
// methods the setup and settlement paths call are implemented; the embedded
// nil walletStore makes any unexpected call panic instead of quietly passing.
type fakeVirtualAccountStore struct {
	walletStore

	account *models.WalletVirtualAccount

	ensureCreated bool
	resetCalled   bool
	failedReason  string
	checked       bool

	creditCalls    int
	creditPaid     int
	creditRef      string
	creditTxnID    string
	creditAmount   string
	creditCurrency string

	pendingRecorded *models.PendingVirtualAccountTransfer
	failedStatus    string
	reversed        bool
	reverseErr      error
}

func (f *fakeVirtualAccountStore) GetVirtualAccount(ctx context.Context, farmerID int) (*models.WalletVirtualAccount, error) {
	if f.account == nil || f.account.FarmerID != farmerID {
		return nil, repository.ErrVirtualAccountNotFound
	}
	copied := *f.account
	return &copied, nil
}

func (f *fakeVirtualAccountStore) GetVirtualAccountByTxRef(ctx context.Context, txRef string) (*models.WalletVirtualAccount, error) {
	if f.account == nil || f.account.TxRef != txRef {
		return nil, repository.ErrVirtualAccountNotFound
	}
	copied := *f.account
	return &copied, nil
}

func (f *fakeVirtualAccountStore) EnsureVirtualAccountAttempt(ctx context.Context, farmerID int, provider, txRef, identityKind, consentVersion string, now time.Time) (*models.WalletVirtualAccount, bool, error) {
	created := f.account == nil
	if created {
		f.account = &models.WalletVirtualAccount{
			ID:             1,
			FarmerID:       farmerID,
			Provider:       provider,
			TxRef:          txRef,
			Status:         models.VirtualAccountPending,
			IdentityKind:   identityKind,
			ConsentVersion: consentVersion,
			ConsentAt:      now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		f.ensureCreated = true
	}
	copied := *f.account
	return &copied, created, nil
}

func (f *fakeVirtualAccountStore) ResetVirtualAccountAttempt(ctx context.Context, farmerID int, identityKind, consentVersion string, now time.Time) error {
	f.resetCalled = true
	f.failedReason = ""
	f.account.Status = models.VirtualAccountPending
	f.account.IdentityKind = identityKind
	f.account.ConsentVersion = consentVersion
	f.account.UpdatedAt = now
	return nil
}

func (f *fakeVirtualAccountStore) ActivateVirtualAccount(ctx context.Context, farmerID int, orderRef, providerRef, accountNumber, bankName, accountName, currency string, now time.Time) error {
	f.account.Status = models.VirtualAccountActive
	f.account.OrderRef = orderRef
	f.account.ProviderRef = providerRef
	f.account.AccountNumber = accountNumber
	f.account.BankName = bankName
	f.account.AccountName = accountName
	f.account.Currency = currency
	f.account.UpdatedAt = now
	return nil
}

func (f *fakeVirtualAccountStore) FailVirtualAccountAttempt(ctx context.Context, farmerID int, reason string, now time.Time) error {
	f.failedReason = reason
	f.account.Status = models.VirtualAccountFailed
	f.account.UpdatedAt = now
	return nil
}

func (f *fakeVirtualAccountStore) MarkVirtualAccountChecked(ctx context.Context, farmerID int, now time.Time) error {
	f.checked = true
	return nil
}

func (f *fakeVirtualAccountStore) CreditVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) (bool, error) {
	f.creditCalls++
	f.creditRef = reference
	f.creditTxnID = providerTxnID
	f.creditAmount = amount
	f.creditCurrency = currency
	if f.creditPaid == 1 {
		return false, nil
	}
	f.creditPaid++
	return true, nil
}

func (f *fakeVirtualAccountStore) RecordPendingVirtualAccountDeposit(ctx context.Context, farmerID int, reference, providerTxnID, amount, currency string) error {
	f.pendingRecorded = &models.PendingVirtualAccountTransfer{
		FarmerID:              farmerID,
		Reference:             reference,
		ProviderTransactionID: providerTxnID,
		Amount:                amount,
		Currency:              currency,
	}
	return nil
}

func (f *fakeVirtualAccountStore) FailVirtualAccountDeposit(ctx context.Context, providerTxnID, status string, now time.Time) error {
	f.failedStatus = status
	return nil
}

func (f *fakeVirtualAccountStore) ReverseVirtualAccountDeposit(ctx context.Context, providerTxnID, amount, currency string, now time.Time) (bool, error) {
	if f.reverseErr != nil {
		return false, f.reverseErr
	}
	f.reversed = true
	return true, nil
}

// fakeVirtualAccountProvider stands in for Flutterwave. Each call is counted so
// the duplicate-prevention tests can prove the provider was not contacted twice.
type fakeVirtualAccountProvider struct {
	createCalls int
	lookupCalls int
	lastCreate  CreateVirtualAccountRequest

	create func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error)
	lookup func(string) (*VirtualAccountDetails, error)
}

func (f *fakeVirtualAccountProvider) CreatePermanentVirtualAccount(req CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
	f.createCalls++
	f.lastCreate = req
	if f.create == nil {
		return nil, errors.New("unexpected create call")
	}
	return f.create(req)
}

func (f *fakeVirtualAccountProvider) GetPermanentVirtualAccount(orderRef string) (*VirtualAccountDetails, error) {
	f.lookupCalls++
	if f.lookup == nil {
		return nil, ErrVirtualAccountNotFound
	}
	return f.lookup(orderRef)
}

func testVirtualFarmer() *models.Farmer {
	return &models.Farmer{
		ID:       7,
		FullName: "Adaeze Okonkwo",
		Email:    "adaeze@example.test",
		Phone:    "08000000000",
	}
}

func newVirtualAccountService(store *fakeVirtualAccountStore, provider *fakeVirtualAccountProvider, notifier *fakeNotifier) *WalletService {
	service := NewWalletService(store, nil, nil, nil, nil, 0, notifier, 0)
	service.virtualAccounts = provider
	return service
}

func TestSetupVirtualAccountRequiresConsentBeforeContactingTheProvider(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{}
	service := newVirtualAccountService(store, provider, nil)

	_, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
	})
	if !errors.Is(err, ErrVirtualAccountConsentRequired) {
		t.Fatalf("error = %v, want ErrVirtualAccountConsentRequired", err)
	}
	if provider.createCalls != 0 || provider.lookupCalls != 0 {
		t.Fatalf("provider was contacted before consent: create=%d lookup=%d", provider.createCalls, provider.lookupCalls)
	}
	if store.account != nil {
		t.Fatal("a pending account row was stored before consent")
	}
}

func TestSetupVirtualAccountRejectsBadIdentityWithoutEchoingIt(t *testing.T) {
	for _, setup := range []VirtualAccountSetup{
		{IdentityKind: "passport", IdentityNumber: "22222222222", Consent: true},
		{IdentityKind: "bvn", IdentityNumber: "2222222", Consent: true},
		{IdentityKind: "nin", IdentityNumber: "2222222222a", Consent: true},
	} {
		store := &fakeVirtualAccountStore{}
		provider := &fakeVirtualAccountProvider{}
		service := newVirtualAccountService(store, provider, nil)

		_, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), setup)
		if !errors.Is(err, ErrVirtualAccountIdentityInvalid) {
			t.Fatalf("setup %+v error = %v, want ErrVirtualAccountIdentityInvalid", setup, err)
		}
		if strings.Contains(err.Error(), setup.IdentityNumber) {
			t.Fatalf("identity number was echoed in the error: %v", err)
		}
		if provider.createCalls != 0 {
			t.Fatal("provider was contacted with an invalid identity")
		}
	}
}

func TestSetupVirtualAccountRequiresProfileEmailAndName(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)
	setup := VirtualAccountSetup{IdentityKind: "bvn", IdentityNumber: "22222222222", Consent: true}

	farmer := testVirtualFarmer()
	farmer.Email = " "
	if _, err := service.SetupVirtualAccount(context.Background(), farmer, setup); !errors.Is(err, ErrVirtualAccountEmailRequired) {
		t.Fatalf("error = %v, want ErrVirtualAccountEmailRequired", err)
	}

	farmer = testVirtualFarmer()
	farmer.FullName = "  "
	if _, err := service.SetupVirtualAccount(context.Background(), farmer, setup); !errors.Is(err, ErrVirtualAccountNameRequired) {
		t.Fatalf("error = %v, want ErrVirtualAccountNameRequired", err)
	}
}

func TestSetupVirtualAccountReturnsAnActiveAccountWithoutCallingTheProvider(t *testing.T) {
	store := &fakeVirtualAccountStore{account: &models.WalletVirtualAccount{
		ID:            1,
		FarmerID:      7,
		TxRef:         "AGRO-VA-7",
		AccountNumber: "0123456789",
		BankName:      "Flutterwave Bank",
		Status:        models.VirtualAccountActive,
	}}
	provider := &fakeVirtualAccountProvider{}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if !account.Ready() || account.AccountNumber != "0123456789" {
		t.Fatalf("account = %+v, want the existing active account", account)
	}
	if provider.createCalls != 0 || provider.lookupCalls != 0 {
		t.Fatalf("provider was contacted for an active account: create=%d lookup=%d", provider.createCalls, provider.lookupCalls)
	}
}

func TestSetupVirtualAccountUsesServerSideIdentityAndDeterministicReference(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{
		lookup: func(string) (*VirtualAccountDetails, error) { return nil, ErrVirtualAccountNotFound },
		create: func(req CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
			return &VirtualAccountDetails{
				OrderRef:      "URF_VA_7",
				ProviderRef:   "FLW-VA-7",
				AccountNumber: "0123456789",
				BankName:      "Flutterwave Bank",
				AccountName:   "ADAEZE OKONKWO",
				Currency:      "NGN",
			}, nil
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "BVN",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if !account.Ready() {
		t.Fatalf("account = %+v, want an active account", account)
	}
	if provider.lastCreate.TxRef != "AGRO-VA-7" {
		t.Errorf("tx_ref = %q, want the deterministic farmer reference", provider.lastCreate.TxRef)
	}
	if provider.lastCreate.Email != "adaeze@example.test" ||
		provider.lastCreate.FirstName != "Adaeze" || provider.lastCreate.LastName != "Okonkwo" ||
		provider.lastCreate.Phone != "08000000000" {
		t.Errorf("provider request used fields that did not come from the farmer: %+v", provider.lastCreate)
	}
	if provider.lastCreate.IdentityKind != "bvn" || provider.lastCreate.IdentityNumber != "22222222222" {
		t.Errorf("identity = %q/%q, want the submitted BVN", provider.lastCreate.IdentityKind, provider.lastCreate.IdentityNumber)
	}
	if store.account.IdentityKind != "bvn" {
		t.Errorf("stored identity kind = %q, want bvn", store.account.IdentityKind)
	}
	if store.account.ConsentVersion != VirtualAccountConsentVersion || store.account.ConsentAt.IsZero() {
		t.Error("consent version and time were not recorded")
	}
	if provider.createCalls != 1 {
		t.Errorf("create calls = %d, want exactly one", provider.createCalls)
	}
	// A brand-new attempt has nothing to look up, so setup must not depend on the
	// provider's single-account read endpoint.
	if provider.lookupCalls != 0 {
		t.Errorf("lookup calls = %d, want none for a first attempt", provider.lookupCalls)
	}
}

func TestSetupVirtualAccountDoesNotRetryWhileAnAttemptIsRecent(t *testing.T) {
	now := time.Now()
	store := &fakeVirtualAccountStore{account: &models.WalletVirtualAccount{
		ID:        1,
		FarmerID:  7,
		TxRef:     "AGRO-VA-7",
		Status:    models.VirtualAccountPending,
		UpdatedAt: now.Add(-30 * time.Second),
	}}
	provider := &fakeVirtualAccountProvider{}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if account.Status != models.VirtualAccountPending {
		t.Fatalf("status = %q, want pending", account.Status)
	}
	if provider.lookupCalls != 0 || provider.createCalls != 0 {
		t.Fatalf("a recent attempt was retried: lookup=%d create=%d", provider.lookupCalls, provider.createCalls)
	}
}

func TestSetupVirtualAccountAdoptsAnAccountFoundAfterAnUnansweredAttempt(t *testing.T) {
	store := &fakeVirtualAccountStore{account: &models.WalletVirtualAccount{
		ID:        1,
		FarmerID:  7,
		TxRef:     "AGRO-VA-7",
		OrderRef:  "URF_VA_7",
		Status:    models.VirtualAccountPending,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}}
	provider := &fakeVirtualAccountProvider{
		lookup: func(ref string) (*VirtualAccountDetails, error) {
			if ref != "URF_VA_7" {
				t.Errorf("lookup reference = %q, want the stored order_ref", ref)
			}
			return &VirtualAccountDetails{
				OrderRef:      "URF_VA_7",
				AccountNumber: "0123456789",
				BankName:      "Flutterwave Bank",
				Currency:      "NGN",
			}, nil
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if !account.Ready() || account.AccountNumber != "0123456789" {
		t.Fatalf("account = %+v, want the adopted account", account)
	}
	if provider.createCalls != 0 {
		t.Fatalf("create calls = %d, want none when an existing account was found", provider.createCalls)
	}
	if store.resetCalled {
		t.Fatal("the attempt was reset even though an account existed")
	}
}

func TestSetupVirtualAccountDoesNotCreateWhenTheLookupIsUnanswered(t *testing.T) {
	store := &fakeVirtualAccountStore{account: &models.WalletVirtualAccount{
		ID:        1,
		FarmerID:  7,
		TxRef:     "AGRO-VA-7",
		Status:    models.VirtualAccountPending,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}}
	provider := &fakeVirtualAccountProvider{
		lookup: func(string) (*VirtualAccountDetails, error) {
			return nil, errors.New("connection reset")
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if account.Status != models.VirtualAccountPending {
		t.Fatalf("status = %q, want pending", account.Status)
	}
	if provider.createCalls != 0 {
		t.Fatalf("create calls = %d, want none after an unanswered lookup", provider.createCalls)
	}
}

func TestSetupVirtualAccountMarksADefiniteRefusalAsFailed(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{
		lookup: func(string) (*VirtualAccountDetails, error) { return nil, ErrVirtualAccountNotFound },
		create: func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
			return nil, fmt.Errorf("%w: merchant not enabled", ErrVirtualAccountRefused)
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	_, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if !errors.Is(err, ErrVirtualAccountRejected) {
		t.Fatalf("error = %v, want ErrVirtualAccountRejected", err)
	}
	if store.account.Status != models.VirtualAccountFailed || store.failedReason == "" {
		t.Fatalf("stored status = %q reason = %q, want failed with a reason", store.account.Status, store.failedReason)
	}
	if strings.Contains(store.failedReason, "22222222222") {
		t.Fatal("identity number leaked into the stored failure reason")
	}
}

func TestSetupVirtualAccountKeepsAnUnansweredCreationPending(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{
		lookup: func(string) (*VirtualAccountDetails, error) { return nil, ErrVirtualAccountNotFound },
		create: func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
			return nil, errors.New("context deadline exceeded")
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("SetupVirtualAccount() error = %v", err)
	}
	if account.Status != models.VirtualAccountPending {
		t.Fatalf("status = %q, want pending after an unanswered creation", account.Status)
	}
	if store.failedReason != "" {
		t.Fatal("an unanswered creation was recorded as a refusal")
	}
}

func TestSetupVirtualAccountRetryReusesTheSameReference(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{
		lookup: func(string) (*VirtualAccountDetails, error) { return nil, ErrVirtualAccountNotFound },
		create: func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
			return nil, errors.New("timeout")
		},
	}
	service := newVirtualAccountService(store, provider, nil)
	setup := VirtualAccountSetup{IdentityKind: "bvn", IdentityNumber: "22222222222", Consent: true}

	if _, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), setup); err != nil {
		t.Fatalf("first setup error = %v", err)
	}
	// Age the attempt past the cooldown, then retry as a farmer would.
	store.account.UpdatedAt = time.Now().Add(-time.Hour)
	if _, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), setup); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if provider.createCalls != 2 {
		t.Fatalf("create calls = %d, want two attempts", provider.createCalls)
	}
	if store.account.TxRef != "AGRO-VA-7" || provider.lastCreate.TxRef != "AGRO-VA-7" {
		t.Fatalf("retry used a new reference: stored=%q sent=%q", store.account.TxRef, provider.lastCreate.TxRef)
	}
}

func TestVerifiedAmountKoboIsExact(t *testing.T) {
	tests := []struct {
		amount float64
		want   int64
		ok     bool
	}{
		{10.10, 1010, true},
		{0.01, 1, true},
		{250000, 25000000, true},
		{10.005, 0, false},
		{0, 0, false},
		{-1, 0, false},
		{math.NaN(), 0, false},
		{math.Inf(1), 0, false},
	}
	for _, tc := range tests {
		got, err := verifiedAmountKobo(tc.amount)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("verifiedAmountKobo(%v) = %d, %v; want %d", tc.amount, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("verifiedAmountKobo(%v) = %d, want an error", tc.amount, got)
		}
	}
	if got := koboAmount(1010); got != "10.10" {
		t.Errorf("koboAmount(1010) = %q, want 10.10", got)
	}
}

func activeVirtualAccount() *models.WalletVirtualAccount {
	return &models.WalletVirtualAccount{
		ID:            1,
		FarmerID:      7,
		Provider:      models.ProviderFlutterwave,
		TxRef:         "AGRO-VA-7",
		AccountNumber: "0123456789",
		Currency:      "NGN",
		Status:        models.VirtualAccountActive,
	}
}

func TestSettleVirtualAccountTransferCreditsExactlyOnce(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	notifier := &fakeNotifier{}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, notifier)

	verified := &VerifiedTransaction{
		TransactionID: "987654",
		TxRef:         "AGRO-VA-7",
		Status:        "successful",
		Amount:        1500.25,
		Currency:      "NGN",
	}
	settled, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil || !settled {
		t.Fatalf("settle = %v, %v; want terminal success", settled, err)
	}
	if store.creditRef != "VA-7-987654" || store.creditTxnID != "987654" {
		t.Errorf("credit reference = %q txn = %q, want the provider transaction id", store.creditRef, store.creditTxnID)
	}
	if store.creditAmount != "1500.25" || store.creditCurrency != "NGN" {
		t.Errorf("credited %s %s, want exactly 1500.25 NGN", store.creditCurrency, store.creditAmount)
	}
	if !notifier.said("wallet_deposit_successful", "1500.25") {
		t.Errorf("notifications = %v, want a credit notice with the exact amount", notifier.sent)
	}

	// A redelivered webhook asks the store to credit again. The store refuses,
	// and no second notification is sent.
	settled, err = service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil || !settled {
		t.Fatalf("duplicate settle = %v, %v; want terminal success", settled, err)
	}
	if store.creditCalls != 2 || store.creditPaid != 1 {
		t.Fatalf("credit calls = %d paid = %d, want two calls and one credit", store.creditCalls, store.creditPaid)
	}
	if got := len(notifier.sent); got != 1 {
		t.Fatalf("notifications = %d, want only the first credit to notify", got)
	}
}

func TestSettleVirtualAccountTransferRejectsMismatchedOrForeignReferences(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	verified := &VerifiedTransaction{TransactionID: "1", TxRef: "AGRO-VA-7", Status: "successful", Amount: 10, Currency: "NGN"}
	if _, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-8"); !errors.Is(err, ErrReferenceMismatch) {
		t.Fatalf("mismatched webhook reference error = %v, want ErrReferenceMismatch", err)
	}

	foreign := &VerifiedTransaction{TransactionID: "1", TxRef: "SOMEONE-ELSE", Status: "successful", Amount: 10, Currency: "NGN"}
	if _, err := service.SettleVirtualAccountTransfer(context.Background(), foreign, "SOMEONE-ELSE"); err == nil {
		t.Fatal("a transaction without a funding account was settled")
	}
	if store.creditCalls != 0 {
		t.Fatalf("credit calls = %d, want none", store.creditCalls)
	}
}

func TestSettleVirtualAccountTransferRequiresAnActiveNGNAccount(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	store.account.Status = models.VirtualAccountPending
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	verified := &VerifiedTransaction{TransactionID: "1", TxRef: "AGRO-VA-7", Status: "successful", Amount: 10, Currency: "NGN"}
	if _, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7"); err == nil {
		t.Fatal("a pending account accepted a transfer")
	}

	store.account = activeVirtualAccount()
	verified.Currency = "USD"
	if _, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7"); err == nil {
		t.Fatal("a non-NGN transfer was accepted")
	}
	if store.creditCalls != 0 {
		t.Fatalf("credit calls = %d, want none", store.creditCalls)
	}
}

func TestSettleVirtualAccountTransferRejectsInexactAmounts(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	verified := &VerifiedTransaction{TransactionID: "1", TxRef: "AGRO-VA-7", Status: "successful", Amount: 10.005, Currency: "NGN"}
	if _, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7"); err == nil {
		t.Fatal("an amount with sub-kobo precision was accepted")
	}
	if store.creditCalls != 0 {
		t.Fatalf("credit calls = %d, want none", store.creditCalls)
	}
}

func TestSettleVirtualAccountTransferRecordsPendingWithoutCrediting(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	notifier := &fakeNotifier{}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, notifier)

	verified := &VerifiedTransaction{TransactionID: "555", TxRef: "AGRO-VA-7", Status: "pending", Amount: 42, Currency: "NGN"}
	settled, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if settled {
		t.Fatal("a pending transfer was treated as terminal")
	}
	if store.creditCalls != 0 {
		t.Fatalf("credit calls = %d, want none for a pending transfer", store.creditCalls)
	}
	if store.pendingRecorded == nil || store.pendingRecorded.ProviderTransactionID != "555" ||
		store.pendingRecorded.Amount != "42.00" {
		t.Fatalf("pending record = %+v, want the verified transfer", store.pendingRecorded)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("notifications = %v, want none for a pending transfer", notifier.sent)
	}
}

func TestSettleVirtualAccountTransferClosesFailedPayments(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	verified := &VerifiedTransaction{TransactionID: "556", TxRef: "AGRO-VA-7", Status: "failed", Amount: 42, Currency: "NGN"}
	settled, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil || !settled {
		t.Fatalf("settle = %v, %v; want terminal failure", settled, err)
	}
	if store.failedStatus != models.DepositFailed {
		t.Errorf("stored status = %q, want failed", store.failedStatus)
	}
	if store.creditCalls != 0 {
		t.Fatalf("credit calls = %d, want none", store.creditCalls)
	}
}

func TestSettleVirtualAccountTransferReversesWithoutOverdrawing(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	notifier := &fakeNotifier{}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, notifier)

	verified := &VerifiedTransaction{TransactionID: "557", TxRef: "AGRO-VA-7", Status: "reversed", Amount: 25, Currency: "NGN"}
	settled, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil || !settled {
		t.Fatalf("settle = %v, %v; want terminal reversal", settled, err)
	}
	if !store.reversed {
		t.Fatal("the reversal was not recorded")
	}
	if !notifier.said("wallet_deposit_reversed", "25.00") {
		t.Errorf("notifications = %v, want a reversal notice", notifier.sent)
	}

	// The store refuses to debit a wallet that no longer covers the amount.
	// That is a reconciliation problem, not a reason to fail the webhook.
	store.reverseErr = repository.ErrDepositReversalUnfunded
	settled, err = service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil || !settled {
		t.Fatalf("unfunded reversal = %v, %v; want a handled terminal result", settled, err)
	}
}

func TestSettleVirtualAccountTransferLeavesUnknownStatesForRetry(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	verified := &VerifiedTransaction{TransactionID: "558", TxRef: "AGRO-VA-7", Status: "queued-for-review", Amount: 25, Currency: "NGN"}
	settled, err := service.SettleVirtualAccountTransfer(context.Background(), verified, "AGRO-VA-7")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if settled {
		t.Fatal("an unknown provider state was treated as terminal")
	}
	if store.creditCalls != 0 || store.failedStatus != "" || store.reversed {
		t.Fatal("an unknown provider state changed the ledger")
	}
}

func TestIsVirtualAccountTransferUsesStoredReferencesOnly(t *testing.T) {
	store := &fakeVirtualAccountStore{account: activeVirtualAccount()}
	service := newVirtualAccountService(store, &fakeVirtualAccountProvider{}, nil)

	if ok, err := service.IsVirtualAccountTransfer(context.Background(), "AGRO-VA-7"); err != nil || !ok {
		t.Fatalf("stored reference = %v, %v; want true", ok, err)
	}
	if ok, err := service.IsVirtualAccountTransfer(context.Background(), "AGRO-VA-8"); err != nil || ok {
		t.Fatalf("unknown reference = %v, %v; want false", ok, err)
	}
	if ok, err := service.IsVirtualAccountTransfer(context.Background(), "  "); err != nil || ok {
		t.Fatalf("blank reference = %v, %v; want false", ok, err)
	}
}

func TestSetupVirtualAccountReportsADeniedMerchantWithoutBlamingIdentity(t *testing.T) {
	store := &fakeVirtualAccountStore{}
	provider := &fakeVirtualAccountProvider{
		create: func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
			return nil, fmt.Errorf("%w: invalid key", ErrVirtualAccountAccessDenied)
		},
	}
	service := newVirtualAccountService(store, provider, nil)

	_, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if !errors.Is(err, ErrVirtualAccountUnavailable) {
		t.Fatalf("error = %v, want ErrVirtualAccountUnavailable", err)
	}
	if errors.Is(err, ErrVirtualAccountRejected) || errors.Is(err, ErrVirtualAccountIdentityInvalid) {
		t.Fatalf("a merchant permission failure was reported as the farmer's fault: %v", err)
	}
	if store.account.Status != models.VirtualAccountFailed {
		t.Fatalf("status = %q, want failed", store.account.Status)
	}
	if store.failedReason == "" || strings.Contains(store.failedReason, "22222222222") {
		t.Fatalf("failure reason = %q, want a safe diagnostic", store.failedReason)
	}

	// A failed row is known not to have an account behind it, so a retry goes
	// straight to the create call rather than depending on the lookup endpoint.
	store.account.UpdatedAt = time.Now().Add(-time.Hour)
	provider.create = func(CreateVirtualAccountRequest) (*VirtualAccountDetails, error) {
		return &VirtualAccountDetails{AccountNumber: "0123456789", Currency: "NGN"}, nil
	}
	account, err := service.SetupVirtualAccount(context.Background(), testVirtualFarmer(), VirtualAccountSetup{
		IdentityKind:   "bvn",
		IdentityNumber: "22222222222",
		Consent:        true,
	})
	if err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if !account.Ready() {
		t.Fatalf("retry account = %+v, want active", account)
	}
	if provider.lookupCalls != 0 {
		t.Fatalf("lookup calls = %d, want none for a failed attempt", provider.lookupCalls)
	}
}
