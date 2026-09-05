package adapter

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

const webhookTestSecret = "0123456789abcdef0123456789abcdef"

func computeHMAC(secret, body string) string {
	m := hmac.New(sha1.New, []byte(secret))
	m.Write([]byte(body))
	return "sha1=" + hex.EncodeToString(m.Sum(nil))
}

func newWebhookTestServer(t *testing.T, publish func(queue string, payload []byte) error) *WebhookServer {
	t.Helper()
	cfg := &config.Config{WebhookSecret: webhookTestSecret, WebhookPort: "0"}
	srv := NewWebhookServer(cfg, nil, zap.NewNop())
	if publish != nil {
		srv.SetPublisher(publish)
	}
	return srv
}

func TestWebhook_ValidHMAC_PublishesIssued(t *testing.T) {
	var publishedQueue string
	srv := newWebhookTestServer(t, func(queue string, _ []byte) error {
		publishedQueue = queue
		return nil
	})
	body := `{"id":"evt-1","event":"nfse.issued","data":{"id":"inv-1","status":"Issued"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature", computeHMAC(webhookTestSecret, body))
	rr := httptest.NewRecorder()
	srv.handle(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d; want 202; body=%s", rr.Code, rr.Body.String())
	}
	if publishedQueue != "enterprise-payments.nfe.emitted.q" {
		t.Errorf("queue = %q; want enterprise-payments.nfe.emitted.q", publishedQueue)
	}
}

func TestWebhook_InvalidHMAC_401(t *testing.T) {
	srv := newWebhookTestServer(t, nil)
	body := `{"id":"e","event":"nfse.issued"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature", "sha1=deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	rr := httptest.NewRecorder()
	srv.handle(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rr.Code)
	}
}

func TestWebhook_DuplicateID_Returns200(t *testing.T) {
	callCount := 0
	srv := newWebhookTestServer(t, func(queue string, _ []byte) error {
		callCount++
		return nil
	})
	body := `{"id":"evt-same","event":"nfse.issued","data":{"id":"i","status":"Issued"}}`
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature", computeHMAC(webhookTestSecret, body))
		rr := httptest.NewRecorder()
		srv.handle(rr, req)
		if i == 0 && rr.Code != http.StatusAccepted {
			t.Errorf("first call status = %d; want 202", rr.Code)
		}
		if i > 0 && rr.Code != http.StatusOK {
			t.Errorf("dup call status = %d; want 200", rr.Code)
		}
	}
	if callCount != 1 {
		t.Errorf("publisher called %d times; want 1", callCount)
	}
}

func TestNormalizeNfeEvent_CollapsesPolymorphicVocabulary(t *testing.T) {
	cases := []struct{ in, want string }{
		{"nfse.issued", "issued"},
		{"nfe.emitted", "issued"},
		{"nfse.cancelled", "cancelled"},
		{"nfse.canceled", "cancelled"},
		{"nfse.processing_failed", "processing_failed"},
		{"nfse.failed", "processing_failed"},
		{"unknown.thing", ""},
	}
	for _, c := range cases {
		if got := normalizeNfeEvent(c.in); got != c.want {
			t.Errorf("normalizeNfeEvent(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestWebhook_EmptyID_FallsBackToBodyHash(t *testing.T) {
	srv := newWebhookTestServer(t, func(queue string, _ []byte) error { return nil })
	body := `{"event":"nfse.issued","data":{"id":"x","status":"Issued"}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature", computeHMAC(webhookTestSecret, body))
		rr := httptest.NewRecorder()
		srv.handle(rr, req)
		if i == 0 && rr.Code != http.StatusAccepted {
			t.Errorf("first status = %d; want 202", rr.Code)
		}
		if i == 1 && rr.Code != http.StatusOK {
			t.Errorf("dup status = %d; want 200 (body-hash dedup)", rr.Code)
		}
	}
}

func TestWebhook_RawBodyReadBeforeParse(t *testing.T) {
	// Documentation guard: a HMAC failure here proves the raw-body-read
	// ordering matches NFe.io's signed bytes.
	srv := newWebhookTestServer(t, func(queue string, _ []byte) error { return nil })
	body := `{"id":"a","event":"nfse.issued","data":{"id":"i","status":"Issued"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", io.NopCloser(bytes.NewReader([]byte(body))))
	req.Header.Set("X-Hub-Signature", computeHMAC(webhookTestSecret, body))
	rr := httptest.NewRecorder()
	srv.handle(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d; want 202", rr.Code)
	}
}

func TestWebhook_RejectsObsoleteOrAmbiguousSignatureHeaders(t *testing.T) {
	body := `{"id":"evt-1","event":"nfse.issued"}`
	tests := []struct {
		name    string
		headers http.Header
	}{
		{name: "obsolete sha256 header", headers: http.Header{"X-Hub-Signature-256": {"sha256=deadbeef"}}},
		{name: "sha256 on canonical header", headers: http.Header{"X-Hub-Signature": {"sha256=deadbeef"}}},
		{name: "duplicate canonical headers", headers: http.Header{"X-Hub-Signature": {computeHMAC(webhookTestSecret, body), computeHMAC(webhookTestSecret, body)}}},
		{name: "comma folded canonical header", headers: http.Header{"X-Hub-Signature": {computeHMAC(webhookTestSecret, body) + ", " + computeHMAC(webhookTestSecret, body)}}},
		{name: "canonical plus obsolete", headers: http.Header{"X-Hub-Signature": {computeHMAC(webhookTestSecret, body)}, "X-Hub-Signature-256": {"sha256=deadbeef"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newWebhookTestServer(t, nil)
			req := httptest.NewRequest(http.MethodPost, "/webhook/nfeio", strings.NewReader(body))
			req.Header = tt.headers
			rr := httptest.NewRecorder()
			srv.handle(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d; want 401", rr.Code)
			}
		})
	}
}

func TestVerifyNFeIOSignature_AcceptsUppercaseHexAndRejectsEmptySecret(t *testing.T) {
	body := []byte(`{"id":"evt-1","event":"nfse.issued"}`)
	digest := strings.TrimPrefix(computeHMAC(webhookTestSecret, string(body)), "sha1=")
	headers := http.Header{"X-Hub-Signature": {"sha1=" + strings.ToUpper(digest)}}
	if !verifyNFeIOSignature(headers, body, webhookTestSecret) {
		t.Fatal("provider-style uppercase hexadecimal digest must verify")
	}
	if verifyNFeIOSignature(headers, body, "") {
		t.Fatal("empty runtime secret must fail closed")
	}
}
