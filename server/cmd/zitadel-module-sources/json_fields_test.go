package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSelectionRejectsAliasedFields prevents a second spelling from hiding bad input.
func TestSelectionRejectsAliasedFields(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
	}{
		{"incomplete package", `"ImportPath":"bytes"`, `"Incomplete":true,"incomplete":false,"ImportPath":"bytes"`},
		{"package error", `"ImportPath":"bytes"`, `"Error":{"Err":"failed"},"error":null,"ImportPath":"bytes"`},
		{"dependency errors", `"ImportPath":"bytes"`, `"DepsErrors":[{}],"depsErrors":[],"ImportPath":"bytes"`},
		{"root dependency list", `"Deps":`, `"deps":["missing"],"Deps":`},
		{"module identity", `"Path":`, `"path":"example.invalid/wrong","Path":`},
		{"module error", `"Module":{`, `"Module":{"Error":{},"error":null,`},
		{"replacement error", `"Replace":{`, `"Replace":{"Error":{},"error":null,`},
		{"module location", `"Module":{`, `"Module":{"dir":"/wrong",`},
		{"module checksum", `"Sum":`, `"sum":"h1:wrong","Sum":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := selectionInputs(t)
			changed := bytes.Replace(f.packages, []byte(tc.before), []byte(tc.after), 1)
			if bytes.Equal(changed, f.packages) {
				t.Fatal("control did not change its compiler record")
			}
			if _, err := selectModules(f.cat, f.mod, f.sums, changed, f.build, digest(f.cat)); err == nil {
				t.Fatal("conflicting field spellings admitted")
			}
		})
	}
}

// TestSelectionPreservesCaseDistinctMapKeys retains native case-sensitive import maps.
func TestSelectionPreservesCaseDistinctMapKeys(t *testing.T) {
	f := selectionInputs(t)
	f.packages = bytes.Replace(f.packages, []byte(`"ImportPath":"bytes"`),
		[]byte(`"ImportMap":{"example.com/Name":"first","example.com/name":"second"},"ImportPath":"bytes"`), 1)
	if selected, err := selectModules(f.cat, f.mod, f.sums, f.packages, f.build, digest(f.cat)); err != nil || len(selected) != 2 {
		t.Fatalf("case-distinct import map refused: %v", err)
	}
}

// TestRetainedManifestRejectsAliasedFields rejects ambiguity even with a matching byte seal.
func TestRetainedManifestRejectsAliasedFields(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
	}{
		{"tagged schema field", `"schemaVersion": 1`, `"schemaVersion": 0,"SchemaVersion":1`},
		{"nested array identity", `"Path":`, `"path":"example.invalid/wrong","Path":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, out := capturedSelection(t)
			file := filepath.Join(out, "manifest.json")
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(raw, []byte(tc.before), []byte(tc.after), 1)
			if bytes.Equal(changed, raw) {
				t.Fatal("control did not change its manifest field")
			}
			writeFixture(t, file, changed)
			if _, err := verify(out, digest(changed), digest(f.cat)); err == nil {
				t.Fatal("sealed manifest with conflicting fields admitted")
			}
		})
	}
}
