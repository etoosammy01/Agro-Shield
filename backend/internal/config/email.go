package config

import (
	"os"
	"strings"
)

// Welcome email is optional and cannot prevent SMS verification from starting.
func loadEmailConfig(cfg *Config) {
	cfg.BrevoAPIKey = strings.TrimSpace(os.Getenv("BREVO_API_KEY"))
	cfg.BrevoFromEmail = strings.TrimSpace(os.Getenv("BREVO_FROM_EMAIL"))
	cfg.BrevoFromName = strings.TrimSpace(os.Getenv("BREVO_FROM_NAME"))
	if cfg.BrevoFromName == "" {
		cfg.BrevoFromName = "Agro-Shield"
	}
}
