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

// noticeOriginalsForTest names the maintained notice inputs independently of the
// capsule implementation; README guidance and authored-tool terms are separate.
func noticeOriginalsForTest() []string {
	return []string{
		"third_party/zitadel-console-notices/catalogue.json",
		"third_party/zitadel-console-notices/connect-LICENSE",
		"third_party/zitadel-console-notices/connect-attribution.txt",
		"third_party/zitadel-console-notices/connect-web-attribution.txt",
		"third_party/zitadel-console-notices/google-protobuf-apache-license.txt",
		"third_party/zitadel-console-notices/google-protobuf-bsd-license.txt",
		"third_party/zitadel-console-notices/google-protobuf-closure-attribution.txt",
		"third_party/zitadel-console-notices/grpc-web-LICENSE",
		"third_party/zitadel-console-notices/grpc-web-attribution.txt",
		"third_party/zitadel-console-notices/grpc-web-google-source-attribution.txt",
		"third_party/zitadel-console-notices/ng-icons-LICENSE.md",
		"third_party/zitadel-console-notices/opentype-LICENSE",
		"third_party/zitadel-console-notices/protobuf-LICENSE",
		"third_party/zitadel-console-notices/protobuf-buf-attribution.txt",
		"third_party/zitadel-console-notices/protobuf-google-bsd-license.txt",
		"third_party/zitadel-console-notices/qrcode-license.txt",
		"third_party/zitadel-console-notices/tiny-inflate-license.txt",
		"third_party/zitadel-copied-assets/Flag-Icons-LICENSE",
		"third_party/zitadel-copied-assets/Google-Mail-LICENSE",
		"third_party/zitadel-copied-assets/Line-Awesome-LICENSE.md",
		"third_party/zitadel-copied-assets/Material-Icons-LICENSE",
		"third_party/zitadel-copied-assets/Pictogrammers-LICENSE",
		"third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md",
		"third_party/zitadel-copied-assets/ZITADEL-LICENSE",
		"third_party/zitadel-copied-assets/ZITADEL-LICENSING.md",
		"third_party/zitadel-copied-assets/catalogue.json",
		"third_party/zitadel-copied-assets/original-line-awesome.css",
		"third_party/zitadel-copied-assets/original-line-awesome.min.css",
		"third_party/zitadel-go-module-notices/catalogue.json",
		"third_party/zitadel-go-module-notices/incorporated/external/armon-go-radix-LICENSE.txt",
		"third_party/zitadel-go-module-notices/incorporated/external/jub0bs-cors-LICENSE.txt",
		"third_party/zitadel-go-module-notices/incorporated/external/zlib-crc32.c.header.txt",
		"third_party/zitadel-go-module-notices/incorporated/external/zlib-zlib.h.header.txt",
		"third_party/zitadel-go-module-notices/incorporated/publicsuffix/MPL-2.0.txt",
		"third_party/zitadel-go-module-notices/incorporated/publicsuffix/public_suffix_list.dat",
		"third_party/zitadel-go-module-notices/incorporated/unicode/BidiPBAReference-2013.java.rights.txt",
		"third_party/zitadel-go-module-notices/incorporated/unicode/BidiReference-2013.java.rights.txt",
		"third_party/zitadel-go-module-notices/incorporated/unicode/LICENSE-V3.txt",
		"third_party/zitadel-login-icons/LICENSE.md",
		"third_party/zitadel-login-icons/NOTICE.md",
		"third_party/zitadel-login-icons/clipboard.svg",
		"third_party/zitadel-text-fonts/Apache-2.0.txt",
		"third_party/zitadel-text-fonts/catalogue.json",
	}
}

