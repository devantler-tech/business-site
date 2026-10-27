package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// TestRetainedInputsArchive exercises the actual closed archive with private
// capsule modes. Compiled output and ambient caches must never enter it.
func TestRetainedInputsArchive(t *testing.T) {
	for _, kind := range []string{"valid", "missing-component", "linked-member", "unsupported-mode"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			components := []string{"source-snapshot", "selected-go-sources", "selected-generator-go-sources", "go-graph-metadata", "selected-pnpm-sources", "selected-bsr-sources", "selected-console-originals"}
			modes := []os.FileMode{0600, 0444, 0644, 0755}
			for i, component := range components {
				if kind == "missing-component" && i == 0 {
					continue
				}
				dir := filepath.Join(root, component)
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(dir, "manifest.json")
				if err := os.WriteFile(file, []byte(component+"\n"), modes[i%len(modes)]); err != nil {
					t.Fatal(err)
				}
				if kind == "linked-member" && i == 0 {
					if err := os.Symlink(file, filepath.Join(dir, "linked")); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "unsupported-mode" && i == 0 {
					if err := os.Chmod(file, 0666); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, name := range []string{"build-receipt.json", "service-binary", "ambient-cache"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(t.TempDir(), "retained")
			var out, diagnostic bytes.Buffer
			status := sourceBundleRun([]string{"--seal-inputs", root, output}, &out, &diagnostic)
			if kind != "valid" {
				if status == 0 || out.Len() != 0 {
					t.Fatalf("invalid retained input published a receipt: %s", kind)
				}
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatalf("failed retention left a sealed output: %v", err)
				}
				return
			}
			if status != 0 {
				t.Fatalf("private retained inputs cannot enter the archive: %s", diagnostic.String())
			}
			var receipt struct{ ManifestSHA256 string }
			if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			manifest, err := sourceVerifyTree(output, receipt.ManifestSHA256)
			if err != nil || manifest.Phase != "retained-build-inputs" || manifest.PreparedProfile != "" || len(manifest.Records) != 0 || len(manifest.Files) != len(components)+1 {
				t.Fatalf("retained inputs impersonate prepared source or include ambient output: %v", err)
			}
			archive := filepath.Join(t.TempDir(), "inputs.tar")
			digest, err := sourcePackTree(output, receipt.ManifestSHA256, archive)
			if err != nil {
				t.Fatal(err)
			}
			restored := filepath.Join(t.TempDir(), "restored")
			if err := sourceUnpackTree(archive, digest, receipt.ManifestSHA256, restored); err != nil {
				t.Fatal(err)
			}
			for i, component := range components {
				for _, tree := range []string{output, restored} {
					directory, err := os.Stat(filepath.Join(tree, "source", component))
					if err != nil || directory.Mode() != os.ModeDir|0700 {
						t.Fatalf("private native capsule directory mode changed: %s: %v", component, err)
					}
				}
				info, err := os.Stat(filepath.Join(restored, "source", component, "manifest.json"))
				if err != nil || info.Mode() != modes[i%len(modes)] {
					t.Fatalf("native capsule mode changed: %s: %v", component, err)
				}
			}
			if _, err := sourceRestoreBuild(restored, receipt.ManifestSHA256, filepath.Join(t.TempDir(), "build")); err == nil {
				t.Fatal("input transport impersonated a prepared source snapshot")
			}
		})
	}
}

// TestSourceRestoreBuildKeepsLiteralInputs proves the replay seam restores a
// verified closed snapshot without re-preparing it or replacing its original API
// record with the serialized source configuration. Frozen-profile admission is
// separately owned by the native builder, as for the existing unpack operation.
func TestSourceRestoreBuildKeepsLiteralInputs(t *testing.T) {
	for _, kind := range []string{"valid", "missing-record", "extra-record", "changed-member", "wrong-seal", "existing-output", "output-inside-snapshot", "output-alias-inside-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			input := t.TempDir()
			files := map[string]string{
				"apps/api/project.json": "serialized API\n",
				"empty.go":              "",
				"scripts/generate.sh":   "#!/bin/sh\nexit 0\n",
			}
			for name, body := range files {
				file := filepath.Join(input, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				mode := os.FileMode(0644)
				if strings.HasSuffix(name, ".sh") {
					mode = 0755
				}
				if err := os.WriteFile(file, []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			profile, err := sourceSealTree(input, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			records := map[string][]byte{
				"source-preparation.json":                       []byte("literal preparation\n"),
				"api-project.original.json":                     []byte("original API\n"),
				"api-project.serial.json":                       []byte("serialized API\n"),
				"inputs.sha256":                                 []byte("literal input hashes\n"),
				"recipes/prepare-zitadel-source.sh":             []byte("original preparer\n"),
				"recipes/zitadel-source-inputs.sha256":          []byte("original locks\n"),
				"recipes/build-zitadel-linux.sh":                []byte("native builder\n"),
				"recipes/zitadel-source-bundle.go":              []byte("source helper\n"),
				"recipes/zitadel-console-assets.go":             []byte("console download adapter\n"),
				"recipes/zitadel-login-icons.go":                []byte("strict statik decoder\n"),
				"recipes/zitadel-text-fonts.go":                 []byte("font notice collector\n"),
				"third_party/zitadel-text-fonts/catalogue.json": []byte("frozen font catalogue\n"),
				"third_party/zitadel-text-fonts/Apache-2.0.txt": []byte("complete Apache terms\n"),
				"recipes/zitadel-pnpm-source/index.mjs":         []byte("native pnpm adapter\n"),
				"recipes/zitadel-pnpm-source/package.json":      []byte("native pnpm SDK\n"),
				"recipes/zitadel-pnpm-source/package-lock.json": []byte("native pnpm SDK locks\n"),
				"ZITADEL-SOURCE.md":                             []byte("source description\n"),
				"modifications.json":                            []byte("dated modification record\n"),
			}
			if kind == "missing-record" {
				delete(records, "api-project.original.json")
			}
			if kind == "extra-record" {
				records["unselected.json"] = []byte("extra")
			}
			seal, err := sourceAddRecords(snapshot, profile, records)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(snapshot, "source-manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "changed-member" {
				if err := os.WriteFile(filepath.Join(snapshot, "source/empty.go"), []byte("changed"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "wrong-seal" {
				seal = strings.Repeat("0", 64)
			}
			output := filepath.Join(t.TempDir(), "build")
			if kind == "output-inside-snapshot" {
				output = filepath.Join(snapshot, "build")
			}
			if kind == "output-alias-inside-snapshot" {
				alias := filepath.Join(t.TempDir(), "snapshot-alias")
				if err := os.Symlink(snapshot, alias); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(alias, "build")
			}
			if kind == "existing-output" {
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, diagnostic bytes.Buffer
			status := sourceBundleRun([]string{"--restore-build", snapshot, seal, output}, &out, &diagnostic)
			if kind == "valid" {
				if status != 0 {
					t.Fatalf("verified prepared source cannot enter the existing build: %s", diagnostic.String())
				}
				var receipt struct {
					ManifestSHA256, PreparedProfileSHA256 string
					Files, Records                        int
				}
				if json.Unmarshal(out.Bytes(), &receipt) != nil || receipt.ManifestSHA256 != seal || receipt.PreparedProfileSHA256 != profile || receipt.Files != len(files) || receipt.Records != len(records) {
					t.Fatalf("unbound restoration receipt: %s", out.String())
				}
				for name, body := range files {
					file := filepath.Join(output, "prepared/source", filepath.FromSlash(name))
					got, err := os.ReadFile(file)
					if err != nil || string(got) != body {
						t.Fatalf("restored source differs: %s: %v", name, err)
					}
					info, err := os.Stat(file)
					wantMode := os.FileMode(0644)
					if strings.HasSuffix(name, ".sh") {
						wantMode = 0755
					}
					if err != nil || info.Mode() != wantMode {
						t.Fatalf("restored mode differs: %s: %v", name, err)
					}
				}
				for name, want := range records {
					gotRecord, err := os.ReadFile(filepath.Join(output, "prepared/records", filepath.FromSlash(name)))
					if err != nil || !bytes.Equal(gotRecord, want) {
						t.Fatalf("literal audit record changed: %s: %v", name, err)
					}
					if name != "inputs.sha256" && name != "source-preparation.json" && !strings.HasPrefix(name, "api-project.") {
						continue
					}
					relative := name
					if name == "source-preparation.json" {
						relative = "prepared/" + name
					}
					got, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(relative)))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("literal build record replaced: %s: %v", name, err)
					}
				}
				after, err := os.ReadFile(filepath.Join(snapshot, "source-manifest.json"))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("restoration modified sealed source")
				}
			} else {
				if status == 0 || out.Len() != 0 {
					t.Fatal("unsafe replay input emitted a success receipt")
				}
				if kind == "existing-output" {
					if body, err := os.ReadFile(filepath.Join(output, "keep")); err != nil || string(body) != "keep" {
						t.Fatal("existing output changed")
					}
				} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("failed restoration retained a success-shaped output")
				}
			}
		})
	}
}

// TestSourceArchiveReplaysOnlyItsCompleteClosedTree rejects altered archive bytes,
// hidden padding, unsafe metadata and incomplete or additional tree members.
func TestSourceArchiveReplaysOnlyItsCompleteClosedTree(t *testing.T) {
	for _, kind := range []string{"valid", "empty-source", "valid-records", "changed-record", "changed-archive", "hidden-padding", "wrong-archive-seal", "wrong-tree-seal", "missing", "extra", "duplicate", "link", "traversal", "mode", "trailing", "truncated", "existing-output"} {
		t.Run(kind, func(t *testing.T) {
			input := t.TempDir()
			body := []byte("package source\n")
			if kind == "empty-source" {
				body = nil
			}
			if err := os.WriteFile(filepath.Join(input, "source.go"), body, 0755); err != nil {
				t.Fatal(err)
			}
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			seal, err := sourceSealTree(input, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "valid-records" || kind == "changed-record" {
				seal, err = sourceAddRecords(snapshot, seal, map[string][]byte{"recipes/prepare.sh": []byte("literal retained build recipe\n")})
				if err != nil {
					t.Fatal(err)
				}
			}
			archive := filepath.Join(t.TempDir(), "source.tar")
			digest, err := sourcePackTree(snapshot, seal, archive)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(archive)
			if err != nil || digest != fmt.Sprintf("%x", sha256.Sum256(raw)) {
				t.Fatalf("archive digest not bound: %v", err)
			}
			output := filepath.Join(t.TempDir(), "replay")
			if kind == "existing-output" {
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "changed-archive" {
				raw[1024] ^= 1
			}
			if kind == "hidden-padding" {
				raw[1536+len(body)] = 0x7f
			}
			if kind == "truncated" {
				raw = raw[:len(raw)-700]
			}
			if kind == "trailing" {
				raw = append(raw, make([]byte, 512)...)
			}
			if kind == "missing" || kind == "extra" || kind == "duplicate" || kind == "link" || kind == "traversal" || kind == "mode" || kind == "changed-record" {
				var altered bytes.Buffer
				w := tar.NewWriter(&altered)
				r := tar.NewReader(bytes.NewReader(raw))
				for {
					header, err := r.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(r)
					if err != nil {
						t.Fatal(err)
					}
					if header.Name == "source/source.go" {
						switch kind {
						case "missing":
							continue
						case "link":
							header.Typeflag, header.Linkname, header.Size, data = tar.TypeSymlink, "../elsewhere", 0, nil
						case "traversal":
							header.Name = "../source.go"
						case "mode":
							header.Mode = 0777
						}
					}
					if kind == "changed-record" && header.Name == "records/recipes/prepare.sh" {
						data[0] ^= 1
					}
					if err := w.WriteHeader(header); err != nil {
						t.Fatal(err)
					}
					if _, err := w.Write(data); err != nil {
						t.Fatal(err)
					}
					if kind == "duplicate" && header.Name == "source/source.go" {
						if err := w.WriteHeader(header); err != nil {
							t.Fatal(err)
						}
						if _, err := w.Write(data); err != nil {
							t.Fatal(err)
						}
					}
				}
				if kind == "extra" {
					if err := w.WriteHeader(&tar.Header{Name: "extra", Mode: 0644, Size: 1, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
						t.Fatal(err)
					}
					if _, err := w.Write([]byte("!")); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				raw = altered.Bytes()
			}
			if err := os.WriteFile(archive, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if kind != "changed-archive" {
				digest = fmt.Sprintf("%x", sha256.Sum256(raw))
			}
			if kind == "wrong-archive-seal" {
				digest = strings.Repeat("0", 64)
			}
			if kind == "wrong-tree-seal" {
				seal = strings.Repeat("0", 64)
			}
			err = sourceUnpackTree(archive, digest, seal, output)
			if kind == "valid" || kind == "empty-source" || kind == "valid-records" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := sourceVerifyTree(output, seal); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(output, "source", "source.go"))
				if err != nil || !bytes.Equal(got, body) {
					t.Fatalf("source not replayed: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("altered or unclosed source archive admitted")
				}
				if kind == "existing-output" {
					if got, err := os.ReadFile(filepath.Join(output, "keep")); err != nil || string(got) != "keep" {
						t.Fatal("foreign output changed")
					}
				} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("failed replay published partial success output")
				}
			}
		})
	}
}

// TestSourceBundleCommandIsInertWithoutExplicitMode keeps routine invocation inert
// and refuses ambiguous or unsupported operation arguments.
func TestSourceBundleCommandIsInertWithoutExplicitMode(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := sourceBundleRun(nil, &out, &diagnostic); code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("unrequested source capture was not inert")
	}
	if code := sourceBundleRun([]string{"--unknown"}, &out, &diagnostic); code == 0 || out.Len() != 0 {
		t.Fatal("unknown source operation was accepted")
	}
}

// TestPreparedSourceCommandRejectsAnOtherwiseValidSubstitute distinguishes a
// structurally valid snapshot from the actual frozen prepared-source profile.
func TestPreparedSourceCommandRejectsAnOtherwiseValidSubstitute(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "snapshot")
	var out, diagnostic bytes.Buffer
	if code := sourceBundleRun([]string{"--seal-prepared", root, output}, &out, &diagnostic); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "frozen complete profile") {
		t.Fatal("substituted prepared profile admitted")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("unadmitted profile retained as success output")
	}
}

// TestSourceBuildRecordsCannotDeclareExecutableModes preserves source executables
// without granting execution permissions to retained provenance records.
func TestSourceBuildRecordsCannotDeclareExecutableModes(t *testing.T) {
	input := t.TempDir()
	if err := os.WriteFile(filepath.Join(input, "source.go"), []byte("package source\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	seal, err := sourceSealTree(input, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	seal, err = sourceAddRecords(snapshot, seal, map[string][]byte{"recipe.sh": []byte("retained recipe\n")})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := sourceVerifyTree(snapshot, seal)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Records[0].Mode = 0755
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if _, err := sourceDecodeManifest(raw); err == nil {
		t.Fatal("build record admitted an undeclared executable mode")
	}
}

// TestSourcePackCannotModifyItsInputOrAnExistingArchive checks output reservation
// safety, including archive destinations that alias captured input.
func TestSourcePackCannotModifyItsInputOrAnExistingArchive(t *testing.T) {
	input := t.TempDir()
	if err := os.WriteFile(filepath.Join(input, "source.go"), []byte("package source\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	seal, err := sourceSealTree(input, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(snapshot, "source.tar"), filepath.Join(snapshot, "source", "source.tar")} {
		if hash, err := sourcePackTree(snapshot, seal, name); err == nil || hash != "" {
			t.Fatal("archive created within closed input")
		}
		if _, err := os.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("closed input modified")
		}
	}
	archive := filepath.Join(t.TempDir(), "source.tar")
	if err := os.WriteFile(archive, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if hash, err := sourcePackTree(snapshot, seal, archive); err == nil || hash != "" {
		t.Fatal("existing archive overwritten")
	}
	if body, err := os.ReadFile(archive); err != nil || string(body) != "existing" {
		t.Fatal("existing archive changed")
	}
	a, b := filepath.Join(t.TempDir(), "a.tar"), filepath.Join(t.TempDir(), "b.tar")
	first, err := sourcePackTree(snapshot, seal, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sourcePackTree(snapshot, seal, b)
	if err != nil || first != second {
		t.Fatalf("same snapshot changed archive identity: %v", err)
	}
}

// TestSourceCaptureBindsFrozenPreparationAndBuildRecords checks literal provenance
// and verifies that recapturing unchanged modifications on another day is stable.
func TestSourceCaptureBindsFrozenPreparationAndBuildRecords(t *testing.T) {
	for _, kind := range []string{"valid", "changed-source", "changed-recipe", "changed-inputs", "missing-console-helper", "missing-pnpm-helper", "wrong-original-api", "wrong-serial-api", "linked-record", "existing-output", "missing font collector", "missing statik decoder", "missing font catalogue", "missing Apache text", "linked font collector", "linked statik decoder", "linked font catalogue", "linked Apache text"} {
		t.Run(kind, func(t *testing.T) {
			build, repo := t.TempDir(), t.TempDir()
			write := func(root, name, body string) {
				t.Helper()
				p := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			hash := func(body string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(body))) }
			write(build, "prepared/source/source.go", "package source\n")
			write(build, "api-project.original.json", "original API\n")
			write(build, "api-project.serial.json", "serial API\n")
			var inputs strings.Builder
			for _, name := range []string{"go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "console/package.json", "proto/buf.lock"} {
				body := "frozen " + name + "\n"
				write(build, "prepared/source/"+name, body)
				fmt.Fprintf(&inputs, "%s  %s\n", hash(body), name)
			}
			write(build, "inputs.sha256", inputs.String())
			for _, name := range []string{"prepare-zitadel-source.sh", "zitadel-source-inputs.sha256", "build-zitadel-linux.sh", "zitadel-source-bundle.go", "zitadel-console-assets.go", "zitadel-login-icons.go", "zitadel-text-fonts.go"} {
				write(repo, "scripts/"+name, name+"\n")
			}
			write(repo, "docs/ZITADEL-SOURCE.md", "Build documentation.\n")
			write(repo, "third_party/zitadel-text-fonts/catalogue.json", "frozen font catalogue\n")
			write(repo, "third_party/zitadel-text-fonts/Apache-2.0.txt", "complete Apache terms\n")
			for _, name := range []string{"index.mjs", "package.json", "package-lock.json"} {
				write(repo, "tools/zitadel-pnpm-source/"+name, name+"\n")
			}
			preparation := fmt.Sprintf(`{"schemaVersion":1,"upstreamCommit":"8afa5dd37a8e723efb9eace8d09fd5c1cccef694","recipeSha256":"%s","inputManifestSha256":"%s","imageAdmission":"unknown","licenseClosure":"unknown","metadataProfile":"machine-id-no-jsonpath-v1"}`, hash("prepare-zitadel-source.sh\n"), hash("zitadel-source-inputs.sha256\n"))
			write(build, "prepared/source-preparation.json", preparation)
			profile, err := sourceSealTree(filepath.Join(build, "prepared/source"), filepath.Join(t.TempDir(), "reference"))
			if err != nil {
				t.Fatal(err)
			}
			policy := sourceCapturePolicy{profile, hash("original API\n"), hash("serial API\n"), hash(preparation)}
			output := filepath.Join(t.TempDir(), "captured")
			newInputs := map[string]string{"font collector": "scripts/zitadel-text-fonts.go", "statik decoder": "scripts/zitadel-login-icons.go", "font catalogue": "third_party/zitadel-text-fonts/catalogue.json", "Apache text": "third_party/zitadel-text-fonts/Apache-2.0.txt"}
			for label, name := range newInputs {
				if kind == "missing "+label || kind == "linked "+label {
					if e := os.Remove(filepath.Join(repo, name)); e != nil {
						t.Fatal(e)
					}
					if kind == "linked "+label {
						if e := os.Symlink(filepath.Join(repo, "docs/ZITADEL-SOURCE.md"), filepath.Join(repo, name)); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			switch kind {
			case "changed-source":
				write(build, "prepared/source/source.go", "package other!\n")
			case "changed-recipe":
				write(repo, "scripts/prepare-zitadel-source.sh", "changed preparation\n")
			case "changed-inputs":
				write(repo, "scripts/zitadel-source-inputs.sha256", "changed input manifest\n")
			case "missing-console-helper":
				if err := os.Remove(filepath.Join(repo, "scripts/zitadel-console-assets.go")); err != nil {
					t.Fatal(err)
				}
			case "missing-pnpm-helper":
				if err := os.Remove(filepath.Join(repo, "tools/zitadel-pnpm-source/index.mjs")); err != nil {
					t.Fatal(err)
				}
			case "wrong-original-api":
				write(build, "api-project.original.json", "substitute original\n")
			case "wrong-serial-api":
				write(build, "api-project.serial.json", "substitute serial\n")
			case "linked-record":
				p := filepath.Join(build, "inputs.sha256")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(repo, "scripts/zitadel-source-inputs.sha256"), p); err != nil {
					t.Fatal(err)
				}
			case "existing-output":
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				write(output, "keep", "keep")
			}
			var got sourceCaptureSummary
			synctest.Test(t, func(t *testing.T) {
				got, err = sourceCaptureBuild(build, repo, output, policy)
				if kind == "valid" && err == nil {
					time.Sleep(48 * time.Hour)
					later, laterErr := sourceCaptureBuild(build, repo, filepath.Join(t.TempDir(), "later"), policy)
					if laterErr != nil || got.ManifestSHA256 != later.ManifestSHA256 {
						t.Fatalf("unchanged modifications depend on capture date: first=%s later=%s: %v", got.ManifestSHA256, later.ManifestSHA256, laterErr)
					}
					raw, readErr := os.ReadFile(filepath.Join(output, "records/modifications.json"))
					var record struct {
						ModifiedOn string `json:"modifiedOn"`
					}
					if readErr != nil || json.Unmarshal(raw, &record) != nil || record.ModifiedOn != "2026-10-24" {
						t.Fatalf("modification date does not describe the frozen changes: %s: %v", raw, readErr)
					}
				}
			})
			if kind == "valid" {
				if err != nil || got.Files != 8 || got.Records != 18 || got.PreparedProfileSHA256 != profile || got.ManifestSHA256 == profile {
					t.Fatalf("incomplete source capture: %+v: %v", got, err)
				}
				manifest, err := sourceVerifyTree(output, got.ManifestSHA256)
				if err != nil || manifest.PreparedProfile != profile || len(manifest.Records) != 18 {
					t.Fatalf("unbound build records: %v", err)
				}
				if raw, err := sourceReadAt(output, "records/recipes/zitadel-console-assets.go"); err != nil || string(raw) != "zitadel-console-assets.go\n" {
					t.Fatal("console helper source was not retained literally")
				}
				for _, entry := range []struct{ name, body string }{{"recipes/zitadel-login-icons.go", "zitadel-login-icons.go\n"}, {"recipes/zitadel-text-fonts.go", "zitadel-text-fonts.go\n"}, {"third_party/zitadel-text-fonts/catalogue.json", "frozen font catalogue\n"}, {"third_party/zitadel-text-fonts/Apache-2.0.txt", "complete Apache terms\n"}} {
					if b, e := sourceReadAt(output, "records/"+entry.name); e != nil || string(b) != entry.body {
						t.Fatal("font verifier input was not retained literally", entry.name, e)
					}
				}
				archive := filepath.Join(t.TempDir(), "prepared.tar")
				archiveHash, err := sourcePackTree(output, got.ManifestSHA256, archive)
				if err != nil {
					t.Fatal(err)
				}
				unpacked := filepath.Join(t.TempDir(), "unpacked")
				if err := sourceUnpackTree(archive, archiveHash, got.ManifestSHA256, unpacked); err != nil {
					t.Fatal(err)
				}
				restored, err := sourceRestoreBuild(unpacked, got.ManifestSHA256, filepath.Join(t.TempDir(), "restored"))
				if err != nil || restored.ManifestSHA256 != got.ManifestSHA256 || restored.Records != got.Records {
					t.Fatalf("actual captured source cannot enter the native restoration: %+v: %v", restored, err)
				}
			} else {
				if err == nil || got.ManifestSHA256 != "" {
					t.Fatal("substituted build input admitted")
				}
				if kind == "existing-output" {
					if body, err := os.ReadFile(filepath.Join(output, "keep")); err != nil || string(body) != "keep" {
						t.Fatal("foreign output changed")
					}
				} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("failed source capture retained success output")
				}
			}
		})
	}
}

// TestSourceSealPreservesEmptySourceBootstrapAndExecutableMode protects maintained
// source that a generated-output exclusion heuristic could incorrectly discard.
func TestSourceSealPreservesEmptySourceBootstrapAndExecutableMode(t *testing.T) {
	root := t.TempDir()
	files := map[string]struct {
		body string
		mode os.FileMode
	}{
		"LICENSE":                            {"Original complete terms.\n", 0644},
		"empty.go":                           {"", 0644},
		"pkg/grpc/protoc/v2/options.pb.go":   {"package protoc\n", 0644},
		"internal/protoc/generator/main.go":  {"package main\n", 0644},
		"internal/protoc/generator/template": {"{{.Name}}\n", 0644},
		"scripts/generate.sh":                {"#!/bin/sh\nexit 0\n", 0755},
	}
	for name, file := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(file.body), file.mode); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), "sealed")
	seal, err := sourceSealTree(root, output)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(output, "source-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if seal != fmt.Sprintf("%x", sha256.Sum256(manifest)) {
		t.Fatal("source seal does not bind the actual member manifest")
	}
	var got struct {
		Files []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			SHA256 string `json:"sha256"`
			Mode   uint32 `json:"mode"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifest, &got); err != nil || len(got.Files) != len(files) {
		t.Fatalf("incomplete source membership: %v", err)
	}
	previous := ""
	for _, member := range got.Files {
		want, found := files[member.Path]
		if !found || member.Path <= previous || member.Bytes != len(want.body) || member.Mode != uint32(want.mode) || member.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want.body))) {
			t.Fatalf("source membership/identity/mode differs: %+v", member)
		}
		previous = member.Path
		body, err := os.ReadFile(filepath.Join(output, "source", filepath.FromSlash(member.Path)))
		if err != nil || string(body) != want.body {
			t.Fatalf("source bytes not retained: %s: %v", member.Path, err)
		}
	}
}

// TestSourceSealRefusesOutputAliasedIntoItsInput checks lexical and physical
// aliases before a snapshot can alter the source it is supposed to preserve.
func TestSourceSealRefusesOutputAliasedIntoItsInput(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(alias, "capture")
	if seal, err := sourceSealTree(root, output); err == nil || seal != "" {
		t.Fatal("source capture wrote its own output through an input-directory alias")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("refused output alias was created")
	}
}

// TestSourceSealRefusesUnsafeMembersWithoutPublishingSuccess checks unsafe tree
// entries and confirms that refusal removes only this operation's reservation.
func TestSourceSealRefusesUnsafeMembersWithoutPublishingSuccess(t *testing.T) {
	for _, kind := range []string{"file-link", "directory-link", "fifo", "private-mode", "oversized", "empty-tree", "linked-root", "existing-output"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "input")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "main.go")
			if kind != "empty-tree" {
				if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(parent, "output")
			var err error
			switch kind {
			case "file-link":
				err = os.Symlink("main.go", filepath.Join(root, "linked.go"))
			case "directory-link":
				err = os.Symlink(parent, filepath.Join(root, "linked"))
			case "fifo":
				err = syscall.Mkfifo(filepath.Join(root, "pipe"), 0644)
			case "private-mode":
				err = os.Chmod(file, 0600)
			case "oversized":
				err = os.Truncate(file, (128<<20)+1)
			case "linked-root":
				alias := filepath.Join(parent, "alias")
				err = os.Symlink(root, alias)
				root = alias
			case "existing-output":
				err = os.Mkdir(output, 0700)
				if err == nil {
					err = os.WriteFile(filepath.Join(output, "keep"), []byte("previous owner's output"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if seal, err := sourceSealTree(root, output); err == nil || seal != "" {
				t.Fatal("unsafe or incomplete source was sealed")
			}
			if kind == "existing-output" {
				body, err := os.ReadFile(filepath.Join(output, "keep"))
				if err != nil || string(body) != "previous owner's output" {
					t.Fatal("pre-existing output was modified")
				}
			} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("failed seal published an output reservation")
			}
		})
	}
}

type sourceReadFixture struct {
	body       []byte
	readError  error
	closeError error
	closed     int
}

type sourceOutputFixture struct {
	mode   string
	closed int
}

// Write simulates incomplete publication even when an output reports no error.
func (f *sourceOutputFixture) Write(body []byte) (int, error) {
	switch f.mode {
	case "short":
		return len(body) - 1, nil
	case "partial-error":
		return 2, errors.New("partial output")
	case "complete-error":
		return len(body), errors.New("complete-looking failed output")
	}
	return len(body), nil
}

// Chmod injects a metadata failure after otherwise successful output writes.
func (f *sourceOutputFixture) Chmod(os.FileMode) error {
	if f.mode == "mode" {
		return errors.New("mode failure")
	}
	return nil
}

// Sync injects a failed durability operation rather than silently accepting it.
func (f *sourceOutputFixture) Sync() error {
	if f.mode == "sync" {
		return errors.New("flush failure")
	}
	return nil
}

// Close tracks closure and can fail after all earlier publication stages pass.
func (f *sourceOutputFixture) Close() error {
	f.closed++
	if f.mode == "close" {
		return errors.New("close failure")
	}
	return nil
}

// TestSourceOutputCannotSucceedAfterIncompletePublication requires every write,
// metadata, durability and close result to succeed before output is accepted.
func TestSourceOutputCannotSucceedAfterIncompletePublication(t *testing.T) {
	for _, mode := range []string{"valid", "short", "partial-error", "complete-error", "mode", "sync", "close"} {
		t.Run(mode, func(t *testing.T) {
			f := &sourceOutputFixture{mode: mode}
			err := sourceWriteComplete(f, []byte("complete source bytes"), 0644)
			if f.closed != 1 || ((mode == "valid") != (err == nil)) {
				t.Fatalf("incorrect output result or close count: %v / %d", err, f.closed)
			}
		})
	}
}

// Read supplies controlled short content, trailing bytes or read failures.
func (f *sourceReadFixture) Read(p []byte) (int, error) {
	if len(f.body) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.body)
	f.body = f.body[n:]
	return n, f.readError
}

// Close models an input failure that arrives after all expected bytes were read.
func (f *sourceReadFixture) Close() error {
	f.closed++
	return f.closeError
}

// TestSourceReadRequiresCompleteBytesAndSuccessfulClose rejects plausible partial
// success, surplus content and a final input-close failure.
func TestSourceReadRequiresCompleteBytesAndSuccessfulClose(t *testing.T) {
	for _, kind := range []string{"valid", "empty", "complete-error", "partial-error", "truncated", "extra", "close-error", "negative-size", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			reader := &sourceReadFixture{body: []byte("original input")}
			expected := int64(len(reader.body))
			switch kind {
			case "empty":
				reader.body, expected = nil, 0
			case "complete-error":
				reader.readError = errors.New("failed after plausible complete input")
			case "partial-error":
				reader.body = reader.body[:4]
				reader.readError = errors.New("failed after partial input")
			case "truncated":
				reader.body = reader.body[:4]
			case "extra":
				reader.body = append(reader.body, '!')
			case "close-error":
				reader.closeError = errors.New("close failed")
			case "negative-size":
				expected = -1
			case "oversized":
				expected = (128 << 20) + 1
			}
			body, err := sourceReadBody(reader, expected)
			if reader.closed != 1 {
				t.Fatalf("input closed %d times", reader.closed)
			}
			if kind == "valid" || kind == "empty" {
				if err != nil || int64(len(body)) != expected {
					t.Fatalf("valid source refused: %v", err)
				}
			} else if err == nil || body != nil {
				t.Fatal("failed source read returned usable bytes")
			}
		})
	}
}

// TestSourceSealReadbackRequiresItsExactClosedTree rechecks actual bytes, modes
// and membership rather than trusting a previously written manifest alone.
func TestSourceSealReadbackRequiresItsExactClosedTree(t *testing.T) {
	for _, kind := range []string{"valid", "changed", "missing", "extra", "empty-directory", "mode", "link", "wrong-seal", "duplicate-key", "trailing-json"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "sealed")
			seal, err := sourceSealTree(root, output)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(output, "source", "source.go")
			manifestName := filepath.Join(output, "source-manifest.json")
			switch kind {
			case "changed":
				err = os.WriteFile(source, []byte("package other!\n"), 0644)
			case "missing":
				err = os.Remove(source)
			case "extra":
				err = os.WriteFile(filepath.Join(output, "source", "extra"), []byte("unrecorded input"), 0644)
			case "empty-directory":
				err = os.Mkdir(filepath.Join(output, "surplus"), 0755)
			case "mode":
				err = os.Chmod(source, 0755)
			case "link":
				err = os.Rename(source, source+".real")
				if err == nil {
					err = os.Symlink("source.go.real", source)
				}
			case "wrong-seal":
				seal = fmt.Sprintf("%x", sha256.Sum256([]byte("another source")))
			case "duplicate-key", "trailing-json":
				body, readErr := os.ReadFile(manifestName)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if kind == "duplicate-key" {
					body = append([]byte("{\"schemaVersion\":1,"), body[1:]...)
				} else {
					body = append(body, []byte("{}\n")...)
				}
				err = os.Chmod(manifestName, 0644)
				if err == nil {
					err = os.WriteFile(manifestName, body, 0644)
				}
				if err == nil {
					err = os.Chmod(manifestName, 0444)
				}
				seal = fmt.Sprintf("%x", sha256.Sum256(body))
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := sourceVerifyTree(output, seal)
			if kind == "valid" {
				if err != nil || len(got.Files) != 1 || got.Files[0].Path != "source.go" {
					t.Fatalf("original closed source refused: %v", err)
				}
			} else if err == nil || len(got.Files) != 0 {
				t.Fatal("altered source or ambiguous manifest admitted")
			}
		})
	}
}
