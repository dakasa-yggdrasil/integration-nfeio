package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/config"
)

const (
	webhookSecretCanary = "CANARY_WEBHOOK_SECRET_DO_NOT_LEAK"
	headerSecretCanary  = "CANARY_AUTHORIZATION_DO_NOT_LEAK"
	headerCaseCanary    = "SECOND_LEGACY_VALUE_DO_NOT_LEAK"
	propertyCanary      = "CANARY_PROPERTY_DO_NOT_LEAK"
)

func falsePointer() *bool {
	value := false
	return &value
}

func truePointer() *bool {
	value := true
	return &value
}

func mustNewClientWithWebhookSecret(t *testing.T, baseURL, secret string) *Client {
	t.Helper()
	cli, err := NewClient(&config.Config{
		APIKey:        "key123",
		WebhookSecret: secret,
		CompanyID:     "cmpDefault",
		BaseURL:       baseURL,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewClient err = %v", err)
	}
	return cli
}

func webhookProviderObject(id string, insecureSSL bool) map[string]any {
	return map[string]any{
		"id":          id,
		"uri":         "https://enterprise.dakasa.me/webhook/nfeio?token=" + propertyCanary,
		"secret":      webhookSecretCanary,
		"contentType": "application/json",
		"insecureSsl": insecureSSL,
		"status":      "active",
		"version":     "2",
		"filters":     []any{"InvoiceIssued", "InvoiceCancelled"},
		"headers": map[string]any{
			"Authorization": headerSecretCanary,
			"X-Preserved":   "yes",
		},
		"properties": map[string]any{
			"opaque": propertyCanary,
		},
		"subscription": map[string]any{
			"companyId": "company-1",
		},
		"createdOn":  "2026-01-02T03:04:05Z",
		"modifiedOn": "2026-01-02T03:04:05Z",
		"futureField": map[string]any{
			"keep": true,
		},
	}
}

func writeWebhookResponse(t *testing.T, w http.ResponseWriter, object map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{webhookEnvelopeField: object}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func assertSecretFree(t *testing.T, value string) {
	t.Helper()
	for _, forbidden := range []string{
		webhookSecretCanary,
		headerSecretCanary,
		headerCaseCanary,
		propertyCanary,
		`"secret"`,
		`"headers"`,
		`"properties"`,
		`"uri"`,
		"Authorization",
	} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("secret-bearing provider data escaped the boundary: found %q in %s", forbidden, value)
		}
	}
}

func TestEnsureWebhookSubscription_ExactIDNoopNeverMutates(t *testing.T) {
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodGet || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Errorf("got %s %s; want exact GET", r.Method, r.URL.Path)
		}
		writeWebhookResponse(t, w, webhookProviderObject("wh-1", false))
	})
	defer srv.Close()

	out, err := EnsureWebhookSubscription(context.Background(), mustNewClientWithWebhookSecret(t, srv.URL, webhookSecretCanary), EnsureWebhookSubscriptionInput{
		ID:          "wh-1",
		InsecureSSL: falsePointer(),
	})
	if err != nil {
		t.Fatalf("EnsureWebhookSubscription err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d; want one exact GET", calls)
	}
	if out.ID != "wh-1" || out.InsecureSSL || !out.Adopted || out.Updated {
		t.Fatalf("unexpected safe output: %+v", out)
	}
	encoded, _ := json.Marshal(out)
	assertSecretFree(t, string(encoded))
}

