package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// One-time passcode policy. These are deliberately tight: the code guards
// money movement and account recovery, and nothing else in this codebase
// rate-limits anything, so the limits have to live here.
const (
	otpCodeLength = 6
	otpTTL        = 5 * time.Minute

	// otpMaxAttempts is how many wrong guesses a single code tolerates before
	// it is burned and the user must request a new one.
	otpMaxAttempts = 5

	// At most otpMaxIssuesPerWindow codes per farmer per purpose, so a
	// compromised session cannot be used to bomb someone's phone (or run up
	// an SMS bill).
	otpIssueWindow        = 15 * time.Minute
	otpMaxIssuesPerWindow = 3
)

// Sentinel errors. Handlers surface these to the user directly, so the text is
// written to be read by a person.
var (
	// ErrOTPInvalid means the supplied code was wrong or malformed.
	ErrOTPInvalid = errors.New("that verification code is not correct")

	// ErrOTPUnavailable means there is nothing to verify against — no code was
	// requested, or the previous one expired or was already used.
	ErrOTPUnavailable = errors.New("that verification code has expired or was already used. Request a new one")

	// ErrOTPTooManyAttempts means this code is spent after repeated failures.
	ErrOTPTooManyAttempts = errors.New("too many incorrect attempts. Request a new verification code")

	// ErrOTPRateLimited means the user asked for codes too often.
	ErrOTPRateLimited = errors.New("you have requested too many codes. Please wait a few minutes and try again")

	// ErrOTPUnusablePhone means the account's phone number cannot receive SMS.
	ErrOTPUnusablePhone = errors.New("your profile needs a valid Nigerian phone number before you can receive a verification code")
)

// otpStore is the persistence OTPService needs.
//
// *repository.OTPRepository satisfies it. It exists as an interface so the
// attempt cap, expiry and single-use rules — the parts that decide whether a
// wallet payout is really verified — can be tested without a database.
type otpStore interface {
	Issue(ctx context.Context, farmerID int, phone, purpose, codeHash string, expiresAt time.Time) error
	LatestActive(ctx context.Context, farmerID int, purpose string) (*models.OTPCode, error)
	IncrementAttempts(ctx context.Context, id int64) error
	Consume(ctx context.Context, id int64) error
	CountIssuedSince(ctx context.Context, farmerID int, purpose string, since time.Time) (int, error)
}

// OTPService issues and verifies one-time passcodes.
//
// Passcodes are generated with crypto/rand, stored only as bcrypt hashes, and
// are single-use with a short expiry.
type OTPService struct {
	repo otpStore
	sms  SMSSender
}

func NewOTPService(repo *repository.OTPRepository, sms SMSSender) *OTPService {
	return &OTPService{repo: repo, sms: sms}
}

// otpPurposeLabel is the human-readable name used in the SMS body.
func otpPurposeLabel(purpose string) string {
	switch purpose {
	case models.OTPPurposeWithdrawal:
		return "withdrawal"
	case models.OTPPurposeTransfer:
		return "transfer"
	case models.OTPPurposePasswordReset:
		return "password reset"
	default:
		return "verification"
	}
}

