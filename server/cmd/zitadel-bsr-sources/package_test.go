package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fixturePackage drives real filesystem capture without acquiring or executing a tool.
func fixturePackage(t *testing.T) (string, string, moduleProfile, string, receipt) {
	t.Helper()
	build := t.TempDir()
	p, files := fixtureModule()
	put := func(name string, body []byte, mode os.FileMode) {
		t.Helper()
		full := filepath.Join(build, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, mode); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range files {
		var name string
		switch {
		case rel == "commit.json":
			name = "buf-cache/v3/commits/shake256/buf.build/" + p.Commit + ".json"
		case rel == "acquisition.json":
			name = "bsr-records/" + p.Commit + ".json"
		case strings.HasPrefix(rel, "export/"):
			name = "bsr-exports/" + p.Owner + "/" + p.Module + "/" + strings.TrimPrefix(rel, "export/")
		default:
			format, tail, _ := strings.Cut(rel, "/")
			name = "buf-cache/v3/modules/" + format + "/buf.build/" + p.Owner + "/" + p.Module + "/" + p.Commit + "/" + tail
		}
		put(name, body, 0o600)
	}
	source := []byte("opaque prepared-source fixture\n")
	lock := []byte("frozen lock fixture\n")
	put("source-snapshot/source-manifest.json", source, 0o600)
	put("prepared/source/proto/buf.lock", lock, 0o600)
	put("recipe.sh", []byte("#!/bin/bash\n"), 0o700)
	for index, phase := range []string{"api", "console"} {
		put("bsr-tools/"+phase, []byte("not executed: "+phase), 0o700)
		put("bsr-tools/"+phase+".version", []byte([]string{"1.67.0\n", "1.72.0\n"}[index]), 0o600)
	}
	output := filepath.Join(t.TempDir(), "package")
	result, err := capture(build, filepath.Join(build, "recipe.sh"), digest(source), output, []moduleProfile{p}, digest(lock))
	if err != nil {
		t.Fatal(err)
	}
	return build, output, p, digest(lock), result
}

// TestRetainedPackage verifies complete capture, private modes and fresh native restore.
func TestRetainedPackage(t *testing.T) {
	_, directory, p, lock, result := fixturePackage(t)
	if result.CacheFiles != 9 || result.ExportFiles != 2 || result.Files != 17 || result.DependencyInputsCaptured || result.NativeOfflineReplayProved {
		t.Fatalf("incorrect receipt: %+v", result)
	}
	cache := filepath.Join(t.TempDir(), "cache")
	old := syscall.Umask(0o777)
	_, err := restore(directory, result.ManifestSHA256, cache, []moduleProfile{p}, lock)
	syscall.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	root, err := checkedRoot(cache)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, format := range []string{"b5", "shake256"} {
		body, err := readInput(root, "v3/modules/"+format+"/buf.build/"+p.Owner+"/"+p.Module+"/"+p.Commit+"/files/a.proto", false, fileLimit)
		if err != nil || string(body) != "syntax = \"proto3\";\n" {
			t.Fatalf("restore differs: %v", err)
		}
	}
	if _, err := restore(directory, result.ManifestSHA256, cache, []moduleProfile{p}, lock); err == nil {
		t.Fatal("existing destination replaced")
	}
	if _, err := checkCache(directory, result.ManifestSHA256, cache, []moduleProfile{p}, lock); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cache, "v3/modules/b5/buf.build/"+p.Owner+"/"+p.Module+"/"+p.Commit+"/files/a.proto")
	if err := os.WriteFile(file, []byte("changed after restore"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkCache(directory, result.ManifestSHA256, cache, []moduleProfile{p}, lock); err == nil {
		t.Fatal("mutated native cache admitted")
	}
}

// TestRestoreSnapshot binds destination bytes to the verified snapshot during source replacement.
func TestRestoreSnapshot(t *testing.T) {
	_, directory, p, lock, r := fixturePackage(t)
	cache := filepath.Join(t.TempDir(), "cache")
	_, err := inspectPackage(directory, r.ManifestSHA256, []moduleProfile{p}, lock, func(snapshot map[string][]byte) error {
		name := filepath.Join(directory, modulePrefix(p), "b5/files/a.proto")
		before, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(name, []byte("transient replacement"), 0o600); err != nil {
			return err
		}
		err = restoreSnapshot(snapshot, []moduleProfile{p}, cache)
		return errors.Join(err, os.WriteFile(name, before, 0o600))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkCache(directory, r.ManifestSHA256, cache, []moduleProfile{p}, lock); err != nil {
		t.Fatal(err)
	}
}

// TestDeclaredBudget rejects an oversized record before any payload read/allocation.
func TestDeclaredBudget(t *testing.T) {
	_, dir, p, lock, _ := fixturePackage(t)
	name := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.Files[0].Bytes = packageLimit + 1
	raw, err = canonical(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = verify(dir, digest(raw), []moduleProfile{p}, lock)
	if err == nil || !strings.Contains(err.Error(), "remaining byte budget") {
		t.Fatalf("budget not checked before reading: %v", err)
	}
}

// TestPackageRefusals checks attacker-controlled trees against an unchanged external seal.
func TestPackageRefusals(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"special file bits": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Chmod(filepath.Join(dir, "records/buf.lock"), os.ModeSetuid|0o600); err != nil {
				t.Fatal(err)
			}
		},
		"special directory bits": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Chmod(filepath.Join(dir, "records"), os.ModeSetgid|0o700); err != nil {
				t.Fatal(err)
			}
		},
		"changed bytes": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, "records/buf.lock"), []byte("wrong"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"extra file": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, "extra"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"extra empty directory": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "extra"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"missing file": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, "records/buf.lock")); err != nil {
				t.Fatal(err)
			}
		},
		"wrong file mode": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Chmod(filepath.Join(dir, "records/buf.lock"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"wrong directory mode": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Chmod(filepath.Join(dir, "records"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"linked parent": func(t *testing.T, dir string) {
			t.Helper()
			target := filepath.Join(t.TempDir(), "records")
			if err := os.Rename(filepath.Join(dir, "records"), target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, "records")); err != nil {
				t.Fatal(err)
			}
		},
		"file with multiple hard links": func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Link(filepath.Join(dir, "records/buf.lock"), filepath.Join(t.TempDir(), "alias")); err != nil {
				t.Fatal(err)
			}
		},
		"linked file": func(t *testing.T, dir string) {
			t.Helper()
			file := filepath.Join(dir, "records/buf.lock")
			target := filepath.Join(t.TempDir(), "file")
			if err := os.Rename(file, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, file); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, dir, p, lock, r := fixturePackage(t)
			mutate(t, dir)
			if _, err := verify(dir, r.ManifestSHA256, []moduleProfile{p}, lock); err == nil {
				t.Fatal("unsafe package admitted")
			}
		})
	}
}

