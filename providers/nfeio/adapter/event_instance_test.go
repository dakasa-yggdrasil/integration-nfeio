package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
	"go.uber.org/zap"
)

// coreEnvelope builds an execute body shaped like the one yggdrasil-core
// sends: operation + capability + input, plus the metadata and integration
// blocks. An empty instanceName omits the integration block entirely.
func coreEnvelope(t *testing.T, operation string, input map[string]any, instanceName, idempotency string) []byte {
	t.Helper()
	env := map[string]any{
		"operation":  operation,
		"capability": operation,
		"input":      input,
		"auth":       map[string]any{},
	}
	if idempotency != "" {
		env["metadata"] = map[string]any{"idempotency": idempotency}
	}
	if instanceName != "" {
		env["integration"] = map[string]any{
			"type":     map[string]any{"namespace": "global", "name": IntegrationType},
			"instance": map[string]any{"namespace": "dakasa", "name": instanceName},
		}
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}

// newCapturingHandler wires the production ExecuteHandler with a capturing
// emitter and the same empty static instance label main.go uses.
func newCapturingHandler(t *testing.T, baseURL string) (sdkadapter.Handler, *webhookCaptureEmitter) {
	t.Helper()
	cli := mustNewClient(t, baseURL)
	emitter := &webhookCaptureEmitter{}
	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	wireReconcilers(a, cli, nil, "", emitter)
	return ExecuteHandler(zap.NewNop(), a, cli, nil, &ExecuteDeps{MunicipalitiesCache: NewMunicipalitiesCache(0)}), emitter
}

func companyInput() map[string]any {
	return map[string]any{
		"name":               "ACME LTDA",
		"federal_tax_number": 12345678000100,
		"email":              "fiscal@example.invalid",
		"tax_regime":         "SimplesNacional",
		"address":            map[string]any{"city": "Sao Paulo"},
		"login_name":         "acme",
	}
}

func TestExecuteHandler_EventInstanceComesFromCoreEnvelopePerCall(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/companies" {
			t.Errorf("got %s %s; want POST /v2/companies", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"cmp-1","federalTaxNumber":12345678000100,"name":"ACME LTDA","status":"Active"}`))
	})
	defer srv.Close()
	handler, emitter := newCapturingHandler(t, srv.URL)

	calls := []struct {
		instance    string
		idempotency string
	}{
		{instance: "nfeio-dakasa-production", idempotency: "idem-production-1"},
		{instance: "nfeio-dakasa-validation", idempotency: "idem-validation-1"},
	}
	for _, call := range calls {
		body := coreEnvelope(t, OpEnsureCompany, companyInput(), call.instance, call.idempotency)
		if _, _, err := handler(context.Background(), rpc.Delivery{Body: body}); err != nil {
			t.Fatalf("ensure_company for %s: %v", call.instance, err)
		}
	}

	if len(emitter.events) != len(calls) {
		t.Fatalf("mutation events = %d; want %d", len(emitter.events), len(calls))
	}
	for i, call := range calls {
		ev := emitter.events[i]
		if ev.InstanceID != call.instance {
			t.Fatalf("event %d instance_id = %q; want %q", i, ev.InstanceID, call.instance)
		}
		if ev.Idempotency != call.idempotency {
			t.Fatalf("event %d idempotency = %q; want %q", i, ev.Idempotency, call.idempotency)
		}
		if ev.EventType != "nfeio.company.ensured" {
			t.Fatalf("event %d type = %q; want nfeio.company.ensured", i, ev.EventType)
		}
		if ev.ResourceID != "cmp-1" {
			t.Fatalf("event %d resource_id = %q; want cmp-1", i, ev.ResourceID)
		}
	}
	if emitter.events[0].InstanceID == emitter.events[1].InstanceID {
		t.Fatalf("both events carry instance_id %q; want the per-call name", emitter.events[0].InstanceID)
	}
}

func TestExecuteHandler_WebhookEnsureStrictDecoderAcceptsCoreEnvelope(t *testing.T) {
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Errorf("got %s %s; want exact GET /v2/webhooks/wh-1", r.Method, r.URL.Path)
		}
		writeWebhookResponse(t, w, webhookProviderObject("wh-1", false))
	})
	defer srv.Close()
	handler, emitter := newCapturingHandler(t, srv.URL)

	input := map[string]any{"id": "wh-1", "insecure_ssl": false}
	body := coreEnvelope(t, OpEnsureWebhookSubscription, input, "nfeio-dakasa-production", "idem-webhook-1")

	// The lifted body must leave input byte-for-byte equivalent: the strict
	// decoder rejects any unknown key that does not start with "__".
	var before, after struct {
		Input      map[string]any `json:"input"`
		InstanceID string         `json:"instance_id"`
	}
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatalf("decode original: %v", err)
	}
	if err := json.Unmarshal(withEnvelopeInstance(body), &after); err != nil {
		t.Fatalf("decode lifted: %v", err)
	}
	if !reflect.DeepEqual(before.Input, after.Input) {
		t.Fatalf("input changed: before=%v after=%v", before.Input, after.Input)
	}
	if after.InstanceID != "nfeio-dakasa-production" {
		t.Fatalf("lifted instance_id = %q; want nfeio-dakasa-production", after.InstanceID)
	}

	resp, _, err := handler(context.Background(), rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("ensure_webhook_subscription through the Core envelope: %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d; want one exact GET", calls)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("mutation events = %d; want one", len(emitter.events))
	}
	ev := emitter.events[0]
	if ev.EventType != "nfeio.webhook_subscription.ensured" || ev.ResourceID != "wh-1" {
		t.Fatalf("event = %s resource_id=%q; want nfeio.webhook_subscription.ensured for wh-1", ev.EventType, ev.ResourceID)
	}
	if ev.InstanceID != "nfeio-dakasa-production" {
		t.Fatalf("event instance_id = %q; want nfeio-dakasa-production", ev.InstanceID)
	}
	encodedEvents, _ := json.Marshal(emitter.events)
	assertSecretFree(t, string(resp)+string(encodedEvents))
}

func TestExecuteHandler_MissingInstanceNameLeavesEventInstanceEmpty(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"cmp-2","federalTaxNumber":12345678000100,"name":"ACME LTDA","status":"Active"}`))
	})
	defer srv.Close()
	handler, emitter := newCapturingHandler(t, srv.URL)

	body := coreEnvelope(t, OpEnsureCompany, companyInput(), "", "")
	if _, _, err := handler(context.Background(), rpc.Delivery{Body: body}); err != nil {
		t.Fatalf("ensure_company without an instance name: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("mutation events = %d; want one", len(emitter.events))
	}
	if got := emitter.events[0].InstanceID; got != "" {
		t.Fatalf("event instance_id = %q; want empty so Core refuses it", got)
	}
}

func TestWithEnvelopeInstance_KeepsCallerValuesAndNonObjects(t *testing.T) {
	body := []byte(`{"operation":"ensure_company","instance_id":"caller-instance","idempotency":"caller-key",` +
		`"metadata":{"idempotency":"core-key"},"integration":{"instance":{"name":"nfeio-dakasa-production"}},"input":{}}`)
	var got struct {
		InstanceID  string `json:"instance_id"`
		Idempotency string `json:"idempotency"`
	}
	if err := json.Unmarshal(withEnvelopeInstance(body), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.InstanceID != "caller-instance" || got.Idempotency != "caller-key" {
		t.Fatalf("caller values overwritten: instance_id=%q idempotency=%q", got.InstanceID, got.Idempotency)
	}

	for _, raw := range []string{``, `null`, `[]`, `"text"`, `{not json`} {
		if out := withEnvelopeInstance([]byte(raw)); string(out) != raw {
			t.Fatalf("withEnvelopeInstance(%q) = %q; want the body unchanged", raw, out)
		}
	}
}
