package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// OTP.dev delivery settings are built into the application. Only the API key
// is supplied by the environment.
const (
	otpDevSender     = "AgroShield"
	otpDevTemplateID = "" // Set to the template ID from the OTP.dev account.
	otpDevBaseURL    = "https://api.otp.dev"
)

// Config holds all application settings in one place.
type Config struct {
	// Welcome email — Brevo SMTP
	// Welcome email — Brevo API
	BrevoAPIKey    string
	BrevoFromEmail string
	BrevoFromName  string
	// Application
	Port   string
	AppEnv string

	// TrustProxyHeaders must only be enabled when a trusted reverse proxy
	// overwrites X-Forwarded-For. TrustedProxyCIDRs are the networks from which
	// those headers are accepted; forwarded values are ignored for any other
	// peer, so a directly reachable client cannot choose its own rate-limit key.
	TrustProxyHeaders bool
	TrustedProxyCIDRs []*net.IPNet

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

	// WalletStaleDepositHours is how long a funding attempt may stay unsettled
	// before it is listed on the admin page for an operator to investigate. It is
	// a reporting threshold only — nothing is credited or expired automatically.
	WalletStaleDepositHours string

	// OTP delivery — SMS
	//
	// SMSProvider selects "log" for local development or "otpdev" for SMS
	// delivery and verification through OTP.dev.
	SMSProvider      string
	OTPDevAPIKey     string
	OTPDevSender     string
	OTPDevTemplateID string
	OTPDevBaseURL    string
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
		// Welcome email — Brevo SMTP
		// Welcome email — Brevo API
		BrevoAPIKey:    strings.TrimSpace(os.Getenv("BREVO_API_KEY")),
		BrevoFromEmail: strings.TrimSpace(os.Getenv("BREVO_FROM_EMAIL")),
		BrevoFromName:  strings.TrimSpace(os.Getenv("BREVO_FROM_NAME")),

		// Application
		Port:   strings.TrimSpace(os.Getenv("PORT")),
		AppEnv: strings.TrimSpace(os.Getenv("APP_ENV")),

		// Database
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),

		// AI providers
		GeminiAPIKey:     strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GroqAPIKey:       strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		OpenRouterAPIKey: strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),

		// Payments
		FlutterwaveSecretKey:  strings.TrimSpace(os.Getenv("FLUTTERWAVE_SECRET_KEY")),
		FlutterwaveSecretHash: strings.TrimSpace(os.Getenv("FLUTTERWAVE_SECRET_HASH")),
		PaymentRedirectURL:    strings.TrimSpace(os.Getenv("PAYMENT_REDIRECT_URL")),

		FlutterwavePaymentOptions: strings.TrimSpace(os.Getenv("FLUTTERWAVE_PAYMENT_OPTIONS")),

		// Wallet administration
		WalletAdminUserIDs:      strings.TrimSpace(os.Getenv("WALLET_ADMIN_USER_IDS")),
		WalletStaleDepositHours: strings.TrimSpace(os.Getenv("WALLET_STALE_DEPOSIT_HOURS")),
		CommissionPercent:       strings.TrimSpace(os.Getenv("COMMISSION_PERCENT")),

		// SMS / OTP
		SMSProvider:      strings.TrimSpace(os.Getenv("SMS_PROVIDER")),
		OTPDevAPIKey:     strings.TrimSpace(os.Getenv("OTPDEV_API_KEY")),
		OTPDevSender:     otpDevSender,
		OTPDevTemplateID: otpDevTemplateID,
		OTPDevBaseURL:    otpDevBaseURL,
	}
	// Accept the previous API key name for existing deployments.
	if cfg.OTPDevAPIKey == "" {
		cfg.OTPDevAPIKey = strings.TrimSpace(os.Getenv("OTP_DEV_API_KEY"))
	}

	rawTrustProxy := strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS"))
	rawTrustedCIDRs := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))

	if rawTrustProxy != "" {
		trust, err := strconv.ParseBool(rawTrustProxy)
		if err != nil {
			return Config{}, fmt.Errorf("TRUST_PROXY_HEADERS must be true or false: %w", err)
		}
		cfg.TrustProxyHeaders = trust
	}

	if cfg.TrustProxyHeaders {
		if rawTrustedCIDRs == "" {
			return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS must list the proxy networks when TRUST_PROXY_HEADERS is true")
		}

		for _, item := range strings.Split(rawTrustedCIDRs, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}

			_, network, err := net.ParseCIDR(item)
			if err != nil {
				return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS contains an invalid network %q: %w", item, err)
			}

			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, network)
		}

		if len(cfg.TrustedProxyCIDRs) == 0 {
			return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain at least one proxy network")
		}
	} else if rawTrustedCIDRs != "" {
		return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS requires TRUST_PROXY_HEADERS=true")
	}

	// Card and bank transfer are the two funding routes we advertise.
	if cfg.FlutterwavePaymentOptions == "" {
		cfg.FlutterwavePaymentOptions = "card,banktransfer"
	}

	// Safe defaults for basic application settings.
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.AppEnv == "" {
		cfg.AppEnv = "development"
	}

	// An API key enables OTP.dev automatically; development without a key uses logs.
	if cfg.SMSProvider == "" {
		cfg.SMSProvider = "log"
		if cfg.OTPDevAPIKey != "" {
			cfg.SMSProvider = "otpdev"
		}
	}

	// Wallet reporting threshold.
	if cfg.WalletStaleDepositHours == "" {
		cfg.WalletStaleDepositHours = "24"
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
	if cfg.GroqAPIKey == "" && cfg.OpenRouterAPIKey == "" {
		return Config{}, fmt.Errorf(
			"at least one learning AI key is required: GROQ_API_KEY or OPENROUTER_API_KEY",
		)
	}

	// Validate the payment redirect URL.
	if cfg.PaymentRedirectURL == "" {
		return Config{}, fmt.Errorf("PAYMENT_REDIRECT_URL must not be empty")
	}

	redirect, err := url.Parse(cfg.PaymentRedirectURL)
	if err != nil {
		return Config{}, fmt.Errorf("PAYMENT_REDIRECT_URL is not a valid URL: %w", err)
	}

	if redirect.Scheme != "http" && redirect.Scheme != "https" {
		return Config{}, fmt.Errorf(
			"PAYMENT_REDIRECT_URL must be an absolute http(s) URL, got %q",
			cfg.PaymentRedirectURL,
		)
	}

	if redirect.Host == "" {
		return Config{}, fmt.Errorf(
			"PAYMENT_REDIRECT_URL must include a host, got %q",
			cfg.PaymentRedirectURL,
		)
	}

	// Validate SMS provider configuration.
	switch cfg.SMSProvider {
	case "log":
		// No provider credentials are needed for local development.
	case "otpdev":
		if cfg.OTPDevAPIKey == "" {
			return Config{}, fmt.Errorf("OTPDEV_API_KEY must not be empty when SMS_PROVIDER=otpdev")
		}
		if cfg.OTPDevSender == "" {
			return Config{}, fmt.Errorf("OTP.dev sender must be set in backend/internal/config/config.go")
		}
		if cfg.OTPDevTemplateID == "" {
			return Config{}, fmt.Errorf("OTP.dev template ID must be set in backend/internal/config/config.go")
		}
	default:
		return Config{}, fmt.Errorf(
			"SMS_PROVIDER must be \"log\" or \"otpdev\", got %q",
			cfg.SMSProvider,
		)
	}

	return cfg, nil
}
