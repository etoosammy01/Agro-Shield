package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/repository"
)

// fakeApprovalStore lets an approval run all the way through, which the store in
// payout_verification_test.go deliberately does not: that one fails the moment
// the payout starts, so it can prove a refused payout never gets there. These
// tests are about what happens after it does.
type fakeApprovalStore struct {
	walletStore

	withdrawal *models.Withdrawal

	began       bool
	beganBy     int
	reverted    []string
	revertErr   error
	setErr      error
	transferErr error

	storedTransferIDs []string
	verdicts          []string
	completed         []string
	rejected          []int64
	createdTransfer   string

	// alreadySettled makes CompleteTransfer report that it moved nothing, which
	// is what a retried webhook or a reconcile-after-webhook sees.
	alreadySettled bool
}

func (f *fakeApprovalStore) GetWithdrawal(ctx context.Context, withdrawalID int64) (*models.Withdrawal, error) {
	if f.withdrawal == nil {
		return nil, repository.ErrWithdrawalNotFound
	}
	return f.withdrawal, nil
}

func (f *fakeApprovalStore) GetWithdrawalByReference(ctx context.Context, reference string) (*models.Withdrawal, error) {
	if f.withdrawal == nil {
		return nil, repository.ErrWithdrawalNotFound
	}
	return f.withdrawal, nil
}

// RejectWithdrawal mirrors the store's own contract: it only matches a pending
// row, so a test that rejects twice sees the second one fail.
func (f *fakeApprovalStore) RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if f.withdrawal == nil || f.withdrawal.Status != "pending" {
		return repository.ErrWithdrawalNotFound
	}
	f.withdrawal.Status = "rejected"
	f.rejected = append(f.rejected, withdrawalID)
	return nil
}

func (f *fakeApprovalStore) BeginWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (*models.Withdrawal, error) {
	f.began = true
	f.beganBy = adminID
	begun := *f.withdrawal
	begun.Status = "processing"
	return &begun, nil
}

func (f *fakeApprovalStore) RecordWithdrawalVerification(ctx context.Context, reference, status, resolvedName, detail string) error {
	f.verdicts = append(f.verdicts, status)
	return nil
}

func (f *fakeApprovalStore) SetTransferID(ctx context.Context, reference, transferID string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.storedTransferIDs = append(f.storedTransferIDs, reference+"="+transferID)
	return nil
}

func (f *fakeApprovalStore) CompleteTransfer(ctx context.Context, reference, transferID, status string, amount int64, currency string) (bool, error) {
	f.completed = append(f.completed, status)
	return !f.alreadySettled, nil
}

func (f *fakeApprovalStore) RevertWithdrawalToPending(ctx context.Context, reference string) error {
	if f.revertErr != nil {
		return f.revertErr
	}
	f.reverted = append(f.reverted, reference)
	return nil
}

func (f *fakeApprovalStore) ApproveTransfer(ctx context.Context, transferID int64, adminID int) (*models.Transfer, error) {
	return nil, f.transferErr
}

func (f *fakeApprovalStore) CreateTransfer(ctx context.Context, senderID, recipientID int, reference, amount, note string) error {
	f.createdTransfer = reference
	return nil
}

func payableWithdrawal() *models.Withdrawal {
	return &models.Withdrawal{
		ID: 3, FarmerID: 7, Reference: "WITHDRAW-7-1", Amount: "5000", Currency: "NGN",
		BankName: "Access Bank", BankCode: "044",
		AccountName: "Adaeze Okonkwo", AccountNumber: "0690000032",
		Status: "pending",
	}
}

// testApprovalService builds a service whose payout details verify cleanly, so a
// test reaches the provider call rather than stopping at the verification.
func testApprovalService(store *fakeApprovalStore, client *FlutterwaveClient) *WalletService {
	return &WalletService{
		repo:      store,
		resolver:  &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}},
		directory: &fakeBankDirectory{banks: testBankDirectory},
		lookups:   newFakeLookupStore(),
		flutter:   client,
		// Two admins, so a payout can be approved by 2 while 1 is the farmer.
		adminIDs: map[int]struct{}{1: {}, 2: {}},
	}
}

// fakeNotifier records what was said to whom. The wallet moves money whether or
// not anybody is told, so these tests are about the telling alone.
type fakeNotifier struct {
	sent []string
	err  error
}

func (f *fakeNotifier) CreateNotification(userID int, title, message, kind string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, fmt.Sprintf("%d|%s|%s", userID, kind, message))
	return nil
}

