package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func imageTestWrite(t *testing.T, root, name string, b []byte) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	var original os.FileMode
	if st, err := os.Stat(p); err == nil {
		original = st.Mode().Perm()
		if err = os.Chmod(p, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if original != 0 {
		if err := os.Chmod(p, original); err != nil {
			t.Fatal(err)
		}
	}
}

func imageTestJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func imageTestHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func imageTestFixture(t *testing.T) (string, string, string) {
	t.Helper()
	build, repo := t.TempDir(), t.TempDir()
	binary := []byte("selected fixture binary\n")
	receipt := imageTestJSON(t, map[string]any{"schemaVersion": 1, "version": "4.20.0", "platform": "linux/amd64", "profile": "api-login-v1-console", "upstreamCommit": imagePin, "binarySha256": imageTestHash(binary), "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false})
	imageTestWrite(t, build, "service/zitadel", binary)
	for _, name := range []string{"scripts/zitadel-image-package.go", "scripts/zitadel-image-smoke.go"} {
		imageTestWrite(t, repo, name, []byte("fixture recipe input\n"))
	}
	imageTestWrite(t, build, "build-receipt.json", receipt)
	text := []byte("complete retained fixture terms\n")
	meta := func(name string) map[string]any {
		return map[string]any{"rawDiagnostic": name, "bytes": len(text), "sha256": imageTestHash(text)}
	}
	console := map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "upstreamCommit": imagePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "inputs": []any{map[string]any{"path": "build-receipt.json", "sha256": imageTestHash(receipt)}}, "toolchain": meta("go-toolchain-notices.txt"), "sourceLicense": meta("zitadel-root-license.txt"), "modules": meta("go-module-notices.txt")}
	for k, v := range meta("console-third-party-notices.txt") {
		console[k] = v
	}
	consoleBytes := imageTestJSON(t, console)
	imageTestWrite(t, build, "console-notices-inventory.json", consoleBytes)
	supplement := map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "upstreamCommit": imagePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "noticeInventorySha256": imageTestHash(consoleBytes)}
	for k, v := range meta("console-notices-supplement.txt") {
		supplement[k] = v
	}
	imageTestWrite(t, build, "console-notices-supplement-inventory.json", imageTestJSON(t, supplement))
	inputs := []any{map[string]any{"path": "build-receipt.json", "sha256": imageTestHash(receipt)}}
	for _, name := range []string{"LICENSE.md", "NOTICE.md", "clipboard.svg"} {
		b := []byte("complete retained icon fixture " + name + "\n")
		imageTestWrite(t, repo, "third_party/zitadel-login-icons/"+name, b)
		inputs = append(inputs, map[string]any{"path": name, "sha256": imageTestHash(b)})
	}
	imageTestWrite(t, build, "login-icons-inventory.json", imageTestJSON(t, map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "upstreamCommit": imagePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "inputs": inputs}))
	fonts := map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "upstreamCommit": imagePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "binarySha256": imageTestHash(binary), "inputs": []any{map[string]any{"path": "build-receipt.json", "sha256": imageTestHash(receipt)}}}
	for k, v := range meta("text-font-notices.txt") {
		fonts[k] = v
	}
	imageTestWrite(t, build, "text-font-notices-inventory.json", imageTestJSON(t, fonts))
	copied := map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "upstreamCommit": imagePin, "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false, "binarySha256": imageTestHash(binary), "inputs": []any{map[string]any{"path": "build-receipt.json", "sha256": imageTestHash(receipt)}}}
	for k, v := range meta("copied-asset-notices.txt") {
		copied[k] = v
	}
	imageTestWrite(t, build, "copied-asset-notices-inventory.json", imageTestJSON(t, copied))
	imageTestWrite(t, build, "copied-asset-notices.txt", text)
	for _, name := range []string{"go-toolchain-notices.txt", "zitadel-root-license.txt", "go-module-notices.txt", "console-third-party-notices.txt", "console-notices-supplement.txt", "text-font-notices.txt"} {
		imageTestWrite(t, build, name, text)
	}
	return build, repo, filepath.Join(t.TempDir(), "package")
}

func TestImagePackageRetainsClosedCheckedContext(t *testing.T) {
	build, repo, output := imageTestFixture(t)
	seal, err := imagePackage(build, repo, output)
	if err != nil {
		t.Fatal(err)
	}
	if len(seal) != 64 {
		t.Fatal("manifest hash absent")
	}
	if err := imageVerifyContext(output, seal); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zitadel", "build-receipt.json", "notices/go-module-notices.txt", "notices/login-icons/NOTICE.md", "notices/text-font-notices.txt", "notices/text-font-notices-inventory.json", "notices/copied-asset-notices.txt", "notices/copied-asset-notices-inventory.json"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := imagePackage(build, repo, output); err == nil {
		t.Fatal("existing package accepted")
	}
}

