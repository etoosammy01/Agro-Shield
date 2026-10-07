package services

import (
	"errors"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// DefaultPhotoURL is the placeholder photo assigned at registration, before
// the account completes the mandatory Complete Your Profile step. It
// matches the same fallback the original database migration already used
// to backfill legacy rows, so this stays consistent with the existing
// schema convention rather than introducing a new one.
const DefaultPhotoURL = "/static/assets/images/un.jpeg"

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
// A profile photo, and for farmers, bank details, are NOT required here —
// they're collected on the mandatory Complete Your Profile step that
// follows registration. If no photo is supplied, the account is created
// with DefaultPhotoURL until a real one is uploaded.
func (s *AuthService) Register(firstName, lastName, phone, email, password, community, lga, state, country, role, photoURL, bankName, accountName, accountNumber string) (*models.Farmer, error) {

	if firstName == "" || lastName == "" {
		return nil, errors.New("name is required")
	}
	if phone == "" {
		return nil, errors.New("phone is required")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}
	if strings.TrimSpace(community) == "" || strings.TrimSpace(lga) == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(country) == "" {
		return nil, errors.New("community, LGA, state, and country are required")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
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
		return nil, errors.New("phone number already registered")
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

	if err := s.repo.Create(farmer); err != nil {
		return nil, err
	}

	return farmer, nil
}

// HasCompletedProfile reports whether this account has uploaded a real
// profile photo yet, as opposed to still carrying DefaultPhotoURL from
// registration. Used to decide whether to show the Complete Your Profile
// step, and can also be used to gate produce listings or negotiations on a
// completed profile if that's wanted later.
func HasCompletedProfile(farmer *models.Farmer) bool {
	return farmer != nil && strings.TrimSpace(farmer.PhotoURL) != "" && farmer.PhotoURL != DefaultPhotoURL
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
func (s *AuthService) ResetPassword(phone, newPassword string) error {
	if phone == "" {
		return errors.New("phone number is required")
	}

	if newPassword == "" {
		return errors.New("new password is required")
	}

	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters")
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
