package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noticeHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

type noticeFixture struct {
	build, recipe string
	raw           []byte
}

func writeNotice(t *testing.T, root, name string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func noticeJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func newNoticeFixture(t *testing.T) noticeFixture {
	t.Helper()
	f := noticeFixture{build: t.TempDir(), raw: []byte("package-one\r\nMIT\n  copyright preserved\n\npackage-two\nApache text\n")}
	f.recipe = filepath.Join(f.build, "recipe", "build-zitadel-linux.sh")
	writeNotice(t, filepath.Dir(f.recipe), filepath.Base(f.recipe), []byte("fixture recipe\n"))
	for _, p := range []string{noticeConsole, noticeEmbedded} {
		writeNotice(t, f.build, "prepared/source/"+p, f.raw)
	}
	lock := []byte("frozen dependency lock\n")
	writeNotice(t, f.build, "prepared/source/pnpm-lock.yaml", lock)
	writeNotice(t, f.build, "inputs.sha256", []byte(noticeHash(lock)+"  pnpm-lock.yaml\n"))
	prep := map[string]any{"schemaVersion": 1, "upstreamCommit": noticePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "recipeSha256": strings.Repeat("a", 64), "inputManifestSha256": strings.Repeat("b", 64)}
	writeNotice(t, f.build, "prepared/source-preparation.json", noticeJSON(t, prep))
	packages := map[string]any{"ImportPath": "github.com/zitadel/zitadel/internal/api/ui/console", "Dir": filepath.Join(f.build, "prepared/source/internal/api/ui/console"), "EmbedPatterns": []string{"static"}, "EmbedFiles": []string{"static/index.html", "static/3rdpartylicenses.txt"}}
	writeNotice(t, f.build, "packages.jsonl", append([]byte("{\"ImportPath\":\"other\"}\n"), noticeJSON(t, packages)...))
	writeNotice(t, f.build, "generated.sha256", []byte(noticeHash(f.raw)+"  "+noticeConsole+"\n"+noticeHash(f.raw)+"  "+noticeEmbedded+"\n"))
	binary := []byte("actual selected fixture binary\n")
	writeNotice(t, f.build, "service/zitadel", binary)
	receipt := map[string]any{"schemaVersion": 1, "upstreamCommit": noticePin, "version": "4.20.0", "sourceDate": "2026-10-22T07:05:44Z", "platform": "linux/amd64", "profile": "api-login-v1-console", "preparation": prep, "recipeSha256": noticeHash([]byte("fixture recipe\n")), "binarySha256": noticeHash(binary), "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "tools": map[string]string{"go": "1.26.10", "node": "22.23.3", "pnpm": "10.30.3"}}
	writeNotice(t, f.build, "build-receipt.pending", noticeJSON(t, receipt))
	snapshot, err := noticeSnapshot(f.build, f.recipe, noticeRead)
	if err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}
	writeNotice(t, f.build, noticeBefore, noticeJSON(t, snapshot))
	return f
}

func TestNoticeCapturePreservesExactBytesAndBindings(t *testing.T) {
	f := newNoticeFixture(t)
	got, raw, err := noticeCapture(f.build, f.recipe, noticeRead)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, f.raw) || got.SHA256 != noticeHash(f.raw) || got.Bytes != len(f.raw) {
		t.Fatal("raw bytes were not preserved")
	}
	if got.UpstreamCommit != noticePin || got.Profile != "api-login-v1-console" || got.ImageAdmission != "unknown" || got.LicenseClosure != "unknown" || got.Reproducible {
		t.Fatal("invalid capture boundary")
	}
	if strings.Join(got.OriginalPaths, ",") != noticeConsole+","+noticeEmbedded || got.RawDiagnostic != noticeRaw || len(got.Inputs) != 9 {
		t.Fatalf("incomplete binding: %+v", got)
	}
	again, rawAgain, err := noticeCapture(f.build, f.recipe, noticeRead)
	if err != nil || !bytes.Equal(raw, rawAgain) || !bytes.Equal(noticeJSON(t, got), noticeJSON(t, again)) {
		t.Fatal("capture is nondeterministic")
	}
}

