package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application settings in one place.
type Config struct {
	// Application
	Port   string
	AppEnv string

	// Database
	DatabaseURL string

	// Diagnosis AI — Gemini only
	GeminiAPIKey string

	// Learning AI — Groq primary, OpenRouter backup
	GroqAPIKey       string
	OpenRouterAPIKey string

	// Payments — Flutterwave
	FlutterwaveSecretKey  string
	FlutterwaveSecretHash string
	PaymentRedirectURL    string

	// Wallet administration
	WalletAdminUserIDs string
}

// Load reads application settings from environment variables.
func Load() (Config, error) {
	// Load .env when available.
	// Deployment environments can provide variables directly.
	_ = godotenv.Load()

	cfg := Config{
		Port:                  strings.TrimSpace(os.Getenv("PORT")),
		AppEnv:                strings.TrimSpace(os.Getenv("APP_ENV")),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		GeminiAPIKey:          strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GroqAPIKey:            strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		OpenRouterAPIKey:      strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		FlutterwaveSecretKey:  strings.TrimSpace(os.Getenv("FLUTTERWAVE_SECRET_KEY")),
		FlutterwaveSecretHash: strings.TrimSpace(os.Getenv("FLUTTERWAVE_SECRET_HASH")),
		PaymentRedirectURL:    strings.TrimSpace(os.Getenv("PAYMENT_REDIRECT_URL")),
		WalletAdminUserIDs:    strings.TrimSpace(os.Getenv("WALLET_ADMIN_USER_IDS")),
	}

	// Use safe defaults for basic application settings.
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.AppEnv == "" {
		cfg.AppEnv = "development"
	}

	// Database access is required to start the application.
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}

	// Gemini is required for the diagnosis feature.
	if cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("GEMINI_API_KEY must not be empty")
	}

	// Groq is the primary provider for the learning feature.
	if cfg.GroqAPIKey == "" {
		return Config{}, fmt.Errorf("GROQ_API_KEY must not be empty")
	}

	return cfg, nil
}
