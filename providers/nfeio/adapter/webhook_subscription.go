package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	webhookEnvelopeField    = "webHook"
	webhookInsecureSSLField = "insecureSsl"
	webhookSecretField      = "secret"
	webhookHeadersField     = "headers"
)

// EnsureWebhookSubscriptionInput deliberately supports only reconciliation of
// a webhook whose provider ID is already known. The adapter never creates or
// discovers a webhook from mutable attributes such as URI or filters.
//
// Ref and ConfirmID are destroy-only bridge fields. They live here because the
// SDK passes the ensure desired type to DestroyWithDesired. Ensure rejects them.
type EnsureWebhookSubscriptionInput struct {
	ID                         string `json:"id"`
	InsecureSSL                *bool  `json:"insecure_ssl"`
	SetHMACFromRuntime         bool   `json:"set_hmac_from_runtime,omitempty"`
	RemoveLegacyAuthorization  bool   `json:"remove_legacy_authorization,omitempty"`
	ConfirmSecurityMigrationID string `json:"confirm_security_migration_id,omitempty"`
	Ref                        string `json:"ref,omitempty"`
	ConfirmID                  string `json:"confirm_id,omitempty"`
}

// UnmarshalJSON rejects legacy create-shaped or otherwise unknown desired
// fields without echoing names or values into the error. Reserved Yggdrasil
// bridge keys are accepted and ignored.
func (in *EnsureWebhookSubscriptionInput) UnmarshalJSON(data []byte) error {
	*in = EnsureWebhookSubscriptionInput{}
	fields, err := decodeWebhookInputObject(data)
	if err != nil {
		return err
	}
	for name, raw := range fields {
		switch name {
		case "id":
			if err := json.Unmarshal(raw, &in.ID); err != nil {
				return errors.New("webhook_subscription input id must be a string")
			}
		case "insecure_ssl":
			if string(raw) == "null" {
				in.InsecureSSL = nil
				continue
			}
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				return errors.New("webhook_subscription input insecure_ssl must be a boolean")
			}
			in.InsecureSSL = &value
		case "set_hmac_from_runtime":
			if err := json.Unmarshal(raw, &in.SetHMACFromRuntime); err != nil {
				return errors.New("webhook_subscription security migration flag must be a boolean")
			}
		case "remove_legacy_authorization":
			if err := json.Unmarshal(raw, &in.RemoveLegacyAuthorization); err != nil {
				return errors.New("webhook_subscription legacy authorization flag must be a boolean")
			}
		case "confirm_security_migration_id":
			if err := json.Unmarshal(raw, &in.ConfirmSecurityMigrationID); err != nil {
				return errors.New("webhook_subscription security migration confirmation must be a string")
			}
		case "ref":
			if err := json.Unmarshal(raw, &in.Ref); err != nil {
				return errors.New("webhook_subscription input ref must be a string")
			}
		case "confirm_id":
			if err := json.Unmarshal(raw, &in.ConfirmID); err != nil {
				return errors.New("webhook_subscription input confirm_id must be a string")
			}
		default:
			if !strings.HasPrefix(name, "__") {
				return errors.New("webhook_subscription input contains unsupported fields")
			}
		}
	}
	return nil
}

// EnsureWebhookSubscriptionOutput is intentionally minimal. Provider-owned
// fields such as URI, secret, headers and properties never enter resource,
// adoption or mutation-event payloads.
type EnsureWebhookSubscriptionOutput struct {
	ID          string `json:"id"`
	InsecureSSL bool   `json:"insecure_ssl"`
	Adopted     bool   `json:"adopted,omitempty"`
	Updated     bool   `json:"updated,omitempty"`
}

// webhookDocument retains the complete provider object as raw JSON fields so
// an exact-ID PUT can preserve documented and future field values without a
// lossy typed projection.
// It accepts the official {"webHook": {...}} response and the legacy flat
// response shape used by older NFe.io installations.
type webhookDocument struct {
	fields map[string]json.RawMessage
}

func (d *webhookDocument) UnmarshalJSON(data []byte) error {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil || outer == nil {
		return errors.New("invalid webhook response")
	}
	if wrapped, ok := outer[webhookEnvelopeField]; ok {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(wrapped, &fields); err != nil || fields == nil {
			return errors.New("invalid webhook response")
		}
		d.fields = fields
		return nil
	}
	d.fields = outer
	return nil
}