func TestEnsureWebhookSubscription_ExactIDUpdatePreservesEveryExistingField(t *testing.T) {
	original := webhookProviderObject("wh-1", true)
	secure := webhookProviderObject("wh-1", false)
	var calls int32

	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v2/webhooks/wh-1" {
			t.Fatalf("call %d path = %s; want exact ID path", call, r.URL.Path)
		}
		switch call {
		case 1:
			if r.Method != http.MethodGet {
				t.Fatalf("call 1 method = %s; want GET", r.Method)
			}
			writeWebhookResponse(t, w, original)
		case 2:
			if r.Method != http.MethodPut {
				t.Fatalf("call 2 method = %s; want PUT", r.Method)
			}
			var body map[string]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			got := body[webhookEnvelopeField]
			if !reflect.DeepEqual(got, secure) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(secure)
				t.Fatalf("PUT changed fields besides insecureSsl\ngot:  %s\nwant: %s", gotJSON, wantJSON)
			}
			w.WriteHeader(http.StatusNoContent)
		case 3:
			if r.Method != http.MethodGet {
				t.Fatalf("call 3 method = %s; want confirmation GET", r.Method)
			}
			writeWebhookResponse(t, w, secure)
		default:
			t.Fatalf("unexpected provider call %d: %s %s", call, r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	out, err := EnsureWebhookSubscription(context.Background(), mustNewClientWithWebhookSecret(t, srv.URL, webhookSecretCanary), EnsureWebhookSubscriptionInput{
		ID:          "wh-1",
		InsecureSSL: falsePointer(),
	})
	if err != nil {
		t.Fatalf("EnsureWebhookSubscription err = %v", err)
	}
	if calls != 3 {
		t.Fatalf("provider calls = %d; want GET, PUT, GET", calls)
	}
	if !out.Adopted || !out.Updated || out.InsecureSSL {
		t.Fatalf("unexpected safe output: %+v", out)
	}
	encoded, _ := json.Marshal(out)
	assertSecretFree(t, string(encoded))
}

func TestEnsureWebhookSubscription_ExactIDSecurityMigrationUsesRuntimeHMACAndRemovesLegacyAuthorization(t *testing.T) {
	original := webhookProviderObject("wh-1", true)
	delete(original, webhookSecretField)
	originalHeaders := original[webhookHeadersField].(map[string]any)
	originalHeaders["authorization"] = headerCaseCanary

	expectedPut := webhookProviderObject("wh-1", false)
	expectedHeaders := expectedPut[webhookHeadersField].(map[string]any)
	delete(expectedHeaders, "Authorization")

	confirmed := webhookProviderObject("wh-1", false)
	delete(confirmed, webhookSecretField)
	confirmedHeaders := confirmed[webhookHeadersField].(map[string]any)
	delete(confirmedHeaders, "Authorization")

	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v2/webhooks/wh-1" {
			t.Fatalf("call %d path = %s; want exact ID path", call, r.URL.Path)
		}
		switch call {
		case 1:
			if r.Method != http.MethodGet {
				t.Fatalf("call 1 method = %s; want GET", r.Method)
			}
			writeWebhookResponse(t, w, original)
		case 2:
			if r.Method != http.MethodPut {
				t.Fatalf("call 2 method = %s; want PUT", r.Method)
			}
			var body map[string]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			got := body[webhookEnvelopeField]
			if !reflect.DeepEqual(got, expectedPut) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(expectedPut)
				t.Fatalf("security migration changed an unapproved field\ngot:  %s\nwant: %s", gotJSON, wantJSON)
			}
			w.WriteHeader(http.StatusNoContent)
		case 3:
			if r.Method != http.MethodGet {
				t.Fatalf("call 3 method = %s; want confirmation GET", r.Method)
			}
			writeWebhookResponse(t, w, confirmed)
		default:
			t.Fatalf("unexpected provider call %d: %s %s", call, r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	cli, err := NewClient(&config.Config{
		APIKey:        "key123",
		WebhookSecret: webhookSecretCanary,
		CompanyID:     "cmpDefault",
		BaseURL:       srv.URL,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	out, err := EnsureWebhookSubscription(context.Background(), cli, EnsureWebhookSubscriptionInput{
		ID:                         "wh-1",
		InsecureSSL:                falsePointer(),
		SetHMACFromRuntime:         true,
		RemoveLegacyAuthorization:  true,
		ConfirmSecurityMigrationID: "wh-1",
	})
	if err != nil {
		t.Fatalf("EnsureWebhookSubscription err = %v", err)
	}
	if calls != 3 || !out.Adopted || !out.Updated || out.InsecureSSL {
		t.Fatalf("unexpected migration result: calls=%d out=%+v", calls, out)
	}
	encoded, _ := json.Marshal(out)
	assertSecretFree(t, string(encoded))
}

func TestEnsureWebhookSubscription_SecurityMigrationReplayProducesIdenticalPUT(t *testing.T) {
	current := webhookProviderObject("wh-1", false)
	delete(current, webhookSecretField)
	delete(current[webhookHeadersField].(map[string]any), "Authorization")
	var calls int32
	var puts []map[string]any
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		switch call % 3 {
		case 1, 0:
			if r.Method != http.MethodGet {
				t.Fatalf("call %d method = %s; want GET", call, r.Method)
			}
			writeWebhookResponse(t, w, current)
		case 2:
			if r.Method != http.MethodPut {
				t.Fatalf("call %d method = %s; want PUT", call, r.Method)
			}
			var body map[string]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode replay PUT: %v", err)
			}
			puts = append(puts, body[webhookEnvelopeField])
			w.WriteHeader(http.StatusNoContent)
		}
	})
	defer srv.Close()

	cli := mustNewClientWithWebhookSecret(t, srv.URL, webhookSecretCanary)
	in := EnsureWebhookSubscriptionInput{
		ID:                         "wh-1",
		InsecureSSL:                falsePointer(),
		SetHMACFromRuntime:         true,
		RemoveLegacyAuthorization:  true,
		ConfirmSecurityMigrationID: "wh-1",
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := EnsureWebhookSubscription(context.Background(), cli, in); err != nil {
			t.Fatalf("migration replay attempt %d: %v", attempt+1, err)
		}
	}
	if calls != 6 || len(puts) != 2 {
		t.Fatalf("migration replay calls=%d puts=%d; want two GET/PUT/GET cycles", calls, len(puts))
	}
	if !reflect.DeepEqual(puts[0], puts[1]) {
		t.Fatal("migration replay changed the semantic PUT body")
	}
}

