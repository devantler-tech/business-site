package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

// The selected-module catalogue and production reader policy ship together.
func TestFrozenGoModuleCatalogueSeal(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "third_party", "zitadel-go-module-notices", "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := noticeDigest(body); got != moduleCatalogueHash {
		t.Fatalf("checked-in module catalogue is not admitted by the production reader: got %s, policy %s", got, moduleCatalogueHash)
	}
}

// sourceLicenseFixture adds the immutable root terms to the normal build fixture.
func sourceLicenseFixture(t *testing.T) (noticeFixture, []byte) {
	t.Helper()
	f := newNoticeFixture(t)
	license, err := os.ReadFile("testdata/zitadel-source/LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	writeNotice(t, f.build, "prepared/source/LICENSE", license)
	return f, license
}

// TestSourceLicenseRetainsFrozenPreparedBytes exercises both real CLI phases.
func TestSourceLicenseRetainsFrozenPreparedBytes(t *testing.T) {
	f, license := sourceLicenseFixture(t)
	if len(license) != 34522 || noticeHash(license) != "4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b" {
		t.Fatal("frozen upstream licence fixture differs")
	}
	var snapshot, inventory, diagnostic bytes.Buffer
	args := []string{"--build", f.build, "--recipe", f.recipe, "--source-license"}
	if code := noticeRun(append([]string{"--snapshot"}, args...), &snapshot, &diagnostic); code != 0 {
		t.Fatalf("snapshot failed: %s", &diagnostic)
	}
	writeNotice(t, f.build, noticeBefore, snapshot.Bytes())
	if code := noticeRun(append([]string{"--inventory"}, args...), &inventory, &diagnostic); code != 0 {
		t.Fatalf("capture failed: %s", &diagnostic)
	}
	raw, err := os.ReadFile(filepath.Join(f.build, "zitadel-root-license.txt"))
	if err != nil || !bytes.Equal(raw, license) {
		t.Fatalf("checked licence was not retained verbatim: %v", err)
	}
	var got struct {
		UpstreamCommit string          `json:"upstreamCommit"`
		Inputs         []noticeBinding `json:"inputs"`
		SourceLicense  struct {
			Source        string `json:"source"`
			RawDiagnostic string `json:"rawDiagnostic"`
			Bytes         int    `json:"bytes"`
			SHA256        string `json:"sha256"`
		} `json:"sourceLicense"`
		ImageAdmission string `json:"imageAdmission"`
		LicenseClosure string `json:"licenseClosure"`
		Reproducible   bool   `json:"reproducible"`
	}
	if err := json.Unmarshal(inventory.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.UpstreamCommit != "8afa5dd37a8e723efb9eace8d09fd5c1cccef694" || got.SourceLicense.Source != "prepared/source/LICENSE" || got.SourceLicense.RawDiagnostic != "zitadel-root-license.txt" || got.SourceLicense.Bytes != 34522 || got.SourceLicense.SHA256 != "4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b" || len(got.Inputs) != 9 || got.LicenseClosure != "unknown" || got.ImageAdmission != "unknown" || got.Reproducible {
		t.Fatalf("incomplete same-build boundary: %+v", got)
	}
	if raw, err := os.ReadFile(filepath.Join(f.build, noticeRaw)); err != nil || !bytes.Equal(raw, f.raw) {
		t.Fatal("original console notice changed")
	}
}

// Invalid source bytes must never yield either a usable snapshot or output.
func TestSourceLicenseRefusesInvalidPreparedInputs(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "truncated", "same-size-substitution", "oversized", "linked-file", "linked-parent", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f, license := sourceLicenseFixture(t)
			p := filepath.Join(f.build, "prepared/source/LICENSE")
			switch kind {
			case "missing", "directory":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if kind == "directory" {
					if err := os.Mkdir(p, 0700); err != nil {
						t.Fatal(err)
					}
				}
			case "empty":
				writeNotice(t, f.build, "prepared/source/LICENSE", nil)
			case "truncated":
				writeNotice(t, f.build, "prepared/source/LICENSE", license[:len(license)-1])
			case "same-size-substitution":
				license[0] = '!'
				writeNotice(t, f.build, "prepared/source/LICENSE", license)
			case "oversized":
				writeNotice(t, f.build, "prepared/source/LICENSE", bytes.Repeat([]byte("x"), 65537))
			case "linked-file":
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("LICENSE.real", p); err != nil {
					t.Fatal(err)
				}
			case "linked-parent":
				parent := filepath.Dir(p)
				if err := os.Rename(parent, parent+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("source.real", parent); err != nil {
					t.Fatal(err)
				}
			}
			var out, diagnostic bytes.Buffer
			if code := noticeRun([]string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--source-license"}, &out, &diagnostic); code == 0 || out.Len() != 0 {
				t.Fatalf("admitted invalid %s: %s", kind, &diagnostic)
			}
			if _, err := os.Lstat(filepath.Join(f.build, "zitadel-root-license.txt")); !os.IsNotExist(err) {
				t.Fatal("failed snapshot emitted terms")
			}
		})
	}
}

