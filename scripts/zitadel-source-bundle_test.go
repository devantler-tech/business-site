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
)

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

func TestSourceBundleCommandIsInertWithoutExplicitMode(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := sourceBundleRun(nil, &out, &diagnostic); code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("unrequested source capture was not inert")
	}
	if code := sourceBundleRun([]string{"--unknown"}, &out, &diagnostic); code == 0 || out.Len() != 0 {
		t.Fatal("unknown source operation was accepted")
	}
}

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

func TestSourceCaptureBindsFrozenPreparationAndBuildRecords(t *testing.T) {
	for _, kind := range []string{"valid", "changed-source", "changed-recipe", "changed-inputs", "wrong-original-api", "wrong-serial-api", "linked-record", "existing-output"} {
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
			for _, name := range []string{"prepare-zitadel-source.sh", "zitadel-source-inputs.sha256", "build-zitadel-linux.sh", "zitadel-source-bundle.go"} {
				write(repo, "scripts/"+name, name+"\n")
			}
			write(repo, "docs/ZITADEL-SOURCE.md", "Build documentation.\n")
			preparation := fmt.Sprintf(`{"schemaVersion":1,"upstreamCommit":"8afa5dd37a8e723efb9eace8d09fd5c1cccef694","recipeSha256":"%s","inputManifestSha256":"%s","imageAdmission":"unknown","licenseClosure":"unknown","metadataProfile":"machine-id-no-jsonpath-v1"}`, hash("prepare-zitadel-source.sh\n"), hash("zitadel-source-inputs.sha256\n"))
			write(build, "prepared/source-preparation.json", preparation)
			profile, err := sourceSealTree(filepath.Join(build, "prepared/source"), filepath.Join(t.TempDir(), "reference"))
			if err != nil {
				t.Fatal(err)
			}
			policy := sourceCapturePolicy{profile, hash("original API\n"), hash("serial API\n"), hash(preparation)}
			output := filepath.Join(t.TempDir(), "captured")
			switch kind {
			case "changed-source":
				write(build, "prepared/source/source.go", "package other!\n")
			case "changed-recipe":
				write(repo, "scripts/prepare-zitadel-source.sh", "changed preparation\n")
			case "changed-inputs":
				write(repo, "scripts/zitadel-source-inputs.sha256", "changed input manifest\n")
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
			got, err := sourceCaptureBuild(build, repo, output, policy)
			if kind == "valid" {
				if err != nil || got.Files != 8 || got.Records != 10 || got.PreparedProfileSHA256 != profile || got.ManifestSHA256 == profile {
					t.Fatalf("incomplete source capture: %+v: %v", got, err)
				}
				manifest, err := sourceVerifyTree(output, got.ManifestSHA256)
				if err != nil || manifest.PreparedProfile != profile || len(manifest.Records) != 10 {
					t.Fatalf("unbound build records: %v", err)
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
func (f *sourceOutputFixture) Chmod(os.FileMode) error {
	if f.mode == "mode" {
		return errors.New("mode failure")
	}
	return nil
}
func (f *sourceOutputFixture) Sync() error {
	if f.mode == "sync" {
		return errors.New("flush failure")
	}
	return nil
}
func (f *sourceOutputFixture) Close() error {
	f.closed++
	if f.mode == "close" {
		return errors.New("close failure")
	}
	return nil
}
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

func (f *sourceReadFixture) Read(p []byte) (int, error) {
	if len(f.body) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.body)
	f.body = f.body[n:]
	return n, f.readError
}

func (f *sourceReadFixture) Close() error {
	f.closed++
	return f.closeError
}

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
