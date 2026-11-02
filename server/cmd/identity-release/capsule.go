package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type baseSourceManifest struct {
	SchemaVersion              int          `json:"schemaVersion"`
	Profile                    baseProfile  `json:"profile"`
	Files                      []sourceFile `json:"files"`
	BaseImageSignatureVerified bool         `json:"baseImageSignatureVerified"`
	OpenPGPVerified            bool         `json:"openpgpVerified"`
	SourceReleaseVerified      bool         `json:"sourceReleaseVerified"`
	ImageAdmission             bool         `json:"imageAdmission"`
}

func baseSourceFiles(p baseProfile) []sourceFile {
	var files []sourceFile
	for _, o := range baseObjects(p) {
		files = append(files, sourceFile{o.Path, o.Bytes, o.SHA256, 0444})
	}
	for _, o := range p.Derived {
		files = append(files, sourceFile{o.Path, o.Bytes, o.SHA256, 0444})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func baseManifestBytes(p baseProfile) ([]byte, error) {
	m := baseSourceManifest{SchemaVersion: 1, Profile: p, Files: baseSourceFiles(p)}
	b, err := json.MarshalIndent(m, "", "  ")
	return append(b, '\n'), err
}

func sourceSHA(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// sourceOutputPath normalizes the destination through its existing physical parent.
// It neither creates nor reserves the leaf; callers must create it exclusively.
func sourceOutputPath(name string) (string, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return "", fmt.Errorf("absolute clean output path required")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(name))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(name)), nil
}

// captureBaseCapsule collects the complete frozen originals before reserving a
// fresh destination, then independently reads back the sealed private capsule.
func captureBaseCapsule(ctx context.Context, p baseProfile, read sourceReader, output string) (seal string, resultErr error) {
	name, err := sourceOutputPath(output)
	if err != nil {
		return "", err
	}
	if _, err = os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("new capsule destination required")
	}
	raw, err := collectBaseOriginals(ctx, p, read)
	if err != nil {
		return "", err
	}
	if err = os.Mkdir(name, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if err = makeBaseCapsuleDirs(root); err != nil {
		return "", err
	}
	files := baseSourceFiles(p)
	if len(files) != 41 || len(raw) != len(files) {
		return "", fmt.Errorf("complete base capsule required")
	}
	for _, f := range files {
		if err = writeSourceFile(root, f.Path, raw[f.Path]); err != nil {
			return "", err
		}
	}
	b, err := baseManifestBytes(p)
	if err != nil {
		return "", err
	}
	if err = writeSourceFile(root, "base-source.json", b); err != nil {
		return "", err
	}
	seal = sourceSHA(b)
	if _, err = verifyBaseCapsule(p, name, seal); err != nil {
		return "", err
	}
	return seal, nil
}

func openBaseCapsule(name string) (*os.Root, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, fmt.Errorf("absolute clean capsule root required")
	}
	st, err := os.Lstat(name)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("private physical capsule root required")
	}
	return os.OpenRoot(name)
}

// The manifest is compared with the complete frozen declaration, not used to
// select arbitrary paths or hashes. Its external seal is independently required.
func verifyBaseCapsule(p baseProfile, name, seal string) (result map[string][]byte, resultErr error) {
	if !sourceDigest.MatchString(seal) {
		return nil, fmt.Errorf("external capsule seal required")
	}
	expected, err := baseManifestBytes(p)
	if err != nil {
		return nil, err
	}
	if sourceSHA(expected) != seal {
		return nil, fmt.Errorf("frozen capsule manifest seal differs")
	}
	root, err := openBaseCapsule(name)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	files := append(baseSourceFiles(p), sourceFile{"base-source.json", int64(len(expected)), seal, 0444})
	wanted := map[string]bool{}
	for _, f := range files {
		if wanted[f.Path] {
			return nil, fmt.Errorf("duplicate capsule declaration")
		}
		wanted[f.Path] = true
	}
	seen := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			if path != "metadata" && path != "layers" && path != "sources" && path != "notices" && path != "status" {
				return fmt.Errorf("extra capsule directory")
			}
			st, e := root.Lstat(path)
			if e != nil || st.Mode().Perm() != 0700 {
				return fmt.Errorf("capsule directory mode differs")
			}
			return nil
		}
		if !wanted[path] || !d.Type().IsRegular() {
			return fmt.Errorf("extra or nonregular capsule member")
		}
		seen++
		return nil
	})
	if err != nil || seen != 42 {
		return nil, errors.Join(fmt.Errorf("closed capsule membership differs"), err)
	}
	raw := map[string][]byte{}
	for _, spec := range files {
		b, e := readSourceFile(root, spec)
		if e != nil {
			return nil, e
		}
		raw[spec.Path] = b
	}
	if !bytes.Equal(raw["base-source.json"], expected) {
		return nil, fmt.Errorf("capsule declaration differs")
	}
	originals := map[string][]byte{}
	for _, o := range baseObjects(p) {
		originals[o.Path] = raw[o.Path]
	}
	derived, err := checkBaseContents(p, originals)
	if err != nil {
		return nil, err
	}
	for name, b := range derived {
		if !bytes.Equal(raw[name], b) {
			return nil, fmt.Errorf("retained derived body differs from original layer")
		}
	}
	return raw, nil
}

// Canonical tar is a transport of verified data only. Original archives remain
// opaque regular members; no programs or archive entries are extracted or run.
func packBaseCapsule(p baseProfile, input, seal, output string) (digest string, resultErr error) {
	raw, err := verifyBaseCapsule(p, input, seal)
	if err != nil {
		return "", err
	}
	name, err := sourceOutputPath(output)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(input)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(physical, name)
	if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive must be outside capsule")
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	created, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return "", err
	}
	success := false
	defer func() {
		_ = f.Close()
		if !success || resultErr != nil {
			_ = removeSourceOutput(name, created)
		}
	}()
	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, h))
	paths := make([]string, 0, len(raw))
	for name := range raw {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	var total int64
	for _, name := range paths {
		b := raw[name]
		total += int64(len(b))
		if total > 32<<20 {
			return "", fmt.Errorf("capsule transport bound")
		}
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0444, Size: int64(len(b)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}); err != nil {
			return "", err
		}
		if _, err = tw.Write(b); err != nil {
			return "", err
		}
	}
	if err = tw.Close(); err != nil {
		return "", err
	}
	if _, err = verifyBaseCapsule(p, input, seal); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Chmod(0444); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	// Admit the actual closed transport file, not only the bytes handed to tar.
	st, err := os.Lstat(name)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 32<<20 {
		return "", fmt.Errorf("completed archive type or length differs")
	}
	parent, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	digest = fmt.Sprintf("%x", h.Sum(nil))
	if _, err = readSourceFile(parent, sourceFile{filepath.Base(name), st.Size(), digest, 0444}); err != nil {
		return "", err
	}
	success = true
	return digest, nil
}
