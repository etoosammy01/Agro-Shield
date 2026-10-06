package services

type AIRequest struct {
	Category    string
	Description string
	Thinking    bool

	Image     []byte
	ImageType string

	Audio     []byte
	AudioType string

	Video     []byte
	VideoType string
}

type AIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type DiagnosisResult struct {
	Result string
}

// backend/internal/services/ai_provider.go
type AIProvider interface {
    Analyze(request AIRequest) (*DiagnosisResult, error)
    Learn(farmingType string, messages []AIChatMessage) (string, error)
}