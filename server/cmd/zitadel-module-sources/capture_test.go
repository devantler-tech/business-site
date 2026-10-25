package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/module"
)

// writeFixture writes a complete regular fixture or fails at the actual write.
func writeFixture(t *testing.T, name string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// materializeSelection supplies only a build's own selected module cache inputs.
func materializeSelection(t *testing.T) (selectionFixture, string) {
	t.Helper()
	f := selectionInputs(t)
	writeFixture(t, filepath.Join(f.build, "prepared/source/go.mod"), f.mod)
	writeFixture(t, filepath.Join(f.build, "prepared/source/go.sum"), f.sums)
	writeFixture(t, filepath.Join(f.build, "packages.jsonl"), f.packages)
	cat := filepath.Join(t.TempDir(), "catalogue.json")
	writeFixture(t, cat, f.cat)
	for i, selected := range f.entries {
		path, err := module.EscapePath(selected.Effective.Path)
		if err != nil {
			t.Fatal(err)
		}
		version, err := module.EscapeVersion(selected.Effective.Version)
		if err != nil {
			t.Fatal(err)
		}
		prefix := filepath.Join(f.build, "go-modules/cache/download", path, "@v", version)
		data, err := os.ReadFile(f.payloads[i])
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, prefix+".zip", data)
		writeFixture(t, prefix+".mod", f.mods[i])
		// A forged marker cannot confer integrity on changed archive bytes.
		writeFixture(t, prefix+".ziphash", []byte(selected.Effective.Sum))
	}
	return f, cat
}

// capturedSelection prepares a complete retained fixture for mutation or mode checks.
func capturedSelection(t *testing.T) (selectionFixture, string) {
	t.Helper()
	f, cat := materializeSelection(t)
	out := filepath.Join(t.TempDir(), "retained")
	if _, err := capture(f.build, cat, out, digest(f.cat)); err != nil {
		t.Fatal(err)
	}
	return f, out
}

// TestCaptureReadback exercises complete bytes, replacement identities and portability.
func TestCaptureReadback(t *testing.T) {
	f, cat := materializeSelection(t)
	out := filepath.Join(t.TempDir(), "retained")
	got, err := capture(f.build, cat, out, digest(f.cat))
	if err != nil || got.Modules != 2 || got.Files != 8 || got.ManifestSHA256 == "" {
		t.Fatalf("capture=%+v err=%v", got, err)
	}
	checked, err := verify(out, got.ManifestSHA256, digest(f.cat))
	if err != nil || checked != got {
		t.Fatalf("readback=%+v err=%v", checked, err)
	}
	copyRoot := filepath.Join(t.TempDir(), "moved")
	if err := os.CopyFS(copyRoot, os.DirFS(out)); err != nil {
		t.Fatal(err)
	}
	if _, err := verify(copyRoot, got.ManifestSHA256, digest(f.cat)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "modules/gopkg.in/ini.v1/@v/v1.2.3.zip"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(f.payloads[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("replacement ZIP was not preserved verbatim")
	}
}

// TestCaptureFailureLeavesNoReceipt refuses partial input and preserves preexisting output.
func TestCaptureFailureLeavesNoReceipt(t *testing.T) {
	for _, missing := range []string{"packages.jsonl", "go-modules/cache/download/gopkg.in/ini.v1/@v/v1.2.3.zip", "go-modules/cache/download/gopkg.in/ini.v1/@v/v1.2.3.mod"} {
		t.Run(missing, func(t *testing.T) {
			f, cat := materializeSelection(t)
			if err := os.Remove(filepath.Join(f.build, missing)); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "retained")
			got, err := capture(f.build, cat, out, digest(f.cat))
			if err == nil || got != (receipt{}) {
				t.Fatal("partial inputs produced success receipt")
			}
			if _, err = os.Lstat(out); !os.IsNotExist(err) {
				t.Fatalf("partial output retained: %v", err)
			}
		})
	}
	f, cat := materializeSelection(t)
	out := t.TempDir()
	writeFixture(t, filepath.Join(out, "keep"), []byte("existing"))
	if _, err := capture(f.build, cat, out, digest(f.cat)); err == nil {
		t.Fatal("preexisting output accepted")
	}
	if _, err := os.Stat(filepath.Join(out, "keep")); err != nil {
		t.Fatal("preexisting output damaged")
	}
}

// TestCaptureNeverTrustsZiphash changes the real archive while retaining its marker.
func TestCaptureNeverTrustsZiphash(t *testing.T) {
	f, cat := materializeSelection(t)
	file := filepath.Join(f.build, "go-modules/cache/download/gopkg.in/ini.v1/@v/v1.2.3.zip")
	other, _, _, _ := modulePayload(t, "gopkg.in/ini.v1", "v1.2.3", []string{"different.go"})
	data, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, file, data)
	if got, err := capture(f.build, cat, filepath.Join(t.TempDir(), "retained"), digest(f.cat)); err == nil || got != (receipt{}) {
		t.Fatal("changed archive with original ziphash accepted")
	}
}

