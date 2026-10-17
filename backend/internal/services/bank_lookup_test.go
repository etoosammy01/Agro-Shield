package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"backend/internal/repository"
)

// fakeResolver answers for a fixed set of bank codes, so a sweep's outcome is
// decided entirely by the test rather than by the network.
type fakeResolver struct {
	// holders maps bank code to the name registered there. A code that is
	// absent is a bank that does not hold this account number.
	holders map[string]string

	// errs forces a failure for a code even when it is in holders, standing in
	// for a timeout or a provider error.
	errs map[string]error

	mu    sync.Mutex
	calls []string
}

func (f *fakeResolver) ResolveAccount(accountNumber, bankCode string) (*ResolvedAccount, error) {
	f.mu.Lock()
	f.calls = append(f.calls, bankCode)
	f.mu.Unlock()

	if err := f.errs[bankCode]; err != nil {
		return nil, err
	}
	holder, ok := f.holders[bankCode]
	if !ok {
		// A bank that does not hold this number has answered the question, and
		// the real client reports that with ErrAccountNotResolved. Modelling it
		// any other way would make the fake's "no" indistinguishable from a
		// provider outage, which callers must treat completely differently - see
		// TestLookupAccountBanksDoesNotCacheAMissWhenTheProviderFails. Tests that
		// want the outage use errs.
		return nil, fmt.Errorf("%w: bank %s holds no account %s", ErrAccountNotResolved, bankCode, accountNumber)
	}
	return &ResolvedAccount{AccountNumber: accountNumber, AccountName: holder}, nil
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeLookupStore is an in-memory bankLookupStore, so the cache-then-probe
// behaviour can be asserted without a database.
type fakeLookupStore struct {
	rows []repository.BankLookup

	saveMatchesErr error
	saveMissErr    error
	findOneErr     error

	savedMatches int
	savedMisses  int
}

func newFakeLookupStore() *fakeLookupStore { return &fakeLookupStore{} }

func (s *fakeLookupStore) Find(_ context.Context, accountNumber string) ([]repository.BankLookup, error) {
	var matches []repository.BankLookup
	for _, row := range s.rows {
		if row.AccountNumber == accountNumber && row.BankCode != "" {
			matches = append(matches, row)
		}
	}
	return matches, nil
}

func (s *fakeLookupStore) HasAny(_ context.Context, accountNumber string) (bool, error) {
	for _, row := range s.rows {
		if row.AccountNumber == accountNumber {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeLookupStore) FindOne(_ context.Context, accountNumber, bankCode string) (*repository.BankLookup, error) {
	if s.findOneErr != nil {
		return nil, s.findOneErr
	}
	for _, row := range s.rows {
		if row.AccountNumber == accountNumber && row.BankCode == bankCode {
			found := row
			return &found, nil
		}
	}
	return nil, repository.ErrNoBankLookup
}

func (s *fakeLookupStore) SaveMatches(_ context.Context, accountNumber string, matches []repository.BankLookup) error {
	if s.saveMatchesErr != nil {
		return s.saveMatchesErr
	}
	s.savedMatches++
	s.rows = append(s.rows, matches...)
	return nil
}

func (s *fakeLookupStore) SaveMiss(_ context.Context, accountNumber string) error {
	if s.saveMissErr != nil {
		return s.saveMissErr
	}
	s.savedMisses++
	s.rows = append(s.rows, repository.BankLookup{AccountNumber: accountNumber})
	return nil
}

func testBankService(resolver accountResolver, lookups bankLookupStore) *WalletService {
	return &WalletService{resolver: resolver, lookups: lookups}
}

// The common case: a number belongs to exactly one bank on the shortlist, and
// the user should be shown that bank together with the registered name.
func TestLookupAccountBanksFindsTheOneMatchingBank(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	store := newFakeLookupStore()

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("LookupAccountBanks() = %+v, want exactly one match", matches)
	}
	if matches[0].Code != "044" || matches[0].Name != "Access Bank" || matches[0].AccountName != "ADAEZE OKONKWO" {
		t.Errorf("match = %+v, want Access Bank 044 with the resolved name", matches[0])
	}
	// Every bank on the shortlist must be probed, or a match at an unprobed
	// bank would be silently missed.
	if resolver.callCount() != len(bankLookupShortlist) {
		t.Errorf("probed %d banks, want the whole shortlist of %d", resolver.callCount(), len(bankLookupShortlist))
	}
	if store.savedMatches != 1 {
		t.Errorf("saved %d match sets, want 1", store.savedMatches)
	}
}

// A NUBAN is unique only within a bank, so the same digits can exist at more
// than one; the user has to be shown all of them and choose.
func TestLookupAccountBanksReturnsEveryMatchingBank(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{
		"057": "ZENITH HOLDINGS LTD",
		"011": "ZENITH HOLDINGS LTD",
	}}
	store := newFakeLookupStore()

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("LookupAccountBanks() = %+v, want two matches", matches)
	}
	// Results follow shortlist order, so the more widely used bank is listed
	// first and the likeliest choice is at the top.
	if matches[0].Code != "011" || matches[1].Code != "057" {
		t.Errorf("matches = %+v, want shortlist order (011 before 057)", matches)
	}
}

