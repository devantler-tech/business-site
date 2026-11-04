package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

type sourceFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

func sourcePath(name string) bool {
	return name != "." && len(name) <= 180 && fs.ValidPath(name) && !strings.ContainsAny(name, "\\\x00\r\n\t")
}

func sourceParents(root *os.Root, name string) error {
	if !sourcePath(name) {
		return fmt.Errorf("unsafe capsule member path")
	}
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, "/") {
		current = path.Join(current, part)
		st, err := root.Lstat(current)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
			return fmt.Errorf("physical private capsule directory required")
		}
	}
	return nil
}

// Recheck the same descriptor and its pathname after the complete read. Links,
// incomplete data and writable/executable transport copies are not originals.
func readSourceFile(root *os.Root, want sourceFile) ([]byte, error) {
	if !sourcePath(want.Path) || want.Bytes < 1 || want.Bytes > 32<<20 || !sourceDigest.MatchString(want.SHA256) || want.Mode != 0444 {
		return nil, fmt.Errorf("invalid sealed source file")
	}
	if err := sourceParents(root, want.Path); err != nil {
		return nil, err
	}
	before, err := root.Lstat(want.Path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0444 || before.Size() != want.Bytes || !sourceSingleLink(before) {
		return nil, fmt.Errorf("source file type, size, mode or links differ")
	}
	f, err := sourceOpenRegular(root, want.Path)
	if err != nil {
		return nil, err
	}
	opened, statErr := f.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Mode() != before.Mode() || opened.Size() != before.Size() || !sourceSingleLink(opened) {
		_ = f.Close()
		return nil, fmt.Errorf("source path changed before read")
	}
	b, readErr := io.ReadAll(io.LimitReader(f, want.Bytes+1))
	after, afterErr := f.Stat()
	closeErr := f.Close()
	current, currentErr := root.Lstat(want.Path)
	if readErr != nil || afterErr != nil || closeErr != nil || currentErr != nil || int64(len(b)) != want.Bytes {
		return nil, errors.Join(fmt.Errorf("incomplete source snapshot"), readErr, afterErr, closeErr, currentErr)
	}
	for _, st := range []os.FileInfo{after, current} {
		if !st.Mode().IsRegular() || !os.SameFile(before, st) || st.Mode() != before.Mode() || st.Size() != before.Size() || !st.ModTime().Equal(before.ModTime()) || !sourceSingleLink(st) {
			return nil, fmt.Errorf("source file changed during read")
		}
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != want.SHA256 {
		return nil, fmt.Errorf("source file digest differs")
	}
	return b, nil
}

func writeSourceFile(root *os.Root, name string, data []byte) error {
	if len(data) < 1 || len(data) > 8<<20 {
		return fmt.Errorf("source write bound")
	}
	if err := sourceParents(root, name); err != nil {
		return err
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(data)
	syncErr := f.Sync()
	modeErr := f.Chmod(0444)
	closeErr := f.Close()
	if n != len(data) || writeErr != nil || syncErr != nil || modeErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("incomplete exclusive source write"), writeErr, syncErr, modeErr, closeErr)
	}
	want := sourceFile{name, int64(len(data)), fmt.Sprintf("%x", sha256.Sum256(data)), 0444}
	_, err = readSourceFile(root, want)
	return err
}

// removeSourceOutput removes only the regular leaf identified at creation.
// A nil result also covers an absent or replaced leaf, which is preserved.
func removeSourceOutput(name string, created os.FileInfo) error {
	if created == nil {
		return nil
	}
	current, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(current, created) {
		return nil
	}
	return os.Remove(name)
}