// said reports whether any recorded notification matches all of these.
func (f *fakeNotifier) said(fragments ...string) bool {
	for _, entry := range f.sent {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(entry, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func (f *fakeNotifier) count(kind string) int {
	n := 0
	for _, entry := range f.sent {
		if strings.Contains(entry, "|"+kind+"|") {
			n++
		}
	}
	return n
}

// --- approving your own request -------------------------------------------

// The review step exists so that someone other than the person being paid looks
// at where the money is going. An admin who can approve their own request is the
// one case where it looks like a control and is not one.
func TestApproveWithdrawalRefusesToApproveYourOwnRequest(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()} // farmer 7
	resolver := &fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))
	service.resolver = resolver
	service.adminIDs = map[int]struct{}{7: {}} // the farmer is the only admin

	err := service.ApproveWithdrawal(context.Background(), 3, 7)

	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("ApproveWithdrawal() error = %v, want %v", err, ErrSelfApproval)
	}
	if store.began {
		t.Fatal("ApproveWithdrawal() started a payout the admin requested themselves")
	}
	// Refused before the bank was asked, so the refusal costs nothing and cannot
	// be mistaken for a verdict on the details.
	if calls := resolver.callCount(); calls != 0 {
		t.Errorf("resolver call count = %d, want 0 - a self-approval must be refused before verification", calls)
	}
}

// confirmingClient answers the two provider calls an approving payout makes -
// create, then confirm - so a test can drive a payout to settlement. Each call
// builds its own, because the transport consumes its answers in order.
func confirmingClient() *FlutterwaveClient {
	client := NewFlutterwaveClient("test-secret-key", "test-hash", "http://localhost/callback", "card")
	client.httpClient = &http.Client{Transport: &sequenceTransport{
		statuses: []int{http.StatusOK, http.StatusOK},
		bodies: []string{
			`{"status":"success","message":"ok","data":{"id":999}}`,
			// The amount Flutterwave reports back is whole naira, matching what
			// CreateTransfer sends.
			`{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"SUCCESSFUL","amount":5000,"currency":"NGN"}}`,
		},
	}}
	return client
}

// The rule is about the requester, not the admin's own account: an admin
// approving somebody else's payout is the normal case and must still work.
func TestApproveWithdrawalAllowsApprovingSomebodyElsesRequest(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()} // farmer 7
	service := testApprovalService(store, confirmingClient())

	if err := service.ApproveWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("ApproveWithdrawal() error = %v, want the payout to start", err)
	}
	if !store.began {
		t.Fatal("ApproveWithdrawal() did not start the payout")
	}
	if store.beganBy != 2 {
		t.Errorf("approved by %d, want the acting admin 2", store.beganBy)
	}
	if len(store.completed) != 1 || store.completed[0] != "SUCCESSFUL" {
		t.Fatalf("completed = %v, want the confirmed payout settled", store.completed)
	}
}

// Wallet-to-wallet transfers carry the same rule, enforced in the store where
// the sender's id is already locked. Reading it as anything but a refusal would
// hide the reason from the admin.
func TestApproveTransferReportsASelfApproval(t *testing.T) {
	store := &fakeApprovalStore{transferErr: repository.ErrSelfApproval}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))

	_, err := service.ApproveTransfer(context.Background(), 5, 2)

	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("ApproveTransfer() error = %v, want %v", err, ErrSelfApproval)
	}
}

// --- a payout that never started ------------------------------------------

// A refusal is the provider saying no transfer exists. The row can go back to
// the queue, where Reject can release the funds that BeginWithdrawal reserved -
// otherwise they sit reserved against a payout that will never happen.
func TestApproveWithdrawalReturnsTheRequestToTheQueueWhenTheProviderRefuses(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"error","message":"Insufficient balance","data":null}`,
	})
	service := testApprovalService(store, client)

	err := service.ApproveWithdrawal(context.Background(), 3, 2)

	if !errors.Is(err, ErrTransferNotCreated) {
		t.Fatalf("ApproveWithdrawal() error = %v, want %v", err, ErrTransferNotCreated)
	}
	if len(store.reverted) != 1 || store.reverted[0] != "WITHDRAW-7-1" {
		t.Fatalf("reverted = %v, want the withdrawal returned to the queue", store.reverted)
	}
}

// The failure that must NOT be reverted: a 500 may have created the transfer
// before it failed. Returning the row to the queue would let an admin refund a
// farmer Flutterwave is also paying.
func TestApproveWithdrawalLeavesAnUnsurePayoutReserved(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusInternalServerError,
		body:   `{"status":"error","message":"internal server error","data":null}`,
	})
	service := testApprovalService(store, client)

	err := service.ApproveWithdrawal(context.Background(), 3, 2)

	if err == nil {
		t.Fatal("ApproveWithdrawal() reported success for a payout the provider never answered")
	}
	if errors.Is(err, ErrTransferNotCreated) {
		t.Errorf("error = %v, want an unknown fate rather than a refusal", err)
	}
	if len(store.reverted) != 0 {
		t.Fatalf("reverted = %v, want the payout left reserved for reconciliation", store.reverted)
	}
}

// The payout exists and only its id failed to save. The row looks exactly like
// one that never started, so the error has to carry the id: it is the only
// record of a transfer the provider is going to pay.
func TestApproveWithdrawalReportsATransferItCouldNotRecord(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal(), setErr: errors.New("connection reset")}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"success","data":{"id":999}}`,
	})
	service := testApprovalService(store, client)

	err := service.ApproveWithdrawal(context.Background(), 3, 2)

	if !errors.Is(err, ErrPayoutSubmitted) {
		t.Fatalf("ApproveWithdrawal() error = %v, want %v", err, ErrPayoutSubmitted)
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error = %q, want it to name the transfer so the payout can be found", err)
	}
	if len(store.reverted) != 0 {
		t.Fatalf("reverted = %v, want a payout that exists left reserved", store.reverted)
	}
}