// One bank failing - a timeout, a 500 - must not lose the banks that did
// answer. A sweep that gave up on the first error would hide real matches.
func TestLookupAccountBanksSurvivesPartialFailure(t *testing.T) {
	resolver := &fakeResolver{
		holders: map[string]string{"058": "CHIDI NWOYE"},
		errs: map[string]error{
			"044": errors.New("connection reset"),
			"011": errors.New("provider returned 500"),
		},
	}
	store := newFakeLookupStore()

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 1 || matches[0].Code != "058" {
		t.Fatalf("LookupAccountBanks() = %+v, want only the bank that answered", matches)
	}
}

// A number that resolves nowhere is a normal outcome, not an error: it may be
// a typo, or an account at a bank outside the shortlist.
func TestLookupAccountBanksReturnsNothingWhenNoBankMatches(t *testing.T) {
	resolver := &fakeResolver{}
	store := newFakeLookupStore()

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("LookupAccountBanks() = %+v, want no matches", matches)
	}
	// The empty answer is worth remembering, otherwise every later attempt at
	// this number repeats the full sweep.
	if store.savedMisses != 1 {
		t.Errorf("saved %d misses, want 1", store.savedMisses)
	}
}

// This is the behaviour the whole design rests on: the second lookup for a
// number must not touch the provider at all.
func TestLookupAccountBanksServesASecondLookupFromCache(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	store := newFakeLookupStore()
	service := testBankService(resolver, store)

	if _, err := service.LookupAccountBanks(context.Background(), "0690000032"); err != nil {
		t.Fatalf("first LookupAccountBanks() error = %v", err)
	}
	afterFirst := resolver.callCount()

	matches, err := service.LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("second LookupAccountBanks() error = %v", err)
	}
	if resolver.callCount() != afterFirst {
		t.Errorf("second lookup made %d more provider calls, want 0", resolver.callCount()-afterFirst)
	}
	if len(matches) != 1 || matches[0].AccountName != "ADAEZE OKONKWO" {
		t.Fatalf("cached LookupAccountBanks() = %+v, want the stored match", matches)
	}
}

// A miss is cached too, so a typo does not re-sweep the shortlist on every
// attempt. This is the case a naive cache would get wrong by storing only hits.
func TestLookupAccountBanksDoesNotResweepACachedMiss(t *testing.T) {
	resolver := &fakeResolver{}
	store := newFakeLookupStore()
	service := testBankService(resolver, store)

	if _, err := service.LookupAccountBanks(context.Background(), "0690000032"); err != nil {
		t.Fatalf("first LookupAccountBanks() error = %v", err)
	}
	afterFirst := resolver.callCount()

	matches, err := service.LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("second LookupAccountBanks() error = %v", err)
	}
	if resolver.callCount() != afterFirst {
		t.Errorf("cached miss triggered %d more probes, want 0", resolver.callCount()-afterFirst)
	}
	if len(matches) != 0 {
		t.Fatalf("cached miss returned %+v, want nothing", matches)
	}
}

