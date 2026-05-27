package contract

import "testing"

func TestAction_HasAllFieldsForCatalog(t *testing.T) {
	a := Action{Name: "x", Category: "capability", Idempotent: true, ResourceTypes: []string{"y"}}
	if a.Name != "x" || a.Category != "capability" || !a.Idempotent || len(a.ResourceTypes) != 1 {
		t.Fatal("Action fields incomplete")
	}
}

func TestAdapterDescribeResponse_NewlyCreatedHasExpectedFields(t *testing.T) {
	r := AdapterDescribeResponse{Provider: "nfeio"}
	r.Adapter.Version = "1.0.0"
	r.Capabilities = []string{"describe", "execute"}
	r.ActionCatalog = []Action{{Name: "issue_nfse", Category: "capability"}}
	if r.Provider != "nfeio" || r.Adapter.Version != "1.0.0" || len(r.ActionCatalog) != 1 {
		t.Fatal("AdapterDescribeResponse shape mismatch")
	}
}