func (d webhookDocument) id() (string, bool) {
	raw, ok := d.fields["id"]
	if !ok {
		return "", false
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil || id == "" {
		return "", false
	}
	return id, true
}

func (d webhookDocument) insecureSSL() (bool, bool) {
	raw, ok := d.fields[webhookInsecureSSLField]
	if !ok {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func (d webhookDocument) desiredFields(in EnsureWebhookSubscriptionInput, runtimeHMAC string) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage, len(d.fields))
	for name, raw := range d.fields {
		fields[name] = append(json.RawMessage(nil), raw...)
	}
	fields[webhookInsecureSSLField] = json.RawMessage("false")
	if !in.SetHMACFromRuntime {
		return fields, nil
	}

	encodedHMAC, err := json.Marshal(runtimeHMAC)
	if err != nil {
		return nil, errors.New("webhook security migration could not encode the runtime credential")
	}
	fields[webhookSecretField] = encodedHMAC

	rawHeaders, ok := fields[webhookHeadersField]
	if !ok || string(rawHeaders) == "null" {
		return fields, nil
	}
	var headers map[string]json.RawMessage
	if err := json.Unmarshal(rawHeaders, &headers); err != nil || headers == nil {
		return nil, errors.New("webhook security migration could not preserve provider headers")
	}
	for name := range headers {
		if strings.EqualFold(name, "Authorization") {
			delete(headers, name)
		}
	}
	encodedHeaders, err := json.Marshal(headers)
	if err != nil {
		return nil, errors.New("webhook security migration could not preserve provider headers")
	}
	fields[webhookHeadersField] = encodedHeaders
	return fields, nil
}

func (d webhookDocument) hasLegacyAuthorization() (bool, error) {
	rawHeaders, ok := d.fields[webhookHeadersField]
	if !ok || string(rawHeaders) == "null" {
		return false, nil
	}
	var headers map[string]json.RawMessage
	if err := json.Unmarshal(rawHeaders, &headers); err != nil || headers == nil {
		return false, errors.New("webhook security migration confirmation is invalid")
	}
	for name := range headers {
		if strings.EqualFold(name, "Authorization") {
			return true, nil
		}
	}
	return false, nil
}

// EnsureWebhookSubscription reconciles one pre-existing webhook by exact ID.
// The ordinary drift repair changes only insecureSsl true -> false. An explicit
// exact-ID security migration may additionally copy the already projected
// runtime HMAC credential into the provider object and remove the legacy static
// Authorization header. It never POSTs, lists, matches by URI, or deletes.
func EnsureWebhookSubscription(ctx context.Context, cli *Client, in EnsureWebhookSubscriptionInput) (*EnsureWebhookSubscriptionOutput, error) {
	if err := validateWebhookEnsureInput(in, cli.cfg.WebhookSecret); err != nil {
		return nil, err
	}

	current, err := getWebhookByExactID(ctx, cli, in.ID, OpEnsureWebhookSubscription)
	if err != nil {
		return nil, err
	}
	insecureSSL, ok := current.insecureSSL()
	if !ok {
		return nil, errors.New("ensure_webhook_subscription: provider response omitted a valid insecureSsl field")
	}
	if !insecureSSL && !in.SetHMACFromRuntime {
		return safeWebhookOutput(in.ID, false, false), nil
	}

	desired, err := current.desiredFields(in, cli.cfg.WebhookSecret)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		webhookEnvelopeField: desired,
	}
	path := webhookExactPath(in.ID)
	if err := cli.do(ctx, http.MethodPut, path, body, nil); err != nil {
		return nil, sanitizeWebhookProviderError(OpEnsureWebhookSubscription, err)
	}

	confirmed, err := getWebhookByExactID(ctx, cli, in.ID, OpEnsureWebhookSubscription)
	if err != nil {
		return nil, err
	}
	confirmedInsecureSSL, ok := confirmed.insecureSSL()
	if !ok || confirmedInsecureSSL {
		return nil, errors.New("ensure_webhook_subscription: secure TLS setting was not confirmed")
	}
	if in.SetHMACFromRuntime {
		hasLegacyAuthorization, err := confirmed.hasLegacyAuthorization()
		if err != nil {
			return nil, err
		}
		if hasLegacyAuthorization {
			return nil, errors.New("ensure_webhook_subscription: security migration was not confirmed")
		}
	}
	return safeWebhookOutput(in.ID, false, true), nil
}

func validateWebhookEnsureInput(in EnsureWebhookSubscriptionInput, runtimeHMAC string) error {
	if !validWebhookID(in.ID) {
		return errors.New("ensure_webhook_subscription: valid exact id required")
	}
	if in.Ref != "" || in.ConfirmID != "" {
		return errors.New("ensure_webhook_subscription: destroy-only fields are not allowed")
	}
	if in.InsecureSSL == nil {
		return errors.New("ensure_webhook_subscription: insecure_ssl=false is required")
	}
	if *in.InsecureSSL {
		return errors.New("ensure_webhook_subscription: insecure_ssl=true is forbidden")
	}
	migrationRequested := in.SetHMACFromRuntime ||
		in.RemoveLegacyAuthorization || in.ConfirmSecurityMigrationID != ""
	if migrationRequested {
		if !in.SetHMACFromRuntime || !in.RemoveLegacyAuthorization ||
			in.ConfirmSecurityMigrationID != in.ID {
			return errors.New("ensure_webhook_subscription: complete exact-ID security migration confirmation is required")
		}
		if len(runtimeHMAC) < 32 || len(runtimeHMAC) > 64 || strings.TrimSpace(runtimeHMAC) != runtimeHMAC {
			return errors.New("ensure_webhook_subscription: runtime security credential is invalid")
		}
	}
	return nil
}

func safeWebhookOutput(id string, insecureSSL, updated bool) *EnsureWebhookSubscriptionOutput {
	return &EnsureWebhookSubscriptionOutput{
		ID:          id,
		InsecureSSL: insecureSSL,
		Adopted:     true,
		Updated:     updated,
	}
}

