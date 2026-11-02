package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// unpackBaseCapsule admits the transport against the existing frozen capsule
// plan. It never uses the incoming manifest to select paths or source hashes.
// Failed private reservations remain ineligible and cannot be resumed.
func unpackBaseCapsule(p baseProfile, input, archiveSeal, capsuleSeal, output string) (resultErr error) {
	if !sourceDigest.MatchString(archiveSeal) || !sourceDigest.MatchString(capsuleSeal) || !filepath.IsAbs(input) || filepath.Clean(input) != input {
		return fmt.Errorf("physical archive and both external seals required")
	}
	manifest, err := baseManifestBytes(p)
	if err != nil || sourceSHA(manifest) != capsuleSeal {
		return fmt.Errorf("frozen capsule manifest seal differs")
	}
	st, err := os.Lstat(input)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 32<<20 {
		return fmt.Errorf("bounded regular base transport required")
	}
	parent, err := os.OpenRoot(filepath.Dir(input))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	body, err := readSourceFile(parent, sourceFile{filepath.Base(input), st.Size(), archiveSeal, 0444})
	if err != nil {
		return err
	}
	name, err := sourceOutputPath(output)
	if err != nil {
		return err
	}
	if err = os.Mkdir(name, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	for _, dir := range []string{"metadata", "layers", "sources", "notices", "status"} {
		if err = root.Mkdir(dir, 0700); err != nil {
			return err
		}
	}
	files := append(baseSourceFiles(p), sourceFile{"base-source.json", int64(len(manifest)), capsuleSeal, 0444})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	tr := tar.NewReader(bytes.NewReader(body))
	canonical := sha256.New()
	tw := tar.NewWriter(canonical)
	for _, f := range files {
		header, err := tr.Next()
		if err != nil || header.Name != f.Path || header.Typeflag != tar.TypeReg || header.Size != f.Bytes || header.Mode != 0444 {
			return fmt.Errorf("closed base transport member differs: %s", f.Path)
		}
		data, err := io.ReadAll(io.LimitReader(tr, f.Bytes+1))
		if err != nil || int64(len(data)) != f.Bytes || sourceSHA(data) != f.SHA256 {
			return fmt.Errorf("complete base transport member differs: %s", f.Path)
		}
		if err = tw.WriteHeader(&tar.Header{Name: f.Path, Mode: 0444, Size: f.Bytes, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}); err != nil {
			return err
		}
		if _, err = tw.Write(data); err != nil {
			return err
		}
		if err = writeSourceFile(root, f.Path, data); err != nil {
			return err
		}
	}
	if _, err = tr.Next(); err != io.EOF {
		return fmt.Errorf("extra or malformed base transport member")
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if fmt.Sprintf("%x", canonical.Sum(nil)) != archiveSeal {
		return fmt.Errorf("noncanonical base transport")
	}
	_, err = verifyBaseCapsule(p, name, capsuleSeal)
	return err
}
