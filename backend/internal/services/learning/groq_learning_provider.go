package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"backend/internal/services"
)

// ============================================================
// GROQ LEARNING PROVIDER
//
// This provider is ONLY for the Agro-Shield learning service.
//
// Gemini is NOT used here.
//
// Groq is the primary AI provider for learning.
// ============================================================

type GroqLearningProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGroqLearningProvider creates the Groq learning provider.
func NewGroqLearningProvider(apiKey string) (*GroqLearningProvider, error) {
	apiKey = strings.TrimSpace(apiKey)

	if apiKey == "" {
		return nil, errors.New("GROQ_API_KEY is required")
	}

	return &GroqLearningProvider{
		apiKey: apiKey,
		model:  "openai/gpt-oss-20b",
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

// ============================================================
// GROQ API REQUEST TYPES
// ============================================================

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqChatRequest struct {
	Model          string         `json:"model"`
	Messages       []groqMessage  `json:"messages"`
	MaxTokens      int            `json:"max_completion_tokens"`
	Temperature    float64        `json:"temperature"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type groqChatResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`

	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ============================================================
// LEARN
//
// This keeps the original Agro-Shield learning prompt.
//
// Only the AI connection has changed from Gemini to Groq.
// ============================================================

func (p *GroqLearningProvider) Learn(
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
	// ORIGINAL AGRO-SHIELD LEARNING PROMPT
	// ========================================================

	instructions := `
You are Agro-Shield Learning Assistant.

You are a friendly agriculture teacher helping people in Nigeria
learn farming step by step.

The learner has selected this farming type:

` + farmingType + `

IMPORTANT:

Teach the learner mainly about the selected farming type.

Do not randomly change to another farming type.

For example:

If the learner selected Crop Farming, focus on crops.

If the learner selected Animal Farming, focus on livestock and animals.

If the learner selected Poultry Farming, focus on poultry.

If the learner selected Fish Farming, focus on fish and aquaculture.

If the learner asks something that is not related to the selected
farming type, explain briefly and guide the learner back to the
selected farming type when appropriate.

============================================================
TEACHING STYLE
============================================================

Use very simple English.

The learner may be a complete beginner.

Do not assume the learner already knows farming.

Teach one idea at a time.

Explain difficult farming words immediately using simple words.

Use practical examples that make sense to farmers in Nigeria.

Use Nigerian farming examples where appropriate.

Explain WHY something is done, not only WHAT to do.

When teaching a process, explain it step by step.

Do not rush the learner.

Do not use big grammar.

Do not sound like a textbook.

Do not sound like a professor.

Talk like a good agriculture mentor teaching a beginner.

============================================================
LEARNING APPROACH
============================================================

When the learner asks to learn a topic:

1. Explain what the topic means.

2. Explain why it is important.

3. Explain the main things the learner needs to know.

4. Give a simple practical example.

5. If the topic involves a process, teach the process step by step.

6. Ask a short question at the end when it would help the learner
check their understanding.

Do not always ask a question if the learner is simply asking for
a direct explanation.

============================================================
FARMING CONTEXT
============================================================

Always keep the selected farming type in mind.

Selected farming type:

` + farmingType + `

============================================================
BEGINNER SAFETY
============================================================

Do not present dangerous pesticide, chemical, veterinary medicine,
or other risky instructions as guaranteed instructions.

When professional help is needed, tell the learner to contact a
qualified agricultural extension worker, veterinarian, or other
appropriate professional.

Be honest when information depends on the animal, crop, location,
weather, soil, farm size, or other conditions.

============================================================
CONVERSATION
============================================================

Use previous messages as context.

Answer the learner's latest question directly.

Do not repeat the learner's question.

Do not give unnecessary information.

Keep the answer focused.

Use plain text.

Do not use Markdown formatting.

Keep the response under 400 words.
`

	// ========================================================
	// BUILD GROQ MESSAGES
	// ========================================================

	groqMessages := make([]groqMessage, 0, len(messages)+1)

	// The original Gemini system instruction becomes the
	// system message for Groq.
	groqMessages = append(groqMessages, groqMessage{
		Role:    "system",
		Content: instructions,
	})

	for _, message := range messages {
		role := strings.TrimSpace(message.Role)

		switch role {
		case "assistant":
			role = "assistant"

		case "user":
			role = "user"

		default:
			role = "user"
		}

		content := strings.TrimSpace(message.Content)

		if content == "" {
			continue
		}

		groqMessages = append(groqMessages, groqMessage{
			Role:    role,
			Content: content,
		})
	}

	if len(groqMessages) == 1 {
		return "", errors.New("no valid learning message was provided")
	}

	// ========================================================
	// GROQ REQUEST
	// ========================================================

	requestBody := groqChatRequest{
		Model:           p.model,
		Messages:        groqMessages,
		MaxTokens:       500,
		Temperature:     0.4,
		ReasoningEffort: "low",
	}

	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", errors.New("failed to prepare learning request")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewReader(requestJSON),
	)
	if err != nil {
		return "", errors.New("failed to create learning request")
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// ========================================================
	// SEND REQUEST
	// ========================================================

	response, err := p.client.Do(req)
	if err != nil {
		log.Printf("Groq learning request failed: %v", err)

		if errors.Is(err, context.DeadlineExceeded) {
			return "", errors.New("groq learning request timed out")
		}

		return "", errors.New("groq learning request failed")
	}

	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", errors.New("failed to read Groq response")
	}

	// ========================================================
	// HANDLE GROQ ERRORS
	// ========================================================

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {

		var errorResponse groqChatResponse

		if err := json.Unmarshal(responseBody, &errorResponse); err == nil {
			if errorResponse.Error != nil &&
				strings.TrimSpace(errorResponse.Error.Message) != "" {

				log.Printf(
					"Groq learning API error: status=%d message=%s",
					response.StatusCode,
					errorResponse.Error.Message,
				)

				return "", errors.New(errorResponse.Error.Message)
			}
		}

		log.Printf(
			"Groq learning API returned HTTP %d",
			response.StatusCode,
		)

		return "", errors.New("groq learning service returned an error")
	}

	// ========================================================
	// PARSE SUCCESSFUL RESPONSE
	// ========================================================

	var result groqChatResponse

	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", errors.New("failed to parse Groq learning response")
	}

	if len(result.Choices) == 0 {
		return "", errors.New("groq returned no learning response")
	}

	answer := strings.TrimSpace(
		result.Choices[0].Message.Content,
	)

	if answer == "" {
		return "", errors.New("groq returned an empty learning response")
	}

	return answer, nil
}