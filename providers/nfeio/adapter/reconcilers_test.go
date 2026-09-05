package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/events"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/reconcile"
	"go.uber.org/zap"
)

func TestE2E_NfeioV3RejectsLegacyIssueNfseAlias(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	WireReconcilersWithInstance(a, cli, templates, "")
	handler := ExecuteHandler(zap.NewNop(), a, cli, templates, &ExecuteDeps{
		MunicipalitiesCache: NewMunicipalitiesCache(0),
	})

	body, _ := json.Marshal(map[string]any{
		"operation": "issue_nfse",
		"input": IssueNFSeInput{
			MunicipioCode: "3550308",
			ExternalID:    "inv-001",
			BorrowerName:  "ACME LTDA",
			BorrowerFTN:   12345678000100,
			BorrowerAddr: map[string]any{
				"street": "X", "number": "1", "district": "Y",
				"city_code": "3550308", "city_name": "São Paulo", "state": "SP",
				"postal_code": "01000-000", "country": "BRA",
			},
			ServiceAmount: 100.0,
			Description:   "Hosting service",
		},
	})
	_, _, err := handler(context.Background(), rpc.Delivery{Body: body})
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("legacy operation must be rejected at v3, got err=%v", err)
	}
	if calls != 0 {
		t.Fatalf("legacy operation made %d provider calls; want zero", calls)
	}
}

func TestE2E_NfeioV3RejectsLegacyWebhookAliasesWithoutProviderCalls(t *testing.T) {
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	WireReconcilersWithInstance(a, cli, nil, "")
	handler := ExecuteHandler(zap.NewNop(), a, cli, nil, nil)

	for _, operation := range []string{
		"create_webhook_endpoint",
		"list_webhook_endpoints",
		"delete_webhook_endpoint",
	} {
		body, _ := json.Marshal(map[string]any{
			"operation": operation,
			"input": map[string]any{
				"id":     "wh-1",
				"url":    "https://example.invalid/" + propertyCanary,
				"events": []string{"Issued"},
			},
		})
		_, _, err := handler(context.Background(), rpc.Delivery{Body: body})
		if err == nil || !strings.Contains(err.Error(), "unknown operation") {
			t.Fatalf("legacy operation %q must be rejected, got err=%v", operation, err)
		}
		assertSecretFree(t, err.Error())
	}
	if calls != 0 {
		t.Fatalf("legacy webhook aliases made %d provider calls; want zero", calls)
	}
}

// TestE2E_NfeioCanonicalEnsureServiceInvoice locks that the canonical name
// dispatches without emitting any WARN entries.
func TestE2E_NfeioCanonicalEnsureServiceInvoice(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inv-canon","status":"Processing","externalId":"inv-002","flowStatus":"WaitingSendToAuthorize"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	var warns int
	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	reconcile.RegisterReconciler[serviceInvoiceDesired, serviceInvoiceObserved](
		a, "service_invoice", "service_invoices",
		newServiceInvoiceReconciler(cli, templates),
		reconcile.WithLegacyNames("issue_nfse"),
		reconcile.WithWarnLogger(func(string, ...any) { warns++ }),
	)

	body, _ := json.Marshal(map[string]any{
		"operation": "ensure_service_invoice",
		"input": IssueNFSeInput{
			MunicipioCode: "3550308",
			ExternalID:    "inv-002",
			BorrowerName:  "ACME LTDA",
			BorrowerFTN:   12345678000100,
			BorrowerAddr: map[string]any{
				"street": "X", "number": "1", "district": "Y",
				"city_code": "3550308", "city_name": "São Paulo", "state": "SP",
				"postal_code": "01000-000", "country": "BRA",
			},
			ServiceAmount: 100.0,
			Description:   "Hosting service",
		},
	})
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("canonical ensure_service_invoice dispatch failed: %v", err)
	}
	if !strings.Contains(string(resp), `"id":"inv-canon"`) {
		t.Fatalf("expected canonical path to return id; got %s", resp)
	}
	if warns != 0 {
		t.Fatalf("expected zero WARN entries on canonical path, got %d", warns)
	}
}

