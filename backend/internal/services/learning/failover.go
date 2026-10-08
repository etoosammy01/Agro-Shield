package learning

import (
	"errors"
	"log"
	"strings"

	"backend/internal/services"
)

// ============================================================
// LEARNING FAILOVER PROVIDER
//
// Groq is the PRIMARY learning provider.
//
// OpenRouter is the BACKUP learning provider.
//
// Flow:
//
//	Question
//	    ↓
//	Groq
//	    ↓
//	Success ─────────────→ Answer
//	    ↓
//	Failure
//	    ↓
//	OpenRouter
//	    ↓
//	Success ─────────────→ Answer
//	    ↓
//	Failure
//	    ↓
//	Friendly error
//
// Gemini is NOT part of this flow.
// ============================================================

type FailoverLearningProvider struct {
	primary  services.LearningProvider
	backup   services.LearningProvider
}

// NewFailoverLearningProvider creates a learning provider that
// tries Groq first and OpenRouter second.
func NewFailoverLearningProvider(
	primary services.LearningProvider,
	backup services.LearningProvider,
) *FailoverLearningProvider {

	return &FailoverLearningProvider{
		primary: primary,
		backup:  backup,
	}
}

// ============================================================
// LEARN
// ============================================================

func (p *FailoverLearningProvider) Learn(
	farmingType string,
	messages []services.AIChatMessage,
) (string, error) {

	farmingType = strings.TrimSpace(farmingType)

	if farmingType == "" {
		return "", errors.New("farming type is required")
	}

	if len(messages) == 0 {
		return "", errors.New("no learning question was provided")
	}

	// ========================================================
	// TRY PRIMARY PROVIDER
	// ========================================================

	if p.primary != nil {
		answer, err := p.primary.Learn(
			farmingType,
			messages,
		)

		if err == nil && strings.TrimSpace(answer) != "" {
			return strings.TrimSpace(answer), nil
		}

		if err != nil {
			log.Printf(
				"primary learning provider failed: %v",
				err,
			)
		}
	}

	// ========================================================
	// TRY BACKUP PROVIDER
	// ========================================================

	if p.backup != nil {
		answer, err := p.backup.Learn(
			farmingType,
			messages,
		)

		if err == nil && strings.TrimSpace(answer) != "" {
			return strings.TrimSpace(answer), nil
		}

		if err != nil {
			log.Printf(
				"backup learning provider failed: %v",
				err,
			)
		}
	}

	// ========================================================
	// BOTH PROVIDERS FAILED
	// ========================================================

	return "", errors.New(
		"the learning AI services are temporarily unavailable. Please try again",
	)
}