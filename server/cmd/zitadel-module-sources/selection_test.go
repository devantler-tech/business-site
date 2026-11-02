package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/module"
)

// digest returns the complete literal-input SHA-256, never a Go content sum.
func digest(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

// The native capture command must admit the catalogue shipped with its recipe.
func TestFrozenCatalogueMatchesRecipe(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "third_party", "zitadel-go-module-notices", "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := digest(body); got != frozenCatalogue {
		t.Fatalf("checked-in catalogue is not admitted by the native command: got %s, policy %s", got, frozenCatalogue)
	}
}

type selectionFixture struct {
	build                    string
	cat, mod, sums, packages []byte
	entries                  []entry
	payloads                 []string
	mods                     [][]byte
}

// selectionInputs supplies actual module ZIPs and their complete compiler graph.
func selectionInputs(t *testing.T) selectionFixture {
	t.Helper()
	f := selectionFixture{build: t.TempDir(), mod: []byte("module github.com/zitadel/zitadel\n\ngo 1.26.0\n")}
	for i, name := range []string{"example.com/Example", "gopkg.in/ini.v1"} {
		version := "v1.2.3"
		zip, mod, sum, modSum := modulePayload(t, name, version, []string{"go.mod", "example.go"})
		resolved := identity{Path: name, Version: version, Sum: sum, GoModSum: modSum}
		original := resolved
		if i == 1 {
			original = identity{Path: "example.com/original", Version: "v1.0.0", Replace: &resolved}
		}
		f.entries = append(f.entries, entry{Original: original, Effective: resolved, Components: json.RawMessage("[]")})
		f.payloads = append(f.payloads, zip)
		f.mods = append(f.mods, mod)
		f.sums = fmt.Appendf(f.sums, "%s %s %s\n%s %s/go.mod %s\n", name, version, sum, name, version, modSum)
	}
	c := catalogue{SchemaVersion: 1, UpstreamCommit: "8afa5dd37a8e723efb9eace8d09fd5c1cccef694", MetadataProfile: "machine-id-no-jsonpath-v1", GoModSHA256: digest(f.mod), GoSumSHA256: digest(f.sums), Modules: f.entries}
	f.cat = marshal(t, c)
	observed := []map[string]any{
		{"ImportPath": "bytes", "Standard": true},
		{"ImportPath": "github.com/zitadel/zitadel", "Dir": filepath.Join(f.build, "prepared/source"), "Module": map[string]any{"Path": "github.com/zitadel/zitadel", "Main": true, "Dir": filepath.Join(f.build, "prepared/source"), "GoMod": filepath.Join(f.build, "prepared/source/go.mod")}},
	}
	for _, selected := range f.entries {
		ePath, err := module.EscapePath(selected.Effective.Path)
		if err != nil {
			t.Fatal(err)
		}
		eVersion, err := module.EscapeVersion(selected.Effective.Version)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(marshal(t, selected.Original), &m); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(f.build, "go-modules", ePath+"@"+eVersion)
		goMod := filepath.Join(f.build, "go-modules/cache/download", ePath, "@v", eVersion+".mod")
		m["Dir"], m["GoMod"] = dir, goMod
		if replacement, ok := m["Replace"].(map[string]any); ok {
			replacement["Dir"], replacement["GoMod"] = dir, goMod
		}
		observed = append(observed, map[string]any{"ImportPath": selected.Original.Path, "Dir": dir, "Module": m})
	}
	for _, obj := range observed {
		if obj["ImportPath"] == "github.com/zitadel/zitadel" {
			obj["Name"] = "main"
			obj["Deps"] = []string{"bytes", f.entries[0].Original.Path, f.entries[1].Original.Path, f.entries[0].Original.Path + "/sub", "github.com/zitadel/zitadel/sub"}
		}
		f.packages = append(f.packages, append(marshal(t, obj), '\n')...)
	}
	for _, parent := range []int{2, 1} {
		obj := map[string]any{"ImportPath": observed[parent]["ImportPath"].(string) + "/sub", "Dir": filepath.Join(observed[parent]["Dir"].(string), "sub"), "Module": observed[parent]["Module"]}
		f.packages = append(f.packages, append(marshal(t, obj), '\n')...)
	}
	return f
}

