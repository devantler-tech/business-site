package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// sealedArchive keeps the digest observation, open descriptor and physical
// parent together so both transport readers enforce the same snapshot checks.
type sealedArchive struct {
	parent   *os.Root
	file     *os.File
	original handoffSnapshot
	name     string
}

// openSealedArchive joins a bounded seal observation to the descriptor used for
// restoration, refusing a different filesystem snapshot between those reads.
func openSealedArchive(input, digest string, limit int64) (_ *sealedArchive, resultErr error) {
	parent, err := os.OpenRoot(filepath.Dir(input))
	if err != nil {
		return nil, err
	}
	var file *os.File
	defer func() {
		if resultErr != nil {
			if file != nil {
				resultErr = errors.Join(resultErr, file.Close())
			}
			resultErr = errors.Join(resultErr, parent.Close())
		}
	}()
	name := filepath.Base(input)
	original, err := readHandoffSnapshot(parent, name, limit, false, 0444)
	if err != nil || original.file.SHA256 != digest {
		return nil, fmt.Errorf("original archive seal differs")
	}
	file, err = sourceOpenRegular(parent, name)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !sameHandoffSnapshot(original.info, opened) {
		return nil, fmt.Errorf("archive changed before restoration")
	}
	return &sealedArchive{parent: parent, file: file, original: original, name: name}, nil
}

// unchanged requires both the open descriptor and its pathname to retain the
// original snapshot metadata after the archive has been consumed.
func (a *sealedArchive) unchanged() error {
	current, statErr := a.file.Stat()
	entry, entryErr := a.parent.Lstat(a.name)
	if statErr != nil || entryErr != nil || !sameHandoffSnapshot(a.original.info, current) || !sameHandoffSnapshot(a.original.info, entry) {
		return fmt.Errorf("archive changed during restoration")
	}
	return nil
}

// close preserves failures from either retained descriptor instead of reporting
// successful restoration after an incomplete close.
func (a *sealedArchive) close() error { return errors.Join(a.file.Close(), a.parent.Close()) }

// reserveSourceRoot creates only a new private leaf. Failed reservations remain
// ineligible; an existing directory is never opened for restoration.
func reserveSourceRoot(output string) (string, *os.Root, error) {
	name, err := sourceOutputPath(output)
	if err != nil {
		return "", nil, err
	}
	if err = os.Mkdir(name, 0700); err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(name)
	return name, root, err
}

// makeBaseCapsuleDirs creates only the closed base-source layout in an already
// reserved private root; pre-existing members remain an error.
func makeBaseCapsuleDirs(root *os.Root) error {
	for _, dir := range []string{"metadata", "layers", "sources", "notices", "status"} {
		if err := root.Mkdir(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}
