package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/module"
)

// verifierBootstrapFixture uses a real native Go ZIP and independent checksums.
func verifierBootstrapFixture(t *testing.T) (verifierBootstrapInputs, identity) {
	t.Helper()
	zip, mod, sum, modSum := modulePayload(t, "golang.org/x/mod", "v0.42.0", []string{"go.mod", "module/module.go", "LICENSE", "PATENTS"})
	dep := identity{Path: "golang.org/x/mod", Version: "v0.42.0", Sum: sum, GoModSum: modSum}
	inputs := verifierBootstrapInputs{ToolSource: t.TempDir(), Cache: t.TempDir(), SourceSHA256: strings.Repeat("1", 64), ToolSourceSHA256: strings.Repeat("2", 64)}
	inputs.Composition = filepath.Join(t.TempDir(), "composition.json")
	compositions := []struct {
		ImportPath, Name string
		GoFiles          []string
	}{
		{"github.com/devantler-tech/business-site/server/cmd/zitadel-module-sources", "main", []string{"capture.go", "generator_sources.go", "generators.go", "graph_metadata.go", "json_fields.go", "main.go", "payload.go", "restore.go", "selection.go"}},
		{"github.com/devantler-tech/business-site/server/cmd/zitadel-bsr-sources", "main", []string{"files.go", "main.go", "package.go", "validation.go"}},
	}
	composition, err := json.Marshal(compositions)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, inputs.Composition, composition)
	for _, command := range compositions {
		for _, file := range command.GoFiles {
			writeFixture(t, filepath.Join(inputs.ToolSource, "server/cmd", filepath.Base(command.ImportPath), file), []byte("package main\n"))
		}
	}
	writeFixture(t, filepath.Join(inputs.ToolSource, "server/go.mod"), []byte("module github.com/devantler-tech/business-site/server\n\ngo 1.27.3\n\nrequire golang.org/x/mod v0.42.0\n"))
	writeFixture(t, filepath.Join(inputs.ToolSource, "server/go.sum"), []byte(dep.Path+" "+dep.Version+" "+sum+"\n"+dep.Path+" "+dep.Version+"/go.mod "+modSum+"\n"))
	prefix := "cache/download/golang.org/x/mod/@v/v0.42.0"
	body, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(inputs.Cache, prefix+".zip"), body)
	writeFixture(t, filepath.Join(inputs.Cache, prefix+".mod"), mod)
	writeFixture(t, filepath.Join(inputs.Cache, prefix+".info"), []byte("{\"Version\":\"v0.42.0\"}\n"))
	return inputs, dep
}

// TestVerifierBootstrapRetainsOnlyOriginalInputs distinguishes the private
// bootstrap dependency from the service graph and from an already built tool.
func TestVerifierBootstrapRetainsOnlyOriginalInputs(t *testing.T) {
	inputs, dep := verifierBootstrapFixture(t)
	capsule := filepath.Join(filepath.Dir(inputs.Composition), "capsule")
	got, err := captureVerifierBootstrap(inputs, capsule, dep)
	if err != nil {
		t.Fatal(err)
	}
	if got.Files != 4 || got.GoVerifiersRebuilt || !got.VerifierBootstrapCaptured || got.SourceManifestSHA256 != inputs.SourceSHA256 {
		t.Fatal("bootstrap receipt widened its proof")
	}
	if _, err := verifyVerifierBootstrap(inputs, capsule, got.ManifestSHA256, dep); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "native")
	restored, err := restoreVerifierBootstrap(inputs, capsule, got.ManifestSHA256, output, dep)
	if err != nil || !restored.VerifierBootstrapRestored || restored.GoVerifiersRebuilt {
		t.Fatalf("original bootstrap cannot be restored: %v", err)
	}
	for _, suffix := range []string{".zip", ".mod", ".info"} {
		original, err := os.ReadFile(filepath.Join(inputs.Cache, "cache/download/golang.org/x/mod/@v/v0.42.0"+suffix))
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(output, "cache/download/golang.org/x/mod/@v/v0.42.0"+suffix))
		if err != nil || !bytes.Equal(body, original) {
			t.Fatal("native cache changed original input")
		}
	}
}