func TestEnsureWebhookSubscription_SecurityMigrationFailsWhenLegacyAuthorizationSurvives(t *testing.T) {
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		switch call {
		case 1:
			writeWebhookResponse(t, w, webhookProviderObject("wh-1", true))
		case 2:
			w.WriteHeader(http.StatusNoContent)
		case 3:
			writeWebhookResponse(t, w, webhookProviderObject("wh-1", false))
		default:
			t.Fatalf("unexpected provider call %d", call)
		}
	})
	defer srv.Close()

	cli, err := NewClient(&config.Config{
		APIKey:        "key123",
		WebhookSecret: webhookSecretCanary,
		CompanyID:     "cmpDefault",
		BaseURL:       srv.URL,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	_, err = EnsureWebhookSubscription(context.Background(), cli, EnsureWebhookSubscriptionInput{
		ID:                         "wh-1",
		InsecureSSL:                falsePointer(),
		SetHMACFromRuntime:         true,
		RemoveLegacyAuthorization:  true,
		ConfirmSecurityMigrationID: "wh-1",
	})
	if err == nil {
		t.Fatal("migration must fail when the provider keeps legacy Authorization")
	}
	if calls != 3 {
		t.Fatalf("provider calls = %d; want GET, PUT, GET", calls)
	}
	assertSecretFree(t, err.Error())
}

func TestEnsureWebhookSubscription_OrdinaryDriftWithoutProviderSecretFailsBeforePUT(t *testing.T) {
	original := webhookProviderObject("wh-1", true)
	delete(original, webhookSecretField)
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call != 1 || r.Method != http.MethodGet {
			t.Fatalf("ordinary reconciliation without a preservable secret must stop after GET, call=%d method=%s", call, r.Method)
		}
		writeWebhookResponse(t, w, original)
	})
	defer srv.Close()

	_, err := EnsureWebhookSubscription(context.Background(), mustNewClient(t, srv.URL), EnsureWebhookSubscriptionInput{
		ID:          "wh-1",
		InsecureSSL: falsePointer(),
	})
	if err == nil {
		t.Fatal("ordinary provider update must require the explicit security migration when GET omits secret")
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d; want one GET and zero PUTs", calls)
	}
	assertSecretFree(t, err.Error())
}

