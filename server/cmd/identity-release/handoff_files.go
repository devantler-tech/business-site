package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type handoffSnapshot struct {
	file sourceFile
	info os.FileInfo
	data []byte
}

func sameHandoffSnapshot(before, after os.FileInfo) bool {
	return before != nil && after != nil && after.Mode().IsRegular() && os.SameFile(before, after) &&
		before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && sourceSingleLink(after)
}

// readHandoffSnapshot reads a complete fixed member through a nonblocking,
// confined regular descriptor. Large archives are streamed, never retained in RAM.
func readHandoffSnapshot(root *os.Root, name string, limit int64, keep bool, mode os.FileMode) (handoffSnapshot, error) {
	var result handoffSnapshot
	if !sourcePath(name) {
		return result, fmt.Errorf("invalid handoff path")
	}
	parent := ""
	if path.Dir(name) != "." {
		for _, part := range strings.Split(path.Dir(name), "/") {
			parent = path.Join(parent, part)
			info, err := root.Lstat(parent)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return result, fmt.Errorf("physical handoff parent required")
			}
		}
	}
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || !sourceSingleLink(before) || before.Size() < 1 || before.Size() > limit {
		return result, fmt.Errorf("handoff member type, links or size differs")
	}
	if (mode != 0 && before.Mode().Perm() != mode) || (mode == 0 && before.Mode().Perm() != 0444 && before.Mode().Perm() != 0600 && before.Mode().Perm() != 0644) {
		return result, fmt.Errorf("handoff member mode differs")
	}
	f, err := sourceOpenRegular(root, name)
	if err != nil {
		return result, err
	}
	opened, err := f.Stat()
	if err != nil || !sameHandoffSnapshot(before, opened) {
		return result, errors.Join(fmt.Errorf("handoff member changed before read"), err, f.Close())
	}
	hash := sha256.New()
	var body bytes.Buffer
	var writer io.Writer = hash
	if keep {
		writer = io.MultiWriter(hash, &body)
	}
	n, readErr := io.CopyBuffer(writer, io.LimitReader(f, before.Size()+1), make([]byte, 1<<20))
	after, statErr := f.Stat()
	closeErr := f.Close()
	current, pathErr := root.Lstat(name)
	if readErr != nil || statErr != nil || closeErr != nil || pathErr != nil || n != before.Size() || !sameHandoffSnapshot(before, after) || !sameHandoffSnapshot(before, current) {
		return result, errors.Join(fmt.Errorf("incomplete or changing handoff snapshot"), readErr, statErr, closeErr, pathErr)
	}
	result.file = sourceFile{Path: name, Bytes: n, SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Mode: uint32(before.Mode().Perm())}
	result.info, result.data = before, body.Bytes()
	return result, nil
}
