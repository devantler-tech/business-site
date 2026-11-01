//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A pathname swapped to this FIFO after Lstat must never make descriptor
// acquisition wait for another process. The focused RED run has a 2s deadline.
func TestSourceOpenRegularDoesNotWaitForFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "leaf"), 0600); err != nil {
		t.Fatal(err)
	}
	if f, err := sourceOpenRegular(openSourceFixtureRoot(t, dir), "leaf"); err == nil {
		_ = f.Close()
		t.Fatal("FIFO admitted as regular source descriptor")
	}
}

func TestSourceOpenRegularRefusesAReplacedSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "original"), []byte("original"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original", filepath.Join(dir, "leaf")); err != nil {
		t.Fatal(err)
	}
	if f, err := sourceOpenRegular(openSourceFixtureRoot(t, dir), "leaf"); err == nil {
		_ = f.Close()
		t.Fatal("replaced source symlink followed")
	}
}