func TestEnsureWebhookSubscription_RejectsCreateShapeBeforeProviderCall(t *testing.T) {
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		t.Fatalf("provider must not be called for a create-shaped request")
	})
	defer srv.Close()

	payload := []byte(`{"operation":"ensure_webhook_subscription","input":{"id":"wh-1","insecure_ssl":false,"url":"https://example.invalid/` + propertyCanary + `","events":["Issued"]}}`)
	_, err := executeRoute(context.Background(), mustNewClient(t, srv.URL), nil, nil, payload)
	if err == nil {
		t.Fatal("create-shaped request must fail")
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d; want zero", calls)
	}
	assertSecretFree(t, err.Error())
}

func TestEnsureWebhookSubscription_RejectsNullMigrationFieldsBeforeProviderCall(t *testing.T) {
	inputs := []string{
		`{"id":"wh-1","insecure_ssl":false,"set_hmac_from_runtime":null}`,
		`{"id":"wh-1","insecure_ssl":false,"remove_legacy_authorization":null}`,
		`{"id":"wh-1","insecure_ssl":false,"confirm_security_migration_id":null}`,
	}
	for _, input := range inputs {
		var calls int32
		srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
		})
		payload := []byte(`{"operation":"ensure_webhook_subscription","input":` + input + `}`)
		_, err := executeRoute(context.Background(), mustNewClient(t, srv.URL), nil, nil, payload)
		srv.Close()
		if err == nil {
			t.Fatalf("null migration field must fail: %s", input)
		}
		if calls != 0 {
			t.Fatalf("null migration field made %d provider calls", calls)
		}
		assertSecretFree(t, err.Error())
	}
}

func TestEnsureWebhookSubscription_RejectsUnsafeDesiredStateBeforeProviderCall(t *testing.T) {
	tests := []struct {
		name  string
		input EnsureWebhookSubscriptionInput
	}{
		{name: "missing exact id", input: EnsureWebhookSubscriptionInput{InsecureSSL: falsePointer()}},
		{name: "missing desired field", input: EnsureWebhookSubscriptionInput{ID: "wh-1"}},
		{name: "allows only false", input: EnsureWebhookSubscriptionInput{ID: "wh-1", InsecureSSL: truePointer()}},
		{name: "path injection", input: EnsureWebhookSubscriptionInput{ID: "../webhooks", InsecureSSL: falsePointer()}},
		{name: "current dot segment", input: EnsureWebhookSubscriptionInput{ID: ".", InsecureSSL: falsePointer()}},
		{name: "parent dot segment", input: EnsureWebhookSubscriptionInput{ID: "..", InsecureSSL: falsePointer()}},
		{name: "partial HMAC request", input: EnsureWebhookSubscriptionInput{ID: "wh-1", InsecureSSL: falsePointer(), SetHMACFromRuntime: true}},
		{name: "partial legacy removal", input: EnsureWebhookSubscriptionInput{ID: "wh-1", InsecureSSL: falsePointer(), RemoveLegacyAuthorization: true}},
		{name: "mismatched security confirmation", input: EnsureWebhookSubscriptionInput{ID: "wh-1", InsecureSSL: falsePointer(), SetHMACFromRuntime: true, RemoveLegacyAuthorization: true, ConfirmSecurityMigrationID: "wh-other"}},
		{name: "invalid runtime HMAC", input: EnsureWebhookSubscriptionInput{ID: "wh-1", InsecureSSL: falsePointer(), SetHMACFromRuntime: true, RemoveLegacyAuthorization: true, ConfirmSecurityMigrationID: "wh-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
			})
			defer srv.Close()
			_, err := EnsureWebhookSubscription(context.Background(), mustNewClient(t, srv.URL), tt.input)
			if err == nil {
				t.Fatal("unsafe desired state must fail")
			}
			if calls != 0 {
				t.Fatalf("provider calls = %d; want zero", calls)
			}
		})
	}
}

func TestEnsureWebhookSubscription_RejectsMismatchedProviderIDWithoutPUT(t *testing.T) {
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodGet {
			t.Fatalf("mismatched identity must never be mutated, got %s", r.Method)
		}
		writeWebhookResponse(t, w, webhookProviderObject("wh-other", true))
	})
	defer srv.Close()

	_, err := EnsureWebhookSubscription(context.Background(), mustNewClient(t, srv.URL), EnsureWebhookSubscriptionInput{
		ID:          "wh-1",
		InsecureSSL: falsePointer(),
	})
	if err == nil {
		t.Fatal("mismatched provider ID must fail")
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d; want one GET and no PUT", calls)
	}
}

