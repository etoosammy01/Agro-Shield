package diagnosis

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"backend/internal/services"

	"google.golang.org/genai"
)

// ============================================================
// GEMINI DIAGNOSIS PROVIDER
//
// Gemini is responsible ONLY for Agro-Shield diagnosis.
//
// Learning does NOT use Gemini.
// Learning uses Groq first, then OpenRouter.
// ============================================================

type GeminiDiagnosisProvider struct {
	client *genai.Client
	model  string
}

// ============================================================
// CREATE GEMINI CLIENT
// ============================================================

func newGeminiClient(apiKey string) (*genai.Client, error) {
	return genai.NewClient(
		context.Background(),
		&genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		},
	)
}

// ============================================================
// CREATE GEMINI DIAGNOSIS PROVIDER
// ============================================================

func NewGeminiDiagnosisProvider(apiKey string) (*GeminiDiagnosisProvider, error) {
	client, err := newGeminiClient(apiKey)
	if err != nil {
		return nil, err
	}

	return &GeminiDiagnosisProvider{
		client: client,
		model:  "gemini-3.5-flash",
	}, nil
}

// ============================================================
// ANALYZE FARMER'S PROBLEM
//
// This is the main diagnosis function.
//
// It can receive:
// - text
// - image
// - audio
// - video
//
// Gemini analyzes the information and returns a diagnosis.
// ============================================================

func (p *GeminiDiagnosisProvider) Analyze(request services.AIRequest) (*services.DiagnosisResult, error) {

	// ----------------------------------------------------------
	// Make sure the farmer actually sent something.
	// ----------------------------------------------------------

	if request.Category == "" &&
		request.Description == "" &&
		len(request.Image) == 0 &&
		len(request.Audio) == 0 &&
		len(request.Video) == 0 {

		return nil, errors.New("no information was provided")
	}

	// ----------------------------------------------------------
	// Diagnosis instructions.
	//
	// Keep this prompt focused on diagnosis only.
	// ----------------------------------------------------------

	prompt := `You are Agro-Shield AI, a friendly farming assistant for everyday farmers in Nigeria.

Talk to the farmer the same simple way a good mentor would talk to a friend.

Use very simple English. Use common Nigerian English that farmers can easily understand.

Do not use big grammar or difficult words.

Keep your sentences short and clear.

If you must use a difficult farming or medical word, explain it immediately in simple words.

Talk naturally and respectfully. Do not sound like a textbook, professor, or robot.

The farmer may not know much about technology, science, or farming terms, so explain things step by step.

Do not assume the farmer already understands the problem.

Do not use Markdown.

Do not use symbols such as **, ##, ###, bullet symbols, or horizontal lines.

Do not write a long introduction.

Do not repeat the farmer's question.

Do not give too much information at once.

Use this exact format:

Problem:

Tell the farmer what may be wrong in one or two simple sentences.

What you will commonly see are:

Give up to five simple signs the farmer may notice.

What you should do:

Give three to five simple things the farmer can do.

Important:

Be honest when you are not sure.

Never say that your answer is a guaranteed diagnosis.

If the problem looks serious, tell the farmer to contact a local agricultural officer, extension worker, or veterinarian.

Always focus on giving advice the farmer can understand and act on.

Your goal is not to impress the farmer with big words.

Your goal is to help the farmer understand the problem and know what to do next.

Category:
` + request.Category + `

Farmer's description:
` + request.Description + `

Analyze the attached image, audio, or video if one was provided.
Keep the complete answer below 250 words.`

	// ----------------------------------------------------------
	// Start with the text prompt.
	// ----------------------------------------------------------

	parts := []*genai.Part{
		{
			Text: prompt,
		},
	}

	// ----------------------------------------------------------
	// Add image if the farmer uploaded one.
	// ----------------------------------------------------------

	if len(request.Image) > 0 {

		processedImage, err := prepareImage(request.Image)
		if err != nil {
			return nil, errors.New("could not process image")
		}

		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				Data:     processedImage,
				MIMEType: "image/jpeg",
			},
		})
	}

	// ----------------------------------------------------------
	// Add audio if the farmer uploaded one.
	// ----------------------------------------------------------

	if len(request.Audio) > 0 {

		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				Data:     request.Audio,
				MIMEType: request.AudioType,
			},
		})
	}

	// ----------------------------------------------------------
	// Add video if the farmer uploaded one.
	// ----------------------------------------------------------

	if len(request.Video) > 0 {

		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				Data:     request.Video,
				MIMEType: request.VideoType,
			},
		})
	}

	// ----------------------------------------------------------
	// Create Gemini content.
	// ----------------------------------------------------------

	contents := []*genai.Content{
		{
			Parts: parts,
		},
	}

	// ----------------------------------------------------------
	// Thinking is normally disabled because diagnosis should
	// respond quickly.
	//
	// If Thinking is requested, allow a small thinking budget.
	// ----------------------------------------------------------

	thinkingBudget := int32(0)

	if request.Thinking {
		thinkingBudget = 1024
	}

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: 300,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget: genai.Ptr(thinkingBudget),
		},
	}

	// ----------------------------------------------------------
	// Give Gemini a maximum amount of time to respond.
	// ----------------------------------------------------------

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	// ----------------------------------------------------------
	// Try Gemini up to 3 times when the problem is temporary.
	// ----------------------------------------------------------

	var result *genai.GenerateContentResponse
	var err error

	for attempt := 0; attempt < 3; attempt++ {

		result, err = p.client.Models.GenerateContent(
			ctx,
			p.model,
			contents,
			config,
		)

		// Success or permanent error.
		if err == nil || !isTemporaryGeminiError(err) {
			break
		}

		log.Printf(
			"temporary Gemini error on attempt %d: %v",
			attempt+1,
			err,
		)

		// ------------------------------------------------------
		// Wait before trying again.
		// ------------------------------------------------------

		if attempt < 2 {

			delay := time.Duration(attempt+1) * time.Second

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				err = ctx.Err()
				attempt = 2
			}
		}
	}

	// ----------------------------------------------------------
	// Handle Gemini error.
	// ----------------------------------------------------------

	if err != nil {

		log.Printf("Gemini diagnosis failed: %v", err)

		if isTemporaryGeminiError(err) {
			return nil, errors.New(
				"the AI service is busy right now. Please wait a moment and try again",
			)
		}

		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New(
				"the AI service took too long to respond. Please try again",
			)
		}

		return nil, errors.New(
			"the AI service could not complete the analysis. Please try again",
		)
	}

	// ----------------------------------------------------------
	// Make sure Gemini actually returned something.
	// ----------------------------------------------------------

	if result == nil || len(result.Candidates) == 0 {
		return nil, errors.New("gemini returned no response")
	}

	// ----------------------------------------------------------
	// Find the text response.
	// ----------------------------------------------------------

	for _, candidate := range result.Candidates {

		if candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {

			if part.Text != "" {
				return &services.DiagnosisResult{
					Result: part.Text,
				}, nil
			}
		}
	}

	return nil, errors.New("gemini returned no diagnosis text")
}

// ============================================================
// CHECK TEMPORARY GEMINI ERRORS
//
// These errors may disappear if we try again.
//
// Examples:
// - 503
// - unavailable
// - high demand
// - resource exhausted
// - 429
// ============================================================

func isTemporaryGeminiError(err error) bool {

	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "503") ||
		strings.Contains(message, "unavailable") ||
		strings.Contains(message, "high demand") ||
		strings.Contains(message, "resource exhausted") ||
		strings.Contains(message, "429")
}