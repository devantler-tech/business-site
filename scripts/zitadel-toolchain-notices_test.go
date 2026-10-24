package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Literal observations from the selected Linux Go 1.26.10 build, not a list
// obtained from the collector being tested.
var vendorFixturePackages = []string{
	"vendor/golang.org/x/crypto/chacha20",
	"vendor/golang.org/x/crypto/chacha20poly1305",
	"vendor/golang.org/x/crypto/cryptobyte",
	"vendor/golang.org/x/crypto/cryptobyte/asn1",
	"vendor/golang.org/x/crypto/internal/alias",
	"vendor/golang.org/x/crypto/internal/poly1305",
	"vendor/golang.org/x/net/dns/dnsmessage",
	"vendor/golang.org/x/net/http/httpguts",
	"vendor/golang.org/x/net/http/httpproxy",
	"vendor/golang.org/x/net/http2/hpack",
	"vendor/golang.org/x/net/idna",
	"vendor/golang.org/x/sys/cpu",
	"vendor/golang.org/x/text/secure/bidirule",
	"vendor/golang.org/x/text/transform",
	"vendor/golang.org/x/text/unicode/bidi",
	"vendor/golang.org/x/text/unicode/norm",
}

func bindVendorFixture(t *testing.T, f noticeFixture, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range vendorFixturePackages {
		data = append(data, noticeJSON(t, map[string]any{"ImportPath": name, "Standard": true, "Dir": filepath.Join(root, "src", name), "GoFiles": []string{"selected.go"}})...)
	}
	writeNotice(t, f.build, "packages.jsonl", data)
	for _, module := range []string{"net", "crypto", "text", "sys"} {
		for _, name := range []string{"LICENSE", "PATENTS"} {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			writeNotice(t, root, "src/vendor/golang.org/x/"+module+"/"+name, data)
		}
	}
}

func changeVendorPackages(t *testing.T, f noticeFixture, change func([]map[string]any) []map[string]any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var packages []map[string]any
	for {
		var item map[string]any
		if err = decoder.Decode(&item); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, item)
	}
	var output []byte
	for _, item := range change(packages) {
		output = append(output, noticeJSON(t, item)...)
	}
	writeNotice(t, f.build, "packages.jsonl", output)
}

// Removing vendor reads must lose provenance even though the root texts match.
func TestToolchainVendorProvenanceRetainsCheckedDeduplicatedSpans(t *testing.T) {
	f := newNoticeFixture(t)
	root := newToolchainRoot(t)
	bindVendorFixture(t, f, root)
	var out, diagnostic bytes.Buffer
	args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--toolchain", root}
	if noticeRun(args, &out, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	writeNotice(t, f.build, noticeBefore, out.Bytes())
	out.Reset()
	args[0] = "--inventory"
	if noticeRun(args, &out, &diagnostic) != 0 {
		t.Fatal(diagnostic.String())
	}
	var result noticeInventory
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Toolchain.Sections) != 11 {
		t.Fatalf("vendor provenance missing: retained %d component identities, want 11", len(result.Toolchain.Sections))
	}
	var object map[string]any
	if err := json.Unmarshal(out.Bytes(), &object); err != nil {
		t.Fatal(err)
	}
	selected := object["toolchain"].(map[string]any)["vendorPackages"]
	want, _ := json.Marshal(vendorFixturePackages)
	got, _ := json.Marshal(selected)
	if !bytes.Equal(got, want) {
		t.Fatalf("selected vendor identities differ: %s", got)
	}
	raw, err := os.ReadFile(filepath.Join(f.build, "go-toolchain-notices.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"LICENSE", "PATENTS"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || bytes.Count(raw, body) != 1 {
			t.Fatalf("equal %s content was omitted or duplicated: %v", name, err)
		}
	}
	for i, module := range []string{"net", "crypto", "text", "sys"} {
		for j, name := range []string{"LICENSE", "PATENTS"} {
			alias := result.Toolchain.Sections[3+i*2+j]
			original := result.Toolchain.Sections[j]
			if alias.Source != "GOROOT/src/vendor/golang.org/x/"+module+"/"+name || alias.Offset != original.Offset || alias.Bytes != original.Bytes || alias.SHA256 != original.SHA256 || alias.Offset < 0 || alias.Bytes > len(raw)-alias.Offset {
				t.Fatalf("incorrect verified alias: %+v", alias)
			}
		}
	}
}

// Merely retaining Go's root terms must not admit absent or altered vendor terms.
func TestToolchainVendorSnapshotRejectsEveryUnusableInput(t *testing.T) {
	for _, module := range []string{"net", "crypto", "text", "sys"} {
		for _, name := range []string{"LICENSE", "PATENTS"} {
			file := "src/vendor/golang.org/x/" + module + "/" + name
			for _, fault := range []string{"absent", "truncated", "substituted", "linked", "linked-directory", "partial-error"} {
				t.Run(module+"/"+name+"/"+fault, func(t *testing.T) {
					f := newNoticeFixture(t)
					root := newToolchainRoot(t)
					bindVendorFixture(t, f, root)
					reader := noticeRead
					switch fault {
					case "absent":
						if err := os.Remove(filepath.Join(root, file)); err != nil {
							t.Fatal(err)
						}
					case "truncated":
						writeNotice(t, root, file, []byte("Copyright 2009 The Go Authors.\n"))
					case "substituted":
						writeNotice(t, root, file, []byte("A different complete grant.\n"))
					case "linked":
						if err := os.Remove(filepath.Join(root, file)); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(root, name), filepath.Join(root, file)); err != nil {
							t.Fatal(err)
						}
					case "linked-directory":
						directory := filepath.Dir(filepath.Join(root, file))
						if err := os.Rename(directory, directory+".real"); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(directory+".real", directory); err != nil {
							t.Fatal(err)
						}
					case "partial-error":
						reader = func(directory, path string, limit int64) ([]byte, error) {
							if directory == root && path == file {
								data, err := noticeRead(directory, path, limit)
								if err != nil {
									t.Fatal(err)
								}
								return data, errors.New("read failed after complete plausible output")
							}
							return noticeRead(directory, path, limit)
						}
					}
					if _, err := noticeToolchainSnapshot(root, reader); err == nil {
						t.Fatal("unusable selected vendor terms accepted")
					}
				})
			}
		}
	}
}

