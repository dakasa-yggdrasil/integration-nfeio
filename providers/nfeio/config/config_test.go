package config

import (
	"os"
	"testing"
)

func setEnvs(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoad_Success(t *testing.T) {
	setEnvs(t, map[string]string{
		"NFEIO_API_KEY":        "k123",
		"NFEIO_WEBHOOK_SECRET": "s456",
		"NFEIO_COMPANY_ID":     "cmp",
		"NFEIO_BASE_URL":       "https://api.nfe.io",
		"WEBHOOK_PORT":         "8082",
		"HEALTHCHECK_PORT":     "8080",
		"TEMPLATES_DIR":        "/etc/nfeio/templates",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.APIKey != "k123" || cfg.WebhookSecret != "s456" || cfg.CompanyID != "cmp" {
		t.Fatalf("config fields mismatch: %+v", cfg)
	}
	if cfg.BaseURL != "https://api.nfe.io" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestLoad_FatalOnEmptyAPIKey(t *testing.T) {
	os.Unsetenv("NFEIO_API_KEY")
	t.Setenv("NFEIO_WEBHOOK_SECRET", "s")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() must error when NFEIO_API_KEY is empty")
	}
}

func TestLoad_FatalOnEmptyWebhookSecret(t *testing.T) {
	t.Setenv("NFEIO_API_KEY", "k")
	os.Unsetenv("NFEIO_WEBHOOK_SECRET")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() must error when NFEIO_WEBHOOK_SECRET is empty")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("NFEIO_API_KEY", "k")
	t.Setenv("NFEIO_WEBHOOK_SECRET", "s")
	os.Unsetenv("NFEIO_BASE_URL")
	os.Unsetenv("WEBHOOK_PORT")
	os.Unsetenv("HEALTHCHECK_PORT")
	os.Unsetenv("TEMPLATES_DIR")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.BaseURL != "https://api.nfe.io" {
		t.Errorf("default BaseURL = %q; want https://api.nfe.io", cfg.BaseURL)
	}
	if cfg.WebhookPort != "8082" {
		t.Errorf("default WebhookPort = %q; want 8082", cfg.WebhookPort)
	}
	if cfg.HealthPort != "8080" {
		t.Errorf("default HealthPort = %q; want 8080", cfg.HealthPort)
	}
	if cfg.TemplatesDir != "manifest/templates" {
		t.Errorf("default TemplatesDir = %q; want manifest/templates", cfg.TemplatesDir)
	}
}
