package trialinput

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGitFixtureWaitsForAutomaticMaintenance(t *testing.T) {
	dir, _, _ := fixture(t)
	// A repository may explicitly request detached work. Fixture commands must
	// still own their complete lifetime without disabling maintenance itself.
	for key, value := range map[string]string{
		"maintenance.autoDetach":           "true",
		"gc.autoDetach":                    "true",
		"maintenance.gc.enabled":           "false",
		"maintenance.commit-graph.enabled": "true",
		"maintenance.commit-graph.auto":    "-1",
	} {
		gitFixture(t, dir, "config", key, value)
	}
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	writeFixture(t, dir, "new-file", "fictional maintenance input\n")
	commitFixture(t, dir)
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal("cannot read real Git maintenance trace", err)
	}
	observed := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal("invalid real Git trace event", err)
		}
		if event.Event != "child_start" || !strings.Contains(strings.Join(event.Argv, " "), "maintenance run --auto") {
			continue
		}
		observed = true
		if !foregroundMaintenanceArgs(event.Argv) {
			t.Fatal("real Git fixture allowed detached automatic maintenance")
		}
	}
	if !observed {
		t.Fatal("real commit did not exercise automatic maintenance")
	}
	// A passing child invocation alone is not proof that maintenance did work.
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graphs", "commit-graph-chain")); err != nil {
		t.Fatal("forced real commit-graph maintenance did not finish", err)
	}
	gitFixture(t, dir, "commit-graph", "verify")
	for _, key := range []string{"maintenance.autoDetach", "gc.autoDetach"} {
		if gitFixture(t, dir, "config", "--bool", "--get", key) != "false" {
			t.Fatal("fixture did not override repository background-maintenance configuration")
		}
	}
}

func TestForegroundMaintenanceTraceArguments(t *testing.T) {
	for _, scenario := range []struct {
		name string
		args []string
		want bool
	}{
		{"explicit-foreground", []string{"git", "maintenance", "run", "--auto", "--no-detach"}, true},
		{"legacy-foreground", []string{"git", "maintenance", "run", "--auto", "--quiet"}, true},
		{"explicit-background", []string{"git", "maintenance", "run", "--auto", "--detach"}, false},
		{"conflicting-flags", []string{"git", "maintenance", "run", "--auto", "--no-detach", "--detach"}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if foregroundMaintenanceArgs(scenario.args) != scenario.want {
				t.Fatal("incorrect foreground maintenance trace classification")
			}
		})
	}
}

func foregroundMaintenanceArgs(args []string) bool {
	// Older Git traces omit both flags. This is only an argv check: the real
	// fixture must still prove effective settings and completed graph work.
	return !slices.Contains(args, "--detach")
}

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
		"server/internal/identity/browser.go":        "Theme: \"Appearance\"\nd.Theme = \"Udseende\"\nunchanged authority\n",
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

func TestFrozenSharedAppearanceCopy(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact-copy", true: "copy-with-authority-change"}[extra], func(t *testing.T) {
			dir, p, input := fixture(t)
			value := "Theme: \"Colour theme\"\nd.Theme = \"Farvetema\"\nunchanged authority\n"
			if extra {
				value += "changed authority\n"
			}
			writeFixture(t, dir, "server/internal/identity/browser.go", value)
			commitFixture(t, dir)
			input.Baseline = gitFixture(t, dir, "rev-parse", "HEAD")
			input.Candidate = input.Baseline
			receipt, err := check(t.Context(), dir, input, p)
			if extra {
				if err == nil {
					t.Fatal("copy change concealed an authority edit")
				}
			} else if err != nil || strings.Join(receipt.Baseline.Changed, ",") != "server/internal/identity/browser.go" {
				t.Fatal("the frozen native shared appearance-copy change was refused")
			}
		})
	}
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

