//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

func sourceOpenRegular(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	// os.Root may resolve an in-root symlink even with O_NOFOLLOW. Recheck
	// the leaf before reading data; confinement still comes from os.Root and
	// NONBLOCK keeps a swapped FIFO from blocking descriptor acquisition.
	leaf, leafErr := root.Lstat(name)
	if err != nil || leafErr != nil || !st.Mode().IsRegular() || !leaf.Mode().IsRegular() || !os.SameFile(st, leaf) {
		_ = f.Close()
		return nil, fmt.Errorf("regular source descriptor required")
	}
	return f, nil
}

func sourceSingleLink(st os.FileInfo) bool {
	raw, ok := st.Sys().(*syscall.Stat_t)
	return ok && raw.Nlink == 1
}
