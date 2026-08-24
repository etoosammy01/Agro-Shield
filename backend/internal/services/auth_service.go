package services

import (
	"errors"

	"backend/internal/models"
	"backend/internal/repository"

	"golang.org/x/crypto/bcrypt"
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
func (s *AuthService) Register(firstName, lastName, phone, email, password, location, role, photoURL string) error {

	if firstName == "" || lastName == "" {
		return errors.New("name is required")
	}
	if phone == "" {
		return errors.New("phone is required")
	}
	if password == "" {
		return errors.New("password is required")
	}
	if photoURL == "" {
		return errors.New("profile picture is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if role != "buyer" {
		role = "farmer"
	}

	existing, err := s.repo.GetByPhone(phone)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New("phone number already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	farmer := &models.Farmer{
		FullName:     firstName + " " + lastName,
		Phone:        phone,
		Email:        email,
		PasswordHash: string(hash),
		Location:     location,
		Role:         role,
		PhotoURL:     photoURL,
	}

	return s.repo.Create(farmer)
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
func (s *AuthService) UpdateProfile(id int, fullName, phone, email, location string) error {
	if fullName == "" {
		return errors.New("full name is required")
	}
	if phone == "" {
		return errors.New("phone is required")
	}
	return s.repo.UpdateProfile(id, fullName, phone, email, location)
}

// UpdatePhoto sets the user's passport photograph.
func (s *AuthService) UpdatePhoto(id int, photoURL string) error {
	if photoURL == "" {
		return errors.New("photo URL is required")
	}

	return s.repo.UpdatePhoto(id, photoURL)
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
