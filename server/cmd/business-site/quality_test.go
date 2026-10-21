package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual optional launcher with synthetic tool metadata, not a
// contributor's installed linter or configuration. CI's ordinary Go suite runs it.
func TestCompleteQualityCheckRequiresPinnedTools(t *testing.T) {
	const goVersion = "go version go1.27.3 linux/amd64"
	const lintVersion = "golangci-lint has version 2.14.0 built with go1.27.3 from fixture"
	for _, tc := range []struct {
		name, goVersion, lintVersion         string
		goExit, lintVersionExit, lintRunExit string
		args                                 []string
		wantRun, wantError                   bool
	}{
		{name: "pinned", goVersion: goVersion, lintVersion: lintVersion, wantRun: true},
		{name: "different Go", goVersion: "go version go1.27.1 linux/amd64", lintVersion: lintVersion, wantError: true},
		{name: "unpatched Go", goVersion: "go version go1.27.2 linux/amd64", lintVersion: lintVersion, wantError: true},
		{name: "different linter", goVersion: goVersion, lintVersion: "golangci-lint has version 2.13.0 built with go1.27.3 from fixture", wantError: true},
		{name: "different linter compiler", goVersion: goVersion, lintVersion: "golangci-lint has version 2.14.0 built with go1.27.1 from fixture", wantError: true},
		{name: "unpatched linter compiler", goVersion: goVersion, lintVersion: "golangci-lint has version 2.14.0 built with go1.27.2 from fixture", wantError: true},
		{name: "missing Go metadata", lintVersion: lintVersion, wantError: true},
		{name: "malformed linter metadata", goVersion: goVersion, lintVersion: "2.14.0", wantError: true},
		{name: "failed Go query", goVersion: goVersion, lintVersion: lintVersion, goExit: "2", wantError: true},
		{name: "failed linter query", goVersion: goVersion, lintVersion: lintVersion, lintVersionExit: "2", wantError: true},
		{name: "linter failure", goVersion: goVersion, lintVersion: lintVersion, lintRunExit: "37", wantRun: true, wantError: true},
		{name: "unexpected arguments", goVersion: goVersion, lintVersion: lintVersion, args: []string{"--disable-all"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			log := filepath.Join(bin, "run.log")
			tools := map[string]string{
				"go":            "#!/usr/bin/env bash\n[[ \"$*\" == version && \"${GOTOOLCHAIN:-}\" == local ]] || exit 90\nprintf '%s\\n' \"$QUALITY_FIXTURE_GO_VERSION\"\nexit \"${QUALITY_FIXTURE_GO_EXIT:-0}\"\n",
				"golangci-lint": "#!/usr/bin/env bash\nif [[ \"$*\" == version ]]; then\n printf '%s\\n' \"$QUALITY_FIXTURE_LINT_VERSION\"\n exit \"${QUALITY_FIXTURE_LINT_VERSION_EXIT:-0}\"\nfi\nprintf '%s\\n' \"$@\" > \"$QUALITY_FIXTURE_LOG\"\nexit \"${QUALITY_FIXTURE_LINT_RUN_EXIT:-0}\"\n",
			}
			for name, body := range tools {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{filepath.Join("..", "..", "..", "scripts", "check-portal-quality.sh")}, tc.args...)
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"QUALITY_FIXTURE_GO_VERSION="+tc.goVersion,
				"QUALITY_FIXTURE_LINT_VERSION="+tc.lintVersion,
				"QUALITY_FIXTURE_GO_EXIT="+tc.goExit,
				"QUALITY_FIXTURE_LINT_VERSION_EXIT="+tc.lintVersionExit,
				"QUALITY_FIXTURE_LINT_RUN_EXIT="+tc.lintRunExit,
				"QUALITY_FIXTURE_LOG="+log)
			output, err := cmd.CombinedOutput()
			runErr := err
			if (err != nil) != tc.wantError {
				t.Fatalf("quality admission error=%v, wantError=%v; output=%s", err, tc.wantError, output)
			}
			data, err := os.ReadFile(log)
			if tc.wantRun {
				if err != nil || string(data) != "run\n--no-config\n--max-same-issues=0\n--max-issues-per-linter=0\n--uniq-by-line=false\n./...\n" {
					t.Fatal("complete quality invocation changed or ambient configuration was admitted")
				}
				if tc.lintRunExit != "" {
					var exit *exec.ExitError
					if !errors.As(runErr, &exit) || exit.ExitCode() != 37 {
						t.Fatal("linter failure was not preserved")
					}
				}
			} else if err == nil || !errors.Is(err, os.ErrNotExist) {
				t.Fatal("linter executed despite invalid tool metadata or arguments")
			}
			if strings.Contains(string(output), "QUALITY_FIXTURE") {
				t.Fatal("fixture metadata leaked into diagnostics")
			}
		})
	}
}
