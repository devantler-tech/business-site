package customeridentity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializePackagedRuntime exercises the distributed entrypoint, not a helper binary.
func TestMaterializePackagedRuntime(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "business-site")
	identity := "-X main.revision=" + strings.Repeat("a", 40) + " -X main.publicDigest=" + strings.Repeat("b", 64) + " -X main.buildMode=release -X main.publicProfile=production-template"
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", identity, "-o", binary, "../../cmd/business-site")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("runtime fixture build failed: %v %s", err, data)
	}
	for _, phase := range []string{"init", "setup", "serve"} {
		t.Run(phase, func(t *testing.T) {
			input, output := materialFixture(t, phase)
			args := []string{"--identity-materialize", "--identity-phase", phase, "--identity-inputs", input, "--identity-output", output}
			data, err := exec.Command(binary, args...).CombinedOutput()
			if err != nil || string(data) != "customer identity phase prepared\n" {
				t.Fatal("packaged runtime cannot prepare the native phase without starting public services")
			}
			materialAssertOutput(t, output, phase)
			// The command delegates to the same maintained native producer exactly.
			comparison := filepath.Join(filepath.Dir(output), "comparison")
			if err := Materialize(phase, input, comparison); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(comparison)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				want, wantErr := os.ReadFile(filepath.Join(comparison, entry.Name()))
				got, gotErr := os.ReadFile(filepath.Join(output, entry.Name()))
				if wantErr != nil || gotErr != nil || string(want) != string(got) {
					t.Fatal("packaged native output differs from the maintained producer")
				}
			}
		})
	}
	t.Run("refusals", func(t *testing.T) {
		input, output := materialFixture(t, "serve")
		base := []string{"--identity-materialize", "--identity-phase", "serve", "--identity-inputs", input, "--identity-output", output}
		var cases [][]string
		for _, extra := range [][]string{
			{"--public", "/private/not-public"}, {"--listen", "127.0.0.1:0"}, {"--check-public=false"},
			{"--version=false"}, {"--company-disclosure-file", "/private/disclosure"},
			{"--company-disclosure-projection", "/private/disclosure"}, {"--check-serving-public=false"},
			{"--migrate=false"}, {"--migration-database-file", "/private/database"},
			{"--identity-materialize"}, {"--identity-phase", "setup"},
			{"--identity-inputs", input}, {"--identity-output", output},
		} {
			cases = append(cases, append(append([]string{}, base...), extra...))
		}
		for _, span := range [][2]int{{0, 1}, {1, 3}, {3, 5}, {5, 7}} {
			args := append([]string{}, base[:span[0]]...)
			cases = append(cases, append(args, base[span[1]:]...))
		}
		cases = append(cases,
			[]string{"--identity-materialize=false", "--identity-phase", "serve", "--identity-inputs", input, "--identity-output", output},
			[]string{"--identity-materialize", "--identity-phase", "unknown", "--identity-inputs", input, "--identity-output", output},
			[]string{"--identity-materialize", "--identity-phase", "serve", "--identity-inputs", "/private/missing", "--identity-output", output},
		)
		for index, args := range cases {
			data, err := exec.Command(binary, args...).CombinedOutput()
			if err == nil || string(data) != "customer identity phase preparation refused\n" {
				t.Fatalf("refusal %d accepted mixed/incomplete authority or reflected private input", index)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("refusal %d reserved a usable output", index)
			}
		}
		// Real symlinked secret projections still require their own future adapter.
		outside := filepath.Join(filepath.Dir(input), "actual-password")
		if err := os.Rename(filepath.Join(input, "database-password"), outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(input, "database-password")); err != nil {
			t.Fatal(err)
		}
		if data, err := exec.Command(binary, base...).CombinedOutput(); err == nil || string(data) != "customer identity phase preparation refused\n" {
			t.Fatal("packaged command widened the owner-only secret transport")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("symlink refusal reserved a usable output")
		}
	})
}