// A package must recheck completed build inputs without rewriting diagnostics.
func TestCompletedNoticeVerificationIsReadOnly(t *testing.T) {
	f := newNoticeFixture(t)
	var inventory, diagnostic bytes.Buffer
	args := []string{"--build", f.build, "--recipe", f.recipe}
	if noticeRun(append([]string{"--inventory"}, args...), &inventory, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	writeNotice(t, f.build, "console-notices-inventory.json", inventory.Bytes())
	if err := os.Rename(filepath.Join(f.build, "build-receipt.pending"), filepath.Join(f.build, "build-receipt.json")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(f.build, noticeRaw))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var out, errors bytes.Buffer
		if rc := noticeRun(append([]string{"--verify"}, args...), &out, &errors); rc != 0 {
			t.Fatalf("completed verification failed: %s", &errors)
		}
		if !bytes.Equal(out.Bytes(), inventory.Bytes()) {
			t.Fatal("verification did not return the complete unchanged inventory")
		}
	}
	after, err := os.ReadFile(filepath.Join(f.build, noticeRaw))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("verification changed retained diagnostics")
	}
}

func TestCompletedNoticeVerificationRefusesChangedEvidence(t *testing.T) {
	for _, kind := range []string{"pending", "ambiguous", "missing-inventory", "changed-inventory", "changed-text", "linked-text", "changed-binary", "conflicting-mode"} {
		t.Run(kind, func(t *testing.T) {
			f := newNoticeFixture(t)
			var inventory, diagnostic bytes.Buffer
			args := []string{"--build", f.build, "--recipe", f.recipe}
			if noticeRun(append([]string{"--inventory"}, args...), &inventory, &diagnostic) != 0 {
				t.Fatal(diagnostic.String())
			}
			writeNotice(t, f.build, "console-notices-inventory.json", inventory.Bytes())
			if kind != "pending" {
				if err := os.Rename(filepath.Join(f.build, "build-receipt.pending"), filepath.Join(f.build, "build-receipt.json")); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "ambiguous":
				writeNotice(t, f.build, "build-receipt.pending", []byte("{}"))
			case "missing-inventory":
				if err := os.Remove(filepath.Join(f.build, "console-notices-inventory.json")); err != nil {
					t.Fatal(err)
				}
			case "changed-inventory":
				writeNotice(t, f.build, "console-notices-inventory.json", append(inventory.Bytes(), '\n'))
			case "changed-text":
				writeNotice(t, f.build, noticeRaw, []byte("plausible retained terms\n"))
			case "linked-text":
				p := filepath.Join(f.build, noticeRaw)
				if err := os.Rename(p, p+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".original", p); err != nil {
					t.Fatal(err)
				}
			case "changed-binary":
				writeNotice(t, f.build, "service/zitadel", []byte("other selected fixture binary\n"))
			case "conflicting-mode":
				args = append(args, "--inventory")
			}
			var out, errors bytes.Buffer
			if rc := noticeRun(append([]string{"--verify"}, args...), &out, &errors); rc == 0 || out.Len() != 0 {
				t.Fatalf("invalid completed evidence accepted: %s", kind)
			}
		})
	}
}

func changeNoticeJSON(t *testing.T, f noticeFixture, path string, change func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.build, path))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	change(v)
	writeNotice(t, f.build, path, noticeJSON(t, v))
}

