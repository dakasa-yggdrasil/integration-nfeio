// Command lint-action-catalog cross-validates the adapter's Describe()
// response against SupportedExecuteOperations using pkg/contractcheck.
//
// Catches drift between spec.go (Describe + SupportedExecuteOperations) and
// what Execute() actually handles. Runs in CI before `go test ./...` so any
// describe ↔ execute mismatch fails the pipeline immediately.
//
// Reactor (category=reactor) entries are excluded from the supportedOps
// comparison — they are triggered by the webhook server, not the RPC bus.
package main

import (
	"fmt"
	"os"

	ad "github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter"
	"github.com/dakasa-yggdrasil/integration-nfeio/pkg/contractcheck"
)

func main() {
	d := ad.Describe()

	// Project the adapter's describe response onto the minimal shape the
	// linter consumes. Reactor (category=reactor) entries are dropped from
	// both the action catalog AND the supportedOps slice the lint compares
	// against — they bypass the RPC execute path entirely.
	spec := contractcheck.DescribeResponse{
		ResourceTypes: make([]contractcheck.ResourceType, 0, len(d.ResourceTypes)),
		ActionCatalog: make([]contractcheck.ActionDefinition, 0, len(d.ActionCatalog)),
		Execution: contractcheck.ExecutionSpec{
			IdempotentActions: append([]string{}, d.Execution.IdempotentActions...),
		},
	}
	for _, rt := range d.ResourceTypes {
		spec.ResourceTypes = append(spec.ResourceTypes, contractcheck.ResourceType{
			Name:           rt.Name,
			DefaultActions: append([]string{}, rt.DefaultActions...),
		})
	}
	for _, a := range d.ActionCatalog {
		if a.Category == "reactor" {
			continue
		}
		spec.ActionCatalog = append(spec.ActionCatalog, contractcheck.ActionDefinition{
			Name:          a.Name,
			ResourceTypes: append([]string{}, a.ResourceTypes...),
		})
	}

	if err := contractcheck.LintDescribeContract(spec, ad.SupportedExecuteOperations); err != nil {
		fmt.Fprintf(os.Stderr, "lint-action-catalog: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("lint-action-catalog: OK")
}
