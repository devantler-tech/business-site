package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

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

// The complete original/replacement metadata set must match, not just a subset.
func TestModuleRootBinaryJoinRequiresExactCheckedBuildInformation(t *testing.T) {
	for _, kind := range []string{"valid", "missing", "extra", "duplicate", "version", "sum", "replacement", "root", "compiler", "platform", "dynamic", "nil"} {
		t.Run(kind, func(t *testing.T) {
			wanted := []*debug.Module{{Path: "example.com/dep", Version: "v1.2.3", Sum: "h1:original"}, {Path: "example.com/old", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/replaced", Version: "v1.1.0", Sum: "h1:replaced"}}}
			info := &debug.BuildInfo{GoVersion: "go1.26.10", Path: "github.com/zitadel/zitadel", Main: debug.Module{Path: "github.com/zitadel/zitadel", Version: "(devel)"}, Deps: []*debug.Module{{Path: "example.com/dep", Version: "v1.2.3", Sum: "h1:original"}, {Path: "example.com/old", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/replaced", Version: "v1.1.0", Sum: "h1:replaced"}}}, Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"}}}
			switch kind {
			case "missing":
				info.Deps = info.Deps[:1]
			case "extra":
				info.Deps = append(info.Deps, &debug.Module{Path: "example.com/extra", Version: "v1.0.0"})
			case "duplicate":
				info.Deps = append(info.Deps, info.Deps[0])
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
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatalf("admitted %s", kind)
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
			if _, _, err := noticeCapture(f.build, f.recipe, noticeRead, options); err == nil {
				t.Fatal("post-snapshot module replacement admitted")
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
