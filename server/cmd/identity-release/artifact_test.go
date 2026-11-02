package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func artifactFixture(t *testing.T, kind string) (string, string) {
	t.Helper()
	_, carrier, receipt := carrierFixture(t)
	body, err := os.ReadFile(carrier)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// JSON object order does not convey authority; closed decoding rejects unknown fields.
	encoded = append(encoded, '\n')
	if kind == "authority" {
		receipt["imagePublished"] = true
		encoded, _ = json.Marshal(receipt)
	}
	if kind == "source" {
		receipt["sourceSha"] = strings.Repeat("b", 40)
		encoded, _ = json.Marshal(receipt)
	}
	if kind == "inner-seal" {
		receipt["archiveSha256"] = strings.Repeat("9", 64)
		encoded, _ = json.Marshal(receipt)
	}
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	names := []string{"carrier.tar", "carrier.json"}
	switch kind {
	case "extra":
		names = append(names, "extra")
	case "duplicate":
		names = append(names, "carrier.json")
	case "traversal":
		names[0] = "../carrier.tar"
	case "missing":
		names = names[:1]
	}
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0600)
		if kind == "link" && name == "carrier.tar" {
			h.SetMode(os.ModeSymlink | 0777)
		}
		member, e := zw.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		b := body
		if name != "carrier.tar" {
			b = encoded
		}
		if _, e = member.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	b := raw.Bytes()
	if kind == "truncated" {
		b = b[:len(b)-8]
	}
	name := filepath.Join(t.TempDir(), "artifact.zip")
	if err = os.WriteFile(name, b, 0444); err != nil {
		t.Fatal(err)
	}
	return name, sourceSHA(b)
}
func TestArtifactRestorationAuthenticatesOuterBytesAndClosedRoles(t *testing.T) {
	for _, kind := range []string{"valid", "digest", "authority", "source", "inner-seal", "extra", "duplicate", "traversal", "missing", "link", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			input, seal := artifactFixture(t, kind)
			if kind == "digest" {
				seal = strings.Repeat("9", 64)
			}
			output := filepath.Join(t.TempDir(), "artifact")
			args := []string{"restore-artifact", "--input", input, "--sha256", seal, "--source-sha", strings.Repeat("a", 40), "--run-id", "123", "--run-attempt", "1", "--output", output}
			err := runRelease(context.Background(), args, &bytes.Buffer{})
			if kind != "valid" {
				if err == nil {
					t.Fatal("not admitted artifact accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(output, "native", carrierHandoff)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
