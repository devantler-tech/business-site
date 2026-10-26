package main

import (
	"os"
	"path/filepath"
	"testing"
)

// successfulSourceCapture shares only fixture reservation and failure reporting.
func successfulSourceCapture[T any](t *testing.T, capture func(string) (T, error)) (string, T) {
	t.Helper()
	out := filepath.Join(physicalGeneratorTestDirectory(t), "captured")
	got, err := capture(out)
	if err != nil {
		t.Fatal(err)
	}
	return out, got
}

// copiedSourceFixture makes a fresh physical copy for independent readback.
func copiedSourceFixture(t *testing.T, directory string) string {
	t.Helper()
	copyRoot := filepath.Join(physicalGeneratorTestDirectory(t), "moved")
	if err := os.CopyFS(copyRoot, os.DirFS(directory)); err != nil {
		t.Fatal(err)
	}
	return copyRoot
}
