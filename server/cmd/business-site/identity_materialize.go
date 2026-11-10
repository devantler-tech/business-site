package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

var errIdentityPhase = errors.New("customer identity phase preparation refused")

// identityPhaseOptions selects only private file preparation, never service startup.
type identityPhaseOptions struct {
	enabled                           bool
	phase, inputs, projection, output string
}

// registerIdentityPhaseOptions keeps private phase preparation inside the packaged executable.
func registerIdentityPhaseOptions(options *flag.FlagSet) *identityPhaseOptions {
	command := &identityPhaseOptions{}
	options.BoolVar(&command.enabled, "identity-materialize", false, "prepare one private native identity phase without starting services")
	options.StringVar(&command.phase, "identity-phase", "", "explicit init, setup, setup-mail, serve or dex-serve preparation")
	options.StringVar(&command.inputs, "identity-inputs", "", "private owner-only phase input directory")
	options.StringVar(&command.projection, "identity-projection", "", "confined atomic secret projection for one phase")
	options.StringVar(&command.output, "identity-output", "", "new private phase output directory")
	return command
}

// selected recognizes even incomplete or explicitly false requests so they cannot serve HTTP.
func (command *identityPhaseOptions) selected(seen map[string]int) bool {
	return seen["identity-projection"] != 0 || seen["identity-materialize"] != 0 || seen["identity-phase"] != 0 || seen["identity-inputs"] != 0 || seen["identity-output"] != 0
}

// run rejects mixed authority before any file access, then uses the maintained native producer.
func (command *identityPhaseOptions) run(ctx context.Context, options *flag.FlagSet, seen map[string]int, output io.Writer) error {
	ownerInput := seen["identity-inputs"] == 1 && seen["identity-projection"] == 0 && command.inputs != ""
	projectedInput := seen["identity-projection"] == 1 && seen["identity-inputs"] == 0 && command.projection != ""
	allowed := command.enabled && (ownerInput || projectedInput) && command.output != "" && validPackageIdentity() && ctx.Err() == nil
	for _, name := range []string{"identity-materialize", "identity-phase", "identity-output"} {
		if seen[name] != 1 {
			allowed = false
		}
	}
	options.Visit(func(option *flag.Flag) {
		switch option.Name {
		case "identity-materialize", "identity-phase", "identity-inputs", "identity-projection", "identity-output":
		default:
			allowed = false
		}
	})
	if !allowed {
		return errIdentityPhase
	}
	prepare := customeridentity.Materialize
	input := command.inputs
	if seen["identity-projection"] == 1 {
		prepare = customeridentity.MaterializeProjection
		input = command.projection
	}
	if prepare(command.phase, input, command.output) != nil || ctx.Err() != nil {
		return errIdentityPhase
	}
	_, err := fmt.Fprintln(output, "customer identity phase prepared")
	return err
}