// A refused payout whose row could not be returned must say so rather than look
// like an ordinary refusal: the funds are still reserved and Reject cannot
// release them.
func TestApproveWithdrawalReportsAFailedReturnToTheQueue(t *testing.T) {
	store := &fakeApprovalStore{
		withdrawal: payableWithdrawal(),
		revertErr:  repository.ErrWithdrawalNotFound,
	}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"error","message":"Insufficient balance","data":null}`,
	})
	service := testApprovalService(store, client)

	err := service.ApproveWithdrawal(context.Background(), 3, 2)

	if !errors.Is(err, repository.ErrWithdrawalNotFound) {
		t.Fatalf("ApproveWithdrawal() error = %v, want the return failure reported", err)
	}
	if !strings.Contains(err.Error(), "could not be returned") {
		t.Errorf("error = %q, want it to say the request is still stuck", err)
	}
}

// --- the manual way back --------------------------------------------------

// The counterpart to the automatic revert, for the payouts whose fate the
// provider never settled.
func TestReturnWithdrawalToQueueReleasesAStalledPayout(t *testing.T) {
	withdrawal := payableWithdrawal()
	withdrawal.Status = "processing" // funds reserved, no provider transfer id
	store := &fakeApprovalStore{withdrawal: withdrawal}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))

	if err := service.ReturnWithdrawalToQueue(context.Background(), 3, 2); err != nil {
		t.Fatalf("ReturnWithdrawalToQueue() error = %v", err)
	}
	if len(store.reverted) != 1 || store.reverted[0] != "WITHDRAW-7-1" {
		t.Fatalf("reverted = %v, want the withdrawal returned to the queue", store.reverted)
	}
}

// A settled payout has nothing to return, and saying so is better than letting
// the store refuse it silently.
func TestReturnWithdrawalToQueueRefusesASettledPayout(t *testing.T) {
	withdrawal := payableWithdrawal()
	withdrawal.Status = "paid"
	store := &fakeApprovalStore{withdrawal: withdrawal}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))

	err := service.ReturnWithdrawalToQueue(context.Background(), 3, 2)

	if err == nil {
		t.Fatal("ReturnWithdrawalToQueue() returned a settled payout to the queue")
	}
	if len(store.reverted) != 0 {
		t.Fatalf("reverted = %v, want nothing touched", store.reverted)
	}
}

func TestReturnWithdrawalToQueueRequiresAnAdmin(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))

	if err := service.ReturnWithdrawalToQueue(context.Background(), 3, 99); err == nil {
		t.Fatal("ReturnWithdrawalToQueue() served a non-admin")
	}
	if len(store.reverted) != 0 {
		t.Fatalf("reverted = %v, want nothing touched", store.reverted)
	}
}

// --- recovering a payout that never recorded its transfer ------------------

// The refund hole this closes: the row is processing with money reserved and no
// transfer id, and settlement matches on that id - so a transfer that failed
// could never be refunded and one that succeeded could never close. The admin
// brings the id back from Flutterwave and it is stored, after which the ordinary
// reconcile path settles or refunds it.
func TestRecordProviderTransferIDStoresAConfirmedTransfer(t *testing.T) {
	withdrawal := payableWithdrawal()
	withdrawal.Status = "processing"
	store := &fakeApprovalStore{withdrawal: withdrawal}
	client := clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"FAILED","amount":5000,"currency":"NGN"}}`,
	})
	service := testApprovalService(store, client)

	if err := service.RecordProviderTransferID(context.Background(), 3, 2, " 999 "); err != nil {
		t.Fatalf("RecordProviderTransferID() error = %v", err)
	}
	if len(store.storedTransferIDs) != 1 || store.storedTransferIDs[0] != "WITHDRAW-7-1=999" {
		t.Fatalf("stored = %v, want the confirmed transfer recorded against the withdrawal", store.storedTransferIDs)
	}
}

