package services

import (
	"context"
	"errors"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// DefaultPhotoURL is the placeholder assigned before a selfie is captured.
const DefaultPhotoURL = "/static/assets/images/un.jpeg"

// Sentinel errors that handlers can match with errors.Is to show the right
// message to the user, instead of comparing error strings.
var (
	// ErrPhoneTaken means the phone number already belongs to an account.
	ErrPhoneTaken = repository.ErrPhoneExists

	// ErrPasswordTooShort means the password is under the minimum length.
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")

	// Account-closure errors. They are separate so the profile page can say
	// what is actually wrong instead of a generic failure.
	ErrDeletePasswordRequired = errors.New("password is required")
	ErrDeletePasswordWrong    = errors.New("password is incorrect")
	ErrDeletePhoneMismatch    = errors.New("phone number does not match this account")
)

type AuthService struct {
	repo *repository.FarmerRepository
}

func NewAuthService(repo *repository.FarmerRepository) *AuthService {
	return &AuthService{
		repo: repo,
	}
}

// Register creates a new farmer or buyer account. role should be "farmer" or
// "buyer" — anything else defaults to "farmer".
//
// Selfie capture and phone verification follow registration. Bank details
// can be added later from the wallet.
//
// Returns ErrPhoneTaken if the phone number is already registered, and
// ErrPasswordTooShort if the password is too short.
func (s *AuthService) Register(firstName, lastName, phone, email, password, community, lga, state, country, role, photoURL, bankName, accountName, accountNumber string) (*models.Farmer, error) {

	if firstName == "" || lastName == "" {
		return nil, errors.New("name is required")
	}
	if phone == "" {
		return nil, errors.New("phone is required")
	}
	if NormalizeNigerianPhone(phone) == "" {
		return nil, ErrOTPUnusablePhone
	}
	if password == "" {
		return nil, errors.New("password is required")
	}
	if strings.TrimSpace(community) == "" || strings.TrimSpace(lga) == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(country) == "" {
		return nil, errors.New("community, LGA, state, and country are required")
	}
	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}
	if role != "buyer" {
		role = "farmer"
	}

	if strings.TrimSpace(photoURL) == "" {
		photoURL = DefaultPhotoURL
	}

	existing, err := s.repo.GetByPhone(phone)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrPhoneTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	farmer := &models.Farmer{
		FullName:     firstName + " " + lastName,
		Phone:        phone,
		Email:        email,
		PasswordHash: string(hash),
		Location:     strings.TrimSpace(community),
		LGA:          strings.TrimSpace(lga),
		State:        strings.TrimSpace(state),
		Country:      strings.TrimSpace(country),
		Role:         role,
		PhotoURL:     photoURL,

		BankName:      bankName,
		AccountName:   accountName,
		AccountNumber: accountNumber,
	}

	// If another signup claimed this phone between the check above and now,
	// the repository returns repository.ErrPhoneExists, which is the same
	// value as ErrPhoneTaken, so callers handle both cases identically.
	if err := s.repo.Create(farmer); err != nil {
		return nil, err
	}

	return farmer, nil
}

// HasCompletedProfile requires both persisted onboarding steps. An existing
// uploaded profile picture alone does not prove a selfie was captured.
func HasCompletedProfile(farmer *models.Farmer) bool {
	return farmer != nil && farmer.SelfieCapturedAt.Valid && farmer.PhoneVerifiedAt.Valid
}

// OnboardingPath returns the next required step, or the dashboard when done.
func OnboardingPath(farmer *models.Farmer) string {
	if farmer == nil || !farmer.SelfieCapturedAt.Valid {
		return "/complete-profile"
	}
	if !farmer.PhoneVerifiedAt.Valid {
		return "/verify-phone"
	}
	return "/dashboard"
}

func (s *AuthService) UpdateSelfie(id int, photoURL string) error {
	if strings.TrimSpace(photoURL) == "" {
		return errors.New("a selfie is required")
	}
	return s.repo.UpdateSelfie(id, photoURL)
}

// Login verifies a phone number + password against the stored hash and
// returns the matching farmer/buyer on success.
func (s *AuthService) Login(phone, password string) (*models.Farmer, error) {

	if phone == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}

	farmer, err := s.repo.GetByPhone(phone)
	if err != nil {
		return nil, err
	}
	if farmer == nil {
		return nil, errors.New("invalid phone number or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(farmer.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid phone number or password")
	}

	return farmer, nil
}

// GetFarmerByID loads a user's full details, e.g. for the profile page.
func (s *AuthService) GetFarmerByID(id int) (*models.Farmer, error) {
	return s.repo.GetByID(id)
}

// FindByPhone loads an account by phone number, returning (nil, nil) when no
// account exists. The password-reset flow uses this to decide where to send a
// one-time passcode, and deliberately does not tell the caller which case it got.
func (s *AuthService) FindByPhone(phone string) (*models.Farmer, error) {
	return s.repo.GetByPhone(strings.TrimSpace(phone))
}

