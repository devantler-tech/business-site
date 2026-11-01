package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceFileRequiresOneCompleteReadonlyRegularOriginal(t *testing.T) {
	for _, kind := range []string{"original", "missing", "changed", "short", "long", "writable", "executable", "symlink", "hardlink", "directory", "escape"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			name := "original"
			file := filepath.Join(dir, name)
			data := []byte("retained original\n")
			want := sourceFile{Path: name, Bytes: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Mode: 0444}
			if err := os.WriteFile(file, data, 0444); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.Chmod(file, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("different bytes!\n"), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(file, 0444); err != nil {
					t.Fatal(err)
				}
			case "short":
				want.Bytes++
			case "long":
				want.Bytes--
			case "writable":
				if err := os.Chmod(file, 0644); err != nil {
					t.Fatal(err)
				}
			case "executable":
				if err := os.Chmod(file, 0555); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(file, file+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("original.target", file); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(file, file+".alias"); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(file, 0700); err != nil {
					t.Fatal(err)
				}
			case "escape":
				want.Path = "../original"
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			got, err := readSourceFile(root, want)
			if kind == "original" {
				if err != nil || string(got) != string(data) {
					t.Fatalf("original refused: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe or incomplete source file admitted")
			}
		})
	}
}

func TestSourceFileCreationNeverOverwritesOrFollowsExistingFiles(t *testing.T) {
	for _, kind := range []string{"fresh", "existing", "symlink-parent", "escape"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "sources"), 0700); err != nil {
				t.Fatal(err)
			}
			name := "sources/original"
			if kind == "existing" {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("existing"), 0444); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink-parent" {
				if err := os.Rename(filepath.Join(dir, "sources"), filepath.Join(dir, "target")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", filepath.Join(dir, "sources")); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "escape" {
				name = "../outside"
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			err = writeSourceFile(root, name, []byte("retained"))
			if kind == "fresh" {
				if err != nil {
					t.Fatal(err)
				}
				st, err := os.Lstat(filepath.Join(dir, name))
				if err != nil || st.Mode().Perm() != 0444 {
					t.Fatalf("noncanonical output: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe or existing output admitted")
			}
		})
	}
}

func TestFailedArchiveCleanupPreservesReplacements(t *testing.T) {
	for _, kind := range []string{"owned", "replaced", "missing", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "archive")
			if err := os.WriteFile(name, []byte("owned archive"), 0600); err != nil {
				t.Fatal(err)
			}
			created, err := os.Lstat(name)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "owned" {
				if err = os.Rename(name, name+".owned"); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "replaced":
				if err = os.WriteFile(name, []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err = os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err = os.Symlink("archive.owned", name); err != nil {
					t.Fatal(err)
				}
			}
			if err = removeSourceOutput(name, created); err != nil {
				t.Fatal(err)
			}
			_, err = os.Lstat(name)
			if kind == "owned" || kind == "missing" {
				if !os.IsNotExist(err) {
					t.Fatal("owned cleanup or missing leaf differs")
				}
			} else if err != nil {
				t.Fatal("replacement was removed")
			}
			if kind == "replaced" {
				b, e := os.ReadFile(name)
				if e != nil || string(b) != "replacement" {
					t.Fatal("replacement bytes changed")
				}
			}
			if kind != "owned" {
				b, e := os.ReadFile(name + ".owned")
				if e != nil || string(b) != "owned archive" {
					t.Fatal("renamed owned archive changed")
				}
			}
		})
	}
}
