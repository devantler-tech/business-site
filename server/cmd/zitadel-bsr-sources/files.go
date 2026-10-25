package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
)

const fileLimit int64 = 8 << 20
const packageLimit int64 = 64 << 20

// checkedRoot refuses a linked root and detects replacement during opening.
func checkedRoot(directory string) (*os.Root, error) {
	before, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("expected an unlinked input directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	after, statErr := root.Stat(".")
	if statErr != nil || !os.SameFile(before, after) {
		return nil, errors.Join(fmt.Errorf("input directory changed"), statErr, root.Close())
	}
	return root, nil
}

// inspectAt rejects linked parents, special members and multiply linked files.
func inspectAt(root *os.Root, name string, executable bool) (os.FileInfo, error) {
	if !inputName(name) {
		return nil, fmt.Errorf("invalid input path")
	}
	parts := strings.Split(name, "/")
	for index := 1; index < len(parts); index++ {
		info, err := root.Lstat(strings.Join(parts[:index], "/"))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked or non-directory parent")
		}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Mode()&0o022 != 0 || (!executable && info.Mode()&0o111 != 0) {
		return nil, fmt.Errorf("unsafe input type or mode")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, fmt.Errorf("ambiguous file identity")
	}
	return info, nil
}

// boundedRead preserves read and close failures, including plausible partial data.
func boundedRead(reader io.ReadCloser, limit int64) (body []byte, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, reader.Close())
		if resultErr != nil {
			body = nil
		}
	}()
	body, resultErr = io.ReadAll(io.LimitReader(reader, limit+1))
	if int64(len(body)) > limit {
		resultErr = errors.Join(resultErr, fmt.Errorf("input exceeds byte limit"))
	}
	return body, resultErr
}

// readInput binds bytes to the same confined regular descriptor inspected before open.
func readInput(root *os.Root, name string, executable bool, limit int64) ([]byte, error) {
	before, err := inspectAt(root, name, executable)
	if err != nil {
		return nil, err
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("input exceeds byte limit")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, statErr := file.Stat()
	if statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() {
		return nil, errors.Join(fmt.Errorf("input changed while opening"), statErr, file.Close())
	}
	body, err := boundedRead(file, limit)
	if err != nil {
		return nil, err
	}
	last, err := inspectAt(root, name, executable)
	if err != nil || !os.SameFile(before, last) || int64(len(body)) != before.Size() || !before.ModTime().Equal(last.ModTime()) {
		return nil, errors.Join(fmt.Errorf("input changed while reading"), err)
	}
	return body, nil
}

// readBudgeted bounds each payload before reading and shares the complete package budget.
func readBudgeted(root *os.Root, name string, executable bool, remaining *int64) ([]byte, error) {
	info, err := inspectAt(root, name, executable)
	if err != nil {
		return nil, err
	}
	if info.Size() > *remaining {
		return nil, fmt.Errorf("input exceeds remaining package budget")
	}
	body, err := readInput(root, name, executable, min(fileLimit, *remaining))
	if err != nil {
		return nil, err
	}
	*remaining -= int64(len(body))
	return body, nil
}

// readTree inventories a complete selected subtree, retaining empty regular files.
func readTree(root *os.Root, prefix string, remaining *int64) (map[string][]byte, error) {
	result := map[string][]byte{}
	err := fs.WalkDir(root.FS(), prefix, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !inputName(name) || len(strings.Split(name, "/")) > 20 {
			return fmt.Errorf("invalid tree path")
		}
		if entry.IsDir() {
			return nil
		}
		body, err := readBudgeted(root, name, false, remaining)
		if err != nil {
			return err
		}
		if len(result) >= 512 {
			return fmt.Errorf("selected tree exceeds limit")
		}
		result[strings.TrimPrefix(name, prefix+"/")] = body
		return nil
	})
	return result, err
}

// writePayload creates private files without execute permissions in a new owned root.
func writePayload(root *os.Root, name string, body []byte) (resultErr error) {
	if !inputName(name) {
		return fmt.Errorf("invalid output path")
	}
	parts := strings.Split(path.Dir(name), "/")
	for index := range parts {
		dir := strings.Join(parts[:index+1], "/")
		if dir == "." {
			continue
		}
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err := root.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe output parent")
		}
		if err := root.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	count, err := file.Write(body)
	if err != nil {
		return err
	}
	if count != len(body) {
		return io.ErrShortWrite
	}
	return nil
}