// TestE2E_NfeioWebhookSubscriptionDestroyWithConfirmation locks the guarded
// canonical destroy path through the SDK dispatch.
func TestE2E_NfeioWebhookSubscriptionDestroyWithConfirmation(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/webhooks/wh-99" {
			t.Errorf("got %s %s; want DELETE /v2/webhooks/wh-99", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(cli),
	)

	body, _ := json.Marshal(map[string]any{
		"operation": "destroy_webhook_subscription",
		"input":     map[string]any{"ref": "wh-99", "confirm_id": "wh-99"},
	})
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("destroy_webhook_subscription dispatch failed: %v", err)
	}
	if !strings.Contains(string(resp), `"deleted":true`) {
		t.Fatalf("expected deleted:true in destroy response, got %s", resp)
	}
}

func TestE2E_NfeioWebhookSubscriptionDestroyWithoutConfirmationNeverDeletes(t *testing.T) {
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(cli),
	)

	body, _ := json.Marshal(map[string]any{
		"operation": "destroy_webhook_subscription",
		"input":     map[string]any{"ref": "wh-99"},
	})
	_, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err == nil {
		t.Fatal("destroy without exact confirmation must fail")
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d; want zero", calls)
	}
}

type webhookCaptureEmitter struct {
	events []events.MutationEvent
}

func (e *webhookCaptureEmitter) Emit(_ context.Context, event events.MutationEvent) error {
	e.events = append(e.events, event)
	return nil
}

func TestE2E_NfeioWebhookSubscriptionEnsureEventNeverExposesProviderSecrets(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Fatalf("got %s %s; want exact GET", r.Method, r.URL.Path)
		}
		writeWebhookResponse(t, w, webhookProviderObject("wh-1", false))
	})
	defer srv.Close()

	emitter := &webhookCaptureEmitter{}
	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(mustNewClient(t, srv.URL)),
		reconcile.WithProvider(Provider),
		reconcile.WithEmitter(emitter),
		reconcile.WithInstanceID("nfeio-dakasa"),
	)

	body, _ := json.Marshal(map[string]any{
		"operation": "ensure_webhook_subscription",
		"input": map[string]any{
			"id":           "wh-1",
			"insecure_ssl": false,
			"__instance_credentials": map[string]any{
				"secret": webhookSecretCanary,
			},
		},
	})
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("ensure dispatch failed: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("mutation events = %d; want one", len(emitter.events))
	}
	encodedEvents, _ := json.Marshal(emitter.events)
	assertSecretFree(t, string(resp)+string(encodedEvents))
}

func TestE2E_NfeioWebhookSecurityMigrationEventNeverExposesRuntimeHMAC(t *testing.T) {
	current := webhookProviderObject("wh-1", true)
	delete(current, webhookSecretField)
	confirmed := webhookProviderObject("wh-1", false)
	delete(confirmed, webhookSecretField)
	delete(confirmed[webhookHeadersField].(map[string]any), "Authorization")
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			writeWebhookResponse(t, w, current)
		case 2:
			if r.Method != http.MethodPut {
				t.Fatalf("migration call 2 method=%s, want PUT", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case 3:
			writeWebhookResponse(t, w, confirmed)
		default:
			t.Fatalf("unexpected provider call %d", calls)
		}
	})
	defer srv.Close()

	emitter := &webhookCaptureEmitter{}
	a := sdkadapter.New(sdkadapter.Config{Provider: Provider, IntegrationType: IntegrationType})
	reconcile.RegisterReconciler[webhookSubDesired, webhookSubObserved](
		a, "webhook_subscription", "webhook_subscriptions",
		newWebhookSubReconciler(mustNewClientWithWebhookSecret(t, srv.URL, webhookSecretCanary)),
		reconcile.WithProvider(Provider),
		reconcile.WithEmitter(emitter),
		reconcile.WithInstanceID("nfeio-dakasa-production"),
	)

	body, _ := json.Marshal(map[string]any{
		"operation": "ensure_webhook_subscription",
		"input": map[string]any{
			"id":                            "wh-1",
			"insecure_ssl":                  false,
			"set_hmac_from_runtime":         true,
			"remove_legacy_authorization":   true,
			"confirm_security_migration_id": "wh-1",
		},
	})
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("security migration dispatch failed: %v", err)
	}
	if calls != 3 || len(emitter.events) != 1 {
		t.Fatalf("provider calls=%d mutation events=%d; want 3 and 1", calls, len(emitter.events))
	}
	encodedEvents, _ := json.Marshal(emitter.events)
	assertSecretFree(t, string(resp)+string(encodedEvents))
}
