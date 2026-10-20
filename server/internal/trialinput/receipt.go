package trialinput

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// WriteReceipt creates one owner-only inputs.json without replacing any file.
func WriteReceipt(directory string, receipt Receipt) error {
	if !filepath.IsAbs(directory) {
		return errRefused
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode() != os.ModeDir|0700 {
		return errRefused
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return errRefused
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errRefused
	}
	// Closing a read-only directory handle cannot commit or change the receipt.
	defer func() { _ = root.Close() }()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || opened.Mode() != os.ModeDir|0700 {
		return errRefused
	}
	file, err := root.OpenFile("inputs.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errRefused
	}
	encoded := json.NewEncoder(file).Encode(receipt)
	synced := file.Sync()
	closed := file.Close()
	if encoded != nil || synced != nil || closed != nil {
		// This call created the exclusive file. Never remove a pre-existing one.
		if err := root.Remove("inputs.json"); err != nil {
			return errors.Join(errRefused, errors.New("incomplete receipt remains"))
		}
		return errRefused
	}
	return nil
}
