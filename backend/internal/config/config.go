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

// LoadDatabaseURL reads only the database URL.
// It lets database tools run without requiring AI API keys.
func LoadDatabaseURL() (string, error) {
	// Load .env when available.
	_ = godotenv.Load()

	// Read the database URL and remove extra spaces.
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))

	// Stop if the database URL is missing.
	if databaseURL == "" {
		return "", fmt.Errorf("DATABASE_URL must not be empty")
	}

	return databaseURL, nil
}

// Load reads the application settings from environment variables.
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

	// At least one learning AI provider must be configured.
	// Groq is primary, and OpenRouter is the backup.
	if cfg.GroqAPIKey == "" && cfg.OpenRouterAPIKey == "" {
		return Config{}, fmt.Errorf(
			"at least one learning AI key is required: GROQ_API_KEY or OPENROUTER_API_KEY",
		)
	}

	return cfg, nil
}