package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// The reviewed catalogue, not a CLI input, chooses the admitted service graph.
const frozenCatalogue = "dfdff48e6a821277bfc9fa1b2a2133e316dfc8850c6f4fc13cdbf97625c17047"

// run is inert without arguments and admits exactly one explicit operation.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "observe-generators" {
		return runGeneratorObservation(args[1:], stdout)
	}
	if args[0] == "verify-generator-checkpoint" {
		return runGeneratorVerification(args[1:], stdout)
	}
	if args[0] == "capture-generator-sources" || args[0] == "verify-generator-sources" {
		return runGeneratorSources(args[0], args[1:], stdout)
	}
	flags := flag.NewFlagSet("zitadel-module-sources", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	captureMode := flags.Bool("capture", false, "capture selected sources from a completed build")
	build := flags.String("build", "", "explicit completed build root")
	cataloguePath := flags.String("catalogue", "", "reviewed module catalogue")
	output := flags.String("output", "", "fresh retained package directory")
	verifyPath := flags.String("verify", "", "retained package to check without execution")
	seal := flags.String("sha256", "", "externally supplied manifest SHA-256")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	var result receipt
	var err error
	switch {
	case *captureMode && *build != "" && *cataloguePath != "" && *output != "" && *verifyPath == "" && *seal == "":
		result, err = capture(*build, *cataloguePath, *output, frozenCatalogue)
	case !*captureMode && *build == "" && *cataloguePath == "" && *output == "" && *verifyPath != "" && *seal != "":
		result, err = verify(*verifyPath, *seal, frozenCatalogue)
	default:
		return fmt.Errorf("one complete explicit capture or verification operation is required")
	}
	if err != nil {
		return err
	}
	if result.Modules != 231 {
		return fmt.Errorf("frozen service selection must contain 231 modules")
	}
	return json.NewEncoder(stdout).Encode(result)
}

// main emits no successful receipt when capture, verification or output fails.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
