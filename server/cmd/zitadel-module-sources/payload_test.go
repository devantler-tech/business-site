package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/sumdb/dirhash"
)

// modulePayload writes real ZIP bytes; expected sums come from Go's implementation.
func modulePayload(t *testing.T, name, version string, names []string) (string, []byte, string, string) {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	mod := []byte("module " + name + "\n\ngo 1.26.0\n")
	for _, member := range names {
		w, err := archive.Create(name + "@" + version + "/" + member)
		if err != nil {
			t.Fatal(err)
		}
		body := []byte("package example\n")
		if member == "go.mod" {
			body = mod
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "module.zip")
	if err := os.WriteFile(file, data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := dirhash.HashZip(file, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	modSum, err := dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(mod)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return file, mod, sum, modSum
}

// TestPayloadIntegrity verifies complete archive bytes, independent of cache markers.
func TestPayloadIntegrity(t *testing.T) {
	file, mod, sum, modSum := modulePayload(t, "example.com/Example", "v1.2.3", []string{"go.mod", "example.go", "empty.txt"})
	if err := verifyPayload("example.com/Example", "v1.2.3", file, mod, sum, modSum); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, version, sum, modSum string
		mod                              []byte
	}{
		{"changed module contents", "example.com/Example", "v1.2.3", sum, modSum, append(bytes.Clone(mod), '\n')},
		{"wrong content sum", "example.com/Example", "v1.2.3", "h1:wrong", modSum, mod},
		{"wrong module sum", "example.com/Example", "v1.2.3", sum, "h1:wrong", mod},
		{"wrong module prefix", "example.com/Other", "v1.2.3", sum, modSum, mod},
		{"wrong version", "example.com/Example", "v1.2.4", sum, modSum, mod},
		{"invalid path", "../example", "v1.2.3", sum, modSum, mod},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyPayload(tc.path, tc.version, file, tc.mod, tc.sum, tc.modSum); err == nil {
				t.Fatal("invalid module payload accepted")
			}
		})
	}
	changed, _, _, _ := modulePayload(t, "example.com/Example", "v1.2.3", []string{"different.go"})
	if err := verifyPayload("example.com/Example", "v1.2.3", changed, mod, sum, modSum); err == nil {
		t.Fatal("changed ZIP with unchanged expected/cache checksum accepted")
	}
}

// TestPayloadStructure rejects malformed ZIPs even when their content sums match.
func TestPayloadStructure(t *testing.T) {
	for _, names := range [][]string{{"../escape.go"}, {"same.go", "same.go"}, {"case.go", "Case.go"}} {
		t.Run(names[0], func(t *testing.T) {
			file, mod, sum, modSum := modulePayload(t, "example.com/module", "v1.0.0", names)
			if err := verifyPayload("example.com/module", "v1.0.0", file, mod, sum, modSum); err == nil {
				t.Fatalf("malformed ZIP accepted: %v", names)
			}
		})
	}
}
