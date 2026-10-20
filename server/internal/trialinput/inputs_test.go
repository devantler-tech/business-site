package trialinput

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These fixtures exercise actual Git history, never a command-success mock.
func fixture(t *testing.T) (string, policy, Inputs) {
	t.Helper()
	dir := t.TempDir()
	gitFixture(t, dir, "init", "-q")
	gitFixture(t, dir, "config", "user.name", "Fictional Trial")
	gitFixture(t, dir, "config", "user.email", "trial@example.invalid")
	files := map[string]string{
		"plan.md": "frozen plan\n", "amendment.md": "frozen amendment\n",
		"lock": "fixed dependencies\n", "authority.go": "unchanged authority\n",
		"server/internal/identity/web/portal.html":   "Appearance\n",
		"server/internal/identity/web/contact.html":  "Name\n",
		"web/src/lib/private-contact/Contact.svelte": "Name\n",
	}
	for name, value := range files {
		writeFixture(t, dir, name, value)
	}
	gitFixture(t, dir, "add", "--", ".")
	gitFixture(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "fictional shared input")
	shared := gitFixture(t, dir, "rev-parse", "HEAD")
	p := policy{plan: shared, amendment: shared, shared: shared,
		pins:      map[string]string{"lock": digest([]byte("fixed dependencies\n"))},
		documents: []document{{shared, "plan.md", digest([]byte("frozen plan\n"))}, {shared, "amendment.md", digest([]byte("frozen amendment\n"))}}}
	return dir, p, Inputs{Plan: shared, Baseline: shared, Candidate: shared}
}

func TestPartialReadAndOverflowAreRefused(t *testing.T) {
	dir, p, input := fixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil || strings.ContainsAny(realGit, "'\n") {
		t.Fatal("cannot locate fixture Git")
	}
	bin := t.TempDir()
	// Only the external read fails: it emits a plausible complete commit kind
	// before exiting nonzero. None of those bytes may become evidence.
	script := "#!/bin/sh\ncase \"$*\" in *cat-file*) printf 'commit\\n'; exit 23;; esac\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := check(t.Context(), dir, input, p); err == nil {
		t.Fatal("plausible output from failed Git read was admitted")
	}
	output := &boundedOutput{}
	// Hide Reader.WriteTo so io.Copy exercises the actual destination's path.
	reader := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 2*1024*1024+1))}
	if _, err := io.Copy(output, reader); err == nil {
		t.Fatal("output limit was bypassed")
	}
}

func TestRepositoryRootRequired(t *testing.T) {
	dir, p, input := fixture(t)
	gitFixture(t, dir, "config", "diff.relative", "true")
	if _, err := check(t.Context(), filepath.Join(dir, "server"), input, p); err == nil {
		t.Fatal("subdirectory could narrow the complete repository observation")
	}
}

func TestReplacementAndAmbientGitControlsDoNotChangeObjects(t *testing.T) {
	dir, p, input := fixture(t)
	wantTree := gitFixture(t, dir, "rev-parse", "HEAD^{tree}")
	writeFixture(t, dir, "lock", "replacement dependencies\n")
	commitFixture(t, dir)
	replacement := gitFixture(t, dir, "rev-parse", "HEAD")
	gitFixture(t, dir, "replace", input.Baseline, replacement)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	receipt, err := check(t.Context(), dir, input, p)
	if err != nil || receipt.Baseline.Tree != wantTree || receipt.Candidate.Tree != wantTree {
		t.Fatal("replacement or ambient control changed the frozen observation")
	}
}