func TestEnsureWebhookSubscription_SanitizesProviderErrorAndLogs(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			writeWebhookResponse(t, w, webhookProviderObject("wh-1", true))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"name":"` + headerSecretCanary + `","message":"` + webhookSecretCanary + propertyCanary + `"}`))
	})
	defer srv.Close()
	cli, err := NewClient(&config.Config{
		APIKey:        "key123",
		WebhookSecret: webhookSecretCanary,
		CompanyID:     "cmpDefault",
		BaseURL:       srv.URL,
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	_, err = EnsureWebhookSubscription(context.Background(), cli, EnsureWebhookSubscriptionInput{
		ID:          "wh-1",
		InsecureSSL: falsePointer(),
	})
	if err == nil {
		t.Fatal("provider PUT failure must remain a failure")
	}
	assertSecretFree(t, err.Error())
	for _, entry := range logs.All() {
		encoded, _ := json.Marshal(entry.ContextMap())
		assertSecretFree(t, entry.Message+string(encoded))
	}
}

func TestObserveWebhookSubscriptions_ExactIDAndSecretFree(t *testing.T) {
	var calls int32
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodGet || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Fatalf("got %s %s; want exact GET", r.Method, r.URL.Path)
		}
		writeWebhookResponse(t, w, webhookProviderObject("wh-1", true))
	})
	defer srv.Close()

	out, err := ObserveWebhookSubscriptions(context.Background(), mustNewClient(t, srv.URL), []byte(`{"id":"wh-1"}`))
	if err != nil {
		t.Fatalf("ObserveWebhookSubscriptions err = %v", err)
	}
	if calls != 1 || len(out.Items) != 1 || out.Items[0].ID != "wh-1" || !out.Items[0].InsecureSSL {
		t.Fatalf("unexpected observe output: calls=%d out=%+v", calls, out)
	}
	encoded, _ := json.Marshal(out)
	assertSecretFree(t, string(encoded))
}

func TestObserveWebhookSubscriptions_NeverLists(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`{"cursor":"next"}`)} {
		var calls int32
		srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
		})
		_, err := ObserveWebhookSubscriptions(context.Background(), mustNewClient(t, srv.URL), raw)
		srv.Close()
		if err == nil {
			t.Fatalf("input %s must fail without an exact ID", raw)
		}
		if calls != 0 {
			t.Fatalf("input %s made %d provider calls; want zero", raw, calls)
		}
	}
}

func TestDestroyWebhookSubscription_RequiresMatchingConfirmation(t *testing.T) {
	for _, input := range []DestroyWebhookSubscriptionInput{
		{ID: "wh-1"},
		{ID: "wh-1", ConfirmID: "wh-other"},
		{ID: "../wh-1", ConfirmID: "../wh-1"},
	} {
		var calls int32
		srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
		})
		_, err := DestroyWebhookSubscription(context.Background(), mustNewClient(t, srv.URL), input)
		srv.Close()
		if err == nil {
			t.Fatalf("unsafe destroy input %+v must fail", input)
		}
		if calls != 0 {
			t.Fatalf("unsafe destroy made %d calls; want zero", calls)
		}
	}
}

func TestDestroyWebhookSubscription_ConfirmedExactID404IsSuccess(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/webhooks/wh-1" {
			t.Fatalf("got %s %s; want exact DELETE", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"` + webhookSecretCanary + `"}`))
	})
	defer srv.Close()

	out, err := DestroyWebhookSubscription(context.Background(), mustNewClient(t, srv.URL), DestroyWebhookSubscriptionInput{
		ID:        "wh-1",
		ConfirmID: "wh-1",
	})
	if err != nil {
		t.Fatalf("confirmed 404 destroy must succeed: %v", err)
	}
	if !out.Deleted || !out.AlreadyAbsent {
		t.Fatalf("unexpected destroy output: %+v", out)
	}
}
