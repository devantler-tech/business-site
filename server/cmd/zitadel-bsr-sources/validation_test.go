package main

import (
	"bytes"
	"crypto/sha3"
	"fmt"
	"maps"
	"strings"
	"testing"
)

// fixtureModule provides both native cache formats and an independent export.
// The actual Linux capture additionally joins the three real registry digests.
func fixtureModule() (moduleProfile, map[string][]byte) {
	p := moduleProfile{"example", "schemas", "0123456789abcdef0123456789abcdef", "2022-10-25T15:05:16.910073Z", ""}
	files := map[string][]byte{}
	for _, prefix := range []string{"b5/files/", "shake256/files/", "export/"} {
		files[prefix+"a.proto"] = []byte("syntax = \"proto3\";\n")
		files[prefix+"empty"] = []byte{}
	}
	files["b5/module.yaml"] = []byte("version: v1\nfiles_dir: files\n")
	files["shake256/module.yaml"] = []byte("version: v1\nfiles_dir: files\nv1_buf_yaml_file: v1_buf_yaml/buf.yaml\nv1_buf_lock_file: v1_buf_lock/buf.lock\n")
	files["shake256/v1_buf_yaml/buf.yaml"] = []byte("version: v1\n")
	files["shake256/v1_buf_lock/buf.lock"] = []byte("version: v1\ndeps: []\n")
	var manifest strings.Builder
	for _, item := range [][2]string{{"a.proto", "export/a.proto"}, {"buf.lock", "shake256/v1_buf_lock/buf.lock"}, {"buf.yaml", "shake256/v1_buf_yaml/buf.yaml"}, {"empty", "export/empty"}} {
		fmt.Fprintf(&manifest, "shake256:%x  %s\n", sha3.SumSHAKE256(files[item[1]], 64), item[0])
	}
	p.Digest = fmt.Sprintf("shake256:%x", sha3.SumSHAKE256([]byte(manifest.String()), 64))
	files["commit.json"] = fmt.Appendf(nil, `{"version":"v1","owner":"%s","module":"%s","create_time":"%s","digest":"%s"}`, p.Owner, p.Module, p.Created, p.Digest)
	files["acquisition.json"] = fmt.Appendf(nil, `{"commit":"%s","create_time":"%s"}`, p.Commit, p.Created)
	return p, files
}

// TestModuleIntegrity refuses plausible metadata over altered or incomplete bytes.
func TestModuleIntegrity(t *testing.T) {
	p, original := fixtureModule()
	if err := validateModule(p, original); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string][]byte){
		"changed native bytes": func(f map[string][]byte) { f["shake256/files/a.proto"] = []byte("wrong") },
		"changed all copies": func(f map[string][]byte) {
			for _, prefix := range []string{"b5/files/", "shake256/files/", "export/"} {
				f[prefix+"a.proto"] = []byte("wrong")
			}
		},
		"missing empty source":    func(f map[string][]byte) { delete(f, "b5/files/empty") },
		"extra source":            func(f map[string][]byte) { f["export/unselected.proto"] = []byte("wrong") },
		"changed original config": func(f map[string][]byte) { f["shake256/v1_buf_yaml/buf.yaml"] = []byte("version: v2\n") },
		"changed original lock":   func(f map[string][]byte) { f["shake256/v1_buf_lock/buf.lock"] = []byte("version: v2\n") },
		"wrong owner": func(f map[string][]byte) {
			f["commit.json"] = bytes.ReplaceAll(f["commit.json"], []byte(`"example"`), []byte(`"foreign"`))
		},
		"wrong module": func(f map[string][]byte) {
			f["commit.json"] = bytes.ReplaceAll(f["commit.json"], []byte(`"schemas"`), []byte(`"foreign"`))
		},
		"wrong acquired commit": func(f map[string][]byte) {
			f["acquisition.json"] = bytes.ReplaceAll(f["acquisition.json"], []byte(p.Commit), []byte(strings.Repeat("f", 32)))
		},
		"wrong time": func(f map[string][]byte) {
			f["acquisition.json"] = bytes.ReplaceAll(f["acquisition.json"], []byte("2022-"), []byte("2023-"))
		},
		"duplicate key": func(f map[string][]byte) {
			f["commit.json"] = append([]byte(`{"owner":"foreign",`), f["commit.json"][1:]...)
		},
		"case alias": func(f map[string][]byte) {
			f["commit.json"] = append([]byte(`{"Owner":"foreign",`), f["commit.json"][1:]...)
		},
		"unknown field": func(f map[string][]byte) {
			f["acquisition.json"] = append([]byte(`{"digest":"made-up",`), f["acquisition.json"][1:]...)
		},
		"trailing record":      func(f map[string][]byte) { f["commit.json"] = append(f["commit.json"], []byte(`{}`)...) },
		"wrong native mapping": func(f map[string][]byte) { f["shake256/module.yaml"] = []byte("version: v1\nfiles_dir: elsewhere\n") },
		"unexpected metadata":  func(f map[string][]byte) { f["script.sh"] = []byte("wrong") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := maps.Clone(original)
			mutate(f)
			if err := validateModule(p, f); err == nil {
				t.Fatal("invalid input admitted")
			}
		})
	}
}
