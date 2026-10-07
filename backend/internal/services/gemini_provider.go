package services

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"google.golang.org/genai"
)

// ============================================================
// 1. GEMINI PROVIDER
// This struct holds the Gemini client and the model we want to use.
// ============================================================

type GeminiDiagnosisProvider struct {
	client *genai.Client
	model  string
}

// ============================================================
// 2. CREATE GEMINI PROVIDERS
// These functions connect Agro-Shield to Gemini with separate responsibilities.
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

// ======================terseerterese4@gmail.com======================================
// 3. ANALYZE FARMER REQUEST
// This function sends the farmer's information to Gemini.
//
// It can receive:
// - Text
// - Image
// - Audio
// - Video
// ============================================================

func (p *GeminiDiagnosisProvider) Analyze(
	request AIRequest,
) (*DiagnosisResult, error) {

	// ========================================================
	// 4. CHECK FARMER INPUT
	// Make sure the farmer actually sent something.
	// ========================================================

	if request.Category == "" &&
		request.Description == "" &&
		len(request.Image) == 0 &&
		len(request.Audio) == 0 &&
		len(request.Video) == 0 {

		return nil, errors.New("no information was provided")
	}

	// ========================================================
	// 5. AGRO-SHIELD AI INSTRUCTIONS
	// This tells Gemini how Agro-Shield should talk to farmers.
	// ========================================================

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

	// ========================================================
	// 6. CREATE AI REQUEST PARTS
	// Start with the text instructions.
	// ========================================================

	parts := []*genai.Part{
		{
			Text: prompt,
		},
	}

	// ========================================================
	// 7. ADD IMAGE
	// If the farmer sent an image, attach it to the AI request.
	// ========================================================

	// ============================================================
	// PROCESS IMAGE BEFORE SENDING TO GEMINI
	//
	// Large phone images can make AI analysis slow.
	// We resize and compress the image first.
	// ============================================================

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

	// ========================================================
	// 8. ADD AUDIO
	// If the farmer sent a voice recording, attach it.
	// ========================================================

	if len(request.Audio) > 0 {
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				Data:     request.Audio,
				MIMEType: request.AudioType,
			},
		})
	}

	// ========================================================
	// 9. ADD VIDEO
	// If the farmer sent a video, attach it.
	// ========================================================

	if len(request.Video) > 0 {
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{
				Data:     request.Video,
				MIMEType: request.VideoType,
			},
		})
	}

	// ========================================================
	// 10. BUILD THE GEMINI CONTENT
	// Put all the information together before sending it.
	// ========================================================

	contents := []*genai.Content{
		{
			Parts: parts,
		},
	}

	// ========================================================
	// 11. SEND REQUEST TO GEMINI
	// This is the part that was taking about 16.8 seconds.
	//
	// We are now:
	// - Limiting the answer size
	// - Turning thinking off for this test
	// ========================================================

	thinkingBudget := int32(0)
	if request.Thinking {
		thinkingBudget = 1024
	}
	config := &genai.GenerateContentConfig{
		MaxOutputTokens: 300,
		ThinkingConfig:  &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(thinkingBudget)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result *genai.GenerateContentResponse
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		result, err = p.client.Models.GenerateContent(ctx, p.model, contents, config)
		if err == nil || !isTemporaryGeminiError(err) {
			break
		}

		log.Printf("temporary Gemini error on attempt %d: %v", attempt+1, err)
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

	// If Gemini gives an error, return it.
	if err != nil {
		log.Printf("Gemini analysis failed: %v", err)
		if isTemporaryGeminiError(err) {
			return nil, errors.New("the AI service is busy right now. Please wait a moment and try again")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("the AI service took too long to respond. Please try again")
		}
		return nil, errors.New("the AI service could not complete the analysis. Please try again")
	}

	// ========================================================
	// 12. CHECK GEMINI RESPONSE
	// Make sure Gemini actually returned something.
	// ========================================================

	if result == nil || len(result.Candidates) == 0 {
		return nil, errors.New("gemini returned no response")
	}

	// ========================================================
	// 13. GET THE AI'S TEXT
	// Find the text inside Gemini's response.
	// ========================================================

	for _, candidate := range result.Candidates {

		// Skip candidates that have no content.
		if candidate.Content == nil {
			continue
		}

		// Look through the returned parts.
		for _, part := range candidate.Content.Parts {

			// We found the AI's answer.
			if part.Text != "" {
				return &DiagnosisResult{
					Result: part.Text,
				}, nil
			}
		}
	}

	// ========================================================
	// 14. NO DIAGNOSIS FOUND
	// Gemini responded, but we couldn't find usable text.
	// ========================================================

	return nil, errors.New("gemini returned no diagnosis text")
}

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