// Changed/omitted source and failed diagnostic retention cannot finalize capture.
func TestSourceLicenseRefusesIncompleteCapture(t *testing.T) {
	for _, kind := range []string{"changed", "omitted-option", "added-option", "snapshot-text", "snapshot-digest", "snapshot-path", "snapshot-output", "snapshot-omission", "existing-output", "linked-output", "directory-output", "short-output"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := sourceLicenseFixture(t)
			var snapshot, out, diagnostic bytes.Buffer
			args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--source-license"}
			if kind == "added-option" {
				args = args[:5]
			}
			if noticeRun(args, &snapshot, &diagnostic) != 0 {
				t.Fatal(diagnostic.String())
			}
			writeNotice(t, f.build, noticeBefore, snapshot.Bytes())
			args[0] = "--inventory"
			switch kind {
			case "changed":
				writeNotice(t, f.build, "prepared/source/LICENSE", []byte("post-compile replacement"))
			case "omitted-option":
				args = args[:5]
			case "added-option":
				args = append(args, "--source-license")
			case "snapshot-text", "snapshot-digest", "snapshot-path", "snapshot-output", "snapshot-omission":
				changeNoticeJSON(t, f, noticeBefore, func(v map[string]any) {
					if kind == "snapshot-omission" {
						delete(v, "sourceLicense")
						return
					}
					root := v["sourceLicense"].(map[string]any)
					if kind == "snapshot-text" {
						root["text"] = "dW5jaGVja2Vk"
						return
					}
					evidence := root["evidence"].(map[string]any)
					switch kind {
					case "snapshot-digest":
						evidence["sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
					case "snapshot-path":
						evidence["source"] = "other/LICENSE"
					case "snapshot-output":
						evidence["rawDiagnostic"] = "other-license.txt"
					}
				})
			case "existing-output":
				writeNotice(t, f.build, "zitadel-root-license.txt", []byte("preserve existing output"))
			case "linked-output":
				if err := os.Symlink("prepared/source/LICENSE", filepath.Join(f.build, "zitadel-root-license.txt")); err != nil {
					t.Fatal(err)
				}
			case "directory-output":
				if err := os.Mkdir(filepath.Join(f.build, "zitadel-root-license.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			code := 0
			if kind == "short-output" {
				code = noticeRun(args, shortNoticeWriter{}, &diagnostic)
			} else {
				code = noticeRun(args, &out, &diagnostic)
			}
			if code == 0 || out.Len() != 0 {
				t.Fatalf("admitted incomplete %s", kind)
			}
			if kind == "existing-output" {
				data, err := os.ReadFile(filepath.Join(f.build, "zitadel-root-license.txt"))
				if err != nil || string(data) != "preserve existing output" {
					t.Fatal("overwrote prior diagnostic")
				}
			}
			if _, err := os.Stat(filepath.Join(f.build, "build-receipt.json")); !os.IsNotExist(err) {
				t.Fatal("failed capture finalized build receipt")
			}
		})
	}
}

// Checked bytes, not a second unchecked path read, enter the stored snapshot.
func TestSourceLicenseUsesCompleteCheckedRead(t *testing.T) {
	for _, kind := range []string{"checked", "complete-with-error", "partial-with-error", "partial-without-error"} {
		t.Run(kind, func(t *testing.T) {
			f, license := sourceLicenseFixture(t)
			reads := 0
			read := func(root, name string, limit int64) ([]byte, error) {
				data, err := noticeRead(root, name, limit)
				if name == "prepared/source/LICENSE" {
					reads++
					if limit != 65536 {
						t.Fatal("root input is not bounded")
					}
					switch kind {
					case "complete-with-error":
						return data, errors.New("read failed after plausible complete bytes")
					case "partial-with-error":
						return data[:100], errors.New("partial read failed")
					case "partial-without-error":
						return data[:100], nil
					default:
						writeNotice(t, f.build, name, []byte("unchecked replacement"))
					}
				}
				return data, err
			}
			state, err := noticeSnapshot(f.build, f.recipe, read, noticeSupplementConfig{SourceLicense: true})
			if kind != "checked" {
				if err == nil || state.SourceLicense != nil {
					t.Fatal("admitted incomplete/erroring read")
				}
				return
			}
			if err != nil || reads != 1 || state.SourceLicense == nil || !bytes.Equal(state.SourceLicense.Text, license) {
				t.Fatal("checked bytes not preserved")
			}
			writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
			if _, _, err := noticeCapture(f.build, f.recipe, noticeRead, noticeSupplementConfig{SourceLicense: true}); err == nil {
				t.Fatal("post-snapshot replacement admitted")
			}
		})
	}
}

// Ordinary raw-only mode ignores even an invalid root file and emits no root material.
func TestSourceLicenseDefaultOffAndArgumentBoundary(t *testing.T) {
	f := newNoticeFixture(t)
	writeNotice(t, f.build, "prepared/source/LICENSE", []byte("not valid source terms"))
	var out, diagnostic bytes.Buffer
	if noticeRun(nil, &out, &diagnostic) != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("no-argument invocation not inert")
	}
	if noticeRun([]string{"--inventory", "--build", f.build, "--recipe", f.recipe}, &out, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	if bytes.Contains(out.Bytes(), []byte("sourceLicense")) {
		t.Fatal("unflagged inventory includes root material")
	}
	if _, err := os.Stat(filepath.Join(f.build, "zitadel-root-license.txt")); !os.IsNotExist(err) {
		t.Fatal("unflagged capture wrote root material")
	}
	for _, args := range [][]string{{"--source-license"}, {"--snapshot", "--build", f.build, "--recipe", f.recipe, "--source-license", "--source-license"}, {"--snapshot", "--build", f.build, "--recipe", f.recipe, "--source-license", "elsewhere"}} {
		out.Reset()
		if noticeRun(args, &out, &diagnostic) == 0 || out.Len() != 0 {
			t.Fatal("ambiguous root option admitted")
		}
	}
}

type moduleRootFixture struct {
	noticeFixture
	catalogue string
	hash      string
	args      []string
}

// Replay reconstructs a new native cache; it does not transplant the original
// cache or its hash markers. The explicit mode must retain the same checked text.
func TestModuleNoticesUseExplicitRetainedCache(t *testing.T) {
	f := moduleOriginFixture(t)
	var original, retained, diagnostic bytes.Buffer
	if noticeRunWithCatalogues(f.args, &original, &diagnostic, supplementCatalogueHash, f.hash) != 0 {
		t.Fatal(&diagnostic)
	}
	for _, args := range [][]string{
		append(append([]string(nil), f.args...), "--retained-go-cache"),
		{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--retained-go-cache"},
		append(append([]string(nil), f.args...), "--retained-go-cache", "--retained-go-cache"),
	} {
		var out, diagnostic bytes.Buffer
		if noticeRunWithCatalogues(args, &out, &diagnostic, supplementCatalogueHash, f.hash) == 0 || out.Len() != 0 {
			t.Fatal("wrong or ambiguous cache selection admitted")
		}
	}
	destination := filepath.Join(f.build, "go-input-native/go-modules")
	if err := os.Mkdir(filepath.Dir(destination), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.build, "go-modules"), destination); err != nil {
		t.Fatal(err)
	}
	graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	graph = bytes.ReplaceAll(graph, []byte(filepath.Join(f.build, "go-modules")), []byte(destination))
	writeNotice(t, f.build, "packages.jsonl", graph)
	args := append(append([]string(nil), f.args...), "--retained-go-cache")
	if noticeRunWithCatalogues(args, &retained, &diagnostic, supplementCatalogueHash, f.hash) != 0 {
		t.Fatalf("restored native cache refused: %s", &diagnostic)
	}
	var before, after noticeState
	if err := json.Unmarshal(original.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(retained.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	expected := bytes.ReplaceAll(before.Modules.Text, []byte("go-modules/"), []byte("go-input-native/go-modules/"))
	if !bytes.Equal(expected, after.Modules.Text) || len(after.Modules.Evidence.Modules[0].Origins) != 1 {
		t.Fatal("retained cache lost complete terms or attribution")
	}
	for _, module := range after.Modules.Evidence.Modules {
		for _, component := range module.Components {
			if !strings.HasPrefix(component.Source, "go-input-native/go-modules/") {
				t.Fatal("retained text was read from another cache")
			}
		}
	}
	for _, fault := range []string{"implicit-cache", "mixed-cache", "linked-cache", "changed-ziphash"} {
		t.Run(fault, func(t *testing.T) {
			selected := args
			switch fault {
			case "implicit-cache":
				selected = f.args
			case "mixed-cache":
				mixed := bytes.Replace(graph, []byte(destination+"/example.com/!dep"), []byte(filepath.Join(f.build, "go-modules/example.com/!dep")), 1)
				writeNotice(t, f.build, "packages.jsonl", mixed)
				t.Cleanup(func() { writeNotice(t, f.build, "packages.jsonl", graph) })
			case "linked-cache":
				if err := os.Rename(destination, destination+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("go-modules.real", destination); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Remove(destination); err != nil {
						t.Error(err)
					}
					if err := os.Rename(destination+".real", destination); err != nil {
						t.Error(err)
					}
				})
			case "changed-ziphash":
				writeNotice(t, f.build, "go-input-native/go-modules/cache/download/example.com/!dep/@v/v1.2.3.ziphash", []byte("h1:changed"))
			}
			var out, diagnostic bytes.Buffer
			if noticeRunWithCatalogues(selected, &out, &diagnostic, supplementCatalogueHash, f.hash) == 0 || out.Len() != 0 {
				t.Fatalf("unsafe retained cache admitted: %s", &diagnostic)
			}
		})
	}
}

// This literal small graph includes case escaping and the same original/effective
// replacement shape observed in the real Linux compiler output.
func newModuleRootFixture(t *testing.T) moduleRootFixture {
	t.Helper()
	f := moduleRootFixture{noticeFixture: newNoticeFixture(t), catalogue: t.TempDir()}
	sumA := "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	sumB := "h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA="
	modA := "h1:kVBYBnC3LOYbb6uanizvGTp0hqgK1tlCzbD9G5T2BB0="
	modB := "h1:ZDTh2q5IlS2q4dIThM36nCs28O3EuPgmvIBGTq0QERg="
	a := map[string]any{"Path": "example.com/Dep", "Version": "v1.2.3", "Sum": sumA, "GoModSum": modA, "Main": false, "Replace": nil}
	b := map[string]any{"Path": "example.com/replaced", "Version": "v1.1.0", "Sum": sumB, "GoModSum": modB, "Main": false, "Replace": nil}
	old := map[string]any{"Path": "example.com/old", "Version": "v1.0.0", "Sum": "", "GoModSum": "", "Main": false, "Replace": b}
	rootMod := []byte("module github.com/zitadel/zitadel\n")
	rootSum := []byte("example.com/Dep v1.2.3 " + sumA + "\nexample.com/Dep v1.2.3/go.mod " + modA + "\nexample.com/replaced v1.1.0 " + sumB + "\nexample.com/replaced v1.1.0/go.mod " + modB + "\n")
	writeNotice(t, f.build, "prepared/source/go.mod", rootMod)
	writeNotice(t, f.build, "prepared/source/go.sum", rootSum)
	changeNoticeJSON(t, f.noticeFixture, "prepared/source-preparation.json", func(v map[string]any) { v["metadataProfile"] = "machine-id-no-jsonpath-v1" })
	packageBytes, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	packageLines := bytes.Split(bytes.TrimSuffix(packageBytes, []byte("\n")), []byte("\n"))
	var consolePackage map[string]any
	if err = json.Unmarshal(packageLines[1], &consolePackage); err != nil {
		t.Fatal(err)
	}
	consolePackage["Module"] = map[string]any{"Path": "github.com/zitadel/zitadel", "Main": true, "Dir": filepath.Join(f.build, "prepared/source"), "GoMod": filepath.Join(f.build, "prepared/source/go.mod")}
	writeNotice(t, f.build, "packages.jsonl", append([]byte("{\"ImportPath\":\"fmt\",\"Standard\":true}\n"), noticeJSON(t, consolePackage)...))
	license := []byte("A complete fixture copyright and grant.\r\n")
	patent := []byte("A complete independent patent notice.\n")
	component := func(name string, body []byte) map[string]any {
		return map[string]any{"file": name, "bytes": len(body), "sha256": noticeHash(body)}
	}
	catalogue := map[string]any{"schemaVersion": 1, "upstreamCommit": noticePin, "metadataProfile": "machine-id-no-jsonpath-v1", "goModSha256": noticeHash(rootMod), "goSumSha256": noticeHash(rootSum), "modules": []any{
		map[string]any{"original": a, "effective": a, "components": []any{component("LICENSE", license), component("PATENTS", patent)}},
		map[string]any{"original": old, "effective": b, "components": []any{component("License", license)}},
	}}
	data := noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	for _, row := range []struct {
		path, version, escaped, sum, modSum, mod string
		original                                 map[string]any
		files                                    map[string][]byte
	}{
		{"example.com/Dep", "v1.2.3", "example.com/!dep", sumA, modA, "module example.com/Dep\n", a, map[string][]byte{"LICENSE": license, "PATENTS": patent}},
		{"example.com/replaced", "v1.1.0", "example.com/replaced", sumB, modB, "module example.com/replaced\n", old, map[string][]byte{"License": license}},
	} {
		dir := "go-modules/" + row.escaped + "@" + row.version
		cache := "go-modules/cache/download/" + row.escaped + "/@v/" + row.version
		for name, body := range row.files {
			writeNotice(t, f.build, dir+"/"+name, body)
		}
		writeNotice(t, f.build, cache+".mod", []byte(row.mod))
		writeNotice(t, f.build, cache+".ziphash", []byte(row.sum))
		observed := map[string]any{}
		for key, value := range row.original {
			observed[key] = value
		}
		observed["Dir"] = filepath.Join(f.build, dir)
		observed["GoMod"] = filepath.Join(f.build, cache+".mod")
		if replacement, ok := observed["Replace"].(map[string]any); ok {
			copy := map[string]any{}
			for key, value := range replacement {
				copy[key] = value
			}
			copy["Dir"] = observed["Dir"]
			copy["GoMod"] = observed["GoMod"]
			observed["Replace"] = copy
		}
		p := filepath.Join(f.build, "packages.jsonl")
		prior, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		writeNotice(t, f.build, "packages.jsonl", append(prior, noticeJSON(t, map[string]any{"ImportPath": row.original["Path"], "Dir": observed["Dir"], "Module": observed})...))
	}
	f.args = []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--modules", f.catalogue}
	return f
}

// This package and notice are independent fixture inputs, not derived from the
// production applicability checks. Both original and replaced modules use it.
func nestedModuleFixture(t *testing.T, index int) moduleRootFixture {
	t.Helper()
	f := newModuleRootFixture(t)
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue map[string]any
	if err := json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	entry := catalogue["modules"].([]any)[index].(map[string]any)
	entry["components"] = append(entry["components"].([]any), map[string]any{
		"file": "nested/NOTICE", "package": "nested", "bytes": 39,
		"sha256": noticeHash([]byte("An independent nested copyright grant.\n")),
	})
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(graph, []byte("\n")), []byte("\n"))
	var observed map[string]any
	if err := json.Unmarshal(lines[index+2], &observed); err != nil {
		t.Fatal(err)
	}
	cache := "go-modules/example.com/!dep@v1.2.3"
	if index == 1 {
		cache = "go-modules/example.com/replaced@v1.1.0"
	}
	observed["ImportPath"] = observed["ImportPath"].(string) + "/nested"
	observed["Dir"] = filepath.Join(f.build, cache, "nested")
	writeNotice(t, f.build, "packages.jsonl", append(graph, noticeJSON(t, observed)...))
	writeNotice(t, f.build, cache+"/nested/NOTICE", []byte("An independent nested copyright grant.\n"))
	return f
}

func TestNestedModuleNoticesRetainSelectedPackageAndCompleteBytes(t *testing.T) {
	for index := range 2 {
		t.Run(fmt.Sprintf("module-%d", index), func(t *testing.T) {
			f := nestedModuleFixture(t, index)
			var out, diagnostic bytes.Buffer
			if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code != 0 {
				t.Fatalf("selected nested notice refused: %s", &diagnostic)
			}
			var got struct {
				Modules struct {
					Text     []byte `json:"text"`
					Evidence struct {
						Modules []struct {
							Components []struct {
								Source, Package string
								Offset, Bytes   int
							} `json:"components"`
						} `json:"modules"`
					} `json:"evidence"`
				} `json:"modules"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			components := got.Modules.Evidence.Modules[index].Components
			section := components[len(components)-1]
			if section.Package != "nested" || section.Bytes != 39 || !strings.HasSuffix(section.Source, "/nested/NOTICE") || !bytes.Equal(got.Modules.Text[section.Offset:section.Offset+section.Bytes], []byte("An independent nested copyright grant.\n")) {
				t.Fatal("nested notice lost complete bytes or selected-package provenance")
			}
		})
	}
}

// An import identity cannot claim a different directory within the cache.
func TestModulePackageDirectoryMustMatchItsImportIdentity(t *testing.T) {
	f := newModuleRootFixture(t)
	data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	var observed map[string]any
	if err := json.Unmarshal(lines[2], &observed); err != nil {
		t.Fatal(err)
	}
	observed["Dir"] = observed["Dir"].(string) + "/unrelated"
	lines[2] = bytes.TrimSuffix(noticeJSON(t, observed), []byte("\n"))
	writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 {
		t.Fatal("package admitted from a directory different from its import identity")
	}
}

func TestNestedNoticeCanBelongToASelectedDescendantPackage(t *testing.T) {
	f := nestedModuleFixture(t, 0)
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue noticeModuleCatalogue
	if err := json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	components := catalogue.Modules[0].Components
	components[len(components)-1].Package = "nested/child"
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(graph, []byte("\n")), []byte("\n"))
	var observed map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &observed); err != nil {
		t.Fatal(err)
	}
	observed["ImportPath"] = "example.com/Dep/nested/child"
	observed["Dir"] = filepath.Join(f.build, "go-modules/example.com/!dep@v1.2.3/nested/child")
	lines[len(lines)-1] = bytes.TrimSuffix(noticeJSON(t, observed), []byte("\n"))
	writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code != 0 {
		t.Fatalf("ancestor notice refused for selected descendant: %s", &diagnostic)
	}
	var state noticeState
	if err := json.Unmarshal(out.Bytes(), &state); err != nil || state.Modules == nil {
		t.Fatalf("missing nested snapshot: %v", err)
	}
	section := state.Modules.Evidence.Modules[0].Components[len(components)-1]
	if section.Package != "nested/child" || section.Bytes != 39 || !bytes.Equal(state.Modules.Text[section.Offset:section.Offset+section.Bytes], []byte("An independent nested copyright grant.\n")) {
		t.Fatal("ancestor notice lost selected-descendant provenance or complete bytes")
	}
}

func TestModuleImportSuffixCannotAliasItsRoot(t *testing.T) {
	f := newModuleRootFixture(t)
	data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	var observed map[string]any
	if err := json.Unmarshal(lines[2], &observed); err != nil {
		t.Fatal(err)
	}
	observed["ImportPath"] = "example.com/Dep/"
	lines[2] = bytes.TrimSuffix(noticeJSON(t, observed), []byte("\n"))
	writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 {
		t.Fatal("noncanonical import suffix aliases the module root")
	}
}

func TestNestedModuleNoticesRefuseUnboundOrChangedComponents(t *testing.T) {
	for _, kind := range []string{"unselected", "unselected-child", "selected-sibling", "wrong-package", "missing-package", "parent-path", "absolute-path", "unclean-path", "backslash", "space", "control", "root-as-nested", "duplicate", "truncated", "same-size-change", "missing", "linked-file", "linked-directory"} {
		t.Run(kind, func(t *testing.T) {
			f := nestedModuleFixture(t, 0)
			data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
			if err != nil {
				t.Fatal(err)
			}
			var catalogue noticeModuleCatalogue
			if err := json.Unmarshal(data, &catalogue); err != nil {
				t.Fatal(err)
			}
			components := catalogue.Modules[0].Components
			component := &components[len(components)-1]
			target := filepath.Join(f.build, "go-modules/example.com/!dep@v1.2.3/nested/NOTICE")
			switch kind {
			case "unselected":
				graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(graph, []byte("\n")), []byte("\n"))
				writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines[:len(lines)-1], []byte("\n")), '\n'))
			case "wrong-package":
				component.Package = "other"
			case "unselected-child":
				component.Package = "nested/child"
			case "selected-sibling":
				component.Package = "nested-other"
				graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(graph, []byte("\n")), []byte("\n"))
				var observed map[string]any
				if err := json.Unmarshal(lines[len(lines)-1], &observed); err != nil {
					t.Fatal(err)
				}
				observed["ImportPath"] = "example.com/Dep/nested-other"
				observed["Dir"] = filepath.Join(f.build, "go-modules/example.com/!dep@v1.2.3/nested-other")
				lines[len(lines)-1] = bytes.TrimSuffix(noticeJSON(t, observed), []byte("\n"))
				writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
			case "missing-package":
				component.Package = ""
			case "parent-path":
				component.File, component.Package = "../NOTICE", ".."
			case "absolute-path":
				component.File, component.Package = "/nested/NOTICE", "/nested"
			case "unclean-path":
				component.File, component.Package = "nested/../nested/NOTICE", "nested/../nested"
			case "backslash":
				component.File, component.Package = "nested\\NOTICE", "nested"
			case "space":
				component.File, component.Package = "nested folder/NOTICE", "nested folder"
			case "control":
				component.File, component.Package = "nested\x00/NOTICE", "nested\x00"
			case "root-as-nested":
				components[0].Package = "nested"
			case "duplicate":
				catalogue.Modules[0].Components = append(components, *component)
			case "truncated":
				writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/nested/NOTICE", []byte("An independent nested copyright"))
			case "same-size-change":
				writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/nested/NOTICE", []byte("Xn independent nested copyright grant.\n"))
			case "missing":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			case "linked-file", "linked-directory":
				if kind == "linked-directory" {
					target = filepath.Dir(target)
				}
				if err := os.Rename(target, target+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(target)+".real", target); err != nil {
					t.Fatal(err)
				}
			}
			data = noticeJSON(t, catalogue)
			f.hash = noticeHash(data)
			writeNotice(t, f.catalogue, "catalogue.json", data)
			var out, diagnostic bytes.Buffer
			if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 {
				t.Fatalf("admitted %s: %s", kind, &diagnostic)
			}
			if !strings.Contains(diagnostic.String(), "component") && !strings.Contains(diagnostic.String(), "selected module terms") {
				t.Fatalf("%s did not reach its component boundary: %s", kind, &diagnostic)
			}
		})
	}
}

func TestNestedModuleNoticeReaderErrorCannotAdmitPlausibleBytes(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete-%v", complete), func(t *testing.T) {
			f := nestedModuleFixture(t, 0)
			reader := func(root, name string, limit int64) ([]byte, error) {
				data, err := noticeRead(root, name, limit)
				if name == "go-modules/example.com/!dep@v1.2.3/nested/NOTICE" {
					if !complete {
						data = data[:5]
					}
					return data, errors.New("read failed after plausible bytes")
				}
				return data, err
			}
			state, err := noticeSnapshot(f.build, f.recipe, reader, noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash})
			if err == nil || state.Modules != nil || !strings.Contains(err.Error(), "selected module terms") {
				t.Fatalf("erroring nested read was admitted or failed elsewhere: %v", err)
			}
		})
	}
}

// Removing module retention must fail this consumer-visible snapshot assertion.
func TestModuleRootSnapshotPreservesAllIdentitiesAndVerbatimComponents(t *testing.T) {
	f := newModuleRootFixture(t)
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code != 0 {
		t.Fatalf("valid module snapshot refused: %s", &diagnostic)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	modules, ok := got["modules"].(map[string]any)
	if !ok {
		t.Fatal("snapshot omitted selected modules")
	}
	evidence := modules["evidence"].(map[string]any)
	if evidence["catalogueSha256"] != f.hash || evidence["rawDiagnostic"] != "go-module-notices.txt" {
		t.Fatal("missing module provenance")
	}
	rows := evidence["modules"].([]any)
	if len(rows) != 2 {
		t.Fatal("incomplete selected module identities")
	}
	first := rows[0].(map[string]any)
	if first["original"].(map[string]any)["Path"] != "example.com/Dep" || len(first["components"].([]any)) != 2 {
		t.Fatal("first module components lost")
	}
	second := rows[1].(map[string]any)
	if second["original"].(map[string]any)["Path"] != "example.com/old" || second["effective"].(map[string]any)["Path"] != "example.com/replaced" {
		t.Fatal("replacement identity lost")
	}
	var text []byte
	encoded, _ := json.Marshal(modules["text"])
	if err := json.Unmarshal(encoded, &text); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(text, []byte("A complete fixture copyright and grant.\r\n")) != 2 || !bytes.Contains(text, []byte("A complete independent patent notice.\n")) {
		t.Fatal("complete verbatim terms not retained for every source")
	}
	if _, err := os.Lstat(filepath.Join(f.build, "go-module-notices.txt")); !os.IsNotExist(err) {
		t.Fatal("snapshot published a capture output")
	}
}

// Dropping origin retention must lose these literal attribution bytes, while
// compiler-unselected source and changed full-file inputs must be refused.
func moduleOriginFixture(t *testing.T) moduleRootFixture {
	t.Helper()
	f := newModuleRootFixture(t)
	source := []byte("package dep\n\n// Origin notice.\n\nfunc Unchanged() {}\n")
	writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/adapted.go", source)
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue map[string]any
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	entry := catalogue["modules"].([]any)[0].(map[string]any)
	entry["origins"] = []any{map[string]any{
		"file": "adapted.go", "package": "example.com/Dep", "bytes": len(source), "sha256": noticeHash(source),
		"spans":        []any{map[string]any{"offset": 13, "bytes": 18, "sha256": noticeHash([]byte("// Origin notice.\n"))}},
		"terms":        []any{map[string]any{"module": "example.com/Dep", "file": "LICENSE", "sha256": noticeHash([]byte("A complete fixture copyright and grant.\r\n"))}},
		"notices":      []any{map[string]any{"file": "incorporated/ORIGINAL.txt", "upstream": "https://example.org/LICENSE", "bytes": 33, "sha256": noticeHash([]byte("Additional full original grant.\r\n"))}},
		"sourceInputs": []any{map[string]any{"file": "incorporated/source.dat", "upstream": "https://example.org/source.dat", "bytes": 29, "sha256": noticeHash([]byte("Corresponding source payload\n"))}},
		"inputs": []any{
			map[string]any{"file": "data/rules", "kind": "embed", "bytes": 6, "sha256": noticeHash([]byte("rules\n"))},
			map[string]any{"file": "gen.go", "kind": "generator", "bytes": 21, "sha256": noticeHash([]byte("//go:build ignore\n//\n"))},
		},
	}}
	writeNotice(t, f.catalogue, "incorporated/ORIGINAL.txt", []byte("Additional full original grant.\r\n"))
	writeNotice(t, f.catalogue, "incorporated/source.dat", []byte("Corresponding source payload\n"))
	writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/data/rules", []byte("rules\n"))
	writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/gen.go", []byte("//go:build ignore\n//\n"))
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(graph, []byte("\n")), []byte("\n"))
	var selected map[string]any
	if err = json.Unmarshal(lines[2], &selected); err != nil {
		t.Fatal(err)
	}
	selected["GoFiles"] = []string{"adapted.go"}
	selected["EmbedFiles"] = []string{"data/rules"}
	selected["IgnoredGoFiles"] = []string{"gen.go"}
	lines[2] = bytes.TrimSuffix(noticeJSON(t, selected), []byte("\n"))
	writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
	return f
}

func TestModuleOriginsRetainSelectedSourceSpans(t *testing.T) {
	f := moduleOriginFixture(t)
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code != 0 {
		t.Fatalf("selected source attribution was not retained: %s", &diagnostic)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	modules := got["modules"].(map[string]any)
	var text []byte
	encoded, _ := json.Marshal(modules["text"])
	if err := json.Unmarshal(encoded, &text); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(text, []byte("// Origin notice.\n")) != 1 || bytes.Contains(text, []byte("func Unchanged")) {
		t.Fatal("attribution was omitted, duplicated or replaced by source code")
	}
	if bytes.Count(text, []byte("Additional full original grant.\r\n")) != 1 || bytes.Contains(text, []byte("Corresponding source payload")) || bytes.Contains(text, []byte("rules\n")) {
		t.Fatal("full notice missing or corresponding-source/embed bytes leaked into diagnostics")
	}
	if !bytes.Contains(text, []byte("https://example.org/source.dat")) {
		t.Fatal("recipient cannot find the verified corresponding-source input")
	}
	rows := modules["evidence"].(map[string]any)["modules"].([]any)
	origins := rows[0].(map[string]any)["origins"].([]any)
	origin := origins[0].(map[string]any)
	if len(origins) != 1 || origin["package"] != "example.com/Dep" || origin["source"] != "go-modules/example.com/!dep@v1.2.3/adapted.go" {
		t.Fatal("actual source/package identity was lost")
	}
}

// A curated JSON catalogue may spell an absent optional list as []. The
// pre-compile snapshot must survive the same JSON save/read used after build.
func TestModuleOriginsSnapshotRoundTripsEmptyOptionalArrays(t *testing.T) {
	for _, field := range []string{"terms", "notices", "sourceInputs", "inputs"} {
		t.Run(field, func(t *testing.T) {
			f := moduleOriginFixture(t)
			data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
			if err != nil {
				t.Fatal(err)
			}
			var catalogue map[string]any
			if err = json.Unmarshal(data, &catalogue); err != nil {
				t.Fatal(err)
			}
			origin := catalogue["modules"].([]any)[0].(map[string]any)["origins"].([]any)[0].(map[string]any)
			origin[field] = []any{}
			data = noticeJSON(t, catalogue)
			f.hash = noticeHash(data)
			writeNotice(t, f.catalogue, "catalogue.json", data)
			state, err := noticeSnapshot(f.build, f.recipe, noticeRead, noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash})
			if err != nil {
				t.Fatal(err)
			}
			var saved noticeState
			if err = json.Unmarshal(noticeJSON(t, state), &saved); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state, saved) {
				t.Fatal("unchanged compile snapshot differs after JSON save/read")
			}
		})
	}
}

func TestModuleOriginsCaptureStillRefusesChangedRecordedAttribution(t *testing.T) {
	for _, field := range []string{"terms", "sourceInputs", "inputs"} {
		t.Run(field, func(t *testing.T) {
			f := moduleOriginFixture(t)
			options := noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash}
			state, err := noticeSnapshot(f.build, f.recipe, noticeRead, options)
			if err != nil {
				t.Fatal(err)
			}
			writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
			changeNoticeJSON(t, f.noticeFixture, noticeBefore, func(value map[string]any) {
				rows := value["modules"].(map[string]any)["evidence"].(map[string]any)["modules"].([]any)
				origin := rows[0].(map[string]any)["origins"].([]any)[0].(map[string]any)
				delete(origin, field)
			})
			binaryReads := 0
			read := func(root, name string, limit int64) ([]byte, error) {
				if name == "service/zitadel" {
					binaryReads++
				}
				return noticeRead(root, name, limit)
			}
			if _, _, err = noticeCapture(f.build, f.recipe, read, options); err == nil || err.Error() != "compile snapshot changed" || binaryReads != 0 {
				t.Fatalf("changed attribution reached capture: binary reads=%d, error=%v", binaryReads, err)
			}
		})
	}
}

func changeModuleOrigins(t *testing.T, f *moduleRootFixture, change func(*noticeModuleOrigin)) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue noticeModuleCatalogue
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	change(&catalogue.Modules[0].Origins[0])
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
}

func TestModuleOriginsRefuseChangedOrUnselectedInputs(t *testing.T) {
	for _, kind := range []string{
		"foreign-package", "unselected-go-file", "traversal", "whole-source-changed", "source-missing", "source-linked",
		"negative-span", "escaping-span", "overlapping-span", "wrong-span-hash", "no-spans", "missing-terms", "wrong-terms",
		"changed-notice", "notice-truncated", "notice-linked", "notice-query", "notice-credentials", "notice-traversal",
		"changed-source-input", "cross-purpose-input", "wrong-embed-kind", "changed-embed", "changed-generator", "compiled-generator",
	} {
		t.Run(kind, func(t *testing.T) {
			f := moduleOriginFixture(t)
			cache := "go-modules/example.com/!dep@v1.2.3/"
			want := "module origin"
			changeModuleOrigins(t, &f, func(o *noticeModuleOrigin) {
				switch kind {
				case "foreign-package":
					o.Package = "example.com/foreign"
				case "unselected-go-file":
					o.File = "unselected.go"
				case "traversal":
					o.File = "../adapted.go"
				case "negative-span":
					o.Spans[0].Offset = -1
				case "escaping-span":
					o.Spans[0].Offset = int(^uint(0) >> 1)
				case "overlapping-span":
					o.Spans = append(o.Spans, o.Spans[0])
				case "wrong-span-hash":
					o.Spans[0].SHA256 = strings.Repeat("0", 64)
				case "no-spans":
					o.Spans = nil
				case "missing-terms":
					o.Terms[0].Module = "example.com/foreign"
				case "wrong-terms":
					o.Terms[0].SHA256 = strings.Repeat("0", 64)
				case "notice-query":
					o.Notices[0].Upstream += "?secret=value"
				case "notice-credentials":
					o.Notices[0].Upstream = "https://user:password@example.org/LICENSE"
				case "notice-traversal":
					o.Notices[0].File = "../ORIGINAL.txt"
				case "cross-purpose-input":
					o.SourceInputs = append(o.SourceInputs, o.Notices[0])
					o.SourceInputs[0], o.SourceInputs[1] = o.SourceInputs[1], o.SourceInputs[0]
				case "wrong-embed-kind":
					o.Inputs[0].Kind = "generator"
				}
			})
			switch kind {
			case "whole-source-changed":
				writeNotice(t, f.build, cache+"adapted.go", []byte("package dep\n\n// Origin notice.\n\nfunc Different() {}\n"))
			case "source-missing":
				if err := os.Remove(filepath.Join(f.build, cache+"adapted.go")); err != nil {
					t.Fatal(err)
				}
			case "source-linked":
				p := filepath.Join(f.build, cache+"adapted.go")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("adapted.go.real", p); err != nil {
					t.Fatal(err)
				}
			case "changed-notice", "notice-truncated":
				text := []byte("Additional full substituted grant.\r\n")
				if kind == "notice-truncated" {
					text = []byte("Additional")
				}
				writeNotice(t, f.catalogue, "incorporated/ORIGINAL.txt", text)
			case "notice-linked":
				p := filepath.Join(f.catalogue, "incorporated/ORIGINAL.txt")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("ORIGINAL.txt.real", p); err != nil {
					t.Fatal(err)
				}
			case "changed-source-input":
				writeNotice(t, f.catalogue, "incorporated/source.dat", []byte("changed corresponding source\n"))
			case "changed-embed":
				writeNotice(t, f.build, cache+"data/rules", []byte("other\n"))
			case "changed-generator":
				writeNotice(t, f.build, cache+"gen.go", []byte("//go:build different\n"))
			case "compiled-generator":
				data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`"GoFiles":["adapted.go"]`), []byte(`"GoFiles":["adapted.go","gen.go"]`), 1)
				writeNotice(t, f.build, "packages.jsonl", data)
			}
			var out, diagnostic bytes.Buffer
			if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), want) {
				t.Fatalf("bad origin admitted or failed elsewhere: code=%d output=%d diagnostic=%s", code, out.Len(), &diagnostic)
			}
		})
	}
}

func TestModuleOriginReaderFailureCannotAdmitPlausibleBytes(t *testing.T) {
	for _, target := range []string{"adapted.go", "ORIGINAL.txt", "source.dat", "data/rules", "gen.go"} {
		t.Run(target, func(t *testing.T) {
			f := moduleOriginFixture(t)
			reader := func(root, name string, limit int64) ([]byte, error) {
				b, err := noticeRead(root, name, limit)
				if strings.HasSuffix(name, target) && err == nil {
					return b, fmt.Errorf("plausible partial read followed by failure")
				}
				return b, err
			}
			state, err := noticeSnapshot(f.build, f.recipe, reader, noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash})
			if err == nil || state.Modules != nil || !strings.Contains(err.Error(), "module origin") {
				t.Fatalf("failed origin read admitted or failed elsewhere: %v", err)
			}
		})
	}
}

func TestModuleOriginSourceRoleCannotChangeAcrossOrigins(t *testing.T) {
	f := moduleOriginFixture(t)
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue noticeModuleCatalogue
	if err := json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	second := catalogue.Modules[0].Origins[0]
	second.File = "zeta.go"
	body := []byte("package dep\n\n// Origin notice.\n\nfunc Unchanged2() {}\n")
	second.Bytes, second.SHA256 = len(body), noticeHash(body)
	second.Notices, second.SourceInputs = second.SourceInputs, nil
	catalogue.Modules[0].Origins = append(catalogue.Modules[0].Origins, second)
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/zeta.go", body)
	graph, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	graph = bytes.Replace(graph, []byte(`"GoFiles":["adapted.go"]`), []byte(`"GoFiles":["adapted.go","zeta.go"]`), 1)
	writeNotice(t, f.build, "packages.jsonl", graph)
	var out, diagnostic bytes.Buffer
	if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "purpose") {
		t.Fatalf("corresponding source became a notice in another origin: code=%d bytes=%d diagnostic=%s", code, out.Len(), &diagnostic)
	}
}

// Individually valid retained texts can exceed the encoded snapshot budget.
// Refuse that combination before emitting a snapshot and starting compilation.
func TestModuleRootSnapshotRefusesOversizedCombinedTextsBeforeOutput(t *testing.T) {
	f := newModuleRootFixture(t)
	raw := bytes.Repeat([]byte("A"), 2<<20)
	writeNotice(t, f.build, "prepared/source/"+noticeConsole, raw)
	writeNotice(t, f.build, "prepared/source/"+noticeEmbedded, raw)
	data, err := os.ReadFile(filepath.Join(f.catalogue, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue noticeModuleCatalogue
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	original := catalogue.Modules[0].Components
	components := append([]noticeModuleComponent(nil), original[0])
	text := bytes.Repeat([]byte("B"), 64<<10)
	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("NOTICE-%02d", i)
		writeNotice(t, f.build, "go-modules/example.com/!dep@v1.2.3/"+name, text)
		components = append(components, noticeModuleComponent{File: name, Bytes: len(text), SHA256: noticeHash(text)})
	}
	catalogue.Modules[0].Components = append(components, original[1])
	data = noticeJSON(t, catalogue)
	f.hash = noticeHash(data)
	writeNotice(t, f.catalogue, "catalogue.json", data)
	state, err := noticeSnapshot(f.build, f.recipe, noticeRead, noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash})
	if err != nil || state.Modules == nil || len(state.Modules.Text) > 2<<20 {
		t.Fatalf("individually valid fixture refused: %v", err)
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil || len(encoded)+1 <= 4<<20 {
		t.Fatalf("fixture does not exercise the aggregate snapshot limit: %v", err)
	}
	var out, diagnostic bytes.Buffer
	code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash)
	if code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "snapshot") || !strings.Contains(diagnostic.String(), "limit") {
		t.Fatalf("oversized aggregate snapshot was not refused before output: code=%d bytes=%d diagnostic=%s", code, out.Len(), &diagnostic)
	}
}

// A plausible prefix or one valid licence cannot hide a changed graph or input.
func TestModuleRootSnapshotRefusesIncompleteOrContradictoryEvidence(t *testing.T) {
	for _, kind := range []string{"missing-terms", "truncated-terms", "same-size-substitution", "linked-terms", "linked-cache", "escaping-directory", "missing-module", "extra-module", "contradictory-module", "duplicate-package", "wrong-version", "wrong-sum", "wrong-replacement", "wrong-mod-sum", "changed-root-mod", "changed-root-sum", "changed-cache-mod", "changed-ziphash", "catalogue-substitution", "missing-catalogue", "graph-producer-prefix"} {
		t.Run(kind, func(t *testing.T) {
			f := newModuleRootFixture(t)
			terms := "go-modules/example.com/!dep@v1.2.3/LICENSE"
			switch kind {
			case "missing-terms":
				if err := os.Remove(filepath.Join(f.build, terms)); err != nil {
					t.Fatal(err)
				}
			case "truncated-terms":
				writeNotice(t, f.build, terms, []byte("A complete fixture copyright"))
			case "same-size-substitution":
				writeNotice(t, f.build, terms, []byte("X complete fixture copyright and grant.\r\n"))
			case "linked-terms":
				if err := os.Rename(filepath.Join(f.build, terms), filepath.Join(f.build, terms)+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("LICENSE.real", filepath.Join(f.build, terms)); err != nil {
					t.Fatal(err)
				}
			case "linked-cache":
				p := filepath.Join(f.build, "go-modules")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("go-modules.real", p); err != nil {
					t.Fatal(err)
				}
			case "changed-root-mod":
				writeNotice(t, f.build, "prepared/source/go.mod", []byte("module other\n"))
			case "changed-root-sum":
				writeNotice(t, f.build, "prepared/source/go.sum", []byte("other v1.0.0 h1:plausible\n"))
			case "changed-cache-mod":
				writeNotice(t, f.build, "go-modules/cache/download/example.com/!dep/@v/v1.2.3.mod", []byte("module different\n"))
			case "changed-ziphash":
				writeNotice(t, f.build, "go-modules/cache/download/example.com/!dep/@v/v1.2.3.ziphash", []byte("h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCA="))
			case "catalogue-substitution":
				writeNotice(t, f.catalogue, "catalogue.json", []byte("{}\n"))
			case "missing-catalogue":
				if err := os.Remove(filepath.Join(f.catalogue, "catalogue.json")); err != nil {
					t.Fatal(err)
				}
			default:
				data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
				var observed map[string]any
				if err = json.Unmarshal(lines[2], &observed); err != nil {
					t.Fatal(err)
				}
				module := observed["Module"].(map[string]any)
				switch kind {
				case "escaping-directory":
					module["Dir"] = filepath.Join(f.build, "../outside")
				case "missing-module":
					lines = lines[:2]
				case "extra-module":
					module["Path"] = "example.com/extra"
				case "wrong-version":
					module["Version"] = "v9.9.9"
				case "wrong-sum":
					module["Sum"] = "h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCA="
				case "wrong-mod-sum":
					module["GoModSum"] = "h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCA="
				case "wrong-replacement":
					module["Replace"] = map[string]any{"Path": "example.com/other", "Version": "v1.0.0"}
				case "graph-producer-prefix":
					lines = append(lines, []byte(`{"ImportPath":`))
				case "contradictory-module":
					duplicate := noticeJSON(t, observed)
					module["Sum"] = "h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCA="
					lines = append(lines, bytes.TrimSuffix(duplicate, []byte("\n")))
				case "duplicate-package":
					lines = append(lines, lines[2])
				}
				if kind != "missing-module" && kind != "duplicate-package" && kind != "graph-producer-prefix" {
					lines[2] = bytes.TrimSuffix(noticeJSON(t, observed), []byte("\n"))
				}
				writeNotice(t, f.build, "packages.jsonl", append(bytes.Join(lines, []byte("\n")), '\n'))
			}
			var out, diagnostic bytes.Buffer
			if code := noticeRunWithCatalogues(f.args, &out, &diagnostic, supplementCatalogueHash, f.hash); code == 0 || out.Len() != 0 {
				t.Fatalf("admitted %s: %s", kind, &diagnostic)
			}
		})
	}
}

// Each invalid fixture must reach its intended identity, target or dependency
// boundary; an earlier, unrelated rejection must not stand in for that proof.
func TestModuleRootBinaryJoinRequiresExactCheckedBuildInformation(t *testing.T) {
	for _, test := range []struct {
		name string
		want string
	}{
		{"valid", ""},
		{"missing", "module binary identity differs"},
		{"surplus", "module binary identity differs"},
		{"extra", "binary dependency set differs"},
		{"duplicate", "binary dependency set differs"},
		{"version", "binary dependency set differs"},
		{"sum", "binary dependency set differs"},
		{"replacement", "binary dependency set differs"},
		{"root", "module binary identity differs"},
		{"compiler", "module binary identity differs"},
		{"platform", "module binary target differs"},
		{"dynamic", "module binary target differs"},
		{"nil", "module binary identity differs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			wanted := []*debug.Module{{Path: "example.com/dep", Version: "v1.2.3", Sum: "h1:original"}, {Path: "example.com/old", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/replaced", Version: "v1.1.0", Sum: "h1:replaced"}}}
			info := &debug.BuildInfo{GoVersion: "go1.26.10", Path: "github.com/zitadel/zitadel", Main: debug.Module{Path: "github.com/zitadel/zitadel", Version: "(devel)"}, Deps: []*debug.Module{{Path: "example.com/dep", Version: "v1.2.3", Sum: "h1:original"}, {Path: "example.com/old", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/replaced", Version: "v1.1.0", Sum: "h1:replaced"}}}, Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"}}}
			switch test.name {
			case "missing":
				info.Deps = info.Deps[:1]
			case "surplus":
				info.Deps = append(info.Deps, &debug.Module{Path: "example.com/extra", Version: "v1.0.0"})
			case "extra":
				info.Deps[1] = &debug.Module{Path: "example.com/extra", Version: "v1.0.0"}
			case "duplicate":
				info.Deps[1] = info.Deps[0]
			case "version":
				info.Deps[0].Version = "v2.0.0"
			case "sum":
				info.Deps[0].Sum = "h1:other"
			case "replacement":
				info.Deps[1].Replace.Path = "example.com/other"
			case "root":
				info.Main.Path = "example.com/unrelated"
			case "compiler":
				info.GoVersion = "go1.26.9"
			case "platform":
				info.Settings[0].Value = "darwin"
			case "dynamic":
				info.Settings[2].Value = "1"
			case "nil":
				info = nil
			}
			err := noticeJoinBuildInfo(info, wanted)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != test.want {
				t.Fatalf("wanted rejection %q, got %v", test.want, err)
			}
		})
	}
}

func TestModuleRootDefaultOffAndOptionBoundary(t *testing.T) {
	f := newNoticeFixture(t)
	var out, diagnostic bytes.Buffer
	if noticeRun(nil, &out, &diagnostic) != 0 || out.Len() != 0 {
		t.Fatal("default invocation is not inert")
	}
	if noticeRun([]string{"--inventory", "--build", f.build, "--recipe", f.recipe}, &out, &diagnostic) != 0 {
		t.Fatal(&diagnostic)
	}
	if strings.Contains(out.String(), `"modules"`) {
		t.Fatal("unflagged capture includes module evidence")
	}
	for _, args := range [][]string{{"--modules"}, {"--snapshot", "--build", f.build, "--recipe", f.recipe, "--modules", "one", "--modules", "two"}, {"--snapshot", "--build", f.build, "--recipe", f.recipe, "--modules", ""}} {
		out.Reset()
		if noticeRun(args, &out, &diagnostic) == 0 || out.Len() != 0 {
			t.Fatal("ambiguous module selection admitted")
		}
	}
}

// A reader that returns plausible bytes and an error is not a successful read;
// checked bytes must be retained rather than reopening their changed pathname.
func TestModuleRootCheckedReadsAndRevalidation(t *testing.T) {
	for _, kind := range []string{"checked", "complete-error", "partial-error", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			f := newModuleRootFixture(t)
			target := "go-modules/example.com/!dep@v1.2.3/LICENSE"
			reads := 0
			reader := func(root, name string, limit int64) ([]byte, error) {
				data, err := noticeRead(root, name, limit)
				if name == target {
					reads++
					switch kind {
					case "complete-error":
						return data, errors.New("read failed after complete bytes")
					case "partial-error":
						return data[:5], errors.New("partial read failed")
					case "truncated":
						return data[:5], nil
					default:
						writeNotice(t, f.build, name, []byte("unchecked replacement"))
					}
				}
				return data, err
			}
			options := noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash}
			state, err := noticeSnapshot(f.build, f.recipe, reader, options)
			if kind != "checked" {
				if err == nil || state.Modules != nil {
					t.Fatal("admitted incomplete/erroring module read")
				}
				return
			}
			if err != nil || reads != 1 || state.Modules == nil || !bytes.Contains(state.Modules.Text, []byte("A complete fixture copyright and grant.\r\n")) {
				t.Fatalf("checked bytes were not retained: %v", err)
			}
			writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
			binaryReads := 0
			captureReader := func(root, name string, limit int64) ([]byte, error) {
				if name == "service/zitadel" {
					binaryReads++
				}
				return noticeRead(root, name, limit)
			}
			if _, _, err := noticeCapture(f.build, f.recipe, captureReader, options); err == nil || binaryReads != 0 {
				t.Fatal("post-snapshot module replacement reached binary capture")
			}
		})
	}
}

// Module selection must inspect the same checked graph whose digest is retained,
// not a replacement read that can hide an extra unreviewed dependency.
func TestModuleRootUsesTheAlreadyCheckedPackageObservation(t *testing.T) {
	for _, kind := range []string{"checked-then-invalid", "unreviewed-then-valid"} {
		t.Run(kind, func(t *testing.T) {
			f := newModuleRootFixture(t)
			original, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			first, later := original, []byte("{}\n")
			if kind == "unreviewed-then-valid" {
				first = append(append([]byte(nil), original...), []byte("{\"ImportPath\":\"unreviewed.example/pkg\",\"Module\":{\"Path\":\"unreviewed.example/pkg\",\"Version\":\"v1.0.0\"}}\n")...)
				later = original
			}
			reads := 0
			reader := func(root, name string, limit int64) ([]byte, error) {
				if root == f.build && name == "packages.jsonl" {
					reads++
					if reads == 1 {
						return first, nil
					}
					return later, nil
				}
				return noticeRead(root, name, limit)
			}
			state, err := noticeSnapshot(f.build, f.recipe, reader, noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash})
			if kind == "unreviewed-then-valid" {
				if err == nil {
					t.Fatal("unreviewed checked graph hidden by a later valid read")
				}
				return
			}
			if err != nil || reads != 1 || state.PackagesSHA256 != noticeHash(first) || state.Modules == nil {
				t.Fatalf("module selection did not use checked package bytes: reads=%d, error=%v", reads, err)
			}
		})
	}
}

// Refuse changed module snapshots/options before consulting any service binary.
func TestModuleRootCaptureRejectsChangedSnapshotBeforeBinaryRead(t *testing.T) {
	for _, kind := range []string{"omitted-option", "added-option", "changed-text", "changed-digest", "changed-path", "missing-snapshot-module"} {
		t.Run(kind, func(t *testing.T) {
			f := newModuleRootFixture(t)
			options := noticeSupplementConfig{ModuleDirectory: f.catalogue, ModuleCatalogueHash: f.hash}
			beforeOptions := options
			if kind == "added-option" {
				beforeOptions = noticeSupplementConfig{}
			}
			state, err := noticeSnapshot(f.build, f.recipe, noticeRead, beforeOptions)
			if err != nil {
				t.Fatal(err)
			}
			writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
			if kind == "omitted-option" {
				options = noticeSupplementConfig{}
			}
			if strings.HasPrefix(kind, "changed-") || kind == "missing-snapshot-module" {
				changeNoticeJSON(t, f.noticeFixture, noticeBefore, func(value map[string]any) {
					if kind == "missing-snapshot-module" {
						delete(value, "modules")
						return
					}
					modules := value["modules"].(map[string]any)
					if kind == "changed-text" {
						modules["text"] = "dW5jaGVja2Vk"
						return
					}
					evidence := modules["evidence"].(map[string]any)
					if kind == "changed-digest" {
						evidence["sha256"] = strings.Repeat("0", 64)
					} else {
						evidence["rawDiagnostic"] = "other.txt"
					}
				})
			}
			binaryReads := 0
			reader := func(root, name string, limit int64) ([]byte, error) {
				if name == "service/zitadel" {
					binaryReads++
				}
				return noticeRead(root, name, limit)
			}
			if _, _, err := noticeCapture(f.build, f.recipe, reader, options); err == nil || binaryReads != 0 {
				t.Fatal("changed snapshot reached binary capture")
			}
		})
	}
}
