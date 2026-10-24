package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