func TestNoticeCaptureRejectsWrongAndIncompleteEvidence(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*testing.T, noticeFixture)
	}{
		{"missing", "notice input", func(t *testing.T, f noticeFixture) {
			if err := os.Remove(filepath.Join(f.build, "prepared/source/"+noticeConsole)); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty", "notice input", func(t *testing.T, f noticeFixture) { writeNotice(t, f.build, "prepared/source/"+noticeConsole, nil) }},
		{"oversized", "notice input", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "prepared/source/"+noticeConsole, bytes.Repeat([]byte("x"), noticeLimit+1))
		}},
		{"directory", "notice input", func(t *testing.T, f noticeFixture) {
			p := filepath.Join(f.build, "prepared/source/"+noticeConsole)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"linked-file", "notice input", func(t *testing.T, f noticeFixture) {
			p := filepath.Join(f.build, "prepared/source/"+noticeConsole)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(f.build, "prepared/source/"+noticeEmbedded), p); err != nil {
				t.Fatal(err)
			}
		}},
		{"linked-parent", "notice input", func(t *testing.T, f noticeFixture) {
			p := filepath.Join(f.build, "prepared/source/console/dist/console")
			if err := os.Rename(p, p+"-real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(p+"-real", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"divergent-copy", "notice copies", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "prepared/source/"+noticeEmbedded, []byte("other notice\n"))
		}},
		{"after-compile-replacement", "compile snapshot", func(t *testing.T, f noticeFixture) {
			for _, p := range []string{noticeConsole, noticeEmbedded} {
				writeNotice(t, f.build, "prepared/source/"+p, []byte("changed after compile\n"))
			}
		}},
		{"missing-manifest-member", "generated manifest", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "generated.sha256", []byte(noticeHash(f.raw)+"  "+noticeConsole+"\n"))
		}},
		{"duplicate-manifest-member", "generated manifest", func(t *testing.T, f noticeFixture) {
			p := filepath.Join(f.build, "generated.sha256")
			b, _ := os.ReadFile(p)
			writeNotice(t, f.build, "generated.sha256", append(b, []byte(noticeHash(f.raw)+"  "+noticeConsole+"\n")...))
		}},
		{"substituted-manifest-member", "generated manifest", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "generated.sha256", []byte(strings.Repeat("0", 64)+"  "+noticeConsole+"\n"+noticeHash(f.raw)+"  "+noticeEmbedded+"\n"))
		}},
		{"malformed-manifest", "generated manifest", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "generated.sha256", []byte("not a digest\n"))
		}},
		{"changed-lock", "frozen lock", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "prepared/source/pnpm-lock.yaml", []byte("different lock\n"))
		}},
		{"duplicate-lock-observation", "frozen lock", func(t *testing.T, f noticeFixture) {
			p := filepath.Join(f.build, "inputs.sha256")
			b, _ := os.ReadFile(p)
			writeNotice(t, f.build, "inputs.sha256", append(b, b...))
		}},
		{"wrong-profile", "build receipt", func(t *testing.T, f noticeFixture) {
			changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["profile"] = "other" })
		}},
		{"wrong-platform", "build receipt", func(t *testing.T, f noticeFixture) {
			changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["platform"] = "darwin/arm64" })
		}},
		{"wrong-upstream", "build receipt", func(t *testing.T, f noticeFixture) {
			changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["upstreamCommit"] = strings.Repeat("0", 40) })
		}},
		{"nested-preparation-disagreement", "build receipt", func(t *testing.T, f noticeFixture) {
			changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["preparation"].(map[string]any)["recipeSha256"] = strings.Repeat("c", 64) })
		}},
		{"binary-from-another-build", "binary binding", func(t *testing.T, f noticeFixture) {
			writeNotice(t, f.build, "service/zitadel", []byte("different binary\n"))
		}},
		{"wrong-recipe", "recipe binding", func(t *testing.T, f noticeFixture) {
			changeNoticeJSON(t, f, "build-receipt.pending", func(v map[string]any) { v["recipeSha256"] = strings.Repeat("0", 64) })
		}},
		{"ambiguous-receipt", "ambiguous build receipt", func(t *testing.T, f noticeFixture) {
			b, _ := os.ReadFile(filepath.Join(f.build, "build-receipt.pending"))
			writeNotice(t, f.build, "build-receipt.json", b)
		}},
		{"missing-receipt", "build receipt", func(t *testing.T, f noticeFixture) {
			if err := os.Remove(filepath.Join(f.build, "build-receipt.pending")); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate-receipt-key", "JSON", func(t *testing.T, f noticeFixture) {
			b, _ := os.ReadFile(filepath.Join(f.build, "build-receipt.pending"))
			writeNotice(t, f.build, "build-receipt.pending", append([]byte("{\"schemaVersion\":1,"), b[1:]...))
		}},
		{"trailing-receipt", "JSON", func(t *testing.T, f noticeFixture) {
			b, _ := os.ReadFile(filepath.Join(f.build, "build-receipt.pending"))
			writeNotice(t, f.build, "build-receipt.pending", append(b, []byte("{}")...))
		}},
		{"truncated-snapshot", "JSON", func(t *testing.T, f noticeFixture) { writeNotice(t, f.build, noticeBefore, []byte("{")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newNoticeFixture(t)
			tc.change(t, f)
			_, _, err := noticeCapture(f.build, f.recipe, noticeRead)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q refusal, got %v", tc.want, err)
			}
		})
	}
}

