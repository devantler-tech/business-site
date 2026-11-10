package customeridentity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// materialProjectionFixture uses the native atomic-writer link layout with fictional values.
func materialProjectionFixture(t *testing.T, phase string) (string, string) {
	t.Helper()
	input, output := materialFixture(t, phase)
	generation := "..2026_11_03_12_00_00.123456789"
	target := filepath.Join(input, generation)
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == generation {
			continue
		}
		name := entry.Name()
		if err := os.Rename(filepath.Join(input, name), filepath.Join(target, name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(target, name), 0440); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("..data/"+name, filepath.Join(input, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(generation, filepath.Join(input, "..data")); err != nil {
		t.Fatal(err)
	}
	return input, output
}

// TestMaterializeProjectionPackagedRuntime checks the actual distributed command for all phases.
func TestMaterializeProjectionPackagedRuntime(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "business-site")
	identity := "-X main.revision=" + strings.Repeat("a", 40) + " -X main.publicDigest=" + strings.Repeat("b", 64) + " -X main.buildMode=release -X main.publicProfile=production-template"
	if data, err := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", identity, "-o", binary, "../../cmd/business-site").CombinedOutput(); err != nil {
		t.Fatalf("runtime build failed: %v %s", err, data)
	}
	for _, phase := range []string{"init", "setup", "setup-mail", "serve", dexServePhase} {
		t.Run(phase, func(t *testing.T) {
			input, output := materialProjectionFixture(t, phase)
			args := []string{"--identity-materialize", "--identity-phase", phase, "--identity-projection", input, "--identity-output", output}
			if data, err := exec.Command(binary, args...).CombinedOutput(); err != nil || string(data) != "customer identity phase prepared\n" {
				t.Fatal("packaged runtime refused coherent projected phase")
			}
			if phase == dexServePhase {
				dexAssertOutput(t, input, output)
			} else {
				materialAssertOutput(t, output, phase)
			}
			names, _, err := materialProfile(phase)
			if err != nil {
				t.Fatal(err)
			}
			ordinary := filepath.Join(filepath.Dir(output), "ordinary")
			comparison := filepath.Join(filepath.Dir(output), "comparison")
			if err := os.Mkdir(ordinary, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				data, e := os.ReadFile(filepath.Join(input, name))
				if e != nil {
					t.Fatal(e)
				}
				materialWrite(t, ordinary, name, data)
			}
			if err := Materialize(phase, ordinary, comparison); err != nil {
				t.Fatal(err)
			}
			materialCompareOutput(t, comparison, output)
		})
	}
	t.Run("mixed inputs", func(t *testing.T) {
		input, output := materialProjectionFixture(t, "serve")
		args := []string{"--identity-materialize", "--identity-phase", "serve", "--identity-projection", input, "--identity-inputs", input, "--identity-output", output}
		if data, err := exec.Command(binary, args...).CombinedOutput(); err == nil || string(data) != "customer identity phase preparation refused\n" {
			t.Fatal("mixed transports accepted or reflected private diagnostics")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("mixed transports reserved output")
		}
	})
}