func noticeOriginalsFixture(t *testing.T) (string, string, string, map[string][]byte) {
	t.Helper()
	repository := t.TempDir()
	originals := map[string][]byte{}
	for _, name := range noticeOriginalsForTest() {
		body, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		originals[name] = body
		destination := filepath.Join(repository, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	prepared := t.TempDir()
	if err := os.WriteFile(filepath.Join(prepared, "go.mod"), []byte("upstream fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	profile, err := sourceSealTree(prepared, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string][]byte{"modifications.json": []byte("fixture modifications\n")}
	for _, entry := range sourceBuildRecordInputs() {
		body, ok := originals[entry.name]
		if !ok {
			body = []byte("fixture record " + entry.retained + "\n")
		}
		records[entry.retained] = body
	}
	if len(records) != 58 {
		t.Fatal("notice transport must preserve the existing 58-record boundary")
	}
	seal, err := sourceAddRecords(snapshot, profile, records)
	if err != nil {
		t.Fatal(err)
	}
	return repository, snapshot, seal, originals
}

// TestNoticeOriginalsSurviveWithoutCheckout catches a capsule that omits any
// original or silently depends on its original checkout during restoration.
func TestNoticeOriginalsSurviveWithoutCheckout(t *testing.T) {
	repository, snapshot, seal, originals := noticeOriginalsFixture(t)
	build := t.TempDir()
	capsule := filepath.Join(build, "selected-notice-originals")
	var out, diagnostic bytes.Buffer
	if status := sourceBundleRun([]string{"--capture-notice-originals", repository, snapshot, seal, capsule}, &out, &diagnostic); status != 0 {
		t.Fatalf("complete retained originals cannot be captured: %s", diagnostic.String())
	}
	// Exercise the actual existing transport; fake receipt prose is insufficient.
	for _, component := range []string{"source-snapshot", "selected-go-sources", "selected-generator-go-sources", "go-graph-metadata", "selected-pnpm-sources", "selected-bsr-sources", "selected-console-originals", "selected-verifier-bootstrap", "selected-pnpm-sdk-bootstrap"} {
		dir := filepath.Join(build, component)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte(component), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(build, "build-receipt.json"), []byte("transport fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained")
	retainedSeal, err := sourceSealTreePhase(build, retained, "retained-build-inputs")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "originals.tar")
	digest, err := sourcePackTree(retained, retainedSeal, archive)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := filepath.Join(t.TempDir(), "roundtrip")
	if err := sourceUnpackTree(archive, digest, retainedSeal, roundtrip); err != nil {
		t.Fatal(err)
	}
	capsule = filepath.Join(roundtrip, "source/selected-notice-originals")
	if err := os.Rename(repository, repository+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	out.Reset()
	diagnostic.Reset()
	if status := sourceBundleRun([]string{"--restore-notice-originals", capsule, snapshot, seal, restored}, &out, &diagnostic); status != 0 {
		t.Fatalf("retained originals still require their checkout: %s", diagnostic.String())
	}
	for name, expected := range originals {
		body, err := os.ReadFile(filepath.Join(restored, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(body, expected) {
			t.Fatalf("original input did not survive: %s: %v", name, err)
		}
	}
}

// TestBuildRecordsUseRetainedNoticeOriginals keeps the thirteen original-input
// joins usable without ambient checkout notices, while authored terms remain
// repository records and a missing capsule never falls back to that checkout.
func TestBuildRecordsUseRetainedNoticeOriginals(t *testing.T) {
	repository, snapshot, seal, originals := noticeOriginalsFixture(t)
	for _, entry := range sourceBuildRecordInputs() {
		if !entry.repository {
			continue
		}
		if _, original := originals[entry.name]; original {
			continue
		}
		body, err := os.ReadFile(filepath.Join(snapshot, "records", filepath.FromSlash(entry.retained)))
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(repository, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	capsule := filepath.Join(t.TempDir(), "capsule")
	if _, err := sourceNoticeMaterialize(repository, snapshot, seal, capsule, true); err != nil {
		t.Fatal(err)
	}
	for name := range originals {
		if err := os.Remove(filepath.Join(repository, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	var out, diagnostic bytes.Buffer
	args := []string{"--verify-build-records", snapshot, seal, repository, capsule}
	if code := sourceBundleRun(args, &out, &diagnostic); code != 0 {
		t.Fatalf("retained record verification requires checkout notices: %s", diagnostic.String())
	}
	// A pristine checkout cannot rescue a missing explicit capsule.
	for name, body := range originals {
		if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(name)), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	diagnostic.Reset()
	args[4] = filepath.Join(t.TempDir(), "missing")
	if code := sourceBundleRun(args, &out, &diagnostic); code == 0 || out.Len() != 0 {
		t.Fatal("missing retained originals fell back to the checkout")
	}
	args[4] = capsule
	if err := os.WriteFile(filepath.Join(repository, "third_party/zitadel-build-tools/COPYING"), []byte("changed authored terms\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := sourceBundleRun(args, &out, &diagnostic); code == 0 || out.Len() != 0 {
		t.Fatal("authored terms were incorrectly routed away from the repository")
	}
}

// TestNoticeOriginalsRejectChangedInputs exercises the actual capsule verifier,
// including same-length edits, private output, strict membership and bindings.
func TestNoticeOriginalsRejectChangedInputs(t *testing.T) {
	for _, test := range []string{"missing", "extra", "same-size", "mode", "linked-original", "linked-parent", "root-mode", "manifest-space", "manifest-source", "manifest-extra", "manifest-trailing", "recipe-seal", "profile-seal"} {
		t.Run(test, func(t *testing.T) {
			repository, snapshot, seal, _ := noticeOriginalsFixture(t)
			capsule := filepath.Join(t.TempDir(), "capsule")
			if _, err := sourceNoticeMaterialize(repository, snapshot, seal, capsule, true); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(capsule, "third_party/zitadel-login-icons/NOTICE.md")
			manifest := filepath.Join(capsule, "notice-originals.json")
			var err error
			switch test {
			case "missing":
				err = os.Remove(name)
			case "extra":
				err = os.WriteFile(filepath.Join(capsule, "extra"), []byte("unexpected"), 0644)
			case "same-size":
				var body []byte
				body, err = os.ReadFile(name)
				if err == nil {
					body[0] ^= 1
					err = os.WriteFile(name, body, 0644)
				}
			case "mode":
				err = os.Chmod(name, 0600)
			case "linked-original":
				err = os.Rename(name, name+".saved")
				if err == nil {
					err = os.Symlink(name+".saved", name)
				}
			case "linked-parent":
				parent := filepath.Dir(name)
				err = os.Rename(parent, parent+".saved")
				if err == nil {
					err = os.Symlink(parent+".saved", parent)
				}
			case "root-mode":
				err = os.Chmod(capsule, 0755)
			case "manifest-space", "manifest-source", "manifest-extra", "manifest-trailing":
				var body []byte
				body, err = os.ReadFile(manifest)
				if err == nil {
					switch test {
					case "manifest-space":
						body = append([]byte(" "), body...)
					case "manifest-trailing":
						body = append(body, []byte("{}\n")...)
					case "manifest-source":
						body = bytes.Replace(body, []byte(seal), []byte(strings.Repeat("f", 64)), 1)
					case "manifest-extra":
						body = bytes.Replace(body, []byte("{\n"), []byte("{\n  \"unexpected\": true,\n"), 1)
					}
					err = os.WriteFile(manifest, body, 0644)
				}
			case "recipe-seal", "profile-seal":
				var body []byte
				body, err = os.ReadFile(manifest)
				if err == nil {
					var fields map[string]any
					if err = json.Unmarshal(body, &fields); err == nil {
						field := "recipeSha256"
						if test == "profile-seal" {
							field = "preparedProfileSha256"
						}
						original, ok := fields[field].(string)
						if !ok || len(original) != 64 {
							t.Fatal("fixture lacks the independent binding")
						}
						// Preserve canonical order/spacing so this case tests its
						// changed binding rather than incidental JSON serialization.
						body = bytes.Replace(body, []byte(original), []byte(strings.Repeat("f", 64)), 1)
						err = os.WriteFile(manifest, body, 0644)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			if code := sourceBundleRun([]string{"--verify-notice-originals", capsule, snapshot, seal}, &out, &diagnostic); code == 0 || out.Len() != 0 {
				t.Fatal("changed retained originals admitted a success summary")
			}
			restored := filepath.Join(t.TempDir(), "restored")
			if _, err := sourceNoticeMaterialize(capsule, snapshot, seal, restored, false); err == nil {
				t.Fatal("changed retained originals were restored")
			}
			if _, err := os.Lstat(restored); !os.IsNotExist(err) {
				t.Fatal("failed restore published output")
			}
		})
	}
}

// sourceApprovedExtraRecordsForTest supplies distinct bodies for the 25 missing
// authored files and three terms documents, independently of the production map.
func sourceApprovedExtraRecordsForTest() map[string][]byte {
	return map[string][]byte{
		"scripts/replay-zitadel-inputs.sh":                       []byte("approved fixture scripts/replay-zitadel-inputs.sh\n"),
		"scripts/zitadel-console-notices.go":                     []byte("approved fixture scripts/zitadel-console-notices.go\n"),
		"server/go.mod":                                          []byte("approved fixture server/go.mod\n"),
		"server/go.sum":                                          []byte("approved fixture server/go.sum\n"),
		"server/cmd/zitadel-module-sources/capture.go":           []byte("approved fixture server/cmd/zitadel-module-sources/capture.go\n"),
		"server/cmd/zitadel-module-sources/generator_sources.go": []byte("approved fixture server/cmd/zitadel-module-sources/generator_sources.go\n"),
		"server/cmd/zitadel-module-sources/generators.go":        []byte("approved fixture server/cmd/zitadel-module-sources/generators.go\n"),
		"server/cmd/zitadel-module-sources/graph_metadata.go":    []byte("approved fixture server/cmd/zitadel-module-sources/graph_metadata.go\n"),
		"server/cmd/zitadel-module-sources/json_fields.go":       []byte("approved fixture server/cmd/zitadel-module-sources/json_fields.go\n"),
		"server/cmd/zitadel-module-sources/main.go":              []byte("approved fixture server/cmd/zitadel-module-sources/main.go\n"),
		"server/cmd/zitadel-module-sources/payload.go":           []byte("approved fixture server/cmd/zitadel-module-sources/payload.go\n"),
		"server/cmd/zitadel-module-sources/restore.go":           []byte("approved fixture server/cmd/zitadel-module-sources/restore.go\n"),
		"server/cmd/zitadel-module-sources/selection.go":         []byte("approved fixture server/cmd/zitadel-module-sources/selection.go\n"),
		"server/cmd/zitadel-bsr-sources/files.go":                []byte("approved fixture server/cmd/zitadel-bsr-sources/files.go\n"),
		"server/cmd/zitadel-bsr-sources/main.go":                 []byte("approved fixture server/cmd/zitadel-bsr-sources/main.go\n"),
		"server/cmd/zitadel-bsr-sources/package.go":              []byte("approved fixture server/cmd/zitadel-bsr-sources/package.go\n"),
		"server/cmd/zitadel-bsr-sources/validation.go":           []byte("approved fixture server/cmd/zitadel-bsr-sources/validation.go\n"),
		"Dockerfile.zitadel":                                     []byte("approved fixture Dockerfile.zitadel\n"),
		"scripts/zitadel-image-package.go":                       []byte("approved fixture scripts/zitadel-image-package.go\n"),
		"scripts/zitadel-image-smoke.go":                         []byte("approved fixture scripts/zitadel-image-smoke.go\n"),
		"scripts/zitadel-service-smoke.go":                       []byte("approved fixture scripts/zitadel-service-smoke.go\n"),
		"scripts/zitadel-browser.go":                             []byte("approved fixture scripts/zitadel-browser.go\n"),
		"server/cmd/zitadel-module-sources/README.md":            []byte("approved fixture server/cmd/zitadel-module-sources/README.md\n"),
		"server/cmd/zitadel-bsr-sources/README.md":               []byte("approved fixture server/cmd/zitadel-bsr-sources/README.md\n"),
		"tools/zitadel-pnpm-source/README.md":                    []byte("approved fixture tools/zitadel-pnpm-source/README.md\n"),
		"third_party/zitadel-build-tools/COPYING":                []byte("approved fixture third_party/zitadel-build-tools/COPYING\n"),
		"third_party/zitadel-build-tools/README.md":              []byte("approved fixture third_party/zitadel-build-tools/README.md\n"),
		"third_party/zitadel-build-tools/scope.json":             []byte("approved fixture third_party/zitadel-build-tools/scope.json\n"),
	}
}

// sourceApprovedAliasesForTest independently names the complete approved grant.
func sourceApprovedAliasesForTest() map[string]string {
	return map[string]string{
		"scripts/build-zitadel-linux.sh":                         "recipes/build-zitadel-linux.sh",
		"scripts/prepare-zitadel-source.sh":                      "recipes/prepare-zitadel-source.sh",
		"scripts/zitadel-source-inputs.sha256":                   "recipes/zitadel-source-inputs.sha256",
		"scripts/zitadel-source-bundle.go":                       "recipes/zitadel-source-bundle.go",
		"scripts/zitadel-console-assets.go":                      "recipes/zitadel-console-assets.go",
		"scripts/zitadel-login-icons.go":                         "recipes/zitadel-login-icons.go",
		"scripts/zitadel-text-fonts.go":                          "recipes/zitadel-text-fonts.go",
		"scripts/zitadel-copied-assets.go":                       "recipes/zitadel-copied-assets.go",
		"tools/zitadel-pnpm-source/index.mjs":                    "recipes/zitadel-pnpm-source/index.mjs",
		"tools/zitadel-pnpm-source/package.json":                 "recipes/zitadel-pnpm-source/package.json",
		"tools/zitadel-pnpm-source/package-lock.json":            "recipes/zitadel-pnpm-source/package-lock.json",
		"docs/ZITADEL-SOURCE.md":                                 "ZITADEL-SOURCE.md",
		"scripts/replay-zitadel-inputs.sh":                       "scripts/replay-zitadel-inputs.sh",
		"scripts/zitadel-console-notices.go":                     "scripts/zitadel-console-notices.go",
		"server/go.mod":                                          "server/go.mod",
		"server/go.sum":                                          "server/go.sum",
		"server/cmd/zitadel-module-sources/capture.go":           "server/cmd/zitadel-module-sources/capture.go",
		"server/cmd/zitadel-module-sources/generator_sources.go": "server/cmd/zitadel-module-sources/generator_sources.go",
		"server/cmd/zitadel-module-sources/generators.go":        "server/cmd/zitadel-module-sources/generators.go",
		"server/cmd/zitadel-module-sources/graph_metadata.go":    "server/cmd/zitadel-module-sources/graph_metadata.go",
		"server/cmd/zitadel-module-sources/json_fields.go":       "server/cmd/zitadel-module-sources/json_fields.go",
		"server/cmd/zitadel-module-sources/main.go":              "server/cmd/zitadel-module-sources/main.go",
		"server/cmd/zitadel-module-sources/payload.go":           "server/cmd/zitadel-module-sources/payload.go",
		"server/cmd/zitadel-module-sources/restore.go":           "server/cmd/zitadel-module-sources/restore.go",
		"server/cmd/zitadel-module-sources/selection.go":         "server/cmd/zitadel-module-sources/selection.go",
		"server/cmd/zitadel-bsr-sources/files.go":                "server/cmd/zitadel-bsr-sources/files.go",
		"server/cmd/zitadel-bsr-sources/main.go":                 "server/cmd/zitadel-bsr-sources/main.go",
		"server/cmd/zitadel-bsr-sources/package.go":              "server/cmd/zitadel-bsr-sources/package.go",
		"server/cmd/zitadel-bsr-sources/validation.go":           "server/cmd/zitadel-bsr-sources/validation.go",
		"Dockerfile.zitadel":                                     "Dockerfile.zitadel",
		"scripts/zitadel-image-package.go":                       "scripts/zitadel-image-package.go",
		"scripts/zitadel-image-smoke.go":                         "scripts/zitadel-image-smoke.go",
		"scripts/zitadel-service-smoke.go":                       "scripts/zitadel-service-smoke.go",
		"scripts/zitadel-browser.go":                             "scripts/zitadel-browser.go",
		"server/cmd/zitadel-module-sources/README.md":            "server/cmd/zitadel-module-sources/README.md",
		"server/cmd/zitadel-bsr-sources/README.md":               "server/cmd/zitadel-bsr-sources/README.md",
		"tools/zitadel-pnpm-source/README.md":                    "tools/zitadel-pnpm-source/README.md",
		"third_party/zitadel-build-tools/COPYING":                "third_party/zitadel-build-tools/COPYING",
		"third_party/zitadel-build-tools/README.md":              "third_party/zitadel-build-tools/README.md",
		"third_party/zitadel-build-tools/scope.json":             "third_party/zitadel-build-tools/scope.json",
	}
}

// TestRetainedInputsArchive exercises the actual closed archive with private
// capsule modes. Compiled output and ambient caches must never enter it.
func TestRetainedInputsArchive(t *testing.T) {
	for _, kind := range []string{"valid", "missing-component", "missing-verifier-bootstrap", "missing-notice-originals", "missing-sdk-bootstrap", "linked-member", "unsupported-mode"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			components := []string{"source-snapshot", "selected-go-sources", "selected-generator-go-sources", "go-graph-metadata", "selected-pnpm-sources", "selected-bsr-sources", "selected-console-originals", "selected-verifier-bootstrap", "selected-notice-originals", "selected-pnpm-sdk-bootstrap"}
			modes := []os.FileMode{0600, 0444, 0644, 0755}
			for i, component := range components {
				if kind == "missing-component" && i == 0 {
					continue
				}
				if kind == "missing-verifier-bootstrap" && component == "selected-verifier-bootstrap" {
					continue
				}
				if kind == "missing-notice-originals" && component == "selected-notice-originals" {
					continue
				}
				if kind == "missing-sdk-bootstrap" && component == "selected-pnpm-sdk-bootstrap" {
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

// sourceToolFixture gives the source layout controls independently named bodies.
func sourceToolFixture(t *testing.T) (string, string, map[string][]byte) {
	t.Helper()
	input := t.TempDir()
	if err := os.WriteFile(filepath.Join(input, "go.mod"), []byte("upstream service module\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	profile, err := sourceSealTree(input, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string][]byte{"modifications.json": []byte("modifications\n")}
	for _, entry := range sourceBuildRecordInputs() {
		records[entry.retained] = []byte("other retained input " + entry.retained + "\n")
	}
	approved := sourceApprovedAliasesForTest()
	for name, alias := range approved {
		records[alias] = []byte("approved tool source " + name + "\n")
	}
	seal, err := sourceAddRecords(snapshot, profile, records)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, seal, records
}

// TestSourceRestoreToolsKeepsOnlyTheApprovedSource preserves the granted layout
// without replacing the upstream service module or claiming tool reconstruction.
func TestSourceRestoreToolsKeepsOnlyTheApprovedSource(t *testing.T) {
	snapshot, seal, records := sourceToolFixture(t)
	approved := sourceApprovedAliasesForTest()
	output := filepath.Join(t.TempDir(), "tools")
	var out, diagnostic bytes.Buffer
	if status := sourceBundleRun([]string{"--restore-tools", snapshot, seal, output}, &out, &diagnostic); status != 0 {
		t.Fatalf("retained source cannot reconstruct the approved tool layout: %s", diagnostic.String())
	}
	count := 0
	if err := filepath.WalkDir(output, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(output, name)
		if err != nil {
			return err
		}
		alias, ok := approved[filepath.ToSlash(name)]
		if !ok {
			return fmt.Errorf("unapproved tool-source destination %s", name)
		}
		body, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || !bytes.Equal(body, records[alias]) {
			return fmt.Errorf("restored tool body differs: %s", name)
		}
		info, err := entry.Info()
		if err != nil || info.Mode() != 0644 {
			return fmt.Errorf("tool records must remain regular inert data: %s", name)
		}
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 40 {
		t.Fatalf("restore the exact 37 approved paths and three terms, got %d", count)
	}
	if _, err := os.Stat(filepath.Join(output, "go.mod")); !os.IsNotExist(err) {
		t.Fatal("tool metadata must not replace the upstream service module")
	}
	out.Reset()
	if status := sourceBundleRun([]string{"--verify-tools", snapshot, seal, output}, &out, &diagnostic); status != 0 {
		t.Fatalf("complete restored tool source cannot be read back: %s", diagnostic.String())
	}
	if err := os.WriteFile(filepath.Join(output, "server/go.mod"), []byte("different module\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if status := sourceBundleRun([]string{"--verify-tools", snapshot, seal, output}, &out, &diagnostic); status == 0 || out.Len() != 0 {
		t.Fatal("changed restored tool source must refuse a success receipt")
	}
}

// TestSourceToolReadbackRejectsALatePrivacyChange checks the readback boundary,
// after initial output admission rather than only the CLI's early root check.
func TestSourceToolReadbackRejectsALatePrivacyChange(t *testing.T) {
	snapshot, seal, _ := sourceToolFixture(t)
	output := filepath.Join(t.TempDir(), "tools")
	if _, err := sourceToolSource(snapshot, seal, output, true); err != nil {
		t.Fatal(err)
	}
	plan, err := sourceToolPlan(snapshot, seal)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Chmod(output, 0755); err != nil {
		t.Fatal(err)
	}
	if err := sourceToolFiles(root, plan); err == nil {
		t.Fatal("late loss of output privacy passed the readback boundary")
	}
}

// TestSourceToolRestorationRefusesIncompleteInputs keeps failed reconstruction
// separate from success and preserves an existing caller-owned output.
func TestSourceToolRestorationRefusesIncompleteInputs(t *testing.T) {
	for _, kind := range []string{"wrong-seal", "changed-source", "missing-source", "linked-source", "mode-source", "partial-profile", "existing-output", "inside-snapshot", "alias-inside-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			snapshot, seal, records := sourceToolFixture(t)
			output := filepath.Join(t.TempDir(), "tools")
			source := filepath.Join(snapshot, "records", sourceApprovedAliasesForTest()["server/go.mod"])
			switch kind {
			case "wrong-seal":
				seal = strings.Repeat("0", 64)
			case "changed-source":
				if err := os.WriteFile(source, []byte("other module"), 0644); err != nil {
					t.Fatal(err)
				}
			case "missing-source":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "linked-source":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(snapshot, "source/go.mod"), source); err != nil {
					t.Fatal(err)
				}
			case "mode-source":
				if err := os.Chmod(source, 0755); err != nil {
					t.Fatal(err)
				}
			case "partial-profile":
				// A valid external seal is not permission to omit an approved body.
				delete(records, sourceApprovedAliasesForTest()["server/go.mod"])
				input := t.TempDir()
				if err := os.WriteFile(filepath.Join(input, "source.go"), []byte("source"), 0644); err != nil {
					t.Fatal(err)
				}
				snapshot = filepath.Join(t.TempDir(), "partial")
				profile, err := sourceSealTree(input, snapshot)
				if err != nil {
					t.Fatal(err)
				}
				seal, err = sourceAddRecords(snapshot, profile, records)
				if err != nil {
					t.Fatal(err)
				}
			case "existing-output":
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "inside-snapshot":
				output = filepath.Join(snapshot, "tools")
			case "alias-inside-snapshot":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(snapshot, alias); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(alias, "tools")
			}
			var out, diagnostic bytes.Buffer
			if code := sourceBundleRun([]string{"--restore-tools", snapshot, seal, output}, &out, &diagnostic); code == 0 || out.Len() != 0 {
				t.Fatal("invalid restoration emitted a successful receipt")
			}
			if kind == "existing-output" {
				if body, err := os.ReadFile(filepath.Join(output, "keep")); err != nil || string(body) != "keep" {
					t.Fatal("existing output changed")
				}
			} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("failed restoration left an output")
			}
		})
	}
}

// TestSourceToolReadbackRefusesMutatedOutputs exercises every retained source
// family and closed output membership through the real explicit CLI operation.
func TestSourceToolReadbackRefusesMutatedOutputs(t *testing.T) {
	for _, kind := range []string{"module", "sum", "command", "recipe", "terms", "missing", "link", "mode", "extra-file", "extra-directory", "directory-mode", "root-mode", "linked-root"} {
		t.Run(kind, func(t *testing.T) {
			snapshot, seal, _ := sourceToolFixture(t)
			output := filepath.Join(t.TempDir(), "tools")
			if _, err := sourceToolSource(snapshot, seal, output, true); err != nil {
				t.Fatal(err)
			}
			member := filepath.Join(output, "server/go.mod")
			switch kind {
			case "module", "sum", "command", "recipe", "terms":
				names := map[string]string{"module": "server/go.mod", "sum": "server/go.sum", "command": "server/cmd/zitadel-module-sources/main.go", "recipe": "scripts/build-zitadel-linux.sh", "terms": "third_party/zitadel-build-tools/COPYING"}
				target := filepath.Join(output, names[kind])
				if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
					t.Fatal("the existing source/terms body must be the mutation target")
				}
				if err := os.WriteFile(target, []byte("changed"), 0644); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
			case "link":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(output, "server/go.sum"), member); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(member, 0755); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				if err := os.WriteFile(filepath.Join(output, "server/cmd/zitadel-module-sources/extra.go"), []byte("extra"), 0644); err != nil {
					t.Fatal(err)
				}
			case "extra-directory":
				if err := os.Mkdir(filepath.Join(output, "unused"), 0755); err != nil {
					t.Fatal(err)
				}
			case "directory-mode":
				if err := os.Chmod(filepath.Join(output, "server/cmd"), 0777); err != nil {
					t.Fatal(err)
				}
			case "root-mode":
				if err := os.Chmod(output, 0755); err != nil {
					t.Fatal(err)
				}
			case "linked-root":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(output, link); err != nil {
					t.Fatal(err)
				}
				output = link
			}
			var out, diagnostic bytes.Buffer
			if code := sourceBundleRun([]string{"--verify-tools", snapshot, seal, output}, &out, &diagnostic); code == 0 || out.Len() != 0 {
				t.Fatal("changed tool-source output emitted success")
			}
		})
	}
}

// TestSourceRestoreBuildKeepsLiteralInputs proves the replay seam restores a
// verified closed snapshot without re-preparing it or replacing its API record.
func TestSourceRestoreBuildKeepsLiteralInputs(t *testing.T) {
	for _, kind := range []string{"valid", "missing-record", "extra-record", "changed-member", "wrong-seal", "existing-output", "output-inside-snapshot", "output-alias-inside-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			input := t.TempDir()
			files := map[string]string{
				"apps/api/project.json": "serialized API\n",
				"empty.go":              "",
				"go.mod":                "module frozen.example/service\n",
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
				"source-preparation.json":                                         []byte("literal preparation\n"),
				"api-project.original.json":                                       []byte("original API\n"),
				"api-project.serial.json":                                         []byte("serialized API\n"),
				"inputs.sha256":                                                   []byte("literal input hashes\n"),
				"recipes/prepare-zitadel-source.sh":                               []byte("original preparer\n"),
				"recipes/zitadel-source-inputs.sha256":                            []byte("original locks\n"),
				"recipes/build-zitadel-linux.sh":                                  []byte("native builder\n"),
				"recipes/zitadel-source-bundle.go":                                []byte("source helper\n"),
				"recipes/zitadel-console-assets.go":                               []byte("console download adapter\n"),
				"recipes/zitadel-login-icons.go":                                  []byte("strict statik decoder\n"),
				"recipes/zitadel-text-fonts.go":                                   []byte("font notice collector\n"),
				"recipes/zitadel-copied-assets.go":                                []byte("copied asset collector\n"),
				"third_party/zitadel-copied-assets/catalogue.json":                []byte("copied asset catalogue\n"),
				"third_party/zitadel-copied-assets/Line-Awesome-LICENSE.md":       []byte("complete Icons8 terms\n"),
				"third_party/zitadel-copied-assets/Material-Icons-LICENSE":        []byte("complete Material terms\n"),
				"third_party/zitadel-copied-assets/Flag-Icons-LICENSE":            []byte("complete flags terms\n"),
				"third_party/zitadel-copied-assets/ZITADEL-LICENSE":               []byte("complete source terms\n"),
				"third_party/zitadel-copied-assets/ZITADEL-LICENSING.md":          []byte("complete source scope\n"),
				"third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md":       []byte("complete contribution context\n"),
				"third_party/zitadel-copied-assets/original-line-awesome.css":     []byte("original CSS\n"),
				"third_party/zitadel-copied-assets/original-line-awesome.min.css": []byte("original minified CSS\n"),
				"third_party/zitadel-text-fonts/catalogue.json":                   []byte("frozen font catalogue\n"),
				"third_party/zitadel-text-fonts/Apache-2.0.txt":                   []byte("complete Apache terms\n"),
				"recipes/zitadel-pnpm-source/index.mjs":                           []byte("native pnpm adapter\n"),
				"recipes/zitadel-pnpm-source/package.json":                        []byte("native pnpm SDK\n"),
				"recipes/zitadel-pnpm-source/package-lock.json":                   []byte("native pnpm SDK locks\n"),
				"ZITADEL-SOURCE.md":                                               []byte("source description\n"),
				"modifications.json":                                              []byte("dated modification record\n"),
			}
			for name, body := range sourceApprovedExtraRecordsForTest() {
				records[name] = body
			}
			for name, body := range sourceNoticeOriginalsForTest(t) {
				records[name] = body
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
					info, err := os.Stat(filepath.Join(output, "prepared/records", filepath.FromSlash(name)))
					if err != nil || info.Mode() != 0644 {
						t.Fatalf("audit record is not inert data: %s: %v", name, err)
					}
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
// sourceNoticeOriginalsForTest binds the two existing originals independently
// of the production capture map; a fixture cannot stand in for their full bodies.
func sourceNoticeOriginalsForTest(t *testing.T) map[string][]byte {
	t.Helper()
	originals := []struct {
		name   string
		bytes  int
		sha256 string
	}{
		{"third_party/zitadel-console-notices/catalogue.json", 12536, "4f964bd659a7656f7951c363535120af0fe881be37b1ac522e0964dd41647206"},
		{"third_party/zitadel-go-module-notices/incorporated/publicsuffix/public_suffix_list.dat", 330467, "a5638281157e8c902b127a5376f9ea2d024bf2ee11524133a6a76cd1d14ee7be"},
	}
	bodies := map[string][]byte{}
	for _, original := range originals {
		body, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(original.name)))
		if err != nil || len(body) != original.bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != original.sha256 {
			t.Fatalf("original notice input changed: %s: %v", original.name, err)
		}
		bodies[original.name] = body
	}
	return bodies
}

// Prepared-source capture must bind the frozen profile and literal build inputs;
// a complete-looking tree cannot substitute for those independent records.
func TestSourceCaptureBindsFrozenPreparationAndBuildRecords(t *testing.T) {
	noticeOriginals := sourceNoticeOriginalsForTest(t)
	kinds := []string{"valid", "changed-source", "changed-recipe", "changed-inputs", "missing-console-helper", "missing-pnpm-helper", "wrong-original-api", "wrong-serial-api", "linked-record", "existing-output", "missing font collector", "missing statik decoder", "missing font catalogue", "missing Apache text", "linked font collector", "linked statik decoder", "linked font catalogue", "linked Apache text", "missing copied collector", "linked copied collector", "missing copied catalogue", "linked copied catalogue", "missing original CSS", "linked original CSS", "missing flags grant", "linked flags grant", "missing source license", "linked source license", "missing source scope", "linked source scope", "missing contribution context", "linked contribution context"}
	for name := range noticeOriginals {
		for _, kind := range []string{"missing notice:", "empty notice:", "linked notice:", "unsafe-mode notice:"} {
			kinds = append(kinds, kind+name)
		}
	}
	for name := range sourceApprovedExtraRecordsForTest() {
		for _, kind := range []string{"missing authored:", "empty authored:", "linked authored:", "unsafe-mode authored:"} {
			kinds = append(kinds, kind+name)
		}
	}
	for _, kind := range kinds {
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
			write(repo, "scripts/zitadel-copied-assets.go", "copied asset collector\n")
			for _, name := range []string{"catalogue.json", "Line-Awesome-LICENSE.md", "Material-Icons-LICENSE", "Flag-Icons-LICENSE", "original-line-awesome.css", "original-line-awesome.min.css", "ZITADEL-LICENSE", "ZITADEL-LICENSING.md", "ZITADEL-CONTRIBUTING.md"} {
				write(repo, "third_party/zitadel-copied-assets/"+name, name+" original complete bytes\n")
			}
			for _, name := range []string{"index.mjs", "package.json", "package-lock.json"} {
				write(repo, "tools/zitadel-pnpm-source/"+name, name+"\n")
			}
			for name, body := range sourceApprovedExtraRecordsForTest() {
				write(repo, name, string(body))
			}
			for name, body := range noticeOriginals {
				write(repo, name, string(body))
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
			newInputs["copied collector"] = "scripts/zitadel-copied-assets.go"
			newInputs["copied catalogue"] = "third_party/zitadel-copied-assets/catalogue.json"
			newInputs["original CSS"] = "third_party/zitadel-copied-assets/original-line-awesome.css"
			newInputs["flags grant"] = "third_party/zitadel-copied-assets/Flag-Icons-LICENSE"
			newInputs["source license"] = "third_party/zitadel-copied-assets/ZITADEL-LICENSE"
			newInputs["source scope"] = "third_party/zitadel-copied-assets/ZITADEL-LICENSING.md"
			newInputs["contribution context"] = "third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md"
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
			for name := range sourceApprovedExtraRecordsForTest() {
				p := filepath.Join(repo, filepath.FromSlash(name))
				switch kind {
				case "missing authored:" + name, "linked authored:" + name:
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(kind, "linked") {
						if err := os.Symlink(filepath.Join(repo, "docs/ZITADEL-SOURCE.md"), p); err != nil {
							t.Fatal(err)
						}
					}
				case "empty authored:" + name:
					write(repo, name, "")
				case "unsafe-mode authored:" + name:
					if err := os.Chmod(p, 0666); err != nil {
						t.Fatal(err)
					}
				}
			}
			for name := range noticeOriginals {
				file := filepath.Join(repo, filepath.FromSlash(name))
				switch kind {
				case "missing notice:" + name, "linked notice:" + name:
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(kind, "linked") {
						if err := os.Symlink(filepath.Join(repo, "docs/ZITADEL-SOURCE.md"), file); err != nil {
							t.Fatal(err)
						}
					}
				case "empty notice:" + name:
					write(repo, name, "")
				case "unsafe-mode notice:" + name:
					if err := os.Chmod(file, 0666); err != nil {
						t.Fatal(err)
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
						ModifiedOn    string   `json:"modifiedOn"`
						Modifications []string `json:"modifications"`
					}
					if readErr != nil || json.Unmarshal(raw, &record) != nil || record.ModifiedOn != "2026-10-29" || len(record.Modifications) != 8 || record.Modifications[3] != "Update go-jose/v4 to 4.1.6 with its native generated checksums; preserve the upstream key-size limit." || record.Modifications[4] != "Update golang.org/x/net to 0.62.0 with its native generated checksums; preserve upstream HTML protections." || record.Modifications[5] != "Update gorilla/csrf to 1.7.3 with its native generated checksums; mark only explicitly configured plaintext login requests." || record.Modifications[6] != "Replace provider image decoration with existing neutral arrow icons in eighteen consumers; remove sixteen original provider image paths." || record.Modifications[7] != "Replace SMTP image decoration with the existing neutral envelope glyph in five consumers; remove nine original SMTP image paths while preserving all nine connection defaults." {
						t.Fatalf("modification date does not describe the frozen changes: %s: %v", raw, readErr)
					}
				}
			})
			if kind == "valid" {
				if err != nil || got.Files != 8 || got.Records != 58 || got.PreparedProfileSHA256 != profile || got.ManifestSHA256 == profile {
					t.Fatalf("incomplete source capture: %+v: %v", got, err)
				}
				manifest, err := sourceVerifyTree(output, got.ManifestSHA256)
				if err != nil || manifest.PreparedProfile != profile || len(manifest.Records) != 58 {
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
				for _, name := range []string{"ZITADEL-LICENSE", "ZITADEL-LICENSING.md", "ZITADEL-CONTRIBUTING.md"} {
					if raw, readErr := sourceReadAt(output, "records/third_party/zitadel-copied-assets/"+name); readErr != nil || string(raw) != name+" original complete bytes\n" {
						t.Fatalf("translation context not retained literally: %s: %v", name, readErr)
					}
				}
				if len(sourceApprovedAliasesForTest()) != 40 {
					t.Fatal("approved grant fixture is incomplete")
				}
				for original, retained := range sourceApprovedAliasesForTest() {
					want, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(original)))
					got, err := sourceReadAt(output, "records/"+retained)
					if readErr != nil || err != nil || !bytes.Equal(want, got) {
						t.Fatalf("approved source not retained at its unique alias: %s: %v / %v", original, readErr, err)
					}
				}
				checkRecords := func(wantSuccess bool) {
					t.Helper()
					var out, diagnostic bytes.Buffer
					status := sourceBundleRun([]string{"--verify-build-records", output, got.ManifestSHA256, repo}, &out, &diagnostic)
					if (status == 0) != wantSuccess || (!wantSuccess && out.Len() != 0) {
						t.Fatalf("current build-record observation: success=%v status=%d: %s", wantSuccess, status, diagnostic.String())
					}
				}
				checkRecords(true)
				for original := range sourceApprovedAliasesForTest() {
					name := filepath.Join(repo, filepath.FromSlash(original))
					before, err := os.ReadFile(name)
					if err != nil {
						t.Fatal(err)
					}
					write(repo, original, string(before)+"changed after capture\n")
					checkRecords(false)
					write(repo, original, string(before))
				}
				for name, want := range noticeOriginals {
					matches := 0
					for _, member := range manifest.Records {
						if member.Path == name {
							matches++
							if member.Bytes != int64(len(want)) || member.Mode != 0644 || member.SHA256 != hash(string(want)) {
								t.Fatalf("notice record metadata differs: %s", name)
							}
						}
					}
					body, err := sourceReadAt(output, "records/"+name)
					if matches != 1 || err != nil || !bytes.Equal(body, want) {
						t.Fatalf("original notice not retained exactly once: %s: %v", name, err)
					}
					for _, mutation := range []string{"changed", "missing", "linked", "unsafe-mode"} {
						file := filepath.Join(repo, filepath.FromSlash(name))
						switch mutation {
						case "changed":
							write(repo, name, string(want)+"changed after capture\n")
						case "missing", "linked":
							if err := os.Remove(file); err != nil {
								t.Fatal(err)
							}
							if mutation == "linked" {
								if err := os.Symlink(filepath.Join(repo, "docs/ZITADEL-SOURCE.md"), file); err != nil {
									t.Fatal(err)
								}
							}
						case "unsafe-mode":
							if err := os.Chmod(file, 0666); err != nil {
								t.Fatal(err)
							}
						}
						checkRecords(false)
						if mutation == "linked" {
							if err := os.Remove(file); err != nil {
								t.Fatal(err)
							}
						}
						write(repo, name, string(want))
						if err := os.Chmod(file, 0644); err != nil {
							t.Fatal(err)
						}
						checkRecords(true)
					}
				}
				checkRecords(true)
				archive := filepath.Join(t.TempDir(), "prepared.tar")
				archiveHash, err := sourcePackTree(output, got.ManifestSHA256, archive)
				if err != nil {
					t.Fatal(err)
				}
				unpacked := filepath.Join(t.TempDir(), "unpacked")
				if err := sourceUnpackTree(archive, archiveHash, got.ManifestSHA256, unpacked); err != nil {
					t.Fatal(err)
				}
				restoredRoot := filepath.Join(t.TempDir(), "restored")
				restored, err := sourceRestoreBuild(unpacked, got.ManifestSHA256, restoredRoot)
				if err != nil || restored.ManifestSHA256 != got.ManifestSHA256 || restored.Records != got.Records {
					t.Fatalf("actual captured source cannot enter the native restoration: %+v: %v", restored, err)
				}
				for original, retained := range sourceApprovedAliasesForTest() {
					want, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(original)))
					name := filepath.Join(restoredRoot, "prepared/records", filepath.FromSlash(retained))
					body, err := os.ReadFile(name)
					info, statErr := os.Stat(name)
					if readErr != nil || err != nil || statErr != nil || !bytes.Equal(want, body) || info.Mode() != 0644 {
						t.Fatalf("approved archived/restored record differs: %s", original)
					}
				}
				for name, want := range noticeOriginals {
					file := filepath.Join(restoredRoot, "prepared/records", filepath.FromSlash(name))
					body, err := os.ReadFile(file)
					info, statErr := os.Stat(file)
					if err != nil || statErr != nil || !bytes.Equal(body, want) || info.Mode() != 0644 {
						t.Fatalf("archived notice original differs: %s", name)
					}
					if _, err := os.Lstat(filepath.Join(restoredRoot, "prepared/source", filepath.FromSlash(name))); !os.IsNotExist(err) {
						t.Fatalf("notice original escaped audit records: %s", name)
					}
				}
				upstream, err := os.ReadFile(filepath.Join(restoredRoot, "prepared/source/go.mod"))
				if err != nil || string(upstream) != "frozen go.mod\n" {
					t.Fatal("helper module overwrote the upstream module", err)
				}
				for _, name := range []string{"server", "scripts", "Dockerfile.zitadel"} {
					if _, err := os.Lstat(filepath.Join(restoredRoot, name)); !os.IsNotExist(err) {
						t.Fatal("audit source escaped fixed restore targets", name, err)
					}
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