// TestReadbackRejectsChangedMembership checks actual retained filesystem mutations.
func TestReadbackRejectsChangedMembership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"extra file", func(t *testing.T, p string) { writeFixture(t, filepath.Join(p, "extra"), []byte("x")) }},
		{"extra directory", func(t *testing.T, p string) {
			if err := os.Mkdir(filepath.Join(p, "extra"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing module", func(t *testing.T, p string) {
			if err := os.Remove(filepath.Join(p, "modules/gopkg.in/ini.v1/@v/v1.2.3.zip")); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed module", func(t *testing.T, p string) {
			writeFixture(t, filepath.Join(p, "modules/gopkg.in/ini.v1/@v/v1.2.3.mod"), []byte("changed"))
		}},
		{"executable mode", func(t *testing.T, p string) {
			if err := os.Chmod(filepath.Join(p, "records/go.mod"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, p string) {
			name := filepath.Join(p, "records/go.mod")
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("go.sum", name); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed catalogue", func(t *testing.T, p string) {
			writeFixture(t, filepath.Join(p, "records/catalogue.json"), []byte("{}"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, cat := materializeSelection(t)
			out := filepath.Join(t.TempDir(), "retained")
			got, err := capture(f.build, cat, out, digest(f.cat))
			if err != nil {
				t.Fatal(err)
			}
			tc.change(t, out)
			if _, err := verify(out, got.ManifestSHA256, digest(f.cat)); err == nil {
				t.Fatal("changed retained package accepted")
			}
		})
	}
}

// TestReadbackChecksContentBeyondTransportSeal rejects a newly sealed but false module sum.
func TestReadbackChecksContentBeyondTransportSeal(t *testing.T) {
	f, cat := materializeSelection(t)
	out := filepath.Join(t.TempDir(), "retained")
	_, err := capture(f.build, cat, out, digest(f.cat))
	if err != nil {
		t.Fatal(err)
	}
	name := "modules/gopkg.in/ini.v1/@v/v1.2.3.mod"
	changed := []byte("changed module")
	writeFixture(t, filepath.Join(out, name), changed)
	manifestFile := filepath.Join(out, "manifest.json")
	body, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for _, raw := range m["files"].([]any) {
		file := raw.(map[string]any)
		if file["path"] == name {
			file["sha256"], file["bytes"] = digest(changed), len(changed)
		}
	}
	body = append(marshal(t, m), '\n')
	writeFixture(t, manifestFile, body)
	if _, err := verify(out, digest(body), digest(f.cat)); err == nil {
		t.Fatal("new transport seal bypassed frozen Go content sum")
	}
}

// TestReadbackDoesNotFollowLinkedParents excludes cache and retained-parent escapes.
func TestReadbackDoesNotFollowLinkedParents(t *testing.T) {
	f, cat := materializeSelection(t)
	cache := filepath.Join(f.build, "go-modules/cache/download/gopkg.in")
	outside := filepath.Join(t.TempDir(), "modules")
	if err := os.Rename(cache, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := capture(f.build, cat, filepath.Join(t.TempDir(), "retained"), digest(f.cat)); err == nil {
		t.Fatal("linked cache parent accepted")
	}
}
