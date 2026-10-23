// Command customer-identity-config prepares dormant Keycloak and Dex documents.
// It does not contact services, create users, write credentials or activate login.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "customer identity configuration refused") // No input or credential values in diagnostics.
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("customer-identity-config", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	prepare := flags.Bool("prepare", false, "prepare dormant source configuration, not a deployment")
	inputs := customeridentity.Endpoints{}
	flags.StringVar(&inputs.DexIssuer, "dex-issuer", "", "approved customer Dex HTTPS origin")
	flags.StringVar(&inputs.KeycloakIssuer, "keycloak-issuer", "", "approved customer Keycloak HTTPS realm")
	flags.StringVar(&inputs.PortalCallback, "portal-callback", "", "approved portal HTTPS callback")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("invalid preparation arguments")
	}
	data, err := customeridentity.Prepare(*prepare, inputs)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	data = append(data, '\n')
	written, err := output.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
