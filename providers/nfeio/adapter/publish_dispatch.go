package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// PublishDispatcher invokes publish_message on the rabbitmq-topology
// instance via the Yggdrasil core RPC bus. Instance reference comes from
// the instance YAML (Task 30) and the auth token from YGGDRASIL_RUN_TOKEN
// at startup.
type PublishDispatcher struct {
	coreURL    string
	instance   string
	token      string
	logger     *zap.Logger
	httpClient *http.Client
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