// generateOTPCode returns a uniformly random otpCodeLength-digit code.
//
// rand.Int is used rather than reducing raw random bytes modulo a range, which
// would bias the result toward low codes.
func generateOTPCode() (string, error) {
	limit := big.NewInt(1)
	for i := 0; i < otpCodeLength; i++ {
		limit.Mul(limit, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("generate one-time passcode: %w", err)
	}
	return fmt.Sprintf("%0*d", otpCodeLength, n.Int64()), nil
}

// isWellFormedOTPCode reports whether code is exactly otpCodeLength digits.
// Checked before any database or hashing work so junk input is rejected cheaply.
func isWellFormedOTPCode(code string) bool {
	if len(code) != otpCodeLength {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Issue generates a fresh code for this farmer and purpose, invalidates any
// code still outstanding for the same purpose, and sends it by SMS.
//
// phone must be the number already on the account — never one supplied by the
// browser, or the second factor proves nothing.
func (s *OTPService) Issue(ctx context.Context, farmerID int, phone, purpose string) error {
	if farmerID <= 0 {
		return errors.New("invalid account for one-time passcode")
	}
	if !models.ValidOTPPurpose(purpose) {
		return fmt.Errorf("unsupported one-time passcode purpose %q", purpose)
	}

	// The account's stored number has to be usable before we promise a code.
	// This is also why a profile with an unparseable number must be corrected
	// before a payout can be requested.
	recipient := NormalizeNigerianPhone(phone)
	if recipient == "" {
		return ErrOTPUnusablePhone
	}

	issued, err := s.repo.CountIssuedSince(ctx, farmerID, purpose, time.Now().Add(-otpIssueWindow))
	if err != nil {
		return fmt.Errorf("check one-time passcode rate limit: %w", err)
	}
	if issued >= otpMaxIssuesPerWindow {
		return ErrOTPRateLimited
	}

	code, err := generateOTPCode()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash one-time passcode: %w", err)
	}

	// Store before sending: if the SMS fails we want the failure to surface to
	// the user, and a stored-but-unsent code simply expires.
	expiresAt := time.Now().Add(otpTTL)
	if err := s.repo.Issue(ctx, farmerID, phone, purpose, string(hash), expiresAt); err != nil {
		return fmt.Errorf("store one-time passcode: %w", err)
	}

	message := fmt.Sprintf(
		"Your Agro-Shield %s code is %s. It expires in %d minutes. Never share this code with anyone.",
		otpPurposeLabel(purpose), code, int(otpTTL.Minutes()),
	)
	if err := s.sms.Send(recipient, message); err != nil {
		return fmt.Errorf("send one-time passcode: %w", err)
	}
	return nil
}

// Verify checks a submitted code and, on success, consumes it so it cannot be
// replayed. Every failure is counted against the code.
func (s *OTPService) Verify(ctx context.Context, farmerID int, purpose, code string) error {
	if farmerID <= 0 {
		return errors.New("invalid account for one-time passcode")
	}
	if !models.ValidOTPPurpose(purpose) {
		return fmt.Errorf("unsupported one-time passcode purpose %q", purpose)
	}
	code = strings.TrimSpace(code)
	if !isWellFormedOTPCode(code) {
		return ErrOTPInvalid
	}

	active, err := s.repo.LatestActive(ctx, farmerID, purpose)
	if errors.Is(err, repository.ErrNoActiveOTP) {
		return ErrOTPUnavailable
	}
	if err != nil {
		return fmt.Errorf("load one-time passcode: %w", err)
	}

	// A code that has already burned through its attempts must not be usable
	// even if the guess that follows happens to be right.
	if active.Attempts >= otpMaxAttempts {
		if err := s.repo.Consume(ctx, active.ID); err != nil {
			return fmt.Errorf("burn exhausted one-time passcode: %w", err)
		}
		return ErrOTPTooManyAttempts
	}

	// bcrypt's comparison is constant-time, so a wrong code leaks nothing about
	// how close it was.
	if err := bcrypt.CompareHashAndPassword([]byte(active.CodeHash), []byte(code)); err != nil {
		// Snapshot the count before recording the failure: the repository may
		// hand back a struct it later mutates, and this must not depend on that.
		attempts := active.Attempts
		if err := s.repo.IncrementAttempts(ctx, active.ID); err != nil {
			return fmt.Errorf("record failed one-time passcode attempt: %w", err)
		}
		if attempts+1 >= otpMaxAttempts {
			if err := s.repo.Consume(ctx, active.ID); err != nil {
				return fmt.Errorf("burn exhausted one-time passcode: %w", err)
			}
			return ErrOTPTooManyAttempts
		}
		return ErrOTPInvalid
	}

	if err := s.repo.Consume(ctx, active.ID); err != nil {
		return fmt.Errorf("consume one-time passcode: %w", err)
	}
	return nil
}