// A failed cache write costs the next lookup a repeat sweep, but must not fail
// the lookup the user is waiting on - the answer is already in hand.
func TestLookupAccountBanksReturnsMatchesWhenCachingFails(t *testing.T) {
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	store := newFakeLookupStore()
	store.saveMatchesErr = errors.New("database is down")

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v, want the resolved matches anyway", err)
	}
	if len(matches) != 1 {
		t.Fatalf("LookupAccountBanks() = %+v, want the match despite the failed write", matches)
	}
}

// A cancelled request stops probing. Crucially it must not record a miss: the
// sweep only covered part of the shortlist, and caching that would tell every
// later lookup that a real account does not exist.
func TestLookupAccountBanksDoesNotCacheAMissAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	store := newFakeLookupStore()
	service := testBankService(resolver, store)

	matches, err := service.LookupAccountBanks(ctx, "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("LookupAccountBanks() = %+v, want nothing after cancellation", matches)
	}
	if store.savedMisses != 0 {
		t.Errorf("cached %d misses after cancellation, want 0", store.savedMisses)
	}
	if resolver.callCount() != 0 {
		t.Errorf("made %d probes after cancellation, want 0", resolver.callCount())
	}
}

// A provider that fails every probe is not evidence that the number exists
// nowhere. Caching a miss from that would tell every later lookup that a real
// account does not exist - permanently, since a miss is never re-swept.
//
// This is not hypothetical: a rejected API key turned every number anyone tried
// while it was broken into a stored "no such account", and the rows outlived the
// fix. The lookup is allowed to return nothing either way; what must differ is
// whether that nothing is written down.
func TestLookupAccountBanksDoesNotCacheAMissWhenEveryProbeFails(t *testing.T) {
	resolver := &fakeResolver{errs: map[string]error{}}
	for _, bank := range bankLookupShortlist {
		resolver.errs[bank.Code] = errors.New(`flutterwave resolve returned 401: {"message":"Invalid authorization key"}`)
	}
	store := newFakeLookupStore()
	service := testBankService(resolver, store)

	matches, err := service.LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("LookupAccountBanks() = %+v, want nothing while the provider is down", matches)
	}
	if store.savedMisses != 0 {
		t.Errorf("cached %d misses from a provider outage, want 0 - this is the row that outlives the outage",
			store.savedMisses)
	}
}

