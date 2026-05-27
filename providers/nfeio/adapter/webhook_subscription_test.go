package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestEnsureWebhookSubscription_Create exercises the happy path POST →
// 201 → adapter returns the freshly-created subscription envelope.
func TestEnsureWebhookSubscription_Create(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/webhooks" {
			t.Errorf("got %s %s; want POST /v2/webhooks", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"wh-1","url":"https://example.com/x","events":["Issued","Cancelled"],"active":true,"companyId":"cmpDefault"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	out, err := EnsureWebhookSubscription(context.Background(), cli, EnsureWebhookSubscriptionInput{
		URL:    "https://example.com/x",
		Events: []string{"Issued", "Cancelled"},
	})
	if err != nil {
		t.Fatalf("EnsureWebhookSubscription err = %v", err)
	}
	if out.ID != "wh-1" || out.Adopted {
		t.Fatalf("expected fresh subscription wh-1 (adopted=false); got %+v", out)
	}
}

// TestEnsureWebhookSubscription_Adopt exercises the 409 duplicate → adopt
// path (ensure_* convention: existing resource is decoded from error body).
func TestEnsureWebhookSubscription_Adopt(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"id":"wh-existing","url":"https://example.com/x","events":["Issued"],"active":true,"companyId":"cmpDefault"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	out, err := EnsureWebhookSubscription(context.Background(), cli, EnsureWebhookSubscriptionInput{
		URL:    "https://example.com/x",
		Events: []string{"Issued"},
	})
	if err != nil {
		t.Fatalf("EnsureWebhookSubscription err = %v", err)
	}
	if out.ID != "wh-existing" || !out.Adopted {
		t.Fatalf("expected adopted wh-existing; got %+v", out)
	}
}

// TestDestroyWebhookSubscription_404IsSuccess locks the convention §5
// invariant: Destroy MUST treat a not-found response as success.
func TestDestroyWebhookSubscription_404IsSuccess(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Errorf("got %s %s; want DELETE /v2/webhooks/wh-1", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	out, err := DestroyWebhookSubscription(context.Background(), cli, DestroyWebhookSubscriptionInput{ID: "wh-1"})
	if err != nil {
		t.Fatalf("DestroyWebhookSubscription on 404 must succeed; err = %v", err)
	}
	if !out.Deleted || !out.AlreadyAbsent {
		t.Fatalf("expected Deleted=true AlreadyAbsent=true; got %+v", out)
	}
}

// TestObserveWebhookSubscriptions_ByID locks the single-resource filter
// path. The convention's observe_* with {id} acts as the canonical GET
// /resource/{id} replacement.
func TestObserveWebhookSubscriptions_ByID(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Errorf("got %s %s; want GET /v2/webhooks/wh-1", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"wh-1","url":"https://x","events":["Issued"],"active":true,"companyId":"cmpDefault"}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)

	raw, _ := json.Marshal(ObserveWebhookSubscriptionsInput{ID: "wh-1"})
	out, err := ObserveWebhookSubscriptions(context.Background(), cli, raw)
	if err != nil {
		t.Fatalf("ObserveWebhookSubscriptions err = %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "wh-1" {
		t.Fatalf("expected single item wh-1; got %+v", out)
	}
}