func TestMissingPromisorObjectsAreNeverFetched(t *testing.T) {
	for _, missing := range []string{"commit", "blob", "packed-blob"} {
		t.Run(missing, func(t *testing.T) {
			dir, p, input := fixture(t)
			remote := t.TempDir()
			gitFixture(t, dir, "clone", "--quiet", "--no-hardlinks", "--", dir, remote)
			gitFixture(t, remote, "config", "user.name", "Fictional Trial")
			gitFixture(t, remote, "config", "user.email", "trial@example.invalid")
			gitFixture(t, remote, "config", "uploadpack.allowFilter", "true")
			gitFixture(t, remote, "config", "uploadpack.allowAnySHA1InWant", "true")
			var absent string
			if missing == "commit" {
				writeFixture(t, remote, "server/internal/identity/web/contact.html", "Name with hint\n")
				commitFixture(t, remote)
				input.Candidate = gitFixture(t, remote, "rev-parse", "HEAD")
				absent = input.Candidate
			} else {
				if missing == "packed-blob" {
					gitFixture(t, dir, "repack", "-ad")
				}
				absent = gitFixture(t, dir, "rev-parse", "HEAD:plan.md")
				omitFixtureObject(t, dir, absent)
			}
			gitFixture(t, remote, "cat-file", "-e", absent)
			gitFixture(t, dir, "config", "remote.promise.url", remote)
			gitFixture(t, dir, "config", "remote.promise.promisor", "true")
			gitFixture(t, dir, "config", "remote.promise.partialclonefilter", "blob:none")
			gitFixture(t, dir, "config", "extensions.partialClone", "promise")
			if _, err := check(t.Context(), dir, input, p); err == nil {
				t.Error("missing promisor input was fetched and admitted")
			}
			// Inspect without lazy fetching: refusal alone must not hide a fetch
			// that has already changed the supposedly read-only object store.
			probe := exec.CommandContext(t.Context(), "git", "--no-lazy-fetch", "-C", dir, "cat-file", "-e", absent)
			probe.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
			if err := probe.Run(); err == nil {
				t.Fatal("source admission fetched a missing promisor object")
			}
		})
	}
}

func omitFixtureObject(t *testing.T, dir, absent string) {
	t.Helper()
	objects := strings.Fields(gitFixture(t, dir, "rev-list", "--objects", "--all", "--no-object-names"))
	kept := make([]string, 0, len(objects))
	for _, object := range objects {
		if object != absent {
			kept = append(kept, object)
		}
	}
	if len(kept) != len(objects)-1 {
		t.Fatal("missing-object fixture did not select exactly one reachable object")
	}
	// Build a complete replacement store except for the selected object. Deleting
	// a loose-object filename cannot remove an object already stored in a pack.
	// Disable delta reuse so no retained object depends on the omitted object.
	replacement := filepath.Join(t.TempDir(), "objects")
	if err := os.MkdirAll(filepath.Join(replacement, "pack"), 0700); err != nil {
		t.Fatal(err)
	}
	gitFixtureInput(t, dir, strings.NewReader(strings.Join(kept, "\n")+"\n"),
		"pack-objects", "--window=0", "--no-reuse-delta", filepath.Join(replacement, "pack", "fixture"))
	store := filepath.Join(dir, ".git", "objects")
	if err := os.Rename(store, filepath.Join(t.TempDir(), "original-objects")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, store); err != nil {
		t.Fatal(err)
	}
}

func TestGitlinkDeltaCannotBeHiddenByLocalConfig(t *testing.T) {
	dir, p, input := fixture(t)
	if err := os.Mkdir(filepath.Join(dir, "unexpected"), 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+p.shared+",unexpected")
	gitFixture(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "fictional extra gitlink")
	input.Candidate = gitFixture(t, dir, "rev-parse", "HEAD")
	gitFixture(t, dir, "config", "diff.ignoreSubmodules", "all")
	if status := gitFixture(t, dir, "status", "--porcelain=v1", "--ignore-submodules=none"); status != "" {
		t.Fatal("gitlink fixture must be clean before source admission")
	}
	if _, err := check(t.Context(), dir, input, p); err == nil {
		t.Fatal("local diff configuration concealed an out-of-scope gitlink")
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
	return gitFixtureInput(t, dir, nil, args...)
}

func gitFixtureInput(t *testing.T, dir string, input io.Reader, args ...string) string {
	t.Helper()
	// Wait for maintenance as part of each real command, including commits and
	// clones. A detached descendant can otherwise race t.TempDir's cleanup.
	prefix := []string{"-c", "maintenance.autoDetach=false", "-c", "gc.autoDetach=false", "-C", dir}
	cmd := exec.CommandContext(t.Context(), "git", append(prefix, args...)...)
	cmd.Stdin = input
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
