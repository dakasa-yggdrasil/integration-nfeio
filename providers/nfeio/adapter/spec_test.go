package adapter

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSpec_Constants(t *testing.T) {
	if Provider != "nfeio" {
		t.Fatalf("Provider = %q; want nfeio", Provider)
	}
	if IntegrationType != "nfeio" {
		t.Fatalf("IntegrationType = %q; want nfeio", IntegrationType)
	}
	if AdapterVersion == "" {
		t.Fatal("AdapterVersion must not be empty")
	}
}

func TestSpec_Describe_ReturnsAllCapabilitiesAndReactor(t *testing.T) {
	d := Describe()
	if d.Provider != Provider {
		t.Fatalf("describe.Provider = %q; want %q", d.Provider, Provider)
	}
	want := map[string]bool{
		"issue_nfse": true, "get_nfse_status": true, "cancel_nfse": true,
		"retrieve_pdf": true, "retrieve_xml": true, "register_company": true,
		"list_municipalities": true, "manage_template": true, "bulk_issue": true,
		"calculate_iss": true, "nfse_webhook_received": true,
	}
	got := map[string]bool{}
	for _, a := range d.ActionCatalog {
		got[a.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("Describe() missing action %q", name)
		}
	}
}

func TestMetrics_AllSpecRequiredAreRegistered(t *testing.T) {
	// Spec §10 mandates these 7 metrics on /metrics. We touch each one
	// (Inc/Set with safe values) so they appear in the default gatherer,
	// then assert every name is present.
	metricBulkIssueItems.WithLabelValues("success").Add(0)
	metricRequestDuration.WithLabelValues("issue_nfse").Observe(0)
	metricRequestErrors.WithLabelValues("issue_nfse", "200").Add(0)
	metricWebhookReceived.WithLabelValues("issued").Add(0)
	metricTemplateLoad.WithLabelValues("3550308").Set(0)
	metricRateLimitRemaining.Set(0)
	metricDedupHits.Add(0)

	want := []string{
		"nfeio_request_duration_seconds",
		"nfeio_request_errors_total",
		"nfeio_webhook_received_total",
		"nfeio_template_load_total",
		"nfeio_rate_limit_remaining",
		"nfeio_bulk_issue_items_total",
		"nfeio_dedup_hits_total",
	}
	gathered, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, mf := range gathered {
		got[mf.GetName()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("metric %q is not registered", name)
		}
	}
}

func TestSpec_SupportedExecuteOperations_ExcludesReactor(t *testing.T) {
	// Reactor (nfse_webhook_received) is NOT exposed via RPC execute.
	for _, op := range SupportedExecuteOperations {
		if op == "nfse_webhook_received" {
			t.Fatal("SupportedExecuteOperations must not include the reactor nfse_webhook_received")
		}
	}
	// All 10 capabilities (including calculate_iss sub-cap) ARE in execute.
	wantExec := []string{
		"issue_nfse", "get_nfse_status", "cancel_nfse", "retrieve_pdf",
		"retrieve_xml", "register_company", "list_municipalities",
		"manage_template", "bulk_issue", "calculate_iss",
	}
	gotSet := map[string]bool{}
	for _, op := range SupportedExecuteOperations {
		gotSet[op] = true
	}
	for _, w := range wantExec {
		if !gotSet[w] {
			t.Errorf("SupportedExecuteOperations missing %q", w)
		}
	}
}
