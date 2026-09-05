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
	if AdapterVersion != "3.1.1" {
		t.Fatalf("AdapterVersion = %q; want 3.1.1 for passive quorum topology", AdapterVersion)
	}
}

func TestSpec_Describe_ReturnsAllCapabilitiesAndReactor(t *testing.T) {
	d := Describe()
	if d.Provider != Provider {
		t.Fatalf("describe.Provider = %q; want %q", d.Provider, Provider)
	}
	// v2.0.0 convention names per docs/superpowers/specs/2026-05-27-yggdrasil-integration-capability-convention.md §7 (Tier C nfe.io).
	want := map[string]bool{
		"ensure_service_invoice":        true,
		"observe_service_invoices":      true,
		"destroy_service_invoice":       true,
		"retrieve_pdf":                  true,
		"retrieve_xml":                  true,
		"ensure_company":                true,
		"observe_companies":             true,
		"observe_municipalities":        true,
		"manage_template":               true,
		"bulk_issue":                    true,
		"calculate_iss":                 true,
		"ensure_webhook_subscription":   true,
		"observe_webhook_subscriptions": true,
		"destroy_webhook_subscription":  true,
		"nfse_webhook_received":         true,
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

// catalogNames returns a set of action_catalog entry names from Describe()
// for use by the v2.0.0 convention tests.
func catalogNames() map[string]bool {
	desc := Describe()
	got := map[string]bool{}
	for _, a := range desc.ActionCatalog {
		got[a.Name] = true
	}
	return got
}

func TestSpec_EnsureServiceInvoiceReplacesIssueNfse(t *testing.T) {
	got := catalogNames()
	if !got["ensure_service_invoice"] {
		t.Error("expected ensure_service_invoice")
	}
	if got["issue_nfse"] {
		t.Error("issue_nfse must be removed")
	}
}

func TestSpec_ObserveServiceInvoicesMergesGetNfseStatus(t *testing.T) {
	got := catalogNames()
	if !got["observe_service_invoices"] {
		t.Error("expected observe_service_invoices")
	}
	if got["get_nfse_status"] {
		t.Error("get_nfse_status must be merged into observe_service_invoices")
	}
	if got["list_service_invoices"] {
		t.Error("list_service_invoices must be removed (already merged)")
	}
}

func TestSpec_DestroyServiceInvoiceReplacesCancelNfse(t *testing.T) {
	got := catalogNames()
	if !got["destroy_service_invoice"] {
		t.Error("expected destroy_service_invoice")
	}
	if got["cancel_nfse"] {
		t.Error("cancel_nfse must be removed")
	}
}

func TestSpec_EnsureCompanyReplacesRegisterCompany(t *testing.T) {
	got := catalogNames()
	if !got["ensure_company"] {
		t.Error("expected ensure_company")
	}
	if got["register_company"] {
		t.Error("register_company must be removed")
	}
	if !got["observe_companies"] {
		t.Error("expected observe_companies (list-by-CNPJ)")
	}
}

func TestSpec_ObserveMunicipalitiesReplacesListMunicipalities(t *testing.T) {
	got := catalogNames()
	if !got["observe_municipalities"] {
		t.Error("expected observe_municipalities")
	}
	if got["list_municipalities"] {
		t.Error("list_municipalities must be removed")
	}
}

func TestSpec_NfeioWebhookSubscriptionTriple(t *testing.T) {
	got := catalogNames()
	for _, want := range []string{
		"ensure_webhook_subscription",
		"observe_webhook_subscriptions",
		"destroy_webhook_subscription",
	} {
		if !got[want] {
			t.Errorf("expected %q present", want)
		}
	}
}

func TestSpec_NfeioWebhookSubscriptionResourceType(t *testing.T) {
	desc := Describe()
	for _, rt := range desc.ResourceTypes {
		if rt.Name == "webhook_subscription" {
			for _, action := range rt.DefaultActions {
				if action == OpDestroyWebhookSubscription {
					t.Fatal("destroy_webhook_subscription must not be a webhook default action")
				}
			}
			return
		}
	}
	t.Fatal("webhook_subscription resource_type missing")
}

// TestSpec_CredentialSchemaModeIsInline locks the credential schema
// mode to "inline" — the only value Yggdrasil core's Phase 1 manifest
// validator accepts. Pre-v2.2.2 the spec advertised "env" which
// forced a manual hand-patch on every re-register (caught Phase C
// 2026-05-27 smoke). The Required env-var names are still declared
// so downstream tooling knows what to bind from secret stores.
func TestSpec_CredentialSchemaModeIsInline(t *testing.T) {
	d := Describe()
	if got := d.CredentialSchema.Mode; got != "inline" {
		t.Fatalf("CredentialSchema.Mode = %q; want %q (Yggdrasil core only accepts inline; see Phase 1 validator)", got, "inline")
	}
	wantRequired := map[string]bool{"NFEIO_API_KEY": true, "NFEIO_WEBHOOK_SECRET": true}
	gotRequired := map[string]bool{}
	for _, k := range d.CredentialSchema.Required {
		gotRequired[k] = true
	}
	for k := range wantRequired {
		if !gotRequired[k] {
			t.Errorf("CredentialSchema.Required missing %q", k)
		}
	}
}

// TestSpec_UIMetadata_Section15 verifies §15 INTEGRATION_CONTRACT.md
// compliance: every property in credential_schema MUST carry UI metadata
// (label, group, order) so surfaces render forms generically without
// per-provider hardcoded knowledge.
func TestSpec_UIMetadata_Section15(t *testing.T) {
	d := Describe()
	for name, prop := range d.CredentialSchema.Properties {
		if prop.Label == "" {
			t.Errorf("CredentialSchema[%q]: missing Label (§15)", name)
		}
		if prop.LabelLocale == nil || prop.LabelLocale["pt-BR"] == "" || prop.LabelLocale["en-US"] == "" {
			t.Errorf("CredentialSchema[%q]: missing LabelLocale pt-BR/en-US (§15)", name)
		}
		if prop.Group == "" {
			t.Errorf("CredentialSchema[%q]: missing Group (§15)", name)
		}
		if prop.Order == 0 {
			t.Errorf("CredentialSchema[%q]: missing Order (§15)", name)
		}
		// Sensitive defaults true for credentials.
		if !prop.Sensitive {
			t.Errorf("CredentialSchema[%q]: credential should be Sensitive=true (§15)", name)
		}
	}
	// instance schema also gets metadata.
	for name, prop := range d.InstanceSchema.Properties {
		if prop.Label == "" {
			t.Errorf("InstanceSchema[%q]: missing Label (§15)", name)
		}
		if prop.Group == "" {
			t.Errorf("InstanceSchema[%q]: missing Group (§15)", name)
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
	// v2.0.0: 14 callable capabilities (canonical triples + helpers + sub-caps).
	wantExec := []string{
		"ensure_service_invoice", "observe_service_invoices", "destroy_service_invoice",
		"retrieve_pdf", "retrieve_xml",
		"ensure_company", "observe_companies",
		"observe_municipalities",
		"manage_template", "bulk_issue", "calculate_iss",
		"ensure_webhook_subscription", "observe_webhook_subscriptions", "destroy_webhook_subscription",
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
