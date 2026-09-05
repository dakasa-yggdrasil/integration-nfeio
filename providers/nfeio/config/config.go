// Package config loads the runtime environment for the nfeio adapter.
// NFEIO_API_KEY and NFEIO_WEBHOOK_SECRET are mandatory. Load returns an error
// if the API key is missing or the webhook secret is outside the provider's
// 32 to 64 character contract (main.go translates that into process exit).
package config

import (
	"errors"
	"os"
	"strings"
)

// Config holds the runtime envs for the nfeio adapter.
type Config struct {
	APIKey        string
	WebhookSecret string
	CompanyID     string // optional default; per-call override allowed
	BaseURL       string
	WebhookPort   string
	HealthPort    string
	TemplatesDir  string
}

// Load reads envs and applies defaults. It validates the provider's webhook
// secret length and whitespace contract before any listener becomes ready.
func Load() (*Config, error) {
	webhookSecret := os.Getenv("NFEIO_WEBHOOK_SECRET")
	cfg := &Config{
		APIKey:        strings.TrimSpace(os.Getenv("NFEIO_API_KEY")),
		WebhookSecret: webhookSecret,
		CompanyID:     strings.TrimSpace(os.Getenv("NFEIO_COMPANY_ID")),
		BaseURL:       envOr("NFEIO_BASE_URL", "https://api.nfe.io"),
		WebhookPort:   envOr("WEBHOOK_PORT", "8082"),
		HealthPort:    envOr("HEALTHCHECK_PORT", "8080"),
		TemplatesDir:  envOr("TEMPLATES_DIR", "manifest/templates"),
	}
	if cfg.APIKey == "" {
		return nil, errors.New("NFEIO_API_KEY must be set")
	}
	if len(cfg.WebhookSecret) < 32 || len(cfg.WebhookSecret) > 64 || strings.TrimSpace(cfg.WebhookSecret) != cfg.WebhookSecret {
		return nil, errors.New("NFEIO_WEBHOOK_SECRET must contain 32 to 64 characters without surrounding whitespace")
	}
	return cfg, nil
}

func envOr(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}