func TestNoticeCompilerObservationsAreCompleteAndUnambiguous(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "wrong-directory", "wrong-pattern", "missing-member", "duplicate-member", "trailing", "truncated", "duplicate-key", "incomplete", "package-error", "dependency-error", "other-package-error"} {
		t.Run(kind, func(t *testing.T) {
			f := newNoticeFixture(t)
			original, _ := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
			var selected map[string]any
			if err := json.Unmarshal(bytes.SplitN(original, []byte("\n"), 2)[1], &selected); err != nil {
				t.Fatal(err)
			}
			var data []byte
			switch kind {
			case "missing":
				data = []byte("{}\n")
			case "duplicate":
				data = append(original, noticeJSON(t, selected)...)
			case "wrong-directory":
				selected["Dir"] = "wrong"
				data = noticeJSON(t, selected)
			case "wrong-pattern":
				selected["EmbedPatterns"] = []string{"other"}
				data = noticeJSON(t, selected)
			case "missing-member":
				selected["EmbedFiles"] = []string{"static/index.html"}
				data = noticeJSON(t, selected)
			case "duplicate-member":
				selected["EmbedFiles"] = []string{"static/3rdpartylicenses.txt", "static/3rdpartylicenses.txt"}
				data = noticeJSON(t, selected)
			case "trailing":
				data = append(original, []byte("garbage")...)
			case "truncated":
				data = append(original, []byte("{\"ImportPath\":")...)
			case "duplicate-key":
				b := noticeJSON(t, selected)
				data = append([]byte("{\"ImportPath\":\"other\","), b[1:]...)
			case "incomplete":
				selected["Incomplete"] = true
				data = noticeJSON(t, selected)
			case "package-error":
				selected["Error"] = map[string]any{"Err": "fixture compiler failure"}
				data = noticeJSON(t, selected)
			case "dependency-error":
				selected["DepsErrors"] = []any{map[string]any{"Err": "fixture dependency failure"}}
				data = noticeJSON(t, selected)
			case "other-package-error":
				data = append(original, []byte("{\"ImportPath\":\"other\",\"Incomplete\":true}\n")...)
			}
			writeNotice(t, f.build, "packages.jsonl", data)
			_, err := noticeSnapshot(f.build, f.recipe, noticeRead)
			if err == nil || !strings.Contains(err.Error(), "compiler observation") {
				t.Fatalf("wanted compiler-observation refusal, got %v", err)
			}
		})
	}
}

type brokenNoticeReader struct {
	io.Reader
	readErr, closeErr error
}

func (r brokenNoticeReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	if e == io.EOF && r.readErr != nil {
		return n, r.readErr
	}
	return n, e
}
func (r brokenNoticeReader) Close() error { return r.closeErr }

func TestNoticeCompleteReadRefusesPartialErrorAndClose(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reader      io.ReadCloser
		size, limit int64
	}{
		{"short", io.NopCloser(strings.NewReader("short")), 8, 100},
		{"growing", io.NopCloser(strings.NewReader("longer")), 3, 100},
		{"oversized", io.NopCloser(strings.NewReader("longer")), 6, 4},
		{"error-after-prefix", brokenNoticeReader{Reader: strings.NewReader("prefix"), readErr: errors.New("read failed")}, 6, 100},
		{"close-error", brokenNoticeReader{Reader: strings.NewReader("prefix"), closeErr: errors.New("close failed")}, 6, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := noticeReadAll(tc.reader, tc.size, tc.limit); e == nil {
				t.Fatal("incomplete read admitted")
			}
		})
	}
}

func TestNoticeOutputUsesCheckedSnapshot(t *testing.T) {
	f := newNoticeFixture(t)
	reads := map[string]int{}
	reader := func(root, path string, limit int64) ([]byte, error) {
		b, e := noticeRead(root, path, limit)
		reads[path]++
		if path == "prepared/source/"+noticeConsole && e == nil {
			writeNotice(t, f.build, path, []byte("replacement after checked read\n"))
		}
		return b, e
	}
	_, raw, e := noticeCapture(f.build, f.recipe, reader)
	if e != nil || !bytes.Equal(raw, f.raw) || reads["prepared/source/"+noticeConsole] != 1 {
		t.Fatalf("capture reopened unchecked notice: %v", e)
	}
}

func TestNoticeCommandIsInertAndExplicit(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if rc := noticeRun(nil, &out, &diagnostic); rc != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("inactive command was not inert")
	}
	for _, args := range [][]string{{"--inventory"}, {"--snapshot", "--inventory"}, {"--unknown"}} {
		if rc := noticeRun(args, &out, &diagnostic); rc == 0 {
			t.Fatal("incomplete mode admitted")
		}
	}
}

func TestNoticeCommandCapturesRawAndRefusesOverwrite(t *testing.T) {
	f := newNoticeFixture(t)
	var out, diagnostic bytes.Buffer
	if rc := noticeRun([]string{"--snapshot", "--build", f.build, "--recipe", f.recipe}, &out, &diagnostic); rc != 0 {
		t.Fatalf("snapshot: %s", diagnostic.String())
	}
	var state noticeState
	if err := json.Unmarshal(out.Bytes(), &state); err != nil || !bytes.Equal(state.Raw, f.raw) {
		t.Fatalf("snapshot output: %v", err)
	}
	out.Reset()
	diagnostic.Reset()
	args := []string{"--inventory", "--build", f.build, "--recipe", f.recipe}
	if rc := noticeRun(args, &out, &diagnostic); rc != 0 {
		t.Fatalf("capture: %s", diagnostic.String())
	}
	raw, err := os.ReadFile(filepath.Join(f.build, noticeRaw))
	if err != nil || !bytes.Equal(raw, f.raw) {
		t.Fatalf("raw diagnostic: %v", err)
	}
	var inventory noticeInventory
	if err = json.Unmarshal(out.Bytes(), &inventory); err != nil || inventory.SHA256 != noticeHash(raw) {
		t.Fatalf("inventory output: %v", err)
	}
	out.Reset()
	diagnostic.Reset()
	if rc := noticeRun(args, &out, &diagnostic); rc == 0 || out.Len() != 0 {
		t.Fatal("existing raw diagnostic overwritten")
	}
	after, _ := os.ReadFile(filepath.Join(f.build, noticeRaw))
	if !bytes.Equal(after, raw) {
		t.Fatal("existing output changed")
	}
}

