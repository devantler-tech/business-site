// Command zitadel-bsr-sources retains and checks selected schema source packages.
// It does not acquire dependencies, execute retained inputs or activate services.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// operationInputs admits only the complete, distinct argument set for one operation.
func operationInputs(args []string) (string, map[string]string, error) {
	operations := map[string][]string{
		"capture":     {"build", "recipe", "source-sha256", "output"},
		"verify":      {"package", "sha256"},
		"restore":     {"package", "sha256", "output"},
		"check-cache": {"package", "sha256", "cache"},
	}
	names, known := operations[args[0]]
	if !known {
		return "", nil, fmt.Errorf("unknown explicit operation")
	}
	parser := flag.NewFlagSet(args[0], flag.ContinueOnError)
	parser.SetOutput(io.Discard)
	inputs := map[string]*string{}
	for _, name := range names {
		inputs[name] = parser.String(name, "", "explicit "+name)
	}
	seen := map[string]bool{}
	for _, arg := range args[1:] {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if seen[name] {
			return "", nil, fmt.Errorf("repeated option")
		}
		seen[name] = true
	}
	if err := parser.Parse(args[1:]); err != nil {
		return "", nil, err
	}
	if parser.NArg() != 0 {
		return "", nil, fmt.Errorf("unexpected positional arguments")
	}
	values := map[string]string{}
	for name, value := range inputs {
		if *value == "" {
			return "", nil, fmt.Errorf("missing required option: %s", name)
		}
		values[name] = *value
	}
	return args[0], values, nil
}

// run is inert until one complete explicit retention/readback operation is selected.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return nil
	}
	op, values, err := operationInputs(args)
	if err != nil {
		return err
	}
	var result receipt
	switch op {
	case "capture":
		result, err = capture(values["build"], values["recipe"], values["source-sha256"], values["output"], frozenModules, frozenLock)
	case "verify":
		result, err = verify(values["package"], values["sha256"], frozenModules, frozenLock)
	case "restore":
		result, err = restore(values["package"], values["sha256"], values["output"], frozenModules, frozenLock)
	case "check-cache":
		result, err = checkCache(values["package"], values["sha256"], values["cache"], frozenModules, frozenLock)
	}
	if err != nil {
		return err
	}
	if result.Modules != 3 || result.CacheFiles != 113 || result.ExportFiles != 49 || result.Files != 170 {
		return fmt.Errorf("frozen schema input cardinality differs")
	}
	return writeReceipt(stdout, result)
}

// main emits no success receipt after an incomplete operation or output failure.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