func TestConfiguredCleanFilterIsNeverExecuted(t *testing.T) {
	dir, p, input := fixture(t)
	writeFixture(t, dir, ".gitattributes", "lock filter=probe\n")
	commitFixture(t, dir)
	p.shared = gitFixture(t, dir, "rev-parse", "HEAD")
	input.Baseline, input.Candidate = p.shared, p.shared
	probe := t.TempDir()
	marker := filepath.Join(probe, "executed")
	script := filepath.Join(probe, "filter")
	if strings.ContainsAny(marker, "'\n") {
		t.Fatal("unsafe fictional fixture path")
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\nexec /bin/cat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "config", "filter.probe.clean", script)
	if err := os.Chtimes(filepath.Join(dir, "lock"), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	_, err := check(t.Context(), dir, input, p)
	if err == nil {
		t.Error("executable repository filter was admitted")
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatal("source admission executed a configured filter")
	}
}

func TestCleanImmutableInputs(t *testing.T) {
	dir, p, input := fixture(t)
	writeFixture(t, dir, "server/internal/identity/web/portal.html", "Colour theme\n")
	commitFixture(t, dir)
	input.Baseline = gitFixture(t, dir, "rev-parse", "HEAD")
	writeFixture(t, dir, "web/src/lib/private-contact/Contact.svelte", "Name with hint\n")
	commitFixture(t, dir)
	input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
	receipt, err := check(t.Context(), dir, input, p)
	if err != nil {
		t.Fatal("clean frozen inputs were refused", err)
	}
	if receipt.Scope != "source-inputs-only" || receipt.Baseline.Commit != input.Baseline || receipt.Candidate.Commit != input.Candidate || receipt.SharedAuthority != p.shared {
		t.Fatal("receipt lost its scope or immutable identities")
	}
	if receipt.Baseline.Tree != gitFixture(t, dir, "rev-parse", input.Baseline+"^{tree}") || receipt.Candidate.Tree != gitFixture(t, dir, "rev-parse", input.Candidate+"^{tree}") {
		t.Fatal("receipt did not bind the actual trees")
	}
	if strings.Join(receipt.Baseline.Changed, ",") != "server/internal/identity/web/portal.html" || len(receipt.Candidate.Changed) != 2 || receipt.Dependencies["lock"] != digest([]byte("fixed dependencies\n")) {
		t.Fatal("receipt did not retain complete changes and dependency identity")
	}
}

func TestRejectUnsuitableSource(t *testing.T) {
	for _, scenario := range []string{"dirty-tracked", "dirty-untracked", "assumed-clean", "skipped-worktree", "short-id", "missing-object", "blob-not-commit", "wrong-plan", "wrong-plan-content", "wrong-amendment-content", "wrong-shared-history", "dependency", "authority", "renamed-authority", "deleted-presentation", "symlink-presentation", "baseline-candidate-only-path", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			dir, p, input := fixture(t)
			ctx := t.Context()
			switch scenario {
			case "dirty-tracked":
				writeFixture(t, dir, "lock", "changed\n")
			case "dirty-untracked":
				writeFixture(t, dir, "unexpected", "untracked\n")
			case "assumed-clean", "skipped-worktree":
				option := map[string]string{"assumed-clean": "--assume-unchanged", "skipped-worktree": "--skip-worktree"}[scenario]
				gitFixture(t, dir, "update-index", option, "lock")
				writeFixture(t, dir, "lock", "hidden dirty input\n")
			case "short-id":
				input.Candidate = input.Candidate[:12]
			case "missing-object":
				input.Candidate = strings.Repeat("0", 40)
			case "blob-not-commit":
				input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD:lock")
			case "wrong-plan":
				input.Plan = strings.Repeat("1", 40)
			case "wrong-plan-content":
				p.documents[0].digest = strings.Repeat("0", 64)
			case "wrong-amendment-content":
				p.documents[1].digest = strings.Repeat("0", 64)
			case "wrong-shared-history":
				gitFixture(t, dir, "checkout", "--orphan", "unrelated")
				commitFixture(t, dir)
				input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
			case "dependency", "authority":
				name := map[string]string{"dependency": "lock", "authority": "authority.go"}[scenario]
				writeFixture(t, dir, name, "changed\n")
				commitFixture(t, dir)
				input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
			case "renamed-authority":
				gitFixture(t, dir, "mv", "authority.go", "server/internal/identity/web/contact.html.new")
				commitFixture(t, dir)
				input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
			case "deleted-presentation", "symlink-presentation":
				path := filepath.Join(dir, "server/internal/identity/web/contact.html")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "symlink-presentation" {
					if err := os.Symlink("../../../../authority.go", path); err != nil {
						t.Fatal(err)
					}
				}
				commitFixture(t, dir)
				input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
			case "baseline-candidate-only-path":
				writeFixture(t, dir, "web/src/lib/private-contact/Contact.svelte", "candidate body\n")
				commitFixture(t, dir)
				input.Baseline = gitFixture(t, dir, "rev-parse", "HEAD")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := check(ctx, dir, input, p); err == nil {
				t.Fatal("unsuitable input was admitted")
			}
		})
	}
}

func commitFixture(t *testing.T, dir string) {
	t.Helper()
	gitFixture(t, dir, "add", "--", ".")
	gitFixture(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "fictional maintenance task")
}

func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("fictional git operation failed", err)
	}
	return strings.TrimSpace(string(data))
}

func writeFixture(t *testing.T, dir, name, value string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