func TestNoticeCaptureRejectsCoherentPostCompileSubstitution(t *testing.T) {
	for _, kind := range []string{"lock-and-manifest", "compiler-stream", "preparation", "recipe"} {
		t.Run(kind, func(t *testing.T) {
			f := newNoticeFixture(t)
			switch kind {
			case "lock-and-manifest":
				b := []byte("new coherent lock\n")
				writeNotice(t, f.build, "prepared/source/pnpm-lock.yaml", b)
				writeNotice(t, f.build, "inputs.sha256", []byte(noticeHash(b)+"  pnpm-lock.yaml\n"))
			case "compiler-stream":
				b, _ := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
				writeNotice(t, f.build, "packages.jsonl", append(b, []byte("{\"ImportPath\":\"another-valid-package\"}\n")...))
			case "preparation":
				changeNoticeJSON(t, f, "prepared/source-preparation.json", func(v map[string]any) { v["recipeSha256"] = strings.Repeat("c", 64) })
			case "recipe":
				writeNotice(t, filepath.Dir(f.recipe), filepath.Base(f.recipe), []byte("different recipe\n"))
			}
			_, _, err := noticeCapture(f.build, f.recipe, noticeRead)
			if err == nil || !strings.Contains(err.Error(), "compile snapshot") {
				t.Fatalf("post-compile %s substitution admitted: %v", kind, err)
			}
		})
	}
}

func TestNoticeSnapshotRejectsInvalidPreparationAndText(t *testing.T) {
	for _, kind := range []string{"wrong-preparation", "missing-preparation-hash", "invalid-utf8", "read-error-after-prefix"} {
		t.Run(kind, func(t *testing.T) {
			f := newNoticeFixture(t)
			reader := noticeReader(noticeRead)
			switch kind {
			case "wrong-preparation":
				changeNoticeJSON(t, f, "prepared/source-preparation.json", func(v map[string]any) { v["upstreamCommit"] = strings.Repeat("0", 40) })
			case "missing-preparation-hash":
				changeNoticeJSON(t, f, "prepared/source-preparation.json", func(v map[string]any) { delete(v, "inputManifestSha256") })
			case "invalid-utf8":
				writeNotice(t, f.build, "prepared/source/"+noticeConsole, []byte{0xff})
			case "read-error-after-prefix":
				reader = func(root, path string, limit int64) ([]byte, error) {
					b, e := noticeRead(root, path, limit)
					if path == "prepared/source/"+noticeConsole {
						return b, errors.New("late read failure")
					}
					return b, e
				}
			}
			if _, err := noticeSnapshot(f.build, f.recipe, reader); err == nil {
				t.Fatalf("invalid %s admitted", kind)
			}
		})
	}
}

func TestNoticeCompletedReceiptIsTheSameBinding(t *testing.T) {
	f := newNoticeFixture(t)
	first, raw, err := noticeCapture(f.build, f.recipe, noticeRead)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(f.build, "build-receipt.pending"), filepath.Join(f.build, "build-receipt.json")); err != nil {
		t.Fatal(err)
	}
	second, again, err := noticeCapture(f.build, f.recipe, noticeRead)
	if err != nil || !bytes.Equal(raw, again) || !bytes.Equal(noticeJSON(t, first), noticeJSON(t, second)) {
		t.Fatalf("receipt finalization changed its binding: %v", err)
	}
}

type shortNoticeWriter struct{}

func (shortNoticeWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }

func TestNoticeCommandPropagatesIncompleteOutput(t *testing.T) {
	f := newNoticeFixture(t)
	var diagnostic bytes.Buffer
	if rc := noticeRun([]string{"--snapshot", "--build", f.build, "--recipe", f.recipe}, shortNoticeWriter{}, &diagnostic); rc == 0 {
		t.Fatal("partial JSON output reported success")
	}
}
