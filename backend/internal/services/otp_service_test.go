package services

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// --- test doubles ---------------------------------------------------------

// recordingSender captures messages instead of sending them, so tests can read
// back the code the user would have received.
type recordingSender struct {
	mu       sync.Mutex
	to       string
	messages []string
	err      error
}

func (s *recordingSender) Send(to, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.to = to
	s.messages = append(s.messages, message)
	return nil
}

// lastCode pulls the passcode out of the most recent message body.
func (s *recordingSender) lastCode(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		t.Fatal("no SMS was sent")
	}
	match := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(s.messages[len(s.messages)-1])
	if match == nil {
		t.Fatalf("no 6-digit code in message %q", s.messages[len(s.messages)-1])
	}
	return match[1]
}

// fakeOTPStore is an in-memory otpStore, so the verification rules can be
// tested without a database.
type fakeOTPStore struct {
	codes  []*models.OTPCode
	nextID int64
	closed bool // when true, every method fails
}

func (f *fakeOTPStore) Issue(_ context.Context, farmerID int, phone, purpose, codeHash string, expiresAt time.Time) error {
	if f.closed {
		return errors.New("store unavailable")
	}
	// Mirror the real repository: a new code invalidates outstanding ones.
	now := time.Now()
	for _, c := range f.codes {
		if c.FarmerID == farmerID && c.Purpose == purpose && !c.ConsumedAt.Valid {
			c.ConsumedAt.Valid = true
			c.ConsumedAt.Time = now
		}
	}
	f.nextID++
	f.codes = append(f.codes, &models.OTPCode{
		ID:        f.nextID,
		FarmerID:  farmerID,
		Phone:     phone,
		Purpose:   purpose,
		CodeHash:  codeHash,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	})
	return nil
}

func (f *fakeOTPStore) LatestActive(_ context.Context, farmerID int, purpose string) (*models.OTPCode, error) {
	var found *models.OTPCode
	for _, c := range f.codes {
		if c.FarmerID != farmerID || c.Purpose != purpose || c.ConsumedAt.Valid {
			continue
		}
		if !c.ExpiresAt.After(time.Now()) {
			continue
		}
		if found == nil || c.ID > found.ID {
			found = c
		}
	}
	if found == nil {
		return nil, repository.ErrNoActiveOTP
	}
	return found, nil
}

func (f *fakeOTPStore) IncrementAttempts(_ context.Context, id int64) error {
	for _, c := range f.codes {
		if c.ID == id {
			c.Attempts++
			return nil
		}
	}
	return nil
}

func (f *fakeOTPStore) Consume(_ context.Context, id int64) error {
	for _, c := range f.codes {
		if c.ID == id && !c.ConsumedAt.Valid {
			c.ConsumedAt.Valid = true
			c.ConsumedAt.Time = time.Now()
		}
	}
	return nil
}

func (f *fakeOTPStore) CountIssuedSince(_ context.Context, farmerID int, purpose string, since time.Time) (int, error) {
	count := 0
	for _, c := range f.codes {
		if c.FarmerID == farmerID && c.Purpose == purpose && !c.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func newTestOTPService() (*OTPService, *fakeOTPStore, *recordingSender) {
	store := &fakeOTPStore{}
	sender := &recordingSender{}
	return NewOTPService(nil, sender).withStore(store), store, sender
}

// withStore swaps in a fake store. The exported constructor keeps taking the
// concrete repository, so production wiring is unchanged.
func (s *OTPService) withStore(store otpStore) *OTPService {
	s.repo = store
	return s
}

const testPhone = "08012345678"

// --- tests ----------------------------------------------------------------

func TestIssueSendsNormalizedNumberAndStoresHashOnly(t *testing.T) {
	svc, store, sender := newTestOTPService()

	if err := svc.Issue(context.Background(), 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if sender.to != "2348012345678" {
		t.Fatalf("SMS sent to %q, want the international form", sender.to)
	}

	code := sender.lastCode(t)
	if len(store.codes) != 1 {
		t.Fatalf("stored %d codes, want 1", len(store.codes))
	}
	// The plaintext must never be persisted.
	if store.codes[0].CodeHash == code {
		t.Fatal("passcode was stored in the clear")
	}
	if bcrypt.CompareHashAndPassword([]byte(store.codes[0].CodeHash), []byte(code)) != nil {
		t.Fatal("stored hash does not match the code that was sent")
	}
}

func TestVerifyAcceptsCorrectCodeExactlyOnce(t *testing.T) {
	svc, _, sender := newTestOTPService()
	ctx := context.Background()

	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	code := sender.lastCode(t)

	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, code); err != nil {
		t.Fatalf("Verify() with correct code error = %v", err)
	}
	// Replay must fail: the code is single-use.
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, code); !errors.Is(err, ErrOTPUnavailable) {
		t.Fatalf("replayed Verify() error = %v, want ErrOTPUnavailable", err)
	}
}

func TestVerifyRejectsWrongCodeAndBurnsAfterMaxAttempts(t *testing.T) {
	svc, _, sender := newTestOTPService()
	ctx := context.Background()

	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	code := sender.lastCode(t)

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	// The first otpMaxAttempts-1 failures report a plain mismatch.
	for i := 1; i < otpMaxAttempts; i++ {
		if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, wrong); !errors.Is(err, ErrOTPInvalid) {
			t.Fatalf("attempt %d error = %v, want ErrOTPInvalid", i, err)
		}
	}
	// The final failure exhausts the code.
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, wrong); !errors.Is(err, ErrOTPTooManyAttempts) {
		t.Fatalf("final attempt error = %v, want ErrOTPTooManyAttempts", err)
	}
	// A burned code must not be usable even with the correct value.
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, code); err == nil {
		t.Fatal("burned code was accepted")
	}
}

