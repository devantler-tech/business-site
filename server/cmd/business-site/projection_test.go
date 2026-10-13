package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
)

// projectionCommand configures the real production-profile command with fictional bytes.
func projectionCommand(t *testing.T) ([]string, string) {
	t.Helper()
	public := productionArtifactDir(t)
	mount := t.TempDir()
	if err := os.Mkdir(filepath.Join(mount, "..timestamp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "..timestamp", "disclosure"), []byte("Example & Partners Road 1\n1234 Exampletown"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(mount, "..timestamp", "disclosure"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(filepath.Join(mount, "..timestamp", "disclosure"), -1, os.Getegid()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..timestamp", filepath.Join(mount, "..data")); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(mount, "disclosure")
	if err := os.Symlink("..data/disclosure", input); err != nil {
		t.Fatal(err)
	}
	return []string{"--public", public, "--company-disclosure-projection", input, "--check-serving-public"}, input
}

// TestProductionDisclosureProjection catches a missing transport or a bypass of the private reader.
func TestProductionDisclosureProjection(t *testing.T) {
	args, input := projectionCommand(t)
	if _, err := runtimeapp.ReadCompanyDisclosure(input); err == nil {
		t.Fatal("ordinary private-file checks accepted group-readable input")
	}
	var output strings.Builder
	listen := func(string, string) (net.Listener, error) {
		t.Fatal("projection validation opened a service")
		return nil, nil
	}
	if err := runWithListen(context.Background(), args, &output, listen); err != nil {
		t.Fatalf("complete explicit projection mode was refused: %v", err)
	}
	receipt := readPublicReceipt(t, output.String())
	if receipt["PublicProfile"] != "production-template" || receipt["PublicDigest"] != publicDigest || receipt["EffectivePublicDigest"] == publicDigest || len(receipt["EffectivePublicDigest"]) != 64 {
		t.Fatal("projection did not produce a bound effective serving snapshot")
	}
	if strings.Contains(output.String(), "Example") || strings.Contains(output.String(), input) {
		t.Fatal("projection receipt disclosed private input")
	}
	entries, err := os.ReadDir(filepath.Dir(input))
	if err != nil || len(entries) != 3 || entries[0].Name() != "..data" || entries[1].Name() != "..timestamp" || entries[2].Name() != "disclosure" {
		t.Fatal("projection changed the input directory")
	}
	contents, err := os.ReadFile(input)
	if err != nil || string(contents) != "Example & Partners Road 1\n1234 Exampletown" {
		t.Fatal("projection changed the source bytes")
	}
	info, err := os.Stat(input)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("projection rewrote the source permissions")
	}
}

// TestProductionProjectionRefusals catches unsafe filesystem or ambiguous startup modes.
func TestProductionProjectionRefusals(t *testing.T) {
	for _, scenario := range []string{"world-readable", "group-writable", "executable", "empty", "oversized", "escape", "fifo", "unsupported-scratch", "mixed", "missing-projection", "duplicate", "default-profile", "version", "check-public"} {
		t.Run(scenario, func(t *testing.T) {
			args, input := projectionCommand(t)
			var err error
			switch scenario {
			case "world-readable":
				err = os.Chmod(input, 0644)
			case "group-writable":
				err = os.Chmod(input, 0660)
			case "executable":
				err = os.Chmod(input, 0740)
			case "empty":
				err = os.WriteFile(input, nil, 0640)
			case "oversized":
				err = os.WriteFile(input, []byte(strings.Repeat("x", 16385)), 0640)
			case "escape", "fifo":
				if err = os.Remove(input); err == nil {
					if scenario == "escape" {
						external := filepath.Join(t.TempDir(), "external")
						if err = os.WriteFile(external, []byte("Example Road 1\n1234 Exampletown"), 0600); err == nil {
							err = os.Symlink(external, input)
						}
					} else {
						err = syscall.Mkfifo(input, 0600)
					}
				}
			case "unsupported-scratch":
				args = append(args, "--company-disclosure-scratch", filepath.Dir(input))
			case "mixed":
				args = append(args, "--company-disclosure-file", input)
			case "missing-projection":
				args[3] = ""
			case "duplicate":
				args = append(args, "--company-disclosure-projection", input)
			case "default-profile":
				publicProfile = "default"
			case "version", "check-public":
				args = append(args, "--"+scenario)
			}
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := runWithListen(context.Background(), args, &output, func(string, string) (net.Listener, error) {
				t.Fatal("unsafe projection opened a service")
				return nil, nil
			}); err == nil || strings.Contains(err.Error(), input) || output.Len() != 0 {
				t.Fatal("unsafe projection was accepted or disclosed")
			}
		})
	}
}