// UpdateProfile edits a user's name, phone and location.
func (s *AuthService) UpdateProfile(id int, fullName, phone, email, community, lga, state, country string) error {
	fullName = strings.TrimSpace(fullName)
	phone = strings.TrimSpace(phone)
	email = strings.TrimSpace(email)
	community = strings.TrimSpace(community)
	lga = strings.TrimSpace(lga)
	state = strings.TrimSpace(state)
	country = strings.TrimSpace(country)
	if fullName == "" {
		return errors.New("full name is required")
	}
	if phone == "" {
		return errors.New("phone is required")
	}
	if NormalizeNigerianPhone(phone) == "" {
		return ErrOTPUnusablePhone
	}
	if community == "" || lga == "" || state == "" || country == "" {
		return errors.New("community, LGA, state, and country are required")
	}
	return s.repo.UpdateProfile(id, fullName, phone, email, community, lga, state, country)
}

// UpdatePhoto sets the user's passport photograph.
func (s *AuthService) UpdatePhoto(id int, photoURL string) error {
	if photoURL == "" {
		return errors.New("photo URL is required")
	}

	return s.repo.UpdatePhoto(id, photoURL)
}

// UpdateBankDetails sets a farmer's payout bank details. Any field may be
// blank — payout details are optional and can be completed later from the
// profile page.
func (s *AuthService) UpdateBankDetails(id int, bankName, bankCode, accountName, accountNumber string) error {
	bankName = strings.TrimSpace(bankName)
	bankCode = strings.TrimSpace(bankCode)
	accountName = strings.TrimSpace(accountName)
	accountNumber = strings.TrimSpace(accountNumber)
	if bankName == "" || bankCode == "" || accountName == "" || len(accountNumber) != 10 {
		return errors.New("complete Nigerian bank payout details are required")
	}
	for _, value := range bankCode + accountNumber {
		if value < '0' || value > '9' {
			return errors.New("bank code and account number must contain digits only")
		}
	}
	return s.repo.UpdateBankDetails(id, bankName, bankCode, accountName, accountNumber)
}

// ResetPassword changes a user's password.
//
// WARNING: this method performs no verification of the caller's identity — it is
// only the write. It previously backed a flow that let anyone who knew a
// registered phone number set a new password and take over that account. Every
// caller MUST first prove control of the account's phone number with a
// one-time passcode (services.OTPService.Verify) before reaching this.
func (s *AuthService) ResetPassword(phone, newPassword string) error {
	if phone == "" {
		return errors.New("phone number is required")
	}

	if newPassword == "" {
		return errors.New("new password is required")
	}

	if len(newPassword) < 8 {
		return ErrPasswordTooShort
	}

	farmer, err := s.repo.GetByPhone(phone)
	if err != nil {
		return err
	}

	if farmer == nil {
		return errors.New("account not found")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(farmer.ID, string(hash))
}

// VerifyPassword reports whether password matches the account's stored hash.
// The caller must already know which account is being acted on; this is an
// ownership proof for destructive actions such as closing an account, not a
// login. It returns ErrDeletePasswordWrong for a bad password and
// repository.ErrAccountNotFound for an unknown or already closed account.
func (s *AuthService) VerifyPassword(farmerID int, password string) error {
	if password == "" {
		return ErrDeletePasswordRequired
	}
	farmer, err := s.repo.GetByID(farmerID)
	if err != nil {
		return err
	}
	if farmer == nil {
		return repository.ErrAccountNotFound
	}
	if err := bcrypt.CompareHashAndPassword([]byte(farmer.PasswordHash), []byte(password)); err != nil {
		return ErrDeletePasswordWrong
	}
	return nil
}

// CloseAccount permanently closes a farmer's account after proving control of
// it with the current password and the account's own phone number. The
// repository keeps the financial history and anonymises the personal details,
// so this is irreversible in the sense that matters to the user: the profile
// and login are gone.
func (s *AuthService) CloseAccount(ctx context.Context, farmerID int, confirmPhone, password string) error {
	farmer, err := s.repo.GetByID(farmerID)
	if err != nil {
		return err
	}
	if farmer == nil {
		return repository.ErrAccountNotFound
	}

	if !samePhone(farmer.Phone, confirmPhone) {
		return ErrDeletePhoneMismatch
	}
	if password == "" {
		return ErrDeletePasswordRequired
	}
	if err := bcrypt.CompareHashAndPassword([]byte(farmer.PasswordHash), []byte(password)); err != nil {
		return ErrDeletePasswordWrong
	}

	return s.repo.DeleteAccount(ctx, farmerID)
}

// samePhone compares two phone numbers by their last ten digits, so the same
// number typed with a country code, spaces, or dashes still matches. The
// confirmation is an "are you sure" check on the caller's own account, not an
// authorization decision, so being forgiving about formatting is safe.
func samePhone(a, b string) bool {
	digits := func(value string) string {
		var out strings.Builder
		for _, r := range value {
			if r >= '0' && r <= '9' {
				out.WriteRune(r)
			}
		}
		value = out.String()
		if len(value) > 10 {
			value = value[len(value)-10:]
		}
		return value
	}
	left, right := digits(a), digits(b)
	return left != "" && left == right
}
