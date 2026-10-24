package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newToolchainRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"LICENSE", "PATENTS"} {
		data, err := os.ReadFile(filepath.Join("testdata", "go1.26.10", name))
		if err != nil {
			t.Fatal(err)
		}
		writeNotice(t, root, name, data)
	}
	writeNotice(t, root, "VERSION", []byte("go1.26.10\ntime 2026-10-01T00:00:00Z\n"))
	return root
}

// A successful explicit capture must preserve both complete distribution terms,
// not merely report the compiler version or continue without a retained output.
func TestToolchainNoticeCLIProducesBoundVerbatimOutput(t *testing.T) {
	f := newNoticeFixture(t)
	root := newToolchainRoot(t)
	var out, diagnostic bytes.Buffer
	args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--toolchain", root}
	if rc := noticeRun(args, &out, &diagnostic); rc != 0 {
		t.Fatalf("valid explicit toolchain snapshot refused: %s", &diagnostic)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil || snapshot["toolchain"] == nil {
		t.Fatalf("toolchain snapshot missing: %v", err)
	}
	writeNotice(t, f.build, noticeBefore, out.Bytes())
	out.Reset()
	diagnostic.Reset()
	args[0] = "--inventory"
	if rc := noticeRun(args, &out, &diagnostic); rc != 0 {
		t.Fatalf("valid explicit toolchain capture refused: %s", &diagnostic)
	}
	var inventory map[string]any
	if err := json.Unmarshal(out.Bytes(), &inventory); err != nil || inventory["toolchain"] == nil {
		t.Fatalf("toolchain inventory missing: %v", err)
	}
	var decoded noticeInventory
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(f.build, "go-toolchain-notices.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"LICENSE", "PATENTS", "VERSION"} {
		original, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Contains(text, original) {
			t.Fatalf("complete %s bytes lost: %v", name, err)
		}
	}
	proof := decoded.Toolchain
	if proof.Version != "1.26.10" || proof.RawDiagnostic != "go-toolchain-notices.txt" || proof.Bytes != len(text) || proof.SHA256 != noticeHash(text) || len(proof.Sections) != 3 || len(decoded.Inputs) != 9 {
		t.Fatal("incomplete toolchain/build binding")
	}
	for i, name := range []string{"LICENSE", "PATENTS", "VERSION"} {
		section := proof.Sections[i]
		original, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || section.Source != "GOROOT/"+name || section.Bytes != len(original) || section.SHA256 != noticeHash(original) || section.Offset < 0 || section.Offset > len(text) || section.Bytes > len(text)-section.Offset || !bytes.Equal(text[section.Offset:section.Offset+section.Bytes], original) {
			t.Fatalf("invalid %s span", name)
		}
	}
	original, err := os.ReadFile(filepath.Join(f.build, noticeRaw))
	if err != nil || !bytes.Equal(original, f.raw) {
		t.Fatal("original Angular notice changed")
	}
	if rc := noticeRun(args, &out, &diagnostic); rc == 0 {
		t.Fatal("existing toolchain diagnostic overwritten")
	}
}

func TestToolchainCaptureRefusesPostSnapshotChanges(t *testing.T) {
	for _, change := range []string{"LICENSE", "PATENTS", "VERSION", "omitted-option", "wrong-compiler-receipt", "existing-output"} {
		t.Run(change, func(t *testing.T) {
			f := newNoticeFixture(t)
			root := newToolchainRoot(t)
			var out, diagnostic bytes.Buffer
			args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--toolchain", root}
			if rc := noticeRun(args, &out, &diagnostic); rc != 0 {
				t.Fatal(diagnostic.String())
			}
			writeNotice(t, f.build, noticeBefore, out.Bytes())
			out.Reset()
			args[0] = "--inventory"
			switch change {
			case "VERSION":
				writeNotice(t, root, "VERSION", []byte("go1.26.10\ntime 2026-10-02T00:00:00Z\n"))
			case "omitted-option":
				args = args[:6]
			case "wrong-compiler-receipt":
				changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["tools"].(map[string]any)["go"] = "1.26.11" })
			case "existing-output":
				if err := os.Mkdir(filepath.Join(f.build, "go-toolchain-notices.txt"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				writeNotice(t, root, change, []byte("post-snapshot replacement\n"))
			}
			if rc := noticeRun(args, &out, &diagnostic); rc == 0 {
				t.Fatalf("admitted %s", change)
			}
			if change == "existing-output" && out.Len() != 0 {
				t.Fatal("emitted a complete inventory before diagnostic retention succeeded")
			}
			if _, err := os.Stat(filepath.Join(f.build, "build-receipt.json")); !os.IsNotExist(err) {
				t.Fatal("failed capture finalized a receipt")
			}
		})
	}
}

func TestToolchainAndSupplementShareTheActualCaptureBoundary(t *testing.T) {
	f := newSupplementFixture(t)
	root := newToolchainRoot(t)
	var out, diagnostic bytes.Buffer
	args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--supplement", f.notices, "--toolchain", root}
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatal(diagnostic.String())
	}
	var snapshot noticeState
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil || snapshot.Supplement == nil || snapshot.Toolchain == nil {
		t.Fatal("combined snapshot incomplete")
	}
	writeNotice(t, f.build, noticeBefore, out.Bytes())
	out.Reset()
	args[0] = "--inventory"
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatal(diagnostic.String())
	}
	for _, name := range []string{noticeRaw, supplementRaw, "go-toolchain-notices.txt"} {
		if data, err := os.ReadFile(filepath.Join(f.build, name)); err != nil || len(data) == 0 {
			t.Fatalf("missing %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(f.build, supplementInventoryPending))
	var proof struct {
		NoticeInventorySHA256 string `json:"noticeInventorySha256"`
	}
	if err != nil || json.Unmarshal(data, &proof) != nil || proof.NoticeInventorySHA256 != noticeHash(out.Bytes()) {
		t.Fatal("combined inventory hash does not bind final output")
	}
}

func TestToolchainCaptureUsesCheckedReadsAndRejectsPartialErrors(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			root := newToolchainRoot(t)
			original, err := os.ReadFile(filepath.Join(root, "LICENSE"))
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			read := func(directory, name string, limit int64) ([]byte, error) {
				data, err := noticeRead(directory, name, limit)
				if name == "LICENSE" {
					reads++
					if failed {
						return data, errors.New("failure after plausible complete input")
					}
					writeNotice(t, root, name, []byte("unchecked replacement"))
				}
				return data, err
			}
			state, err := noticeToolchainSnapshot(root, read)
			if failed {
				if err == nil || state != nil {
					t.Fatal("partial read error admitted")
				}
				return
			}
			if err != nil || reads != 1 || !bytes.Contains(state.Text, original) || bytes.Contains(state.Text, []byte("unchecked replacement")) {
				t.Fatal("unchecked reread used")
			}
		})
	}
}