// marshal makes fixture construction fail explicitly on incomplete serialization.
func marshal(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestSelectionRetainsReplacement checks original/effective joins and full coverage.
func TestSelectionRetainsReplacement(t *testing.T) {
	f := selectionInputs(t)
	selected, err := selectModules(f.cat, f.mod, f.sums, f.packages, f.build, digest(f.cat))
	if err != nil || len(selected) != 2 {
		t.Fatalf("complete selection: count=%d err=%v", len(selected), err)
	}
	if selected[1].Original.Replace == nil || selected[1].Effective.Path != "gopkg.in/ini.v1" {
		t.Fatal("replacement identity lost")
	}
}

// TestSelectionRefusesBrokenObservations exercises malformed real JSON streams.
func TestSelectionRefusesBrokenObservations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*selectionFixture)
	}{
		{"changed catalogue", func(f *selectionFixture) { f.cat = append(f.cat, ' ') }},
		{"changed root module", func(f *selectionFixture) { f.mod = append(f.mod, '\n') }},
		{"changed frozen sums", func(f *selectionFixture) { f.sums = append(f.sums, '\n') }},
		{"missing selected module", func(f *selectionFixture) {
			lines := bytes.Split(bytes.TrimSpace(f.packages), []byte("\n"))
			f.packages = bytes.Join(append(lines[:3], lines[4:]...), []byte("\n"))
		}},
		{"missing same-module peer", func(f *selectionFixture) {
			lines := bytes.Split(bytes.TrimSpace(f.packages), []byte("\n"))
			f.packages = bytes.Join(append(lines[:4], lines[5:]...), []byte("\n"))
		}},
		{"missing exact root", func(f *selectionFixture) {
			lines := bytes.Split(bytes.TrimSpace(f.packages), []byte("\n"))
			f.packages = bytes.Join(append(lines[:1], lines[2:]...), []byte("\n"))
		}},
		{"wrong root package kind", func(f *selectionFixture) {
			f.packages = bytes.Replace(f.packages, []byte(`"Name":"main"`), []byte(`"Name":"library"`), 1)
		}},
		{"missing root dependencies", func(f *selectionFixture) {
			f.packages = bytes.Replace(f.packages, []byte(`"Deps":`), []byte(`"IgnoredDeps":`), 1)
		}},
		{"extra unselected standard package", func(f *selectionFixture) {
			f.packages = append(f.packages, []byte("{\"ImportPath\":\"extra\",\"Standard\":true}\n")...)
		}},
		{"duplicate package", func(f *selectionFixture) {
			f.packages = append(f.packages, bytes.Split(f.packages, []byte("\n"))[2]...)
		}},
		{"incomplete package", func(f *selectionFixture) {
			f.packages = bytes.Replace(f.packages, []byte(`"ImportPath":"bytes"`), []byte(`"Incomplete":true,"ImportPath":"bytes"`), 1)
		}},
		{"error bearing package", func(f *selectionFixture) {
			f.packages = bytes.Replace(f.packages, []byte(`"ImportPath":"bytes"`), []byte(`"Error":{},"ImportPath":"bytes"`), 1)
		}},
		{"wrong replacement", func(f *selectionFixture) {
			f.packages = bytes.ReplaceAll(f.packages, []byte("gopkg.in/ini.v1"), []byte("gopkg.in/other.v1"))
		}},
		{"wrong version", func(f *selectionFixture) {
			f.packages = bytes.ReplaceAll(f.packages, []byte("v1.2.3"), []byte("v1.2.4"))
		}},
		{"wrong cache", func(f *selectionFixture) {
			f.packages = bytes.ReplaceAll(f.packages, []byte("go-modules/"), []byte("foreign-cache/"))
		}},
		{"duplicate JSON key", func(f *selectionFixture) {
			f.packages = bytes.Replace(f.packages, []byte(`"Standard":true`), []byte(`"Standard":false,"Standard":true`), 1)
		}},
		{"truncated final record", func(f *selectionFixture) {
			f.packages = bytes.TrimSpace(f.packages)
			f.packages = f.packages[:len(f.packages)-1]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := selectionInputs(t)
			expected := digest(f.cat)
			tc.change(&f)
			if _, err := selectModules(f.cat, f.mod, f.sums, f.packages, f.build, expected); err == nil {
				t.Fatal("invalid selection accepted")
			}
		})
	}
}
