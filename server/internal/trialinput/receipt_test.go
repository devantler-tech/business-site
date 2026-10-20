package trialinput

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateExclusiveReceipt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	want := Receipt{Scope: "source-inputs-only", Plan: "fictional plan"}
	if err := WriteReceipt(dir, want); err != nil {
		t.Fatal("private receipt creation failed", err)
	}
	path := filepath.Join(dir, "inputs.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("receipt was not an owner-only regular file")
	}
	data, err := os.ReadFile(path)
	var actual Receipt
	if err != nil || json.Unmarshal(data, &actual) != nil || actual.Scope != want.Scope || actual.Plan != want.Plan {
		t.Fatal("receipt lost its declared content")
	}
	if err := WriteReceipt(dir, Receipt{}); err == nil {
		t.Fatal("existing receipt was overwritten")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("refusal changed an existing receipt")
	}
}

func TestUnsafeReceiptDestination(t *testing.T) {
	for _, scenario := range []string{"accessible-directory", "symlink-directory", "symlink-file", "missing-directory", "relative-directory"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "accessible-directory":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink-directory":
				link := filepath.Join(t.TempDir(), "output")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			case "symlink-file":
				if err := os.Symlink("missing", filepath.Join(dir, "inputs.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-directory":
				dir = filepath.Join(dir, "missing")
			case "relative-directory":
				dir = "."
			}
			if err := WriteReceipt(dir, Receipt{}); err == nil {
				t.Fatal("unsafe destination accepted")
			}
		})
	}
}