// An otherwise plausible VERSION prefix must not admit unrelated trailing text;
// invalid distribution inputs must never produce a usable snapshot or output.
func TestToolchainRefusesInvalidDistributionInputs(t *testing.T) {
	for _, kind := range []string{"missing-license", "empty-patents", "truncated-license", "substituted-patents", "wrong-version", "version-append", "invalid-time", "linked-file", "linked-root", "oversized-version"} {
		t.Run(kind, func(t *testing.T) {
			f := newNoticeFixture(t)
			root := newToolchainRoot(t)
			switch kind {
			case "missing-license":
				if err := os.Remove(filepath.Join(root, "LICENSE")); err != nil {
					t.Fatal(err)
				}
			case "empty-patents":
				writeNotice(t, root, "PATENTS", nil)
			case "truncated-license":
				writeNotice(t, root, "LICENSE", []byte("Copyright 2009 The Go Authors.\n"))
			case "substituted-patents":
				writeNotice(t, root, "PATENTS", []byte("different terms\n"))
			case "wrong-version":
				writeNotice(t, root, "VERSION", []byte("go1.26.11\n"))
			case "version-append":
				writeNotice(t, root, "VERSION", []byte("go1.26.10\nnot distribution metadata\n"))
			case "invalid-time":
				writeNotice(t, root, "VERSION", []byte("go1.26.10\ntime not-a-time\n"))
			case "oversized-version":
				writeNotice(t, root, "VERSION", []byte("go1.26.10\n"+strings.Repeat("x", 20<<10)))
			case "linked-file":
				p := filepath.Join(root, "LICENSE")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("LICENSE.real", p); err != nil {
					t.Fatal(err)
				}
			case "linked-root":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, alias); err != nil {
					t.Fatal(err)
				}
				root = alias
			}
			var out, diagnostic bytes.Buffer
			if rc := noticeRun([]string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--toolchain", root}, &out, &diagnostic); rc == 0 || out.Len() != 0 {
				t.Fatalf("invalid %s admitted", kind)
			}
			if _, err := os.Stat(filepath.Join(f.build, "go-toolchain-notices.txt")); !os.IsNotExist(err) {
				t.Fatal("failed snapshot emitted terms")
			}
		})
	}
}
