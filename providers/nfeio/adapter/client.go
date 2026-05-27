package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

// Client wraps the NFe.io REST API. Targets the current /v2 surface; the
// legacy implementation in dakasa-enterprise-payments-api/client/nfe-io.go
// used /v1 paths.
type Client struct {
	httpClient *http.Client
	cfg        *config.Config
	logger     *zap.Logger
}

// NfeIoAPIError matches the NFe.io error envelope.
type NfeIoAPIError struct {
	Status  int    `json:"status"`
	Name    string `json:"name"`
	Message string `json:"message"`
	// RawBody preserves the response body bytes so callers (e.g. issue_nfse
	// 409 idempotent path) can decode the upstream payload that arrived
	// alongside the error envelope.
	RawBody []byte `json:"-"`
}

func (e *NfeIoAPIError) Error() string {
	return fmt.Sprintf("nfeio: status=%d name=%s message=%s", e.Status, e.Name, e.Message)
}

// IsTerminal classifies an NFe.io API error as terminal (don't retry).
// 400, 404, 422 are terminal. 409 is "already exists" (idempotent —
// caller handles). 429 + 5xx are transient.
func (e *NfeIoAPIError) IsTerminal() bool {
	return e.Status == http.StatusBadRequest ||
		e.Status == http.StatusNotFound ||
		e.Status == http.StatusUnprocessableEntity ||
		e.Status == http.StatusConflict
}

// NewClient constructs a NFe.io HTTP client. The httptest server URL passed
// in cfg.BaseURL drives both production traffic and unit tests; no separate
// "mock" mode exists.
func NewClient(cfg *config.Config, logger *zap.Logger) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cfg:        cfg,
		logger:     logger,
	}, nil
}

// do executes a single NFe.io HTTP call with retry on transient errors.
// 429 honors Retry-After (3 attempts: configurable backoff). 5xx retries
// once. Decodes successful 2xx responses into out. Returns *NfeIoAPIError
// for non-2xx, with RawBody populated so callers can decode the upstream
// payload (idempotent 409 paths rely on this).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err, retryAfter, transient := c.doOnce(ctx, method, path, body, out)
		if err == nil {
			return nil
		}
		if !transient || attempt == maxAttempts {
			return err
		}
		c.logger.Warn("nfeio retry",
			zap.Int("attempt", attempt),
			zap.Duration("retry_after", retryAfter))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryAfter):
		}
	}
	return errors.New("unreachable")
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any, out any) (error, time.Duration, bool) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err), 0, false
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err), 0, false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	injectAuth(req, c.cfg.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network error → transient with default backoff.
		return fmt.Errorf("http do: %w", err), time.Second, true
	}
	defer resp.Body.Close()

	// Expose rate-limit gauge if header present.
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, errN := strconv.Atoi(remaining); errN == nil {
			metricRateLimitRemaining.Set(float64(n))
		}
	}

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(respBody) == 0 {
			return nil, 0, false
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode: %w", err), 0, false
		}
		return nil, 0, false
	}

	apiErr := &NfeIoAPIError{Status: resp.StatusCode, RawBody: respBody}
	_ = json.Unmarshal(respBody, apiErr)
	if apiErr.Message == "" {
		apiErr.Message = string(respBody)
	}

	// Transient classification: 429 + 5xx.
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := parseRetryAfter(resp.Header.Get("Retry-After"))
		return apiErr, wait, true
	}
	if resp.StatusCode >= 500 {
		return apiErr, 2 * time.Second, true
	}
	return apiErr, 0, false
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 2 * time.Second
	}
	if n, err := strconv.Atoi(header); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 2 * time.Second
}
