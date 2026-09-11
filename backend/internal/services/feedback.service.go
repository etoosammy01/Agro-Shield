package services

import (
	"backend/internal/models"
	"backend/internal/repository"
	"errors"
)

type FeedbackService struct {
	repo *repository.FeedbackRepository
}

func NewFeedbackService(repo *repository.FeedbackRepository) *FeedbackService {
	return &FeedbackService{
		repo: repo,
	}
}

func (s *FeedbackService) CreateFeedback(feedback *models.Feedback) error {
	if feedback.UserID == 0 {
		return errors.New("user is required")
	}

	if feedback.FarmerID == 0 {
		return errors.New("farmer is required")
	}

	if feedback.Rating < 1 || feedback.Rating > 5 {
		return errors.New("rating must be between 1 and 5")
	}

	if feedback.Comment == "" {
		return errors.New("feedback comment is required")
	}

	return s.repo.Create(feedback)
}

func (s *FeedbackService) GetFeedbackByFarmer(farmerID int) ([]models.Feedback, error) {

	if farmerID == 0 {
		return nil, errors.New("farmer is required")
	}

	return s.repo.GetByFarmerID(farmerID)
}

func (s *FeedbackService) GetFarmerRating(farmerID int) (float64, int, error) {
	if farmerID == 0 {
		return 0, 0, errors.New("farmer is required")
	}

	return s.repo.GetFarmerRating(farmerID)
}
