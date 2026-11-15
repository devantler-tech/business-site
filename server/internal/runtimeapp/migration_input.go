package runtimeapp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// migrationConnection retains private bytes and removes just one line ending.
// These stricter operator-path checks do not change ordinary web projections.
func migrationConnection(filename string) (string, error) {
	return readMigrationConnection(filename, 0077, false)
}

// migrationProjectedConnection reads a Kubernetes Secret projection, which the
// kubelet writes root-owned and, with the pod's fsGroup, readable by that group.
// Only this non-root job's own primary group may read it; every path, ownership
// and size check is unchanged, and other users still get no access at all.
func migrationProjectedConnection(filename string) (string, error) {
	if os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return "", ErrMigration
	}
	return readMigrationConnection(filename, 0037, true)
}

func readMigrationConnection(filename string, forbidden os.FileMode, ownGroupReads bool) (string, error) {
	data, err := readMigrationInput(filename, 16384, forbidden, ownGroupReads)
	if err != nil {
		return "", ErrMigration
	}
	value := trimInputLineEnding(string(data))
	if value == "" {
		return "", ErrMigration
	}
	return value, nil
}

// readMigrationInput first establishes trust along every selected path, then
// checks ownership, permissions and identity on the descriptor supplying bytes.
// With ownGroupReads, a group-read bit is accepted only for this process's group.
func readMigrationInput(filename string, limit int64, forbidden os.FileMode, ownGroupReads bool) ([]byte, error) {
	expected, err := trustedMigrationPath(filename)
	if err != nil {
		return nil, ErrMigration
	}
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return nil, ErrMigration
	}
	defer func() {
		// Read-only descriptor cleanup cannot change the input validation verdict.
		_ = root.Close()
	}()
	file, err := root.OpenFile(filepath.Base(filename), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrMigration
	}
	defer func() {
		// No writes need flushing; preserve the read and validation verdict.
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !migrationOperatorOwns(info) || info.Mode().Perm()&forbidden != 0 || !os.SameFile(expected, info) {
		return nil, ErrMigration
	}
	if ownGroupReads && info.Mode().Perm()&0040 != 0 && !migrationOwnGroup(info) {
		return nil, ErrMigration
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, ErrMigration
	}
	return data, nil
}

// trustedMigrationPath walks before descending, including every symlink target
// component rather than only the final resolved spelling. A root/operator-owned
// sticky shared directory protects each selected root/operator-owned entry;
// non-sticky group/other-writable directories do not. Links are bounded, and the
// later os.Root open independently retains projected-file escape confinement.
func trustedMigrationPath(filename string) (os.FileInfo, error) {
	if !filepath.IsAbs(filename) || filepath.Clean(filename) != filename {
		return nil, ErrMigration
	}
	info, err := os.Lstat("/")
	if err != nil || !migrationOperatorOwns(info) || !migrationDirectoryTrusted(info) {
		return nil, ErrMigration
	}
	directory := "/"
	pending := strings.Split(strings.TrimPrefix(filename, "/"), "/")
	links := 0
	for len(pending) != 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			directory = filepath.Dir(directory)
			continue
		}
		selected := filepath.Join(directory, part)
		info, err := os.Lstat(selected)
		if err != nil || !migrationOperatorOwns(info) {
			return nil, ErrMigration
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 40 {
				return nil, ErrMigration
			}
			target, err := os.Readlink(selected)
			if err != nil || target == "" {
				return nil, ErrMigration
			}
			if filepath.IsAbs(target) {
				directory = "/"
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if info.IsDir() {
			if !migrationDirectoryTrusted(info) {
				return nil, ErrMigration
			}
			directory = selected
			continue
		}
		if len(pending) != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
			return nil, ErrMigration
		}
		return info, nil
	}
	return nil, ErrMigration
}

// Ownership, not a read-only mode, determines who may replace trust material.
func migrationOperatorOwns(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat != nil && (stat.Uid == 0 || int64(stat.Uid) == int64(os.Geteuid()))
}

// migrationOwnGroup reports whether the file's group is this process's primary group.
func migrationOwnGroup(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat != nil && os.Getegid() > 0 && int64(stat.Gid) == int64(os.Getegid())
}

func migrationDirectoryTrusted(info os.FileInfo) bool {
	return info.IsDir() && (info.Mode().Perm()&0022 == 0 || info.Mode()&os.ModeSticky != 0)
}
