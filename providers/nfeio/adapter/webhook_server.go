package adapter

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"

	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

// WebhookServer handles inbound NFe.io callbacks. HMAC verify → LRU dedup →
// normalize event → publish_message to enterprise-payments.* queue.
type WebhookServer struct {
	cfg       *config.Config
	cli       *Client
	logger    *zap.Logger
	dedup     *lru.Cache[string, time.Time]
	publisher func(queue string, payload []byte) error
	httpSrv   *http.Server
}

// NewWebhookServer constructs the third listener. publisher defaults to a
// no-op stub; main.go wires the real PublishDispatcher via SetPublisher
// before Start.
func NewWebhookServer(cfg *config.Config, cli *Client, logger *zap.Logger) *WebhookServer {
	cache, _ := lru.New[string, time.Time](4096)
	if logger == nil {
		logger = zap.NewNop()
	}
	return &WebhookServer{
		cfg:    cfg,
		cli:    cli,
		logger: logger,
		dedup:  cache,
		publisher: func(queue string, _ []byte) error {
			logger.Warn("webhook publisher not wired; dropping event", zap.String("queue", queue))
			return nil
		},
	}
}

// SetPublisher injects the production publisher (typically a
// PublishDispatcher that hits yggdrasil-core's capabilities/invoke).
func (s *WebhookServer) SetPublisher(p func(queue string, body []byte) error) {
	if p != nil {
		s.publisher = p
	}
}

// Start binds to cfg.WebhookPort and serves until ctx cancels.
func (s *WebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/nfeio", s.handle)
	s.httpSrv = &http.Server{
		Addr:              ":" + s.cfg.WebhookPort,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = s.httpSrv.Shutdown(context.Background())
	}()
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully terminates the webhook server.
func (s *WebhookServer) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

type webhookPayload struct {
	ID    string          `json:"id"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// handle implements the reactor pipeline. Body is read once, before any
// parsing, so the HMAC covers the exact bytes NFe.io signed.
func (s *WebhookServer) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if !verifyNFeIOSignature(r.Header, body, s.cfg.WebhookSecret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "decode", http.StatusBadRequest)
		return
	}

	dedupKey := p.ID
	if dedupKey == "" {
		sum := sha256.Sum256(body)
		dedupKey = hex.EncodeToString(sum[:])
	}
	if _, hit := s.dedup.Get(dedupKey); hit {
		metricDedupHits.Inc()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"duplicate"}`))
		return
	}
	s.dedup.Add(dedupKey, time.Now())

	normalized := normalizeNfeEvent(p.Event)
	queue := queueFor(normalized)
	if normalized != "" {
		metricWebhookReceived.WithLabelValues(normalized).Inc()
	}
	if queue == "" {
		// Unknown event: log and 202 (don't trigger NFe.io retry storm).
		s.logger.Warn("webhook unknown event", zap.String("event", p.Event))
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if err := s.publisher(queue, body); err != nil {
		s.logger.Error("webhook publish", zap.Error(err))
		http.Error(w, "publish failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// verifyNFeIOSignature mirrors the provider wire contract and the Payments
// receiver: exactly one X-Hub-Signature value containing sha1=<40 hex> over
// the exact request bytes. The obsolete SHA-256 header is rejected so callers
// cannot accidentally authenticate with a contract NFe.io does not emit.
func verifyNFeIOSignature(headers http.Header, body []byte, secret string) bool {
	if !validRuntimeWebhookSecret(secret) {
		return false
	}
	if _, present := singleHTTPHeader(headers, "X-Hub-Signature-256"); present {
		return false
	}
	signature, present := singleHTTPHeader(headers, "X-Hub-Signature")
	if !present || !strings.HasPrefix(signature, "sha1=") {
		return false
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(signature, "sha1="))
	if err != nil || len(digest) != sha1.Size {
		return false
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(digest, mac.Sum(nil))
}

func singleHTTPHeader(headers http.Header, name string) (string, bool) {
	var values []string
	found := false
	for key, candidates := range headers {
		if strings.EqualFold(key, name) {
			found = true
			values = append(values, candidates...)
		}
	}
	if len(values) != 1 {
		return "", found
	}
	value := strings.TrimSpace(values[0])
	if value == "" || strings.Contains(value, ",") {
		return "", true
	}
	return value, true
}

// normalizeNfeEvent collapses NFe.io's polymorphic event vocabulary to
// {issued, processing_failed, cancelled}. Mirrors the taxonomy in
// dakasa-enterprise-payments-api/controllers/webhook-nfe.go::normalizeNfeEvent.
func normalizeNfeEvent(raw string) string {
	switch raw {
	case "nfse.issued", "nfe.emitted", "nfse.emitted":
		return "issued"
	case "nfse.cancelled", "nfse.canceled", "nfe.cancelled", "nfe.canceled":
		return "cancelled"
	case "nfse.processing_failed", "nfse.failed", "nfe.failed", "nfe.rejected":
		return "processing_failed"
	}
	return ""
}

// queueFor maps a normalized event to its target RabbitMQ queue. Queue
// names match dakasa-enterprise-payments-api/controllers/message/register.go.
func queueFor(normalized string) string {
	switch normalized {
	case "issued":
		return "enterprise-payments.nfe.emitted.q"
	case "processing_failed":
		return "enterprise-payments.nfe.rejected.q"
	case "cancelled":
		return "enterprise-payments.nfe.canceled.q"
	}
	return ""
}