// TestResealedManifest rejects semantic forgeries even when the caller supplies a new seal.
func TestResealedManifest(t *testing.T) {
	for _, mutation := range []string{"duplicate", "alias", "unknown", "escape", "tool", "source"} {
		t.Run(mutation, func(t *testing.T) {
			_, dir, p, lock, _ := fixturePackage(t)
			file := filepath.Join(dir, "manifest.json")
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var m manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "duplicate":
				raw = bytes.Replace(raw, []byte("{\n"), []byte("{\n  \"schemaVersion\": 1,\n"), 1)
			case "alias":
				raw = bytes.Replace(raw, []byte("\"schemaVersion\""), []byte("\"SchemaVersion\""), 1)
			case "unknown":
				raw = bytes.Replace(raw, []byte("{\n"), []byte("{\n  \"extra\": true,\n"), 1)
			default:
				switch mutation {
				case "escape":
					m.Files[0].Path = "../outside"
				case "tool":
					m.Tools[0].Version = "1.72.0"
				case "source":
					m.SourceManifestSHA256 = strings.Repeat("0", 64)
				}
				raw, err = canonical(m)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(file, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := verify(dir, digest(raw), []moduleProfile{p}, lock); err == nil {
				t.Fatal("forged manifest admitted")
			}
		})
	}
}

type failedReadClose struct{ closeErr error }

func (f failedReadClose) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (f failedReadClose) Close() error               { return f.closeErr }

// TestIncompleteIO preserves both read and close failures and emits no plausible bytes.
func TestIncompleteIO(t *testing.T) {
	closeErr := errors.New("close failed")
	body, err := boundedRead(failedReadClose{closeErr}, 10)
	if body != nil || !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, closeErr) {
		t.Fatalf("lost failure: %q %v", body, err)
	}
	if _, err := boundedRead(io.NopCloser(strings.NewReader("too long")), 3); err == nil {
		t.Fatal("oversize read admitted")
	}
	if err := writeReceipt(failedWriter{}, receipt{}); err == nil {
		t.Fatal("lost output error")
	}
}

type failedWriter struct{}

func (failedWriter) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

// TestExplicitCLI keeps ordinary builds inert and refuses partial/mixed operations.
func TestExplicitCLI(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err != nil || out.Len() != 0 {
		t.Fatal("ordinary invocation was active")
	}
	for _, args := range [][]string{{"capture"}, {"verify", "--package", "x"}, {"restore", "--package", "x", "--sha256", "wrong"}, {"verify", "--package", "x", "--package", "y"}, {"verify", "--package", "x", "extra"}, {"unknown"}, {"verify", "--output", "x"}} {
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("partial operation admitted: %v", args)
		}
	}
	missing := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{
		{"capture", "--build", missing, "--recipe", missing, "--source-sha256", strings.Repeat("0", 64), "--output", missing},
		{"verify", "--package", missing, "--sha256", strings.Repeat("0", 64)},
		{"restore", "--package", missing, "--sha256", strings.Repeat("0", 64), "--output", missing},
		{"check-cache", "--package", missing, "--sha256", strings.Repeat("0", 64), "--cache", missing},
	} {
		if err := run(args, &out); !errors.Is(err, os.ErrNotExist) || out.Len() != 0 {
			t.Fatalf("complete operation did not reach its checked input boundary: %v", err)
		}
	}
}

// TestSharedReadBudget bounds a selected subtree before its file body is allocated.
func TestSharedReadBudget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := checkedRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	budget := int64(2)
	body, err := readBudgeted(root, "source", false, &budget)
	if err == nil || body != nil || budget != 2 {
		t.Fatalf("pre-read budget lost: bytes=%q remaining=%d err=%v", body, budget, err)
	}
}

// TestToolInputRefusals rejects missing executable identity before retaining tool observations.
func TestToolInputRefusals(t *testing.T) {
	for _, kind := range []string{"empty", "missing execute permission"} {
		t.Run(kind, func(t *testing.T) {
			build, _, p, lock, _ := fixturePackage(t)
			tool := filepath.Join(build, "bsr-tools/api")
			if kind == "empty" {
				if err := os.Truncate(tool, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Chmod(tool, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source, err := os.ReadFile(filepath.Join(build, "source-snapshot/source-manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "refused")
			_, err = capture(build, filepath.Join(build, "recipe.sh"), digest(source), output, []moduleProfile{p}, lock)
			if err == nil {
				t.Fatal("invalid native tool retained")
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("success-shaped output created: %v", err)
			}
		})
	}
}
