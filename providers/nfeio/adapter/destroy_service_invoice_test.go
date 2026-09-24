package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
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

func TestWithServiceInvoiceDestroyRef_OnlyFillsAbsentRefForDestroy(t *testing.T) {
	unchanged := []string{
		`{"operation":"retrieve_pdf","input":{"invoice_id":"inv-1"}}`,
		`{"operation":"destroy_webhook_subscription","input":{"id":"wh-1","confirm_id":"wh-1"}}`,
		`{"operation":"destroy_service_invoice","input":{"ref":"inv-explicit","invoice_id":"inv-other"}}`,
		`{"operation":"destroy_service_invoice","input":{"company_id":"cmp-1"}}`,
		`{"operation":"destroy_service_invoice"}`,
		`not json`,
	}
	for _, raw := range unchanged {
		if out := withServiceInvoiceDestroyRef([]byte(raw)); string(out) != raw {
			t.Fatalf("withServiceInvoiceDestroyRef(%s) = %s; want the body unchanged", raw, out)
		}
	}

	out := withServiceInvoiceDestroyRef([]byte(`{"capability":"destroy_service_invoice","input":{"invoice_id":"inv-2","company_id":"cmp-1"}}`))
	var got struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{"invoice_id": "inv-2", "company_id": "cmp-1", "ref": "inv-2"}
	if !reflect.DeepEqual(got.Input, want) {
		t.Fatalf("input = %v; want %v", got.Input, want)
	}
}
