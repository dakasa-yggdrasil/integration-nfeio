package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dakasa-yggdrasil/yggdrasil-sdk-go/rpc"
)

func TestExecuteHandler_DestroyServiceInvoiceDocumentedInput(t *testing.T) {
	cases := []struct {
		name     string
		input    map[string]any
		wantPath string
	}{
		{
			name:     "explicit company",
			input:    map[string]any{"invoice_id": "inv-9", "company_id": "cmp-override"},
			wantPath: "/v2/companies/cmp-override/serviceinvoices/inv-9/cancel",
		},
		{
			name:     "instance default company",
			input:    map[string]any{"invoice_id": "inv-9"},
			wantPath: "/v2/companies/cmpDefault/serviceinvoices/inv-9/cancel",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPut || r.URL.Path != tc.wantPath {
					t.Errorf("got %s %s; want PUT %s", r.Method, r.URL.Path, tc.wantPath)
				}
				_, _ = w.Write([]byte(`{"status":"Cancelled","flowMessage":""}`))
			})
			defer srv.Close()
			handler, emitter := newCapturingHandler(t, srv.URL)

			body := coreEnvelope(t, OpDestroyServiceInvoice, tc.input, "nfeio-dakasa-production", "idem-cancel-1")
			resp, _, err := handler(context.Background(), rpc.Delivery{Body: body})
			if err != nil {
				t.Fatalf("destroy_service_invoice with {invoice_id}: %v", err)
			}
			if calls != 1 {
				t.Fatalf("provider calls = %d; want one cancel PUT", calls)
			}
			if string(resp) != `{"deleted":true}` {
				t.Fatalf("response = %s; want {\"deleted\":true}", resp)
			}
			if len(emitter.events) != 1 {
				t.Fatalf("mutation events = %d; want one", len(emitter.events))
			}
			ev := emitter.events[0]
			if ev.EventType != "nfeio.service_invoice.destroyed" {
				t.Fatalf("event type = %q; want nfeio.service_invoice.destroyed", ev.EventType)
			}
			if ev.ResourceID != "inv-9" {
				t.Fatalf("event resource_id = %q; want the invoice id inv-9", ev.ResourceID)
			}
			if ev.InstanceID != "nfeio-dakasa-production" {
				t.Fatalf("event instance_id = %q; want nfeio-dakasa-production", ev.InstanceID)
			}
		})
	}
}

func TestExecuteHandler_DestroyServiceInvoiceKeepsCallerIDPrecedence(t *testing.T) {
	var gotPath string
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"status":"Cancelled","flowMessage":""}`))
	})
	defer srv.Close()
	handler, emitter := newCapturingHandler(t, srv.URL)

	body := coreEnvelope(t, OpDestroyServiceInvoice,
		map[string]any{"id": "inv-by-id", "invoice_id": "inv-other"},
		"nfeio-dakasa-production", "")
	if _, _, err := handler(context.Background(), rpc.Delivery{Body: body}); err != nil {
		t.Fatalf("destroy_service_invoice with {id, invoice_id}: %v", err)
	}
	if gotPath != "/v2/companies/cmpDefault/serviceinvoices/inv-by-id/cancel" {
		t.Fatalf("cancel path = %q; want the caller's id to keep precedence over invoice_id", gotPath)
	}
	if len(emitter.events) != 1 || emitter.events[0].ResourceID != "inv-by-id" {
		t.Fatalf("events = %+v; want one destroyed event for inv-by-id", emitter.events)
	}
}

func TestExecuteHandler_DestroyServiceInvoicePendingCancelIsRetryableAndSilent(t *testing.T) {
	var calls int
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/v2/companies/cmp-override/serviceinvoices/inv-9/cancel" {
			t.Errorf("got %s %s; want the cancel PUT", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"WaitingSendCancel","flowMessage":"Aguardando retorno da prefeitura"}`))
	})
	defer srv.Close()
	handler, emitter := newCapturingHandler(t, srv.URL)

	body := coreEnvelope(t, OpDestroyServiceInvoice,
		map[string]any{"invoice_id": "inv-9", "company_id": "cmp-override"},
		"nfeio-dakasa-production", "")
	resp, _, err := handler(context.Background(), rpc.Delivery{Body: body})
	if err == nil {
		t.Fatalf("pending cancel returned success %s; want cancellation_pending", resp)
	}
	var pending *CancellationPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("error = %T %v; want *CancellationPendingError", err, err)
	}
	if !pending.Retryable() || pending.Code() != CancellationPendingCode {
		t.Fatalf("pending error retryable=%v code=%q; want retryable %s", pending.Retryable(), pending.Code(), CancellationPendingCode)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, CancellationPendingCode+":") {
		t.Fatalf("error %q does not start with the %s code", msg, CancellationPendingCode)
	}
	for _, want := range []string{"inv-9", "WaitingSendCancel", "Aguardando retorno da prefeitura"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not name %q", msg, want)
		}
	}
	if strings.Contains(msg, "key123") {
		t.Fatalf("error %q leaks the NFe.io API key", msg)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d; want one cancel PUT", calls)
	}
	if len(emitter.events) != 0 {
		t.Fatalf("mutation events = %d; want none while NFe.io has not confirmed the cancel", len(emitter.events))
	}
}

func TestCancellationPendingError_SanitizesProviderText(t *testing.T) {
	err := &CancellationPendingError{
		InvoiceID:   "inv-1",
		Status:      "Waiting\nSend\"Cancel",
		FlowMessage: strings.Repeat("x", maxProviderTextRunes+50),
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\n\\") {
		t.Fatalf("error %q keeps control characters or backslashes", msg)
	}
	if strings.Contains(msg, `Send"Cancel`) {
		t.Fatalf("error %q keeps a provider double quote", msg)
	}
	if strings.Contains(msg, strings.Repeat("x", maxProviderTextRunes+1)) {
		t.Fatalf("error %q keeps an untruncated flow message", msg)
	}
}

func TestWithServiceInvoiceDestroyRef_OnlyFillsAbsentRefForDestroy(t *testing.T) {
	unchanged := []string{
		`{"operation":"retrieve_pdf","input":{"invoice_id":"inv-1"}}`,
		`{"operation":"destroy_webhook_subscription","input":{"id":"wh-1","confirm_id":"wh-1"}}`,
		`{"operation":"destroy_service_invoice","input":{"ref":"inv-explicit","invoice_id":"inv-other"}}`,
		`{"operation":"destroy_service_invoice","input":{"service_invoice_id":"inv-explicit","invoice_id":"inv-other"}}`,
		`{"operation":"destroy_service_invoice","input":{"id":"inv-explicit","invoice_id":"inv-other"}}`,
		`{"operation":"destroy_service_invoice","input":{"company_id":"cmp-1"}}`,
		`{"operation":"destroy_service_invoice"}`,
		`not json`,
	}
	for _, raw := range unchanged {
		if out := withServiceInvoiceDestroyRef([]byte(raw)); string(out) != raw {
			t.Fatalf("withServiceInvoiceDestroyRef(%s) = %s; want the body unchanged", raw, out)
		}
	}

	// Empty values do not count as a caller-supplied ref.
	out := withServiceInvoiceDestroyRef([]byte(`{"capability":"destroy_service_invoice","input":{"invoice_id":"inv-2","company_id":"cmp-1","ref":"","id":null}}`))
	var got struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{"invoice_id": "inv-2", "company_id": "cmp-1", "ref": "inv-2", "id": nil}
	if !reflect.DeepEqual(got.Input, want) {
		t.Fatalf("input = %v; want %v", got.Input, want)
	}
}
