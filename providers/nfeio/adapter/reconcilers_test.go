package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sdkadapter "github.com/dakasa-yggdrasil/yggdrasil-sdk-go/adapter"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/sdk/reconcile"
)

// TestE2E_NfeioLegacyIssueNfseShim locks Task 32 step 2: invoking the
// pre-v2.0.0 `issue_nfse` name through the SDK dispatch path MUST route
// to ensure_service_invoice AND emit exactly one WARN log entry. Without
// this, callers that fail to migrate would silently dispatch to the wrong
// canonical handler.
func TestE2E_NfeioLegacyIssueNfseShim(t *testing.T) {
	templates := mustLoadTestTemplates(t)
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"inv-shim","status":"Processing","externalId":"inv-001","flowStatus":"WaitingSendToAuthorize"}`))
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
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("legacy issue_nfse dispatch failed: %v", err)
	}
	if !strings.Contains(string(resp), `"id":"inv-shim"`) {
		t.Fatalf("expected canonical ensure_service_invoice path to return id; got %s", resp)
	}
	if warns != 1 {
		t.Fatalf("expected exactly 1 WARN entry, got %d", warns)
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

// TestE2E_NfeioWebhookSubscriptionDestroyShim locks the destroy_webhook_subscription
// canonical → DELETE /v2/webhooks/{id} path through the SDK dispatch.
func TestE2E_NfeioWebhookSubscriptionDestroyShim(t *testing.T) {
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
		"input":     map[string]any{"ref": "wh-99"},
	})
	resp, _, err := reconcile.ExecuteForTest(context.Background(), a, rpc.Delivery{Body: body})
	if err != nil {
		t.Fatalf("destroy_webhook_subscription dispatch failed: %v", err)
	}
	if !strings.Contains(string(resp), `"deleted":true`) {
		t.Fatalf("expected deleted:true in destroy response, got %s", resp)
	}
}
