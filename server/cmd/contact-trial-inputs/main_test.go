package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestClosedArguments(t *testing.T) {
	valid := []string{"--repository", "/fictional/source", "--plan", strings.Repeat("a", 40), "--baseline", strings.Repeat("b", 40), "--candidate", strings.Repeat("c", 40), "--output", "/fictional/private"}
	options, err := arguments(valid)
	if err != nil || options["--repository"] != "/fictional/source" || options["--candidate"] != strings.Repeat("c", 40) {
		t.Fatal("complete explicit arguments were refused")
	}
	for _, scenario := range []string{"missing", "extra", "duplicate", "empty", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			args := append([]string{}, valid...)
			switch scenario {
			case "missing":
				args = args[:8]
			case "extra":
				args = append(args, "unexpected")
			case "duplicate":
				args[8] = "--candidate"
			case "empty":
				args[1] = ""
			case "unknown":
				args[8] = "--execute"
			}
			var output bytes.Buffer
			if err := run(context.Background(), args, &output); err == nil || output.Len() != 0 {
				t.Fatal("invalid arguments were admitted or emitted a receipt")
			}
		})
	}
}
