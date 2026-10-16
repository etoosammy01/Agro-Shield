package config

import (
	"fmt"
	"net/url"
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

	// FlutterwavePaymentOptions is the comma-separated list of funding methods
	// offered on the hosted checkout, e.g. "card,banktransfer". Sent explicitly
	// so which methods appear is decided by this file and not by whatever the
	// Flutterwave dashboard happens to be set to.
	FlutterwavePaymentOptions string

	// CommissionPercent is the platform's cut of marketplace sales, e.g. "2.5".
	// Empty or "0" means no commission, so leaving it unset changes nothing.
	CommissionPercent string

	// Wallet administration
	WalletAdminUserIDs string

	// OTP delivery — SMS
	//
	// SMSProvider selects the transport: "log" writes one-time codes to the
	// server log (the default, so local development needs no vendor account),
	// and "termii" sends them over the Termii SMS API.
	SMSProvider    string
	TermiiAPIKey   string
	TermiiSenderID string
	TermiiBaseURL  string
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

		FlutterwavePaymentOptions: strings.TrimSpace(os.Getenv("FLUTTERWAVE_PAYMENT_OPTIONS")),

		WalletAdminUserIDs: strings.TrimSpace(os.Getenv("WALLET_ADMIN_USER_IDS")),

		CommissionPercent: strings.TrimSpace(os.Getenv("COMMISSION_PERCENT")),

		SMSProvider:    strings.TrimSpace(os.Getenv("SMS_PROVIDER")),
		TermiiAPIKey:   strings.TrimSpace(os.Getenv("TERMII_API_KEY")),
		TermiiSenderID: strings.TrimSpace(os.Getenv("TERMII_SENDER_ID")),
		TermiiBaseURL:  strings.TrimSpace(os.Getenv("TERMII_BASE_URL")),
	}

	// Card and bank transfer are the two funding routes we advertise; naming
	// them here keeps the checkout consistent instead of inheriting whatever
	// the Flutterwave dashboard is set to.
	if cfg.FlutterwavePaymentOptions == "" {
		cfg.FlutterwavePaymentOptions = "card,banktransfer"
	}

	// Use safe defaults for basic application settings.
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.AppEnv == "" {
		cfg.AppEnv = "development"
	}

	// One-time codes go to the server log unless a real SMS provider is
	// selected, so a missing vendor account never blocks startup.
	if cfg.SMSProvider == "" {
		cfg.SMSProvider = "log"
	}
	if cfg.TermiiBaseURL == "" {
		cfg.TermiiBaseURL = "https://api.ng.termii.com"
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

	// The hosted checkout is only usable if the provider knows where to send the
	// buyer afterwards. Flutterwave rejects a missing redirect_url mid-deposit
	// with a 400 that names the request field, not this setting, so by the time
	// it surfaces the cause is three layers from the symptom. Fail here instead.
	if cfg.PaymentRedirectURL == "" {
		return Config{}, fmt.Errorf("PAYMENT_REDIRECT_URL must not be empty")
	}
	redirect, err := url.Parse(cfg.PaymentRedirectURL)
	if err != nil {
		return Config{}, fmt.Errorf("PAYMENT_REDIRECT_URL is not a valid URL: %w", err)
	}
	if redirect.Scheme != "http" && redirect.Scheme != "https" {
		return Config{}, fmt.Errorf(
			"PAYMENT_REDIRECT_URL must be an absolute http(s) URL, got %q", cfg.PaymentRedirectURL,
		)
	}
	if redirect.Host == "" {
		return Config{}, fmt.Errorf(
			"PAYMENT_REDIRECT_URL must include a host, got %q", cfg.PaymentRedirectURL,
		)
	}

	// One-time codes protect password resets and every wallet payout, so a
	// half-configured SMS provider must fail loudly at startup rather than
	// silently dropping codes at runtime.
	switch cfg.SMSProvider {
	case "log":
		// Always available — no credentials required.
	case "termii":
		if cfg.TermiiAPIKey == "" {
			return Config{}, fmt.Errorf("TERMII_API_KEY must not be empty when SMS_PROVIDER=termii")
		}
		if cfg.TermiiSenderID == "" {
			return Config{}, fmt.Errorf("TERMII_SENDER_ID must not be empty when SMS_PROVIDER=termii")
		}
	default:
		return Config{}, fmt.Errorf("SMS_PROVIDER must be \"log\" or \"termii\", got %q", cfg.SMSProvider)
	}

	return cfg, nil
}