func TestVerifyRejectsExpiredCode(t *testing.T) {
	svc, store, _ := newTestOTPService()
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	store.codes = append(store.codes, &models.OTPCode{
		ID: 1, FarmerID: 1, Purpose: models.OTPPurposeWithdrawal,
		CodeHash: string(hash), ExpiresAt: time.Now().Add(-time.Second), CreatedAt: time.Now().Add(-otpTTL),
	})

	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, "123456"); !errors.Is(err, ErrOTPUnavailable) {
		t.Fatalf("Verify() on expired code error = %v, want ErrOTPUnavailable", err)
	}
}

func TestVerifyRejectsMalformedCodeWithoutTouchingStore(t *testing.T) {
	svc, store, sender := newTestOTPService()
	ctx := context.Background()

	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	_ = sender

	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, bad); !errors.Is(err, ErrOTPInvalid) {
			t.Fatalf("Verify(%q) error = %v, want ErrOTPInvalid", bad, err)
		}
	}
	// Malformed input must not count against the real code's attempt budget.
	if store.codes[0].Attempts != 0 {
		t.Fatalf("malformed input consumed %d attempts", store.codes[0].Attempts)
	}
}

func TestIssueInvalidatesPreviousCode(t *testing.T) {
	svc, _, sender := newTestOTPService()
	ctx := context.Background()

	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	first := sender.lastCode(t)

	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}
	second := sender.lastCode(t)

	if first == second {
		t.Skip("generated the same code twice; nothing to assert")
	}
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, first); err == nil {
		t.Fatal("the superseded code was still accepted")
	}
	if err := svc.Verify(ctx, 1, models.OTPPurposeWithdrawal, second); err != nil {
		t.Fatalf("the current code was rejected: %v", err)
	}
}

func TestIssueRateLimits(t *testing.T) {
	svc, _, _ := newTestOTPService()
	ctx := context.Background()

	for i := 0; i < otpMaxIssuesPerWindow; i++ {
		if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); err != nil {
			t.Fatalf("Issue() %d error = %v", i+1, err)
		}
	}
	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeWithdrawal); !errors.Is(err, ErrOTPRateLimited) {
		t.Fatalf("Issue() past the limit error = %v, want ErrOTPRateLimited", err)
	}

	// The limit is per purpose, so a different purpose is unaffected.
	if err := svc.Issue(ctx, 1, testPhone, models.OTPPurposeTransfer); err != nil {
		t.Fatalf("Issue() for another purpose error = %v", err)
	}
}

func TestIssueRejectsUnusablePhone(t *testing.T) {
	svc, _, _ := newTestOTPService()

	for _, bad := range []string{"", "12345", "not-a-number", "+1 555 0100"} {
		err := svc.Issue(context.Background(), 1, bad, models.OTPPurposeWithdrawal)
		if !errors.Is(err, ErrOTPUnusablePhone) {
			t.Fatalf("Issue(%q) error = %v, want ErrOTPUnusablePhone", bad, err)
		}
	}
}

func TestIssueRejectsUnknownPurpose(t *testing.T) {
	svc, _, _ := newTestOTPService()

	if err := svc.Issue(context.Background(), 1, testPhone, "not_a_purpose"); err == nil {
		t.Fatal("Issue() accepted an unsupported purpose")
	}
	if err := svc.Verify(context.Background(), 1, "not_a_purpose", "123456"); err == nil {
		t.Fatal("Verify() accepted an unsupported purpose")
	}
}

func TestGenerateOTPCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := generateOTPCode()
		if err != nil {
			t.Fatalf("generateOTPCode() error = %v", err)
		}
		if !isWellFormedOTPCode(code) {
			t.Fatalf("generateOTPCode() = %q, which is not %d digits", code, otpCodeLength)
		}
		seen[code] = true
	}
	// A generator returning a constant would pass the shape check above.
	if len(seen) < 100 {
		t.Fatalf("only %d distinct codes in 500 draws — generator looks non-random", len(seen))
	}
}

func TestIsWellFormedOTPCode(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"123456", true},
		{"000000", true},
		{"999999", true},
		{"12345", false},
		{"1234567", false},
		{"12345a", false},
		{"", false},
		{" 123456", false},
		{"12 456", false},
	}
	for _, tt := range tests {
		if got := isWellFormedOTPCode(tt.code); got != tt.want {
			t.Errorf("isWellFormedOTPCode(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
