// Command contact-trial-inputs checks only the frozen comparison's source inputs.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/business-site/server/internal/trialinput"
)

func arguments(args []string) (map[string]string, error) {
	if len(args) != 10 {
		return nil, errors.New("explicit source and output arguments required")
	}
	options := map[string]string{"--repository": "", "--plan": "", "--baseline": "", "--candidate": "", "--output": ""}
	for i := 0; i < len(args); i += 2 {
		value, known := options[args[i]]
		if !known || value != "" || args[i+1] == "" {
			return nil, errors.New("explicit source and output arguments required")
		}
		options[args[i]] = args[i+1]
	}
	return options, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	options, err := arguments(args)
	if err != nil {
		return err
	}
	receipt, err := trialinput.Check(ctx, options["--repository"], trialinput.Inputs{
		Plan: options["--plan"], Baseline: options["--baseline"], Candidate: options["--candidate"],
	})
	if err != nil {
		return err
	}
	if err := trialinput.WriteReceipt(options["--output"], receipt); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "contact-trial-inputs ready")
	return err
}

func execute() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		// Inspected source, paths and raw Git errors are not diagnostics.
		_, _ = fmt.Fprintln(os.Stderr, "contact-trial-inputs refused")
		return 1
	}
	return 0
}

func main() { os.Exit(execute()) }
