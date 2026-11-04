package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"testing"
)

type layerMember struct {
	Name string
	Data []byte
	Mode int64
	Type byte
}

func sourceLayerFixture(t *testing.T, members ...layerMember) ([]byte, sourceObject, string) {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, m := range members {
		size := int64(len(m.Data))
		if m.Type == tar.TypeSymlink {
			size = 0
		}
		h := &tar.Header{Name: m.Name, Size: size, Mode: m.Mode, Typeflag: m.Type, Linkname: ""}
		if m.Type == tar.TypeSymlink {
			h.Linkname = "elsewhere"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := tw.Write(m.Data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	b := compressed.Bytes()
	return b, sourceObject{Path: "layers/fixture.tar.gz", Bytes: int64(len(b)), SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}, fmt.Sprintf("sha256:%x", sha256.Sum256(raw.Bytes()))
}
func sourceWanted(spec sourceObject, mode uint32) []sourceDerived {
	return []sourceDerived{{ImagePath: "usr/share/doc/fixture/copyright", Path: "notices/fixture-copyright.txt", LayerSHA256: spec.SHA256, Bytes: 4, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))), ImageMode: mode}}
}
func TestBaseLayerReadsOriginalBytesAndModesWithoutExtraction(t *testing.T) {
	for _, mode := range []uint32{0644, 0555} {
		t.Run(fmt.Sprintf("mode-%o", mode), func(t *testing.T) {
			b, s, id := sourceLayerFixture(t, layerMember{Name: "usr/share/doc/fixture/copyright", Data: []byte("test"), Mode: int64(mode), Type: tar.TypeReg})
			files, err := checkBaseLayer(b, s, id, sourceWanted(s, mode))
			if err != nil || len(files.Files) != 1 || !bytes.Equal(files.Files["usr/share/doc/fixture/copyright"].Data, []byte("test")) || files.Files["usr/share/doc/fixture/copyright"].ImageMode != mode {
				t.Fatalf("complete original layer differs: %v", err)
			}
		})
	}
}
func TestBaseLayerRefusesInconsistentOrUnsafeData(t *testing.T) {
	for _, kind := range []string{"compressed-sha", "diffid", "crc", "missing", "duplicate", "linked", "wrong-mode", "wrong-size", "wrong-body", "traversal", "whiteout", "extra-package", "expanded-bound"} {
		t.Run(kind, func(t *testing.T) {
			member := layerMember{Name: "usr/share/doc/fixture/copyright", Data: []byte("test"), Mode: 0644, Type: tar.TypeReg}
			members := []layerMember{member}
			switch kind {
			case "missing":
				members = nil
			case "duplicate":
				members = append(members, member)
			case "linked":
				members[0].Type = tar.TypeSymlink
				members[0].Data = nil
			case "wrong-mode":
				members[0].Mode = 0600
			case "wrong-size":
				members[0].Data = []byte("extra")
			case "wrong-body":
				members[0].Data = []byte("nope")
			case "traversal":
				members = append(members, layerMember{Name: "../escape", Mode: 0644, Type: tar.TypeReg})
			case "whiteout":
				members = append(members, layerMember{Name: "usr/share/doc/.wh.fixture", Mode: 0644, Type: tar.TypeReg})
			case "extra-package":
				members = append(members, layerMember{Name: "var/lib/dpkg/status.d/unexpected", Data: []byte("Package: unexpected\n"), Mode: 0644, Type: tar.TypeReg})
			case "expanded-bound":
				members = append(members, layerMember{Name: "oversized-data", Data: bytes.Repeat([]byte("x"), 9<<20), Mode: 0644, Type: tar.TypeReg})
			}
			b, s, id := sourceLayerFixture(t, members...)
			wanted := sourceWanted(s, 0644)
			if kind == "compressed-sha" {
				s.SHA256 = "wrong"
			}
			if kind == "diffid" {
				id = "sha256:wrong"
			}
			if kind == "crc" {
				b = append([]byte{}, b...)
				b[len(b)-8] ^= 1
				s.SHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
				wanted[0].LayerSHA256 = s.SHA256
			}
			if _, err := checkBaseLayer(b, s, id, wanted); err == nil {
				t.Fatal("inconsistent or unsafe base layer accepted")
			}
		})
	}
}