func getWebhookByExactID(ctx context.Context, cli *Client, id, operation string) (*webhookDocument, error) {
	var current webhookDocument
	if err := cli.do(ctx, http.MethodGet, webhookExactPath(id), nil, &current); err != nil {
		return nil, sanitizeWebhookProviderError(operation, err)
	}
	providerID, ok := current.id()
	if !ok || providerID != id {
		return nil, fmt.Errorf("%s: provider response did not match the requested id", operation)
	}
	return &current, nil
}

func sanitizeWebhookProviderError(operation string, err error) error {
	var apiErr *NfeIoAPIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: provider request failed with HTTP %d", operation, apiErr.Status)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: provider request canceled", operation)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: provider request timed out", operation)
	}
	return fmt.Errorf("%s: provider request failed", operation)
}

func validWebhookID(id string) bool {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	for _, char := range id {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func webhookExactPath(id string) string {
	return "/v2/webhooks/" + url.PathEscape(id)
}

func decodeWebhookInputObject(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if len(data) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, errors.New("webhook_subscription input must be a JSON object")
	}
	return fields, nil
}

// DestroyWebhookSubscriptionInput requires the same exact ID twice. This keeps
// destroy available for deliberate operator actions without allowing a normal
// reconcile or a partially populated request to delete provider state.
type DestroyWebhookSubscriptionInput struct {
	ID        string `json:"id"`
	ConfirmID string `json:"confirm_id"`
}

func (in *DestroyWebhookSubscriptionInput) UnmarshalJSON(data []byte) error {
	*in = DestroyWebhookSubscriptionInput{}
	fields, err := decodeWebhookInputObject(data)
	if err != nil {
		return err
	}
	for name, raw := range fields {
		switch name {
		case "id":
			if err := json.Unmarshal(raw, &in.ID); err != nil {
				return errors.New("destroy_webhook_subscription input id must be a string")
			}
		case "confirm_id":
			if err := json.Unmarshal(raw, &in.ConfirmID); err != nil {
				return errors.New("destroy_webhook_subscription input confirm_id must be a string")
			}
		default:
			if !strings.HasPrefix(name, "__") {
				return errors.New("destroy_webhook_subscription input contains unsupported fields")
			}
		}
	}
	return nil
}

type DestroyWebhookSubscriptionOutput struct {
	Deleted       bool `json:"deleted"`
	AlreadyAbsent bool `json:"already_absent,omitempty"`
}

func DestroyWebhookSubscription(ctx context.Context, cli *Client, in DestroyWebhookSubscriptionInput) (*DestroyWebhookSubscriptionOutput, error) {
	if !validWebhookID(in.ID) {
		return nil, errors.New("destroy_webhook_subscription: valid exact id required")
	}
	if in.ConfirmID != in.ID {
		return nil, errors.New("destroy_webhook_subscription: confirm_id must exactly match id")
	}
	err := cli.do(ctx, http.MethodDelete, webhookExactPath(in.ID), nil, nil)
	if err == nil {
		return &DestroyWebhookSubscriptionOutput{Deleted: true}, nil
	}
	var apiErr *NfeIoAPIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return &DestroyWebhookSubscriptionOutput{Deleted: true, AlreadyAbsent: true}, nil
	}
	return nil, sanitizeWebhookProviderError(OpDestroyWebhookSubscription, err)
}

// ObserveWebhookSubscriptionsInput requires one exact provider ID. Enumeration
// is intentionally unavailable so observation cannot become accidental
// discovery or attribute-based adoption.
type ObserveWebhookSubscriptionsInput struct {
	ID string `json:"id"`
}

type ObserveWebhookSubscriptionsOutput struct {
	Items []EnsureWebhookSubscriptionOutput `json:"items"`
}

func ObserveWebhookSubscriptions(ctx context.Context, cli *Client, raw []byte) (*ObserveWebhookSubscriptionsOutput, error) {
	fields, err := decodeWebhookInputObject(raw)
	if err != nil {
		return nil, err
	}
	var id string
	for name, value := range fields {
		switch name {
		case "id":
			if err := json.Unmarshal(value, &id); err != nil {
				return nil, errors.New("observe_webhook_subscriptions: id must be a string")
			}
		default:
			if !strings.HasPrefix(name, "__") {
				return nil, errors.New("observe_webhook_subscriptions: only exact id lookup is supported")
			}
		}
	}
	if !validWebhookID(id) {
		return nil, errors.New("observe_webhook_subscriptions: valid exact id required")
	}
	current, err := getWebhookByExactID(ctx, cli, id, OpObserveWebhookSubscriptions)
	if err != nil {
		return nil, err
	}
	insecureSSL, ok := current.insecureSSL()
	if !ok {
		return nil, errors.New("observe_webhook_subscriptions: provider response omitted a valid insecureSsl field")
	}
	return &ObserveWebhookSubscriptionsOutput{
		Items: []EnsureWebhookSubscriptionOutput{{ID: id, InsecureSSL: insecureSSL}},
	}, nil
}
