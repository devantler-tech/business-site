package runtimeapp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjectedCompanySnapshot exercises the real descriptor boundary, without services.
func TestProjectedCompanySnapshot(t *testing.T) {
	input := filepath.Join(t.TempDir(), "disclosure")
	if err := os.WriteFile(input, []byte("Example Road 1\n1234 Exampletown"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(input, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(input, -1, os.Getegid()); err != nil {
		t.Fatal(err)
	}
	data, err := projectionBytes(input)
	if err != nil || string(data) != "Example Road 1\n1234 Exampletown" {
		t.Fatal("matching-group projected input was refused")
	}
	value, err := ReadProjectedCompanyDisclosure(input)
	if err != nil || value != "Example Road 1\n1234 Exampletown" {
		t.Fatal("owner-only snapshot handoff was refused")
	}
	retained, err := os.ReadFile(input)
	if err != nil || string(retained) != "Example Road 1\n1234 Exampletown" {
		t.Fatal("snapshot changed the input")
	}
}
