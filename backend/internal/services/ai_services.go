package services

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
)

type AIService struct {
	repo      *repository.DiagnosisRepository
	provider  AIProvider
	eventRepo *repository.MarketEventRepository
}

func (s *AIService) DeleteDiagnosis(farmerID, diagnosisID int) error {
	return s.repo.DeleteForFarmer(diagnosisID, farmerID)
}

func NewAIService(
	repo *repository.DiagnosisRepository,
	provider AIProvider,
	eventRepo *repository.MarketEventRepository,
) *AIService {
	return &AIService{
		repo:      repo,
		provider:  provider,
		eventRepo: eventRepo,
	}
}

func (s *AIService) Diagnose(
	farmerID int,
	request AIRequest,
) (*models.Diagnosis, error) {

	if request.Category == "" &&
		request.Description == "" &&
		len(request.Image) == 0 &&
		len(request.Audio) == 0 &&
		len(request.Video) == 0 {
		return nil, errors.New("no information was provided")
	}

	aiStart := time.Now()
	log.Println("⏱️ AI analysis started")

	aiResult, err := s.provider.Analyze(request)

	log.Printf("⏱️ AI analysis took: %v", time.Since(aiStart))

	if err != nil {
		return nil, err
	}

	diagnosis := &models.Diagnosis{
		FarmerID:    farmerID,
		Category:    request.Category,
		Description: request.Description,
		Result:      aiResult.Result,
	}

	dbStart := time.Now()
	log.Println("⏱️ Saving diagnosis to database")

	if err := s.repo.Create(diagnosis); err != nil {
		return nil, err
	}

	log.Printf("⏱️ Database save took: %v", time.Since(dbStart))

	// Record this diagnosis as an activity event for the dashboard.
	if s.eventRepo != nil {
		uid := farmerID
		metadata := "{}"
		if b, mErr := json.Marshal(map[string]string{"category": diagnosis.Category}); mErr == nil {
			metadata = string(b)
		}
		_ = s.eventRepo.Record(&models.MarketEvent{
			EventType: "diagnosis_completed",
			UserID:    &uid,
			Metadata:  metadata,
		})
	}

	return diagnosis, nil
}

func (s *AIService) History(farmerID int) ([]models.Diagnosis, error) {
	return s.repo.ListByFarmer(farmerID)
}

func (s *AIService) CountThisMonth(farmerID int) (int, error) {
	return s.repo.CountThisMonth(farmerID)
}