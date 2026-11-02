// Command customer-identity-materialize prepares private native phase inputs.
// It never executes a service, contacts an identity provider or activates login.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// main keeps ordinary execution inert and admits only the explicit private phase command.
func main() {
	if len(os.Args) == 1 {
		return
	}
	args := os.Args[1:]
	if len(args) != 6 || args[0] != "--phase" || args[2] != "--inputs" || args[4] != "--output" || strings.HasPrefix(args[1], "--") || strings.HasPrefix(args[3], "--") || strings.HasPrefix(args[5], "--") {
		refuse()
	}
	if err := customeridentity.Materialize(args[1], args[3], args[5]); err != nil {
		refuse()
	}
}

// refuse reports no filesystem path, credential, account or configuration value.
func refuse() {
	_, _ = fmt.Fprintln(os.Stderr, "customer identity private material refused")
	os.Exit(1)
}
