package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGraphMetadataCLIRejectsArtifactAuthority prevents selecting a checksum key through retained data.
func TestGraphMetadataCLIRejectsArtifactAuthority(t *testing.T) {
	build, source, key, sums := graphMetadataFixture(t)
	out := filepath.Join(physicalGeneratorTestDirectory(t), "metadata")
	got, err := captureGoGraphMetadata(build, source, out, key, sums)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = run([]string{"verify-graph-metadata", "--package", out, "--sha256", got.ManifestSHA256, "--source-sha256", source}, &output)
	if err == nil || output.Len() != 0 {
		t.Fatalf("artifact authority accepted: %q %v", output.String(), err)
	}
}

// TestGraphMetadataExistingOutputIsPreserved prevents reuse of another package directory.
func TestGraphMetadataExistingOutputIsPreserved(t *testing.T) {
	build, source, key, sums := graphMetadataFixture(t)
	out := physicalGeneratorTestDirectory(t)
	writeFixture(t, filepath.Join(out, "keep"), []byte("existing"))
	got, err := captureGoGraphMetadata(build, source, out, key, sums)
	body, readErr := os.ReadFile(filepath.Join(out, "keep"))
	if err == nil || got.ManifestSHA256 != "" || readErr != nil || string(body) != "existing" {
		t.Fatal("existing package changed", err, readErr)
	}
}

// graphMetadataFixture uses the real checksum client and signed local log fixture.
func graphMetadataFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	build, _, source, key := generatorSourceFixture(t)
	writeFixture(t, filepath.Join(build, "prepared/source/go.sum"), nil)
	return build, source, key, hash(nil)
}

// TestGraphMetadataPortableReadback prevents losing resolver inputs outside ZIP capture.
func TestGraphMetadataPortableReadback(t *testing.T) {
	build, source, key, sums := graphMetadataFixture(t)
	out := filepath.Join(physicalGeneratorTestDirectory(t), "metadata")
	got, err := captureGoGraphMetadata(build, source, out, key, sums)
	if err != nil {
		t.Fatal(err)
	}
	if got.Modules != 12 || got.MetadataFiles != 24 || !got.GoGraphMetadataCaptured || got.DependencyInputsCaptured || got.NativeOfflineReplayProved {
		t.Fatalf("incorrect scope: %+v", got)
	}
	copyRoot := filepath.Join(physicalGeneratorTestDirectory(t), "moved")
	if err := os.CopyFS(copyRoot, os.DirFS(out)); err != nil {
		t.Fatal(err)
	}
	checked, err := verifyGoGraphMetadata(copyRoot, got.ManifestSHA256, source, key, sums)
	if err != nil || checked != got {
		t.Fatalf("portable metadata: %+v %v", checked, err)
	}
}

// TestGraphMetadataIncludesResolverOnlyMod prevents equating binary modules with the resolver graph.
func TestGraphMetadataIncludesResolverOnlyMod(t *testing.T) {
	build, source, key, _ := graphMetadataFixture(t)
	mod := []byte("module example.com/graph-only\n\ngo 1.16\n")
	sums := []byte("example.com/graph-only v1.0.0/go.mod h1:TPma/ATNdyP34QZ2DJWidLvwiyRGULeORPHPK6nQMNc=\n")
	writeFixture(t, filepath.Join(build, "prepared/source/go.sum"), sums)
	writeFixture(t, filepath.Join(build, "go-modules/cache/download/example.com/graph-only/@v/v1.0.0.mod"), mod)
	out := filepath.Join(physicalGeneratorTestDirectory(t), "metadata")
	got, err := captureGoGraphMetadata(build, source, out, key, hash(sums))
	if err != nil || got.Modules != 13 || got.MetadataFiles != 25 {
		t.Fatalf("resolver-only metadata lost: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "metadata/example.com/graph-only/@v/v1.0.0.mod")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "metadata/example.com/graph-only/@v/v1.0.0.zip")); !os.IsNotExist(err) {
		t.Fatal("unselected ZIP retained")
	}
}

// TestGraphMetadataRejectsAmbiguousOrOversizedInfo pins the actual metadata read boundary.
func TestGraphMetadataRejectsAmbiguousOrOversizedInfo(t *testing.T) {
	for _, body := range []string{`{"Version":"v1.0.0"} {}`, `{"Version":"v1.0.0","Version":"v2.0.0"}`, `{"Version":"v1.0.0","Origin":{"URL":"a","URL":"b"}}`, strings.Repeat("x", (64<<10)+1)} {
		build, source, key, sums := graphMetadataFixture(t)
		changed := false
		err := filepath.WalkDir(build, func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !changed && !entry.IsDir() && strings.HasSuffix(name, ".info") {
				changed = true
				return os.WriteFile(name, []byte(body), 0o600)
			}
			return nil
		})
		if err != nil || !changed {
			t.Fatal("fixture not changed", err)
		}
		if _, err := captureGoGraphMetadata(build, source, filepath.Join(physicalGeneratorTestDirectory(t), "metadata"), key, sums); err == nil {
			t.Fatal("unsafe info admitted")
		}
	}
}