func TestImagePackageRejectsChangedInputs(t *testing.T) {
	for _, kind := range []string{"binary", "notice", "inventory", "receipt", "linked-file", "linked-parent", "missing", "pending", "icon", "font notice", "font inventory", "font receipt", "font binary", "font pending", "copied notice", "copied inventory", "copied receipt", "copied binary", "copied pending"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, output := imageTestFixture(t)
			switch kind {
			case "copied notice":
				imageTestWrite(t, build, "copied-asset-notices.txt", []byte("replacement"))
			case "copied inventory":
				imageTestWrite(t, build, "copied-asset-notices-inventory.json", []byte("{}"))
			case "copied pending":
				imageTestWrite(t, build, "copied-asset-notices-inventory.pending", []byte("{}"))
			case "copied receipt", "copied binary":
				var v map[string]any
				if e := json.Unmarshal(mustImageTestRead(t, build, "copied-asset-notices-inventory.json"), &v); e != nil {
					t.Fatal(e)
				}
				if kind == "copied receipt" {
					v["inputs"] = []any{}
				} else {
					v["binarySha256"] = imageTestHash([]byte("other"))
				}
				imageTestWrite(t, build, "copied-asset-notices-inventory.json", imageTestJSON(t, v))
			case "font notice":
				imageTestWrite(t, build, "text-font-notices.txt", []byte("replacement"))
			case "font inventory":
				imageTestWrite(t, build, "text-font-notices-inventory.json", []byte("{}"))
			case "font receipt", "font binary":
				var v map[string]any
				if err := json.Unmarshal(mustImageTestRead(t, build, "text-font-notices-inventory.json"), &v); err != nil {
					t.Fatal(err)
				}
				if kind == "font receipt" {
					v["inputs"] = []any{}
				} else {
					v["binarySha256"] = imageTestHash([]byte("other"))
				}
				imageTestWrite(t, build, "text-font-notices-inventory.json", imageTestJSON(t, v))
			case "font pending":
				imageTestWrite(t, build, "text-font-notices-inventory.pending", []byte("{}"))
			case "binary":
				imageTestWrite(t, build, "service/zitadel", []byte("other binary\n"))
			case "notice":
				imageTestWrite(t, build, "go-module-notices.txt", []byte("truncated\n"))
			case "inventory":
				imageTestWrite(t, build, "console-notices-inventory.json", []byte("{}"))
			case "receipt":
				imageTestWrite(t, build, "build-receipt.json", []byte("{}"))
			case "missing":
				if err := os.Remove(filepath.Join(build, "console-notices-supplement.txt")); err != nil {
					t.Fatal(err)
				}
			case "pending":
				imageTestWrite(t, build, "build-receipt.pending", []byte("{}"))
			case "icon":
				imageTestWrite(t, repo, "third_party/zitadel-login-icons/NOTICE.md", []byte("other notice"))
			case "linked-file":
				p := filepath.Join(build, "go-module-notices.txt")
				if err := os.Rename(p, p+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".original", p); err != nil {
					t.Fatal(err)
				}
			case "linked-parent":
				p := filepath.Join(build, "service")
				if err := os.Rename(p, p+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+"-original", p); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := imagePackage(build, repo, output); err == nil {
				t.Fatal("changed input admitted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid inputs left a usable package")
			}
		})
	}
}

func mustImageTestRead(t *testing.T, root, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, name))
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestImageContextRejectsSubstitutionAndExtraMembers(t *testing.T) {
	for _, kind := range []string{"binary", "extra", "missing", "manifest", "mode", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, output := imageTestFixture(t)
			seal, err := imagePackage(build, repo, output)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "binary":
				imageTestWrite(t, output, "zitadel", []byte("other binary\n"))
			case "extra":
				imageTestWrite(t, output, "unexpected", []byte("extra"))
			case "manifest":
				imageTestWrite(t, output, "package-manifest.json", []byte("{}"))
			case "mode":
				if err := os.Chmod(filepath.Join(output, "zitadel"), 04755); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(output, "notices/go-module-notices.txt")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				p := filepath.Join(output, "zitadel")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(build, "service/zitadel"), p); err != nil {
					t.Fatal(err)
				}
			}
			if imageVerifyContext(output, seal) == nil {
				t.Fatal("changed context admitted")
			}
		})
	}
}

func TestImageOutputDoesNotAlterInputTrees(t *testing.T) {
	for _, kind := range []string{"inside", "linked-parent"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			output := filepath.Join(build, "unexpected-output")
			if kind == "linked-parent" {
				alias := filepath.Join(t.TempDir(), "linked-input")
				if err := os.Symlink(build, alias); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(alias, "unexpected-output")
			}
			if _, err := imagePackage(build, repo, output); err == nil {
				t.Fatal("input-contained package accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("rejected package altered inputs")
			}
			if err := imageSmoke(build, repo, output, imageSmokeDouble(t, build, "")); err == nil {
				t.Fatal("input-contained smoke accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("rejected smoke altered inputs")
			}
		})
	}
}

func TestImageEntryHasNoUnverifiedPackagingShortcut(t *testing.T) {
	build, repo, output := imageTestFixture(t)
	var out, diagnostic bytes.Buffer
	if imageRun(nil, &out, &diagnostic) != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("ordinary entrypoint is not inert")
	}
	if imageRun([]string{"--package", "--build", build, "--repository", repo, "--output", output}, &out, &diagnostic) == 0 {
		t.Fatal("standalone packaging bypasses completed-input revalidation")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("refused shortcut created output")
	}
}
