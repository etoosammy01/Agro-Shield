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

type DiagnosisProvider interface {
	Analyze(request AIRequest) (*DiagnosisResult, error)
}

type LearningProvider interface {
	Learn(farmingType string, messages []AIChatMessage) (string, error)
}