// TestVerifierBootstrapRefusesIncompleteCapture preserves existing output and
// refuses missing, linked, changed, or wrong-version inputs without a receipt.
func TestVerifierBootstrapRefusesIncompleteCapture(t *testing.T) {
	for _, kind := range []string{"missing-zip", "changed-zip", "linked-mod", "bad-info", "missing-terms", "wrong-module", "replacement", "wrong-sum", "bad-source-seal", "bad-tool-seal", "bad-composition", "existing-output"} {
		t.Run(kind, func(t *testing.T) {
			inputs, dep := verifierBootstrapFixture(t)
			output := filepath.Join(t.TempDir(), "capsule")
			prefix := filepath.Join(inputs.Cache, "cache/download/golang.org/x/mod/@v/v0.42.0")
			switch kind {
			case "missing-zip":
				if err := os.Remove(prefix + ".zip"); err != nil {
					t.Fatal(err)
				}
			case "changed-zip":
				writeFixture(t, prefix+".zip", []byte("changed"))
			case "linked-mod":
				if err := os.Remove(prefix + ".mod"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(inputs.ToolSource, "server/go.mod"), prefix+".mod"); err != nil {
					t.Fatal(err)
				}
			case "bad-info":
				writeFixture(t, prefix+".info", []byte("{\"Version\":\"v0.41.0\"}\n"))
			case "missing-terms":
				zip, _, sum, _ := modulePayload(t, dep.Path, dep.Version, []string{"go.mod", "module/module.go", "LICENSE"})
				body, err := os.ReadFile(zip)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, prefix+".zip", body)
				// Recomputing the transport/content hashes cannot supply omitted terms.
				dep.Sum = sum
				writeFixture(t, filepath.Join(inputs.ToolSource, "server/go.sum"), []byte(dep.Path+" "+dep.Version+" "+sum+"\n"+dep.Path+" "+dep.Version+"/go.mod "+dep.GoModSum+"\n"))
			case "wrong-module":
				writeFixture(t, filepath.Join(inputs.ToolSource, "server/go.mod"), []byte("module other.example/server\n\ngo 1.27.3\nrequire golang.org/x/mod v0.42.0\n"))
			case "replacement":
				f, err := os.OpenFile(filepath.Join(inputs.ToolSource, "server/go.mod"), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("replace golang.org/x/mod => ../replacement\n")
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
			case "wrong-sum":
				writeFixture(t, filepath.Join(inputs.ToolSource, "server/go.sum"), []byte("golang.org/x/mod v0.42.0 h1:wrong\n"))
			case "bad-source-seal":
				inputs.SourceSHA256 = "not-a-seal"
			case "bad-tool-seal":
				inputs.ToolSourceSHA256 = "not-a-seal"
			case "bad-composition":
				writeFixture(t, inputs.Composition, []byte("[]"))
			case "existing-output":
				writeFixture(t, filepath.Join(output, "keep"), []byte("keep"))
			}
			got, err := captureVerifierBootstrap(inputs, output, dep)
			if err == nil || got.ManifestSHA256 != "" {
				t.Fatal("invalid bootstrap capture emitted success")
			}
			if kind == "existing-output" {
				if body, err := os.ReadFile(filepath.Join(output, "keep")); err != nil || string(body) != "keep" {
					t.Fatal("existing output changed")
				}
			} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("failed capture left partial output")
			}
		})
	}
}

// writeFixture writes a complete regular fixture or fails at the actual write.
func TestVerifierBootstrapRefusesChangedBytesDuringConsumption(t *testing.T) {
	inputs, dep := verifierBootstrapFixture(t)
	capsule := filepath.Join(t.TempDir(), "capsule")
	receipt, err := captureVerifierBootstrap(inputs, capsule, dep)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "native")
	copier := func(root *os.Root, source, output, target string, limit int64) (fileRecord, error) {
		if !strings.HasSuffix(source, ".zip") {
			return copyAt(root, source, output, target, limit)
		}
		file := filepath.Join(capsule, source)
		original, err := os.ReadFile(file)
		if err != nil {
			return fileRecord{}, err
		}
		writeFixture(t, file, []byte("different bytes during the copy"))
		got, err := copyAt(root, source, output, target, limit)
		writeFixture(t, file, original)
		return got, err
	}
	got, err := restoreVerifierBootstrapWithCopier(inputs, capsule, receipt.ManifestSHA256, output, dep, copier)
	if err == nil || got.ManifestSHA256 != "" {
		t.Fatal("restoration trusted copied bytes without their authenticated original record")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("failed consumption retained partial native output")
	}
}

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
