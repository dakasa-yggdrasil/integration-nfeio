package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInjectAuth_SetsRawAuthorizationHeader(t *testing.T) {
	// NFe.io requires Authorization=<api_key> with NO "Bearer " prefix.
	// See client/nfe-io.go in dakasa-enterprise-payments-api — the legacy
	// client sets the raw value.
	req := httptest.NewRequest(http.MethodGet, "https://api.nfe.io/v2/companies", nil)
	injectAuth(req, "secretkey123")
	got := req.Header.Get("Authorization")
	if got != "secretkey123" {
		t.Fatalf("Authorization = %q; want raw apiKey without Bearer prefix", got)
	}
}

func TestInjectAuth_EmptyKey_StillSetsHeader(t *testing.T) {
	// Empty key is caught at config.Load() — but the helper itself is
	// permissive so callers can pass any string. The server returns 401.
	req := httptest.NewRequest(http.MethodGet, "https://api.nfe.io/v2/x", nil)
	injectAuth(req, "")
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q; want empty", got)
	}
}
