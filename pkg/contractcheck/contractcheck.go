// Package contractcheck provides the describe-contract lint used to catch
// the recurring manifest drift pattern that has bitten DaKasa integrations
// (e.g. integration-aws v1.22.0 and integration-grafana): the
// SupportedExecuteOperations list, the ResourceTypes slice and the
// ActionCatalog drift out of sync, and the describe contract validator in
// yggdrasil-core rejects the integration at runtime with `version_mismatch`
// or `action_catalog_mismatch`.
//
// This package is the public, importable home for the lint. Any adapter
// repo can:
//
//	import "github.com/dakasa-yggdrasil/integration-nfeio/pkg/contractcheck"
//
// and call LintDescribeContract() from a unit test or from a small cmd/lint
// binary. The package defines its own minimal types (DescribeResponse,
// ResourceType, ActionDefinition, ExecutionSpec) mirroring the JSON shape
// of the adapter describe contract, so consumers do NOT need to depend on
// any internal/protocol package of integration-template.
//
// Adapter implementations typically already have their own Go types for
// describe; they can either convert to these or construct a
// DescribeResponse on the fly from the same source-of-truth used to build
// their own Describe() response. The types here intentionally mirror the
// JSON wire format so callers can also unmarshal raw JSON into them.
package contractcheck

import (
	"fmt"
	"sort"
	"strings"
)

// DescribeResponse is the minimal projection of the adapter Describe()
// response needed for contract linting. Fields match the JSON wire format
// of yggdrasil-core's integration describe contract.
type DescribeResponse struct {
	ResourceTypes []ResourceType     `json:"resource_types"`
	ActionCatalog []ActionDefinition `json:"action_catalog,omitempty"`
	Execution     ExecutionSpec      `json:"execution"`
}

// ResourceType mirrors integration manifest resource_type entries.
type ResourceType struct {
	Name           string   `json:"name"`
	DefaultActions []string `json:"default_actions"`
}

// ActionDefinition mirrors integration manifest action_catalog entries.
type ActionDefinition struct {
	Name          string   `json:"name"`
	ResourceTypes []string `json:"resource_types,omitempty"`
}

// ExecutionSpec mirrors the subset of execution spec the lint inspects.
type ExecutionSpec struct {
	IdempotentActions []string `json:"idempotent_actions,omitempty"`
}

// LintDescribeContract cross-validates the adapter spec against the
// supportedOps slice. Returns nil when consistent, otherwise an error
// whose message is a sorted, human-readable diff suitable for surfacing
// in test failures or CI logs.
//
// Validations performed:
//  1. Every operation in supportedOps appears in spec.ActionCatalog.
//  2. Every spec.ActionCatalog entry name appears in supportedOps.
//  3. Every spec.ActionCatalog entry references at least one ResourceType.
//  4. Every spec.ActionCatalog ResourceType refers to a name that exists
//     in spec.ResourceTypes.
//  5. Every spec.ResourceTypes default_actions entry exists in supportedOps.
//  6. Every spec.Execution.IdempotentActions entry exists in supportedOps.
func LintDescribeContract(spec DescribeResponse, supportedOps []string) error {
	var issues []string

	supportedSet := newStringSet(supportedOps)
	catalogSet := make(map[string]ActionDefinition, len(spec.ActionCatalog))
	for _, action := range spec.ActionCatalog {
		catalogSet[action.Name] = action
	}
	resourceSet := make(map[string]struct{}, len(spec.ResourceTypes))
	for _, rt := range spec.ResourceTypes {
		resourceSet[rt.Name] = struct{}{}
	}

	// 1 — Every SupportedExecuteOperations entry must appear in action_catalog.
	for _, op := range supportedOps {
		if _, ok := catalogSet[op]; !ok {
			issues = append(issues, fmt.Sprintf("operation %q in SupportedExecuteOperations is missing from ActionCatalog", op))
		}
	}

	// 2 — Every action_catalog entry must appear in SupportedExecuteOperations.
	for name := range catalogSet {
		if _, ok := supportedSet[name]; !ok {
			issues = append(issues, fmt.Sprintf("action %q in ActionCatalog is missing from SupportedExecuteOperations", name))
		}
	}

	// 3 + 4 — Every action_catalog entry must declare resource_types, and
	// each must exist in spec.ResourceTypes.
	for _, action := range spec.ActionCatalog {
		if len(action.ResourceTypes) == 0 {
			issues = append(issues, fmt.Sprintf("action %q has no resource_types", action.Name))
			continue
		}
		for _, ref := range action.ResourceTypes {
			if _, ok := resourceSet[ref]; !ok {
				issues = append(issues, fmt.Sprintf("action %q references unknown resource_type %q", action.Name, ref))
			}
		}
	}

	// 5 — Every default_actions entry on a resource_type must be in supportedOps.
	for _, rt := range spec.ResourceTypes {
		for _, op := range rt.DefaultActions {
			if _, ok := supportedSet[op]; !ok {
				issues = append(issues, fmt.Sprintf("resource_type %q default_actions references unknown operation %q", rt.Name, op))
			}
		}
	}

	// 6 — Every idempotent_actions entry must be in supportedOps.
	for _, op := range spec.Execution.IdempotentActions {
		if _, ok := supportedSet[op]; !ok {
			issues = append(issues, fmt.Sprintf("idempotent_actions references unknown operation %q", op))
		}
	}

	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return fmt.Errorf("describe contract drift detected:\n  - %s", strings.Join(issues, "\n  - "))
}

func newStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
