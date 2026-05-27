// Package config loads the runtime environment for the nfeio adapter.
// NFEIO_API_KEY and NFEIO_WEBHOOK_SECRET are mandatory — Load() returns
// an error if either is missing (main.go translates that into a fatal
// log + process exit).
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

// Load reads envs and applies defaults. Returns error if NFEIO_API_KEY or
// NFEIO_WEBHOOK_SECRET is empty.
func Load() (*Config, error) {
	cfg := &Config{
		APIKey:        strings.TrimSpace(os.Getenv("NFEIO_API_KEY")),
		WebhookSecret: strings.TrimSpace(os.Getenv("NFEIO_WEBHOOK_SECRET")),
		CompanyID:     strings.TrimSpace(os.Getenv("NFEIO_COMPANY_ID")),
		BaseURL:       envOr("NFEIO_BASE_URL", "https://api.nfe.io"),
		WebhookPort:   envOr("WEBHOOK_PORT", "8082"),
		HealthPort:    envOr("HEALTHCHECK_PORT", "8080"),
		TemplatesDir:  envOr("TEMPLATES_DIR", "manifest/templates"),
	}
	if cfg.APIKey == "" {
		return nil, errors.New("NFEIO_API_KEY must be set")
	}
	if cfg.WebhookSecret == "" {
		return nil, errors.New("NFEIO_WEBHOOK_SECRET must be set")
	}
	return cfg, nil
}

func envOr(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}
