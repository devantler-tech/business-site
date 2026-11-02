package customeridentity

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type materialSnapshot struct {
	root     *os.Root
	path     string
	identity os.FileInfo
	files    map[string][]byte
	metadata map[string]os.FileInfo
}

// materialPath requires the caller's explicit canonical absolute private location.
func materialPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved == path
}

// materialOwned rejects shared permissions, links and nonregular credential inputs.
func materialOwned(info os.FileInfo, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(stat.Uid) != int64(os.Geteuid()) || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 && stat.Nlink == 1 && info.Size() > 0 && info.Size() <= 65536
}

// materialMembers verifies complete membership through the retained directory descriptor.
func materialMembers(root *os.Root, names []string) error {
	directory, err := root.Open(".")
	if err != nil {
		return errMaterial
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil || closeErr != nil || len(entries) != len(names) {
		return errMaterial
	}
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return errMaterial
		}
	}
	return nil
}

// materialRead retains independent bounded snapshots and their original file identities.
func materialRead(path string, names []string) (*materialSnapshot, error) {
	if !materialPath(path) {
		return nil, errMaterial
	}
	identity, err := os.Lstat(path)
	if err != nil || !materialOwned(identity, true) {
		return nil, errMaterial
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errMaterial
	}
	snapshot := &materialSnapshot{root: root, path: path, identity: identity, files: map[string][]byte{}, metadata: map[string]os.FileInfo{}}
	success := false
	defer func() {
		if !success {
			snapshot.close()
		}
	}()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(identity, actual) || materialMembers(root, names) != nil {
		return nil, errMaterial
	}
	for _, name := range names {
		data, info, err := materialReadFile(root, name)
		if err != nil {
			return nil, errMaterial
		}
		snapshot.files[name], snapshot.metadata[name] = bytes.Clone(data), info
	}
	if err := snapshot.unchanged(); err != nil {
		return nil, errMaterial
	}
	success = true
	return snapshot, nil
}

// materialReadFile refuses symlink/FIFO races before performing bounded descriptor reads.
func materialReadFile(root *os.Root, name string) (contents []byte, identity os.FileInfo, result error) {
	before, err := root.Lstat(name)
	if err != nil || !materialOwned(before, false) {
		return nil, nil, errMaterial
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, errMaterial
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			contents, identity, result = nil, nil, errMaterial
		}
	}()
	opened, err := file.Stat()
	if err != nil || !materialOwned(opened, false) || !os.SameFile(before, opened) {
		return nil, nil, errMaterial
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	after, statErr := file.Stat()
	pathInfo, pathErr := root.Lstat(name)
	if err != nil || statErr != nil || pathErr != nil || !materialOwned(after, false) || !os.SameFile(opened, after) || !os.SameFile(after, pathInfo) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(data)) != after.Size() {
		return nil, nil, errMaterial
	}
	return data, after, nil
}

// unchanged rejects a replaced directory, altered member, mode or credential bytes.
func (snapshot *materialSnapshot) unchanged() error {
	identity, err := os.Lstat(snapshot.path)
	if err != nil || !materialOwned(identity, true) || !os.SameFile(identity, snapshot.identity) {
		return errMaterial
	}
	names := make([]string, 0, len(snapshot.files))
	for name := range snapshot.files {
		names = append(names, name)
	}
	if materialMembers(snapshot.root, names) != nil {
		return errMaterial
	}
	for name, want := range snapshot.files {
		data, info, err := materialReadFile(snapshot.root, name)
		before := snapshot.metadata[name]
		if err != nil || !os.SameFile(info, before) || info.Size() != before.Size() || !info.ModTime().Equal(before.ModTime()) || !bytes.Equal(data, want) {
			return errMaterial
		}
	}
	return nil
}

// close releases only the private input directory descriptor.
func (snapshot *materialSnapshot) close() {
	if snapshot.root != nil {
		_ = snapshot.root.Close()
	}
}

// materialWriteOutputs reserves a new private directory after complete validation.
// On failure, only unchanged files created here are removed through its pinned root.
// The empty failed reservation is retained; a replacement directory is never removed.
func materialWriteOutputs(path string, outputs map[string][]byte, snapshot *materialSnapshot) (err error) {
	parent := filepath.Dir(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !materialPath(parent) || path == snapshot.path || strings.HasPrefix(path, snapshot.path+string(filepath.Separator)) || len(outputs) == 0 {
		return errMaterial
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return errMaterial
	}
	identity, err := os.Lstat(path)
	if err != nil || !materialOwned(identity, true) {
		return errMaterial
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return errMaterial
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			err = errMaterial
		}
	}()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(identity, actual) {
		return errMaterial
	}
	created := map[string]os.FileInfo{}
	success := false
	defer func() {
		if success {
			return
		}
		for name, original := range created {
			current, e := root.Lstat(name)
			if e == nil && os.SameFile(original, current) {
				_ = root.Remove(name)
			}
		}
	}()
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errMaterial
		}
		info, e := file.Stat()
		if e != nil {
			_ = file.Close()
			return errMaterial
		}
		created[name] = info
		n, e := file.Write(outputs[name])
		closeErr := file.Close()
		if e != nil || closeErr != nil || n != len(outputs[name]) {
			return errMaterial
		}
	}
	current, err := os.Lstat(path)
	if err != nil || !materialOwned(current, true) || !os.SameFile(identity, current) || materialMembers(root, names) != nil || snapshot.unchanged() != nil {
		return errMaterial
	}
	for _, name := range names {
		data, info, e := materialReadFile(root, name)
		if e != nil || !os.SameFile(info, created[name]) || !bytes.Equal(data, outputs[name]) {
			return errMaterial
		}
	}
	success = true
	return nil
}
