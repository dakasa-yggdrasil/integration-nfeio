package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

func newMockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func mustNewClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	cfg := &config.Config{
		APIKey:        "key123",
		WebhookSecret: "ws",
		CompanyID:     "cmpDefault",
		BaseURL:       baseURL,
	}
	cli, err := NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient err = %v", err)
	}
	return cli
}

func TestClient_BearerAuthHeader_Present(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "key123" {
			t.Errorf("upstream Authorization = %q; want raw key (no Bearer prefix)", got)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "Processing", "id": "abc"})
	})
	defer srv.Close()

	cli := mustNewClient(t, srv.URL)
	var out map[string]any
	if err := cli.do(context.Background(), http.MethodGet, "/v2/companies/cmp/serviceinvoices/abc", nil, &out); err != nil {
		t.Fatalf("do() err = %v", err)
	}
	if out["id"] != "abc" {
		t.Fatalf("decode mismatch: %+v", out)
	}
}

func TestClient_RateLimitRetry_RespectsRetryAfter(t *testing.T) {
	calls := 0
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"status":429,"name":"rate_limited","message":"slow down"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	})
	defer srv.Close()

	cli := mustNewClient(t, srv.URL)
	start := time.Now()
	var out map[string]any
	if err := cli.do(context.Background(), http.MethodGet, "/v2/x", nil, &out); err != nil {
		t.Fatalf("do() err = %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Errorf("client did not honor Retry-After=1 (elapsed=%v)", elapsed)
	}
	if calls != 2 {
		t.Errorf("calls = %d; want 2 (first 429, second 200)", calls)
	}
}

func TestClient_TerminalError_NoRetry(t *testing.T) {
	calls := 0
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":422,"name":"municipal_rejection","message":"bad iss"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	err := cli.do(context.Background(), http.MethodGet, "/v2/x", nil, nil)
	if err == nil {
		t.Fatal("expected error on 422")
	}
	apiErr := &NfeIoAPIError{}
	if !errors.As(err, &apiErr) || apiErr.Status != 422 {
		t.Errorf("err = %v; want *NfeIoAPIError{Status:422}", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d; want 1 (terminal — no retry)", calls)
	}
}
