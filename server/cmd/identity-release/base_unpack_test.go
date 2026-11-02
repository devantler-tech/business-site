package main

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseTransportRefusesAlteredMemberStructureWithMatchingOuterHash(t *testing.T) {
	for _, kind := range []string{"mode", "link", "missing", "extra", "duplicate", "order", "pax"} {
		t.Run(kind, func(t *testing.T) {
			p, input, seal := capturedBaseFixture(t)
			archive := filepath.Join(t.TempDir(), "original.tar")
			if _, err := packBaseCapsule(p, input, seal, archive); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			type member struct {
				header tar.Header
				body   []byte
			}
			var entries []member
			tr := tar.NewReader(bytes.NewReader(raw))
			for {
				h, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(tr)
				if err != nil {
					t.Fatal(err)
				}
				entries = append(entries, member{*h, body})
			}
			switch kind {
			case "mode":
				entries[0].header.Mode = 0644
			case "link":
				entries[0].header.Typeflag = tar.TypeSymlink
				entries[0].header.Linkname = "../outside"
				entries[0].header.Size, entries[0].body = 0, nil
			case "missing":
				entries = entries[1:]
			case "extra":
				x := entries[0]
				x.header.Name = "unexpected"
				entries = append(entries, x)
			case "duplicate":
				entries = append(entries, entries[0])
			case "order":
				entries[0], entries[1] = entries[1], entries[0]
			case "pax":
				entries[0].header.Format = tar.FormatPAX
				entries[0].header.PAXRecords = map[string]string{"comment": "not canonical"}
			}
			var changed bytes.Buffer
			tw := tar.NewWriter(&changed)
			for _, entry := range entries {
				if err := tw.WriteHeader(&entry.header); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(entry.body); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(archive, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(archive, changed.Bytes(), 0444); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(archive, 0444); err != nil {
				t.Fatal(err)
			}
			if err := unpackBaseCapsule(p, archive, sourceSHA(changed.Bytes()), seal, filepath.Join(t.TempDir(), "out")); err == nil {
				t.Fatal("altered transport accepted with a matching outer hash")
			}
		})
	}
}

func TestBaseTransportRoundTripUsesOriginalCapsuleAndCanonicalArchive(t *testing.T) {
	p, input, seal := capturedBaseFixture(t)
	archive := filepath.Join(t.TempDir(), "original.tar")
	digest, err := packBaseCapsule(p, input, seal, archive)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "restored")
	if err := unpackBaseCapsule(p, archive, digest, seal, out); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBaseCapsule(p, out, seal); err != nil {
		t.Fatal(err)
	}
	again, err := packBaseCapsule(p, out, seal, filepath.Join(t.TempDir(), "again.tar"))
	if err != nil || again != digest {
		t.Fatalf("canonical archive changed: %s %v", again, err)
	}
}

func TestBaseTransportRefusesIndependentSealsAndAlteredBytes(t *testing.T) {
	for _, kind := range []string{"archive-seal", "capsule-seal", "truncated", "trailing", "linked", "existing", "inside"} {
		t.Run(kind, func(t *testing.T) {
			p, input, seal := capturedBaseFixture(t)
			archive := filepath.Join(t.TempDir(), "original.tar")
			digest, err := packBaseCapsule(p, input, seal, archive)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "restored")
			switch kind {
			case "archive-seal":
				digest = strings.Repeat("0", 64)
			case "capsule-seal":
				seal = strings.Repeat("0", 64)
			case "truncated", "trailing":
				body, err := os.ReadFile(archive)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "truncated" {
					body = body[:len(body)-512]
				} else {
					body = append(body, make([]byte, 512)...)
				}
				if err := os.Chmod(archive, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(archive, body, 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(archive, 0444); err != nil {
					t.Fatal(err)
				}
				digest = sourceSHA(body)
			case "linked":
				link := archive + ".link"
				if err := os.Symlink(archive, link); err != nil {
					t.Fatal(err)
				}
				archive = link
			case "existing":
				if err := os.Mkdir(out, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, "keep"), []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			case "inside":
				out = filepath.Join(archive, "restored")
			}
			if err := unpackBaseCapsule(p, archive, digest, seal, out); err == nil {
				t.Fatal("invalid original transport accepted")
			}
			if kind == "existing" {
				body, err := os.ReadFile(filepath.Join(out, "keep"))
				if err != nil || string(body) != "untouched" {
					t.Fatal("existing output changed")
				}
			}
		})
	}
}