// Recording the wrong id would hand a later settlement the authority to close
// this row on another payout's outcome, and settlement is what refunds.
func TestRecordProviderTransferIDRefusesATransferThatIsNotThisPayout(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{
			name: "another withdrawal's transfer",
			body: `{"status":"success","data":{"id":999,"reference":"WITHDRAW-9-9","status":"FAILED","amount":5000,"currency":"NGN"}}`,
		},
		{
			name: "a different amount",
			body: `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"FAILED","amount":7000,"currency":"NGN"}}`,
		},
		{
			name: "a different currency",
			body: `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"FAILED","amount":5000,"currency":"USD"}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			withdrawal := payableWithdrawal()
			withdrawal.Status = "processing"
			store := &fakeApprovalStore{withdrawal: withdrawal}
			service := testApprovalService(store, clientWithTransport(&fakeTransport{
				status: http.StatusOK, body: test.body,
			}))

			if err := service.RecordProviderTransferID(context.Background(), 3, 2, "999"); err == nil {
				t.Fatal("RecordProviderTransferID() accepted a transfer that is not this payout")
			}
			if len(store.storedTransferIDs) != 0 {
				t.Fatalf("stored = %v, want nothing recorded", store.storedTransferIDs)
			}
		})
	}
}

// A number that is not a transfer id never reaches the provider, and neither
// does anything for a row that is not waiting on one.
func TestRecordProviderTransferIDRejectsWhatItCannotStore(t *testing.T) {
	processing := func() *models.Withdrawal {
		withdrawal := payableWithdrawal()
		withdrawal.Status = "processing"
		return withdrawal
	}
	recorded := func() *models.Withdrawal {
		withdrawal := processing()
		withdrawal.TransferID = sql.NullString{String: "999", Valid: true}
		return withdrawal
	}
	settled := func() *models.Withdrawal {
		withdrawal := payableWithdrawal()
		withdrawal.Status = "paid"
		return withdrawal
	}

	for _, test := range []struct {
		name       string
		withdrawal *models.Withdrawal
		transferID string
	}{
		{name: "no transfer id", withdrawal: processing(), transferID: "  "},
		{name: "not a number", withdrawal: processing(), transferID: "FLW-999"},
		{name: "zero", withdrawal: processing(), transferID: "0"},
		{name: "negative", withdrawal: processing(), transferID: "-5"},
		{name: "already recorded", withdrawal: recorded(), transferID: "999"},
		{name: "already settled", withdrawal: settled(), transferID: "999"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeApprovalStore{withdrawal: test.withdrawal}
			// A body that would satisfy every check, so a failure here can only
			// come from the guard being tested.
			service := testApprovalService(store, clientWithTransport(&fakeTransport{
				status: http.StatusOK,
				body:   `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"FAILED","amount":5000,"currency":"NGN"}}`,
			}))

			if err := service.RecordProviderTransferID(context.Background(), 3, 2, test.transferID); err == nil {
				t.Fatal("RecordProviderTransferID() stored a transfer it should have refused")
			}
			if len(store.storedTransferIDs) != 0 {
				t.Fatalf("stored = %v, want nothing recorded", store.storedTransferIDs)
			}
		})
	}
}

func TestRecordProviderTransferIDRequiresAnAdmin(t *testing.T) {
	withdrawal := payableWithdrawal()
	withdrawal.Status = "processing"
	store := &fakeApprovalStore{withdrawal: withdrawal}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))

	if err := service.RecordProviderTransferID(context.Background(), 3, 99, "999"); err == nil {
		t.Fatal("RecordProviderTransferID() served a non-admin")
	}
	if len(store.storedTransferIDs) != 0 {
		t.Fatalf("stored = %v, want nothing recorded", store.storedTransferIDs)
	}
}

// The provider being unreachable must not record an unconfirmed id: nothing has
// told us the transfer exists yet.
func TestRecordProviderTransferIDRefusesToGuessWhenTheProviderIsUnreachable(t *testing.T) {
	withdrawal := payableWithdrawal()
	withdrawal.Status = "processing"
	store := &fakeApprovalStore{withdrawal: withdrawal}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{
		status: http.StatusUnauthorized,
		body:   `{"status":"error","message":"Invalid authorization key"}`,
	}))

	if err := service.RecordProviderTransferID(context.Background(), 3, 2, "999"); err == nil {
		t.Fatal("RecordProviderTransferID() recorded an id the provider never confirmed")
	}
	if len(store.storedTransferIDs) != 0 {
		t.Fatalf("stored = %v, want nothing recorded", store.storedTransferIDs)
	}
}

// --- the payout that works ------------------------------------------------

// sequenceTransport answers in the order requests arrive, so a test can drive
// the two-call payout - create, then confirm - without a network.
type sequenceTransport struct {
	statuses []int
	bodies   []string
	calls    int
}

func (s *sequenceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.calls >= len(s.statuses) {
		return nil, errors.New("unexpected extra provider request")
	}
	resp := &http.Response{
		StatusCode: s.statuses[s.calls],
		Body:       io.NopCloser(strings.NewReader(s.bodies[s.calls])),
		Header:     make(http.Header),
		Request:    req,
	}
	s.calls++
	return resp, nil
}

// The guardrails must not have broken the ordinary payout: details that match,
// a provider that accepts, and a payout that settles immediately.
func TestApproveWithdrawalSettlesAPayoutTheProviderConfirms(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	service := testApprovalService(store, confirmingClient())

	if err := service.ApproveWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("ApproveWithdrawal() error = %v", err)
	}
	if len(store.completed) != 1 || store.completed[0] != "SUCCESSFUL" {
		t.Fatalf("completed = %v, want the confirmed payout settled", store.completed)
	}
	if len(store.reverted) != 0 {
		t.Fatalf("reverted = %v, want a successful payout left alone", store.reverted)
	}
}

// --- telling people what happened ------------------------------------------

// A request nobody is told about is found only by somebody opening the approvals
// page. The sender's balance is already reduced by the time this notification is
// sent, so the money waits on that page being opened.
func TestRequestWithdrawalTellsEveryAdmin(t *testing.T) {
	store := &fakeWalletStore{}
	notifier := &fakeNotifier{}
	service := testRequestService(store,
		&models.Farmer{ID: 7, FullName: "Bola Farms", BankName: "Access Bank", BankCode: "044",
			AccountName: "Adaeze Okonkwo", AccountNumber: "0690000032"},
		&fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}})
	service.notify = notifier
	service.adminIDs = map[int]struct{}{1: {}, 2: {}}

	if err := service.RequestWithdrawal(context.Background(), 7, "5000"); err != nil {
		t.Fatalf("RequestWithdrawal() error = %v", err)
	}

	for _, adminID := range []int{1, 2} {
		if !notifier.said(fmt.Sprintf("%d|wallet_withdrawal_request|", adminID), "Bola Farms", "5000") {
			t.Errorf("admin %d was not told about the request: %v", adminID, notifier.sent)
		}
	}
}

// The same for a wallet-to-wallet transfer, which reserves the sender's money
// just as a withdrawal does.
func TestRequestTransferTellsEveryAdmin(t *testing.T) {
	store := &fakeApprovalStore{}
	notifier := &fakeNotifier{}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))
	service.farmers = &fakeFarmerStore{farmer: &models.Farmer{ID: 7, FullName: "Bola Farms"}}
	service.notify = notifier

	if err := service.RequestTransfer(context.Background(), 7, 42, "250", "seed money"); err != nil {
		t.Fatalf("RequestTransfer() error = %v", err)
	}
	if store.createdTransfer == "" {
		t.Fatal("RequestTransfer() did not write the transfer")
	}

	for _, adminID := range []int{1, 2} {
		if !notifier.said(fmt.Sprintf("%d|wallet_transfer_request|", adminID), "Bola Farms", "250", "#42") {
			t.Errorf("admin %d was not told about the transfer: %v", adminID, notifier.sent)
		}
	}
}

// A rejected withdrawal and a refunded one look the same from the farmer's
// wallet: the money they asked for is not there. Silence in either case leaves
// them unable to tell a decision from a payout still in flight.
func TestRejectWithdrawalTellsTheFarmerTheMoneyCameBack(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	notifier := &fakeNotifier{}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{status: http.StatusOK}))
	service.notify = notifier

	if err := service.RejectWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("RejectWithdrawal() error = %v", err)
	}

	if len(store.rejected) != 1 {
		t.Fatalf("rejected = %v, want the withdrawal rejected", store.rejected)
	}
	if !notifier.said("7|wallet_withdrawal_rejected|", "5000") {
		t.Errorf("the farmer was not told their withdrawal was rejected: %v", notifier.sent)
	}
}

// The refund happens deep in the settlement path, where nobody is watching a
// page. Without this the farmer's money simply disappears from their balance.
func TestAFailedPayoutTellsTheFarmerItWasRefunded(t *testing.T) {
	for _, status := range []string{"FAILED", "REVERSED"} {
		t.Run(status, func(t *testing.T) {
			store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
			notifier := &fakeNotifier{}
			service := testApprovalService(store, clientWithTransport(&fakeTransport{
				status: http.StatusOK,
				body:   `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"` + status + `","amount":5000,"currency":"NGN"}}`,
			}))
			service.notify = notifier

			if _, err := service.ProcessTransferWebhook(context.Background(), []byte(
				`{"event":"transfer.completed","data":{"id":999,"status":"`+status+`","reference":"WITHDRAW-7-1"}}`),
				"test-hash"); err != nil {
				t.Fatalf("ProcessTransferWebhook() error = %v", err)
			}

			if !notifier.said("7|wallet_withdrawal_refunded|", "5000") {
				t.Errorf("the farmer was not told their payout had been refunded: %v", notifier.sent)
			}
		})
	}
}

// A payout that succeeds takes the money out of the wallet for good, so the
// farmer has to be told which of the two happened - both leave the balance lower.
func TestASuccessfulPayoutTellsTheFarmerItWasPaid(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	notifier := &fakeNotifier{}
	service := testApprovalService(store, confirmingClient())
	service.notify = notifier

	if err := service.ApproveWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("ApproveWithdrawal() error = %v", err)
	}

	if !notifier.said("7|wallet_withdrawal_paid|", "5000") {
		t.Errorf("the farmer was not told their payout was sent: %v", notifier.sent)
	}
}

// Flutterwave retries a webhook until it is acknowledged, and an admin can also
// press "check provider status" on a payout the webhook has already settled.
// Neither is a second movement of money, so neither may read as one.
func TestARetriedSettlementDoesNotTellTheFarmerTwice(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal(), alreadySettled: true}
	notifier := &fakeNotifier{}
	service := testApprovalService(store, clientWithTransport(&fakeTransport{
		status: http.StatusOK,
		body:   `{"status":"success","data":{"id":999,"reference":"WITHDRAW-7-1","status":"FAILED","amount":5000,"currency":"NGN"}}`,
	}))
	service.notify = notifier

	if _, err := service.ProcessTransferWebhook(context.Background(), []byte(
		`{"event":"transfer.completed","data":{"id":999,"status":"FAILED","reference":"WITHDRAW-7-1"}}`),
		"test-hash"); err != nil {
		t.Fatalf("ProcessTransferWebhook() error = %v", err)
	}

	if kind := "wallet_withdrawal_refunded"; notifier.count(kind) != 0 {
		t.Errorf("sent %d %s notifications, want none for a settlement the store reports it did not make", notifier.count(kind), kind)
	}
}

// Telling somebody about their money must never be what stops the money moving.
func TestANotificationFailureDoesNotFailThePayout(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	service := testApprovalService(store, confirmingClient())
	service.notify = &fakeNotifier{err: errors.New("notification store is down")}

	if err := service.ApproveWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("ApproveWithdrawal() error = %v, want the payout to settle regardless", err)
	}
	if len(store.completed) != 1 {
		t.Fatalf("completed = %v, want the payout settled", store.completed)
	}
}

// A wallet built without a notification store is a valid configuration, and the
// nil must not surface as a panic on the path that moves money.
func TestAServiceWithNoNotifierStillMovesMoney(t *testing.T) {
	store := &fakeApprovalStore{withdrawal: payableWithdrawal()}
	service := testApprovalService(store, confirmingClient())

	if err := service.ApproveWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("ApproveWithdrawal() error = %v", err)
	}
	if err := service.RejectWithdrawal(context.Background(), 3, 2); err != nil {
		t.Fatalf("RejectWithdrawal() error = %v", err)
	}
	if len(store.rejected) != 1 {
		t.Fatalf("rejected = %v, want the withdrawal rejected", store.rejected)
	}
}

// The self-approval rule has a consequence for who gets told: an admin who made
// the request cannot act on it, so "waiting for you" would be false for the one
// person most likely to open the page.
func TestTheAdminWhoMadeTheRequestIsNotToldItIsWaitingForThem(t *testing.T) {
	store := &fakeWalletStore{}
	notifier := &fakeNotifier{}
	service := testRequestService(store,
		&models.Farmer{ID: 7, FullName: "Bola Farms", BankName: "Access Bank", BankCode: "044",
			AccountName: "Adaeze Okonkwo", AccountNumber: "0690000032"},
		&fakeResolver{holders: map[string]string{"044": "ADAEZE OKONKWO"}})
	service.notify = notifier
	service.adminIDs = map[int]struct{}{7: {}, 2: {}} // 7 requests, 2 can act

	if err := service.RequestWithdrawal(context.Background(), 7, "5000"); err != nil {
		t.Fatalf("RequestWithdrawal() error = %v", err)
	}

	if notifier.said("7|wallet_withdrawal_request|") {
		t.Errorf("the requesting admin was told their own request was waiting for them: %v", notifier.sent)
	}
	if !notifier.said("2|wallet_withdrawal_request|") {
		t.Errorf("the other admin was not told about the request: %v", notifier.sent)
	}
}

// fakeCancelStore answers the cancel methods from a configured error, so these
// tests exercise the service's translation rather than a database. The
// distinction matters: "not found", "already being paid" and "the database is
// down" are three different things to tell a user.
type fakeCancelStore struct {
	walletStore
	withdrawalErr error
	transferErr   error
}

func (f fakeCancelStore) CancelWithdrawal(context.Context, int64, int) error { return f.withdrawalErr }
func (f fakeCancelStore) CancelTransfer(context.Context, int64, int) error   { return f.transferErr }

func TestCancelWithdrawalTranslatesStoreRefusals(t *testing.T) {
	tests := []struct {
		name    string
		store   error
		want    error
		wantNot error
	}{
		{"missing request", repository.ErrWithdrawalNotFound, ErrRequestNotFound, nil},
		{"already being paid", repository.ErrRequestNotPending, ErrRequestNotPending, nil},
		{"database failure", errors.New("connection reset"), ErrWalletUnavailable, ErrRequestNotPending},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &WalletService{repo: fakeCancelStore{withdrawalErr: tc.store}}
			err := service.CancelWithdrawal(context.Background(), 7, 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CancelWithdrawal() error = %v, want %v", err, tc.want)
			}
			if tc.wantNot != nil && errors.Is(err, tc.wantNot) {
				t.Errorf("a database failure was reported as %v, which the user can read as a status", tc.wantNot)
			}
		})
	}
}

func TestCancelTransferTranslatesStoreRefusals(t *testing.T) {
	tests := []struct {
		name  string
		store error
		want  error
	}{
		{"missing request", repository.ErrTransferNotFound, ErrRequestNotFound},
		{"already settled", repository.ErrRequestNotPending, ErrRequestNotPending},
		{"database failure", errors.New("connection reset"), ErrTransferUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &WalletService{repo: fakeCancelStore{transferErr: tc.store}}
			if err := service.CancelTransfer(context.Background(), 7, 1); !errors.Is(err, tc.want) {
				t.Errorf("CancelTransfer() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// fakeAdminStore answers the admin lookup with a fixed set of flagged accounts.
type fakeAdminStore struct {
	walletStore
	flagged []int
	err     error
}

func (f fakeAdminStore) ListAdminIDs(context.Context) ([]int, error) {
	return f.flagged, f.err
}

// Flagging an account in the database is how an admin is created now. A refresh
// must add those accounts without losing the ones configured in the environment,
// which is what a deployment that still sets WALLET_ADMIN_USER_IDS relies on.
func TestRefreshAdminIDsAddsFlaggedAccountsAndKeepsConfiguredOnes(t *testing.T) {
	store := fakeAdminStore{flagged: []int{14}}
	service := NewWalletService(store, nil, nil, nil, []int{2}, 0, nil)

	if !service.IsAdmin(2) {
		t.Fatal("the configured admin was dropped at construction")
	}
	if service.IsAdmin(14) {
		t.Fatal("a flagged account counted as an admin before the first refresh")
	}

	if err := service.RefreshAdminIDs(context.Background()); err != nil {
		t.Fatalf("RefreshAdminIDs() error = %v", err)
	}
	if !service.IsAdmin(14) {
		t.Error("the flagged account was not picked up")
	}
	if !service.IsAdmin(2) {
		t.Error("the configured admin was dropped by the refresh")
	}

	store.flagged = nil
	service.repo = store
	if err := service.RefreshAdminIDs(context.Background()); err != nil {
		t.Fatalf("RefreshAdminIDs() error = %v", err)
	}
	if service.IsAdmin(14) {
		t.Error("an un-flagged account still counts as an admin")
	}
	if !service.IsAdmin(2) {
		t.Error("the configured admin did not survive a refresh that found no flags")
	}
}

func TestRefreshAdminIDsReportsAReadFailureWithoutChangingTheSet(t *testing.T) {
	service := NewWalletService(fakeAdminStore{err: errors.New("connection reset")}, nil, nil, nil, []int{2}, 0, nil)

	if err := service.RefreshAdminIDs(context.Background()); err == nil {
		t.Fatal("RefreshAdminIDs() hid a database failure")
	}
	if !service.IsAdmin(2) {
		t.Error("a failed refresh emptied the admin set")
	}
}

// fakeAdminChangeStore stands in for the account writes behind the admin list.
type fakeAdminChangeStore struct {
	walletStore
	flagged  []models.AdminAccount
	ids      []int
	setFound bool
	setErr   error
}

func (f *fakeAdminChangeStore) ListAdminIDs(context.Context) ([]int, error) { return f.ids, nil }
func (f *fakeAdminChangeStore) ListAdminAccounts(context.Context) ([]models.AdminAccount, error) {
	return f.flagged, nil
}
func (f *fakeAdminChangeStore) SetAdmin(context.Context, int, bool) (bool, error) {
	return f.setFound, f.setErr
}

// fakeAdminFarmers resolves the accounts the page names by phone.
type fakeAdminFarmers struct {
	farmerStore
	byPhone map[string]*models.Farmer
	byID    map[int]*models.Farmer
}

func (f fakeAdminFarmers) GetByPhone(phone string) (*models.Farmer, error) {
	return f.byPhone[phone], nil
}
func (f fakeAdminFarmers) GetByID(id int) (*models.Farmer, error) {
	return f.byID[id], nil
}

func TestSetAdminRefusesAnyoneWhoIsNotAnAdmin(t *testing.T) {
	store := &fakeAdminChangeStore{setFound: true}
	service := NewWalletService(store, nil, nil, nil, nil, 0, nil)

	if err := service.SetAdmin(context.Background(), 5, 9, true); !errors.Is(err, ErrNotAdmin) {
		t.Errorf("SetAdmin() error = %v, want %v", err, ErrNotAdmin)
	}
}

func TestSetAdminRefusesSelfRemovalAndConfiguredAdmins(t *testing.T) {
	// Actor and target are flagged in the database, not configured.
	store := &fakeAdminChangeStore{ids: []int{5}, setFound: true}
	service := NewWalletService(store, nil, nil, nil, nil, 0, nil)
	if err := service.RefreshAdminIDs(context.Background()); err != nil {
		t.Fatalf("RefreshAdminIDs() error = %v", err)
	}
	if err := service.SetAdmin(context.Background(), 5, 5, false); !errors.Is(err, ErrAdminSelfRemoval) {
		t.Errorf("removing yourself: error = %v, want %v", err, ErrAdminSelfRemoval)
	}

	// A role granted by the environment cannot be revoked from the page.
	configured := NewWalletService(&fakeAdminChangeStore{setFound: true}, nil, nil, nil, []int{2, 3}, 0, nil)
	if err := configured.SetAdmin(context.Background(), 2, 3, false); !errors.Is(err, ErrAdminFromConfig) {
		t.Errorf("removing a configured admin: error = %v, want %v", err, ErrAdminFromConfig)
	}
}

func TestSetAdminByPhoneNarrowsToTheAccount(t *testing.T) {
	farmers := fakeAdminFarmers{byPhone: map[string]*models.Farmer{
		"08012345678": {ID: 9, FullName: "Ada Obi", Phone: "08012345678"},
	}}
	store := &fakeAdminChangeStore{ids: []int{5}, setFound: true}
	service := NewWalletService(store, farmers, nil, nil, nil, 0, nil)
	if err := service.RefreshAdminIDs(context.Background()); err != nil {
		t.Fatalf("RefreshAdminIDs() error = %v", err)
	}

	if err := service.SetAdminByPhone(context.Background(), 5, " 08012345678 ", true); err != nil {
		t.Errorf("SetAdminByPhone() error = %v", err)
	}
	if err := service.SetAdminByPhone(context.Background(), 5, "", true); !errors.Is(err, ErrAdminPhoneRequired) {
		t.Errorf("empty phone: error = %v, want %v", err, ErrAdminPhoneRequired)
	}
	if err := service.SetAdminByPhone(context.Background(), 5, "08000000000", true); !errors.Is(err, ErrAdminNotFound) {
		t.Errorf("unknown phone: error = %v, want %v", err, ErrAdminNotFound)
	}
}

// The page has to show an admin granted in the environment even though no
// database row records it, and it has to mark that one as unremovable.
func TestAdminAccountsListsFlaggedAndConfiguredAdmins(t *testing.T) {
	store := &fakeAdminChangeStore{flagged: []models.AdminAccount{
		{FarmerID: 7, FullName: "Bola Farms", Phone: "08099999999"},
	}}
	farmers := fakeAdminFarmers{byID: map[int]*models.Farmer{
		2: {ID: 2, FullName: "Ada Obi", Phone: "08012345678"},
	}}
	service := NewWalletService(store, farmers, nil, nil, []int{2}, 0, nil)

	accounts, err := service.AdminAccounts(context.Background())
	if err != nil {
		t.Fatalf("AdminAccounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("AdminAccounts() returned %d accounts, want 2: %+v", len(accounts), accounts)
	}

	byID := make(map[int]models.AdminAccount, len(accounts))
	for _, account := range accounts {
		byID[account.FarmerID] = account
	}
	if byID[7].FullName != "Bola Farms" || byID[7].FromConfig {
		t.Errorf("flagged admin = %+v, want the database account", byID[7])
	}
	if byID[2].FullName != "Ada Obi" || !byID[2].FromConfig {
		t.Errorf("configured admin = %+v, want the configured account, marked", byID[2])
	}
}
