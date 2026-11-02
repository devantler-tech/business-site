package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the maintained builder's actual native-consumption block, not a copy.
func TestBuilderConsumesServiceZIPsBeforeNativeVerification(t *testing.T) {
	inputs, policy := restorationFixtureOptions(t, false, true)
	for _, fault := range []string{"complete", "changed-zip", "partial-native-result", "changed-source-sums", "failed-manifest-reader"} {
		t.Run(fault, func(t *testing.T) {
			output := filepath.Join(physicalGeneratorTestDirectory(t), "build")
			cache := filepath.Join(output, "go-input-native")
			if err := os.Mkdir(output, 0700); err != nil {
				t.Fatal(err)
			}
			receipt, err := restoreGoInputs(inputs, cache, policy)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(output, "retained-go-restoration.json"), marshal(t, receipt))
			retained := filepath.Join(t.TempDir(), "retained")
			if err := os.CopyFS(filepath.Join(retained, "source/selected-go-sources"), os.DirFS(inputs.Service)); err != nil {
				t.Fatal(err)
			}
			project := filepath.Join(t.TempDir(), "project")
			for _, name := range []string{"go.mod", "go.sum"} {
				body, err := os.ReadFile(filepath.Join(inputs.Service, "records", name))
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(project, name), body)
			}
			denied, requests := deniedChecksumServer(t)
			env := restoredNativeGoEnvironment(t, cache, policy, denied.URL)
			nativeGo, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			verify := func() ([]byte, error) {
				cmd := exec.CommandContext(t.Context(), nativeGo, "mod", "verify")
				cmd.Dir, cmd.Env = project, env
				return cmd.CombinedOutput()
			}
			if body, err := verify(); err == nil || !bytes.Contains(body, []byte("missing ziphash")) {
				t.Fatalf("original required-module failure was not reproduced: %v %s", err, body)
			}
			builder, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "build-zitadel-linux.sh"))
			if err != nil {
				t.Fatal(err)
			}
			_, block, found := strings.Cut(string(builder), "# BEGIN native service consumption\n")
			block, _, closed := strings.Cut(block, "# END native service consumption\n")
			if !found || !closed {
				t.Fatal("builder does not consume verified service ZIPs before native verification")
			}
			switch fault {
			case "changed-zip":
				writeFixture(t, filepath.Join(cache, "go-modules/cache/download/example.com/!example/@v/v1.2.3.zip"), []byte("changed archive"))
			case "partial-native-result":
				bin := nativeServiceCommandFixture(t, "go", "printf '{}\\n'\n")
				env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			case "failed-manifest-reader":
				bin := nativeServiceCommandFixture(t, "shasum", "printf '%s  fixture\\n' '"+receipt.ManifestSHA256+"'\nexit 42\n")
				env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			case "changed-source-sums":
				writeFixture(t, filepath.Join(project, "go.sum"), []byte("changed checksums\n"))
			}
			cmd := exec.CommandContext(t.Context(), "bash", "-c", "set -euo pipefail\nfail() { echo \"$1\" >&2; exit 1; }\noutput=$1 retained_inputs=$2\n"+block+"\nconsume_retained_service_go\n", "native-service", output, retained)
			cmd.Dir, cmd.Env = project, env
			body, runErr := cmd.CombinedOutput()
			result := filepath.Join(output, "retained-go-service-readback.json")
			if fault != "complete" {
				if runErr == nil {
					t.Fatal("incomplete native service consumption was accepted")
				}
				if _, err := os.Lstat(result); !os.IsNotExist(err) {
					t.Fatal("refused native consumption left a success receipt", err)
				}
				expected := map[string]string{"changed-zip": "native service module consumption failed", "partial-native-result": "native service module readback differs", "changed-source-sums": "frozen service module records changed", "failed-manifest-reader": "restored native module manifest read failed"}[fault]
				if !bytes.Contains(body, []byte(expected)) {
					t.Fatalf("native consumption refused for the wrong reason: %s", body)
				}
				return
			}
			if runErr != nil || requests.Load() != 0 {
				t.Fatalf("actual builder block did not consume the authenticated ZIPs locally: %v %s", runErr, body)
			}
			if body, err := verify(); err != nil {
				t.Fatalf("native verification still refused after consumption: %v %s", err, body)
			}
			body, err = os.ReadFile(result)
			var proof map[string]any
			if err != nil || json.Unmarshal(body, &proof) != nil || proof["serviceModules"] != float64(2) || proof["nativeCacheReadbackPassed"] != true || proof["sourceChecksumsUnchanged"] != true || proof["dependencyInputsCaptured"] != false || proof["nativeOfflineReplayProved"] != false {
				t.Fatal("complete native read has no scoped receipt", err, string(body))
			}
		})
	}
}

// nativeServiceCommandFixture writes private executable failure shims for the
// negative controls. These shims never supply positive native-build evidence.
func nativeServiceCommandFixture(t *testing.T, name, body string) string {
	t.Helper()
	bin := t.TempDir()
	file := filepath.Join(bin, name)
	writeFixture(t, file, []byte("#!/bin/sh\n"+body))
	if err := os.Chmod(file, 0700); err != nil {
		t.Fatal(err)
	}
	return bin
}