// A second read can substitute a different graph for the bytes bound in the
// snapshot. Vendor selection must consume that same first checked observation.
func TestToolchainVendorUsesTheAlreadyCheckedPackageObservation(t *testing.T) {
	f := newNoticeFixture(t)
	root := newToolchainRoot(t)
	bindVendorFixture(t, f, root)
	original, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	reader := func(directory, name string, limit int64) ([]byte, error) {
		data, err := noticeRead(directory, name, limit)
		if directory == f.build && name == "packages.jsonl" {
			reads++
			writeNotice(t, f.build, name, []byte("{}\n"))
		}
		return data, err
	}
	state, err := noticeSnapshot(f.build, f.recipe, reader, noticeSupplementConfig{ToolchainRoot: root})
	if err != nil || reads != 1 || state.PackagesSHA256 != noticeHash(original) || state.Toolchain == nil || len(state.Toolchain.Evidence.VendorPackages) != 16 {
		t.Fatalf("vendor selection did not use checked package bytes: reads=%d, error=%v", reads, err)
	}
}

func TestToolchainVendorCaptureRejectsChangedSelection(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "extra", "nonstandard", "escaping-directory", "incomplete", "deps-error", "malformed-tail", "post-snapshot-terms"} {
		t.Run(fault, func(t *testing.T) {
			f := newNoticeFixture(t)
			root := newToolchainRoot(t)
			bindVendorFixture(t, f, root)
			var out, diagnostic bytes.Buffer
			args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--toolchain", root}
			if fault == "post-snapshot-terms" {
				if noticeRun(args, &out, &diagnostic) != 0 {
					t.Fatal(diagnostic.String())
				}
				writeNotice(t, f.build, noticeBefore, out.Bytes())
				writeNotice(t, root, "src/vendor/golang.org/x/net/LICENSE", []byte("changed after snapshot\n"))
				args[0] = "--inventory"
			} else {
				changeVendorPackages(t, f, func(items []map[string]any) []map[string]any {
					last := items[len(items)-1]
					switch fault {
					case "missing":
						return items[:len(items)-1]
					case "duplicate":
						return append(items, last)
					case "extra":
						last["ImportPath"] = "vendor/example.com/unreviewed"
					case "nonstandard":
						last["Standard"] = false
					case "escaping-directory":
						last["Dir"] = filepath.Join(root, "..", "unrelated")
					case "incomplete":
						last["Incomplete"] = true
					case "deps-error":
						last["DepsErrors"] = []any{map[string]any{"Err": "dependency observation failed"}}
					}
					return items
				})
				if fault == "malformed-tail" {
					data, err := os.ReadFile(filepath.Join(f.build, "packages.jsonl"))
					if err != nil {
						t.Fatal(err)
					}
					writeNotice(t, f.build, "packages.jsonl", append(data, []byte("{\"ImportPath\":")...))
				}
			}
			out.Reset()
			if noticeRun(args, &out, &diagnostic) == 0 || out.Len() != 0 {
				t.Fatalf("invalid vendor observation admitted: %s", fault)
			}
		})
	}
}

func newToolchainRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"LICENSE", "PATENTS"} {
		data, err := os.ReadFile(filepath.Join("testdata", "go1.26.10", name))
		if err != nil {
			t.Fatal(err)
		}
		writeNotice(t, root, name, data)
		for _, module := range []string{"net", "crypto", "text", "sys"} {
			writeNotice(t, root, "src/vendor/golang.org/x/"+module+"/"+name, data)
		}
	}
	writeNotice(t, root, "VERSION", []byte("go1.26.10\ntime 2026-10-01T00:00:00Z\n"))
	return root
}

// A successful explicit capture must preserve both complete distribution terms,
// not merely report the compiler version or continue without a retained output.
func TestToolchainNoticeCLIProducesBoundVerbatimOutput(t *testing.T) {
	f := newNoticeFixture(t)
	root := newToolchainRoot(t)
	bindVendorFixture(t, f, root)
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
	if proof.Version != "1.26.10" || proof.RawDiagnostic != "go-toolchain-notices.txt" || proof.Bytes != len(text) || proof.SHA256 != noticeHash(text) || len(proof.Sections) != 11 || len(decoded.Inputs) != 9 {
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
			bindVendorFixture(t, f, root)
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
	bindVendorFixture(t, f.noticeFixture, root)
	license, err := os.ReadFile("testdata/zitadel-source/LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	writeNotice(t, f.build, "prepared/source/LICENSE", license)
	var out, diagnostic bytes.Buffer
	args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--supplement", f.notices, "--toolchain", root, "--source-license"}
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatal(diagnostic.String())
	}
	var snapshot noticeState
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil || snapshot.Supplement == nil || snapshot.Toolchain == nil || snapshot.SourceLicense == nil {
		t.Fatal("combined snapshot incomplete")
	}
	writeNotice(t, f.build, noticeBefore, out.Bytes())
	out.Reset()
	args[0] = "--inventory"
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatal(diagnostic.String())
	}
	for _, name := range []string{noticeRaw, supplementRaw, "go-toolchain-notices.txt", "zitadel-root-license.txt"} {
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
			bindVendorFixture(t, f, root)
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
