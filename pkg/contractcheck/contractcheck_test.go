package contractcheck

import (
	"strings"
	"testing"
)

// validSpec is a hand-rolled DescribeResponse that satisfies every rule
// of LintDescribeContract. Tests mutate copies of it to exercise each
// failure mode.
func validSpec() (DescribeResponse, []string) {
	spec := DescribeResponse{
		ResourceTypes: []ResourceType{
			{Name: "component", DefaultActions: []string{"create_component"}},
		},
		ActionCatalog: []ActionDefinition{
			{Name: "create_component", ResourceTypes: []string{"component"}},
			{Name: "delete_component", ResourceTypes: []string{"component"}},
		},
		Execution: ExecutionSpec{
			IdempotentActions: []string{"create_component"},
		},
	}
	supported := []string{"create_component", "delete_component"}
	return spec, supported
}

func TestLintPassesOnValidSpec(t *testing.T) {
	spec, supported := validSpec()
	if err := LintDescribeContract(spec, supported); err != nil {
		t.Fatalf("expected nil error on valid spec, got: %v", err)
	}
}

func TestLintFlagsMissingActionForOperation(t *testing.T) {
	spec, supported := validSpec()
	supported = append(supported, "ghost_operation")

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `operation "ghost_operation" in SupportedExecuteOperations is missing from ActionCatalog`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLintFlagsActionMissingFromSupportedOperations(t *testing.T) {
	spec, supported := validSpec()
	spec.ActionCatalog = append(spec.ActionCatalog, ActionDefinition{
		Name:          "rogue_action",
		ResourceTypes: []string{"component"},
	})

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `action "rogue_action" in ActionCatalog is missing from SupportedExecuteOperations`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLintFlagsActionWithoutResourceTypes(t *testing.T) {
	spec, supported := validSpec()
	spec.ActionCatalog = append(spec.ActionCatalog, ActionDefinition{Name: "orphan_action"})
	supported = append(supported, "orphan_action")

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `action "orphan_action" has no resource_types`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLintFlagsUnknownResourceTypeReference(t *testing.T) {
	spec, supported := validSpec()
	spec.ActionCatalog = append(spec.ActionCatalog, ActionDefinition{
		Name:          "phantom_action",
		ResourceTypes: []string{"phantom_resource"},
	})
	supported = append(supported, "phantom_action")

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `action "phantom_action" references unknown resource_type "phantom_resource"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLintFlagsResourceTypeDefaultActionMissingFromSupported(t *testing.T) {
	spec, supported := validSpec()
	spec.ResourceTypes = append(spec.ResourceTypes, ResourceType{
		Name:           "extra_resource",
		DefaultActions: []string{"undefined_op"},
	})

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `resource_type "extra_resource" default_actions references unknown operation "undefined_op"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLintFlagsIdempotentActionMissingFromSupported(t *testing.T) {
	spec, supported := validSpec()
	spec.Execution.IdempotentActions = append(spec.Execution.IdempotentActions, "phantom_idempotent")

	err := LintDescribeContract(spec, supported)
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), `idempotent_actions references unknown operation "phantom_idempotent"`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}
