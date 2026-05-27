// Command validate-templates exits non-zero on any schema error in any
// *.yaml under the supplied <dir>. Runs in CI before `go test ./...` so
// template drift fails the pipeline immediately.
//
//	usage: validate-templates <dir>
package main

import (
	"fmt"
	"os"

	ad "github.com/dakasa-yggdrasil/integration-nfeio/providers/nfeio/adapter"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: validate-templates <dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	templates, err := ad.LoadTemplatesDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate-templates: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("validate-templates: %d templates OK\n", len(templates))
}
