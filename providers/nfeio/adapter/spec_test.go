package adapter

import "testing"

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
