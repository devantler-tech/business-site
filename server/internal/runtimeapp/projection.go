package runtimeapp

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ErrCompanyProjection is the entire diagnostic; private paths and bytes stay private.
var ErrCompanyProjection = errors.New("company disclosure projection is unavailable")

// projectionBytes snapshots a Unix projection without changing its ownership or modes.
// Only root/the current user may own it, and group reads require the current primary group.
func projectionBytes(filename string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return nil, ErrCompanyProjection
	}
	defer func() { _ = root.Close() }() // Read-only descriptor cleanup owns no private copy.
	f, err := root.OpenFile(filepath.Base(filename), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrCompanyProjection
	}
	defer func() { _ = f.Close() }() // No source writes or buffered state need flushing.
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0137 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, ErrCompanyProjection
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !projectionOwnerAllowed(owner, info.Mode()) {
		return nil, ErrCompanyProjection
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(data) == 0 || len(data) > 16384 {
		return nil, ErrCompanyProjection
	}
	return data, nil
}

// projectionOwnerAllowed checks metadata from the opened descriptor against this process.
func projectionOwnerAllowed(owner *syscall.Stat_t, mode os.FileMode) bool {
	return owner != nil && (owner.Uid == 0 || uint64(owner.Uid) == uint64(os.Geteuid())) &&
		(mode.Perm()&0040 == 0 || uint64(owner.Gid) == uint64(os.Getegid()))
}

// ReadProjectedCompanyDisclosure snapshots only this public-content input in memory.
// The ordinary owner-only reader remains a separate, strict transport.
func ReadProjectedCompanyDisclosure(filename string) (string, error) {
	if !filepath.IsAbs(filename) || os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return "", ErrCompanyProjection
	}
	data, err := projectionBytes(filename)
	if err != nil {
		return "", ErrCompanyProjection
	}
	value := trimInputLineEnding(string(data))
	if value == "" {
		return "", ErrCompanyProjection
	}
	return value, nil
}
