package services

import (
	"errors"
	"testing"
)

type learningProviderStub struct {
	farmingType string
}

func (p *learningProviderStub) Analyze(AIRequest) (*DiagnosisResult, error) {
	return nil, errors.New("not implemented")
}

func (p *learningProviderStub) Learn(farmingType string, _ []AIChatMessage) (string, error) {
	p.farmingType = farmingType
	return "lesson", nil
}

func TestLearnPassesSelectedFarmingType(t *testing.T) {
	provider := &learningProviderStub{}
	service := NewAIService(nil, provider, nil)
	answer, err := service.Learn("  Poultry farming  ", []AIChatMessage{
		{Role: "user", Content: "How should I prepare a coop?"},
	})
	if err != nil {
		t.Fatalf("Learn returned error: %v", err)
	}
	if answer != "lesson" {
		t.Fatalf("Learn answer = %q, want %q", answer, "lesson")
	}
	if provider.farmingType != "Poultry farming" {
		t.Fatalf("provider received farming type %q", provider.farmingType)
	}
}

func TestLearnRequiresFarmingType(t *testing.T) {
	service := NewAIService(nil, &learningProviderStub{}, nil)
	_, err := service.Learn(" ", []AIChatMessage{{Role: "user", Content: "Question?"}})
	if !errors.Is(err, ErrInvalidLearningChat) {
		t.Fatalf("Learn error = %v, want ErrInvalidLearningChat", err)
	}
}
