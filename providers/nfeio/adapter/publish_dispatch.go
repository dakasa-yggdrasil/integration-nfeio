package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
)

// The legacy webhook reactor's publish dispatcher reads its own env pair.
// It never reads YGGDRASIL_CORE_URL or YGGDRASIL_RUN_TOKEN: those belong to
// the mutation event emitter, and YGGDRASIL_RUN_TOKEN is this adapter's own
// event publisher bearer.
const (
	// EnvPublishCoreURL is the Core base URL the dispatcher POSTs to. There
	// is no default: when it is unset the dispatcher is disabled.
	EnvPublishCoreURL = "YGGDRASIL_CORE_BASE_URL"
	// EnvPublishToken is the bearer the dispatcher sends. When it is unset
	// the dispatcher is disabled, so it never calls Core unauthenticated.
	EnvPublishToken = "YGGDRASIL_WORKFLOW_RUN_TOKEN"
)

// PublishDispatcher invokes publish_message on the rabbitmq-topology
// instance through POST /api/v1/capabilities/invoke on yggdrasil-core.
// Core has no such route, so an enabled dispatcher gets 404 on every
// publish. It stays disabled unless both EnvPublishCoreURL and
// EnvPublishToken are set (see PublishDispatcherFromEnv).
type PublishDispatcher struct {
	coreURL    string
	instance   string
	token      string
	logger     *zap.Logger
	httpClient *http.Client
}

// PublishDispatcherFromEnv builds the legacy reactor publish dispatcher from
// EnvPublishCoreURL and EnvPublishToken. It returns nil, meaning disabled,
// unless both are set. getenv defaults to os.Getenv; instance is the
// rabbitmq-topology integration_instance name.
func PublishDispatcherFromEnv(getenv func(string) string, instance string, logger *zap.Logger) *PublishDispatcher {
	if getenv == nil {
		getenv = os.Getenv
	}
	coreURL := strings.TrimRight(strings.TrimSpace(getenv(EnvPublishCoreURL)), "/")
	token := strings.TrimSpace(getenv(EnvPublishToken))
	if coreURL == "" || token == "" {
		return nil
	}
	return NewPublishDispatcher(coreURL, instance, token, logger)
}

// NewPublishDispatcher constructs a dispatcher pointing at yggdrasil-core.
// instance is the integration_instance name for the rabbitmq-topology
// adapter that owns the target vhost.
func NewPublishDispatcher(coreURL, instance, token string, logger *zap.Logger) *PublishDispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PublishDispatcher{
		coreURL:    coreURL,
		instance:   instance,
		token:      token,
		logger:     logger,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// PublishMessage POSTs a publish_message capability invocation to
// yggdrasil-core. The core forwards to the rabbitmq-topology adapter
// instance referenced by d.instance.
func (d *PublishDispatcher) PublishMessage(queue string, body []byte) error {
	env := map[string]any{
		"capability":   "publish_message",
		"instance_ref": d.instance,
		"input": map[string]any{
			"vhost":        "/",
			"exchange":     "",
			"routing_key":  queue,
			"payload":      json.RawMessage(body),
			"payload_kind": "json",
		},
	}
	buf, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal publish envelope: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, d.coreURL+"/api/v1/capabilities/invoke", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch publish_message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("publish_message status=%d", resp.StatusCode)
	}
	return nil
}
