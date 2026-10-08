package learning

// ============================================================
// LEARNING PROVIDER CONFIGURATION
//
// This file contains values shared by the learning providers.
//
// Groq is the primary provider.
// OpenRouter is the backup provider.
// ============================================================

const (
	// Groq is the primary AI provider for learning.
	groqAPIURL = "https://api.groq.com/openai/v1/chat/completions"

	// OpenRouter is the backup AI provider for learning.
	openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"

	// Current Groq model used by Agro-Shield learning.
	groqLearningModel = "openai/gpt-oss-20b"

	// OpenRouter backup model.
	openRouterLearningModel = "openai/gpt-oss-20b"
)