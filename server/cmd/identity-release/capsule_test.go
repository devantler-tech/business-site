package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// All bytes are synthetic data. The fixture preserves the thirteen-layer,
// six-package/five-source and thirteen-derived-file relationships; it does not
// impersonate the frozen public image or contact an upstream service.
func syntheticBaseCollection(t *testing.T) (baseProfile, map[string][]byte) {
	t.Helper()
	p, err := loadBaseProfile()
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][]byte{}
	set := func(spec *sourceObject, data []byte) {
		spec.Bytes = int64(len(data))
		spec.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		raw[spec.Path] = data
	}
	for i := range p.Descriptors {
		d := &p.Descriptors[i]
		checks := ""
		for j := range d.Archives {
			a := &d.Archives[j]
			set(a, []byte("synthetic source "+a.Path))
			checks += fmt.Sprintf(" %s %d %s\n", a.SHA256, a.Bytes, path.Base(a.Path))
		}
		body := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nSource: " + d.Name + "\nVersion: " + d.Version + "\nChecksums-Sha256:\n" + checks + "\n-----BEGIN PGP SIGNATURE-----\nopaque fixture\n-----END PGP SIGNATURE-----\n"
		set(&d.Descriptor, []byte(body))
	}
	ids := []string{}
	for i := range p.Derived {
		d := &p.Derived[i]
		body := []byte("synthetic notice: " + d.ImagePath + "\n")
		if strings.HasPrefix(d.Path, "status/") {
			for _, pkg := range p.Packages {
				if d.Path == "status/"+pkg.Name {
					body = []byte("Package: " + pkg.Name + "\nSource: " + pkg.Source + "\nVersion: " + pkg.Version + "\n")
				}
			}
		}
		d.Bytes = int64(len(body))
		d.SHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
		blob, spec, id := sourceLayerFixture(t, layerMember{d.ImagePath, body, int64(d.ImageMode), tar.TypeReg})
		spec.Path = "layers/" + spec.SHA256 + ".tar.gz"
		spec.URL = "https://gcr.io/v2/distroless/static-debian13/blobs/sha256:" + spec.SHA256
		p.Layers[i] = spec
		raw[spec.Path] = blob
		d.LayerSHA256 = spec.SHA256
		ids = append(ids, id)
	}
	config := map[string]any{"architecture": "amd64", "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": ids}}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	set(&p.Metadata[2], b)
	manifest := map[string]any{"schemaVersion": 2, "config": map[string]any{"digest": "sha256:" + p.Metadata[2].SHA256, "size": p.Metadata[2].Bytes}}
	layers := []any{}
	for _, l := range p.Layers {
		layers = append(layers, map[string]any{"digest": "sha256:" + l.SHA256, "size": l.Bytes})
	}
	manifest["layers"] = layers
	b, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	set(&p.Metadata[1], b)
	index := map[string]any{"schemaVersion": 2, "manifests": []any{map[string]any{"digest": "sha256:" + p.Metadata[1].SHA256, "size": p.Metadata[1].Bytes, "platform": map[string]any{"architecture": "amd64", "os": "linux"}}}}
	b, err = json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	set(&p.Metadata[0], b)
	return p, raw
}

func TestBaseCapsuleCapturesClosedOriginalsAndRefusesLaterChanges(t *testing.T) {
	for _, kind := range []string{"original", "missing", "changed", "writable", "extra", "manifest", "external-seal", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			p, raw := syntheticBaseCollection(t)
			out := filepath.Join(t.TempDir(), "new-capsule")
			read := func(_ context.Context, s sourceObject) ([]byte, error) { return raw[s.Path], nil }
			seal, err := captureBaseCapsule(context.Background(), p, read, out)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(out, p.Descriptors[0].Archives[0].Path)
			switch kind {
			case "missing":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.Chmod(file, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("substituted"), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(file, 0444); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(file, 0644); err != nil {
					t.Fatal(err)
				}
			case "extra":
				if err := os.WriteFile(filepath.Join(out, "unexpected"), []byte("extra"), 0444); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				file = filepath.Join(out, "base-source.json")
				if err := os.Chmod(file, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(`{}`), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(file, 0444); err != nil {
					t.Fatal(err)
				}
			case "external-seal":
				seal = strings.Repeat("0", 64)
			case "symlink":
				if err := os.Rename(file, file+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path.Base(file)+".target", file); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(file, file+".alias"); err != nil {
					t.Fatal(err)
				}
			}
			_, err = verifyBaseCapsule(p, out, seal)
			if kind == "original" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = captureBaseCapsule(context.Background(), p, read, out); err == nil {
					t.Fatal("existing capsule overwritten")
				}
			} else if err == nil {
				t.Fatal("changed or unsealed capsule admitted")
			}
		})
	}
}

func TestBaseCapsuleArchiveIsDeterministicAndNeverOverwrites(t *testing.T) {
	p, raw := syntheticBaseCollection(t)
	dir := t.TempDir()
	root := filepath.Join(dir, "capsule")
	seal, err := captureBaseCapsule(context.Background(), p, func(_ context.Context, s sourceObject) ([]byte, error) { return raw[s.Path], nil }, root)
	if err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a.tar"), filepath.Join(dir, "b.tar")
	first, err := packBaseCapsule(p, root, seal, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := packBaseCapsule(p, root, seal, b)
	if err != nil || first != second {
		t.Fatalf("archive differs: %v", err)
	}
	if _, err = packBaseCapsule(p, root, seal, a); err == nil {
		t.Fatal("archive overwritten")
	}
	data, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(data))
	retained, err := verifyBaseCapsule(p, root, seal)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if h.Typeflag != tar.TypeReg || h.Mode != 0444 || h.Uid != 0 || h.Gid != 0 || h.ModTime.Unix() != 0 {
			t.Fatal("noncanonical archive metadata")
		}
		body, err := io.ReadAll(tr)
		if err != nil || !bytes.Equal(body, retained[h.Name]) || int64(len(body)) != h.Size {
			t.Fatal("archive body differs from verified capsule")
		}
		delete(retained, h.Name)
	}
	if count != 42 || len(retained) != 0 {
		t.Fatalf("archive membership differs: %d", count)
	}
}
