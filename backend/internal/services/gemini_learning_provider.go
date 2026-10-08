package services

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// ============================================================
// AGRO-SHIELD LEARNING SERVICE
//
// This file is ONLY for learning.
//
// It is different from the farmer diagnosis service.
//
// The learner first selects a farming type, for example:
// - Crop farming
// - Animal farming
// - Poultry farming
// - Fish farming
//
// The selected farming type is then used to guide the AI.
// ============================================================

type GeminiLearningProvider struct {
	client *genai.Client
	model  string
}

func NewGeminiLearningProvider(apiKey string) (*GeminiLearningProvider, error) {
	client, err := newGeminiClient(apiKey)
	if err != nil {
		return nil, err
	}

	return &GeminiLearningProvider{
		client: client,
		model:  getEnvOrDefault("GEMINI_LEARNING_MODEL", "gemini-3.5-flash"),
	}, nil
}

func getEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// Learn handles the learning conversation.
func (p *GeminiLearningProvider) Learn(
	farmingType string,
	messages []AIChatMessage,
) (string, error) {

	// ========================================================
	// 1. CHECK FARMING TYPE
	// ========================================================

	farmingType = strings.TrimSpace(farmingType)

	if farmingType == "" {
		return "", errors.New("farming type is required")
	}

	// ========================================================
	// 2. CHECK LEARNING MESSAGE
	// ========================================================

	if len(messages) == 0 {
		return "", errors.New("no learning question was provided")
	}

	// ========================================================
	// 3. BUILD CONVERSATION
	//
	// We convert our application messages into Gemini messages.
	// ========================================================

	contents := make([]*genai.Content, 0, len(messages))

	for _, message := range messages {

		role := "user"

		if message.Role == "assistant" {
			role = "model"
		}

		contents = append(contents, &genai.Content{
			Role: role,
			Parts: []*genai.Part{
				{
					Text: message.Content,
				},
			},
		})
	}

	// ========================================================
	// 4. LEARNING INSTRUCTIONS
	//
	// This is the main brain of the learning assistant.
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
	// 5. GEMINI REQUEST CONFIGURATION
	// ========================================================

	thinkingBudget := int32(0)

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{
					Text: instructions,
				},
			},
		},
		MaxOutputTokens: 1500,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget: genai.Ptr(thinkingBudget),
		},
	}
	// ========================================================
	// 6. CREATE REQUEST TIMEOUT
	// ========================================================

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	// ========================================================
	// 7. ASK GEMINI
	// ========================================================

	result, err := p.client.Models.GenerateContent(
		ctx,
		p.model,
		contents,
		config,
	)

	// ========================================================
	// 8. HANDLE GEMINI ERROR
	// ========================================================

	if err != nil {

		log.Printf(
			"Gemini learning request failed: %v",
			err,
		)

		if isTemporaryGeminiError(err) {
			return "",
				errors.New(
					"the AI service is busy right now. Please wait a moment and try again",
				)
		}

		if errors.Is(err, context.DeadlineExceeded) {
			return "",
				errors.New(
					"the AI service took too long to respond. Please try again",
				)
		}

		return "",
			errors.New(
				"the AI service could not complete the lesson. Please try again",
			)
	}

	// ========================================================
	// 9. CHECK GEMINI RESPONSE
	// ========================================================
	if result != nil && len(result.Candidates) > 0 {
		log.Printf("learning: finish reason = %v", result.Candidates[0].FinishReason)
	}

	if result == nil || len(result.Candidates) == 0 {
		return "",
			errors.New(
				"gemini returned no learning response",
			)
	}

	// ========================================================
	// 10. GET THE LEARNING ANSWER
	// ========================================================

	for _, candidate := range result.Candidates {

		if candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {

			answer := strings.TrimSpace(part.Text)

			if answer != "" {
				return answer, nil
			}
		}
	}

	// ========================================================
	// 11. NO USABLE RESPONSE
	// ========================================================

	return "",
		errors.New(
			"gemini returned no learning response",
		)
}