// TestGraphMetadataCaptureRefusesUnverifiableInput catches fail-open discovery or hashing.
func TestGraphMetadataCaptureRefusesUnverifiableInput(t *testing.T) {
	for _, kind := range []string{"mod", "info", "duplicate", "lookup", "tile", "latest", "sum", "key", "link"} {
		t.Run(kind, func(t *testing.T) {
			build, source, key, sums := graphMetadataFixture(t)
			switch kind {
			case "sum":
				sums = strings.Repeat("b", 64)
			case "key":
				key = officialGeneratorSumDBKey
			default:
				changed := false
				err := filepath.WalkDir(build, func(name string, entry os.DirEntry, err error) error {
					if err != nil || changed || entry.IsDir() {
						return err
					}
					matches := (kind == "mod" || kind == "link") && strings.HasSuffix(name, ".mod") ||
						(kind == "info" || kind == "duplicate") && strings.HasSuffix(name, ".info") ||
						(kind == "lookup" && strings.Contains(name, "/lookup/")) ||
						(kind == "tile" && strings.Contains(name, "/tile/")) ||
						(kind == "latest" && strings.HasSuffix(name, "/latest"))
					if !matches {
						return nil
					}
					changed = true
					if kind == "link" {
						if err := os.Remove(name); err != nil {
							return err
						}
						return os.Symlink("/etc/hosts", name)
					}
					body := []byte("altered")
					if kind == "duplicate" {
						body = []byte(`{"Version":"v1.0.0","version":"v2.0.0"}`)
					}
					return os.WriteFile(name, body, 0o600)
				})
				if err != nil || !changed {
					t.Fatalf("control not applied: %v", err)
				}
			}
			out := filepath.Join(physicalGeneratorTestDirectory(t), "metadata")
			got, err := captureGoGraphMetadata(build, source, out, key, sums)
			if err == nil || got.ManifestSHA256 != "" {
				t.Fatalf("unsafe capture admitted: %+v %v", got, err)
			}
		})
	}
}

// TestGraphMetadataReadbackRequiresClosedMembership prevents sealed partial copies.
func TestGraphMetadataReadbackRequiresClosedMembership(t *testing.T) {
	for _, kind := range []string{"changed", "missing", "extra", "seal", "source"} {
		t.Run(kind, func(t *testing.T) {
			build, source, key, sums := graphMetadataFixture(t)
			out := filepath.Join(physicalGeneratorTestDirectory(t), "metadata")
			got, err := captureGoGraphMetadata(build, source, out, key, sums)
			if err != nil {
				t.Fatal(err)
			}
			seal := got.ManifestSHA256
			switch kind {
			case "changed":
				writeFixture(t, filepath.Join(out, "records/go.sum"), []byte("changed"))
			case "missing":
				if err := os.Remove(filepath.Join(out, "records/go.sum")); err != nil {
					t.Fatal(err)
				}
			case "extra":
				writeFixture(t, filepath.Join(out, "extra"), []byte("unrelated"))
			case "seal":
				seal = strings.Repeat("c", 64)
			case "source":
				source = strings.Repeat("d", 64)
			}
			checked, err := verifyGoGraphMetadata(out, seal, source, key, sums)
			if err == nil || checked.ManifestSHA256 != "" {
				t.Fatalf("unsafe readback admitted: %+v %v", checked, err)
			}
		})
	}
}