// One bank failing is not the same as every bank answering "no". A sweep where
// nineteen banks said no and one timed out has not established that the number
// exists nowhere, so it must not be remembered as a miss either.
func TestLookupAccountBanksDoesNotCacheAMissWhenOneProbeFails(t *testing.T) {
	resolver := &fakeResolver{
		errs: map[string]error{"044": errors.New("connection reset")},
	}
	store := newFakeLookupStore()

	matches, err := testBankService(resolver, store).LookupAccountBanks(context.Background(), "0690000032")
	if err != nil {
		t.Fatalf("LookupAccountBanks() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("LookupAccountBanks() = %+v, want nothing", matches)
	}
	if store.savedMisses != 0 {
		t.Errorf("cached %d misses for a partly-failed sweep, want 0", store.savedMisses)
	}
}

// A malformed number must not reach the provider: a sweep costs a request per
// bank, and a typo does not deserve twenty of them.
func TestLookupAccountBanksRejectsMalformedNumbers(t *testing.T) {
	for _, number := range []string{"", "069000003", "06900000321", "06900000ab", "069000003 "} {
		t.Run(number, func(t *testing.T) {
			resolver := &fakeResolver{}
			store := newFakeLookupStore()
			service := testBankService(resolver, store)

			if _, err := service.LookupAccountBanks(context.Background(), number); err == nil {
				t.Fatal("LookupAccountBanks() accepted a malformed account number")
			}
			if resolver.callCount() != 0 {
				t.Errorf("made %d probes for a malformed number, want 0", resolver.callCount())
			}
			if store.savedMisses != 0 {
				t.Errorf("cached a miss for a malformed number, want 0")
			}
		})
	}
}

// The save path pins the name to what the bank actually has on file, but only
// when we know it. A provider we have never reached must not block a farmer
// from saving their own payout details.
func TestUpdateBankDetailsRejectsAMismatchedAccountName(t *testing.T) {
	cached := newFakeLookupStore()
	cached.rows = []repository.BankLookup{{
		AccountNumber: "0690000032",
		BankCode:      "044",
		BankName:      "Access Bank",
		AccountName:   "ADAEZE OKONKWO",
	}}

	// The name is checked before the save is attempted, so no farmer repository
	// is needed to prove the rejection.
	err := (&WalletService{lookups: cached}).UpdateBankDetails(
		context.Background(), 1, "Access Bank", "044", "SOMEONE ELSE", "0690000032")
	if err == nil {
		t.Fatal("UpdateBankDetails() accepted a name that contradicts the bank's records")
	}
	if !strings.Contains(err.Error(), "ADAEZE OKONKWO") {
		t.Errorf("error = %q, want it to name the registered holder", err)
	}
}

// Banks return names with inconsistent spacing and casing, and a user retyping
// a verified name will not reproduce it exactly. An exact compare would reject
// honest saves.
func TestAccountNamesMatchIgnoresCaseAndSpacing(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"ADAEZE OKONKWO", "Adaeze Okonkwo", true},
		{"ADAEZE  OKONKWO", "adaeze okonkwo", true},
		{"  ADAEZE OKONKWO  ", "ADAEZE OKONKWO", true},
		{"ADAEZE OKONKWO", "ADAEZE O OKONKWO", false},
		{"ADAEZE OKONKWO", "OKONKWO ADAEZE", false},
		{"ADAEZE OKONKWO", "ADAEZE OKONKWO CHIDI", false},
		{"", "ADAEZE OKONKWO", false},
	}
	for _, test := range tests {
		t.Run(test.a+"|"+test.b, func(t *testing.T) {
			if got := accountNamesMatch(test.a, test.b); got != test.want {
				t.Errorf("accountNamesMatch(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
			}
		})
	}
}

// A name we have never resolved has to be allowed through, or a provider
// outage would lock every farmer out of correcting their payout details.
func TestUpdateBankDetailsAllowsAnUnverifiableName(t *testing.T) {
	// Reaching the farmer repository is the signal that the name check passed;
	// a nil repository panics there, which is how this test detects it.
	defer func() {
		if recover() == nil {
			t.Fatal("UpdateBankDetails() did not proceed past the name check")
		}
	}()

	err := (&WalletService{lookups: newFakeLookupStore()}).UpdateBankDetails(
		context.Background(), 1, "Access Bank", "044", "ANY NAME AT ALL", "0690000032")
	if err != nil {
		t.Fatalf("UpdateBankDetails() error = %v before reaching the save", err)
	}
}

// The payout form reads these exact keys out of the lookup response, so the
// wire names are a contract across the language boundary - renaming a Go field
// would silently blank the bank list in the browser with no failing test.
func TestBankMatchSerialisesToTheFrontendContract(t *testing.T) {
	raw, err := json.Marshal(BankMatch{Name: "Access Bank", Code: "044", AccountName: "ADAEZE OKONKWO"})
	if err != nil {
		t.Fatalf("marshal BankMatch: %v", err)
	}
	want := `{"name":"Access Bank","code":"044","account_name":"ADAEZE OKONKWO"}`
	if string(raw) != want {
		t.Errorf("BankMatch JSON = %s, want %s", raw, want)
	}
}

// Only the last two digits of an account number may reach a log line.
func TestMaskAccountNumberKeepsOnlyTheLastTwoDigits(t *testing.T) {
	got := maskAccountNumber("0690000032")
	if got != "********32" {
		t.Errorf("maskAccountNumber() = %q, want %q", got, "********32")
	}
	if strings.Contains(got, "06900000") {
		t.Errorf("maskAccountNumber() leaked the leading digits: %q", got)
	}
	// Short input must not panic on a negative repeat count.
	if got := maskAccountNumber("1"); got != "*" {
		t.Errorf("maskAccountNumber(%q) = %q, want %q", "1", got, "*")
	}
}
