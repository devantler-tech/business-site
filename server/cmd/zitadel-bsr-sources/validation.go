package main

import (
	"bytes"
	"crypto/sha3"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
)

type moduleProfile struct {
	Owner   string `json:"owner"`
	Module  string `json:"module"`
	Commit  string `json:"commit"`
	Created string `json:"created"`
	Digest  string `json:"digest"`
}

var frozenModules = []moduleProfile{
	{"envoyproxy", "protoc-gen-validate", "6607b10f00ed4a3d98f906807131c44a", "2022-10-25T15:05:16.910073Z", "shake256:acc7b2ededb2f88d296862943a003b157bdb68ec93ed13dcd8566b2d06e47993ea6daf12013b9655658aaf6bbdb141cf65bfe400ce2870f4654b0a5b45e57c09"},
	{"googleapis", "googleapis", "75b4300737fb4efca0831636be94e517", "2022-12-14T15:02:16.942623Z", "shake256:d865f55b8ceb838c90c28b09894ab43d07f42551108c23760004a6a4e28fe24d3a1f7380a3c9278edb329a338a9cc5db8ad9f394de548e70d534e98504972d67"},
	{"grpc-ecosystem", "grpc-gateway", "a1ecdc58eccd49aa8bea2a7a9022dc27", "2022-11-27T06:09:15.276627Z", "shake256:efdd86fbdc42e8b7259fe461a49656827a03fb7cba0b3b9eb622ca10654ec6beccb9a051229c1553ccd89ed3e95d69ad4d7c799f1da3f3f1bd447b7947a4893e"},
}

var portableName = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// inputName excludes manifest delimiters and platform-dependent path syntax.
func inputName(name string) bool {
	return len(name) <= 512 && fs.ValidPath(name) && name != "." && portableName.MatchString(name)
}

// exactStrings reads one closed flat native record, including duplicate keys.
func exactStrings(raw []byte, expected map[string]string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("expected metadata object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		value, err := d.Token()
		if err != nil {
			return err
		}
		text, stringValue := value.(string)
		want, known := expected[key]
		if !ok || !known || !stringValue || seen[key] || text != want {
			return fmt.Errorf("unknown, repeated or mismatched native metadata")
		}
		seen[key] = true
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(seen) != len(expected) {
		return fmt.Errorf("incomplete native metadata")
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing native metadata")
	}
	return nil
}

// moduleDigest implements the pinned Buf B4 manifest over all source and v1 records.
// It does not fabricate the older dependency-sensitive b5 digest.
func moduleDigest(files map[string][]byte) string {
	var manifest strings.Builder
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(&manifest, "shake256:%x  %s\n", sha3.SumSHAKE256(files[name], 64), name)
	}
	return fmt.Sprintf("shake256:%x", sha3.SumSHAKE256([]byte(manifest.String()), 64))
}

// validateModule checks retained native and independently acquired source inputs.
func validateModule(p moduleProfile, files map[string][]byte) error {
	metadata := map[string]map[string]string{
		"commit.json":      {"version": "v1", "owner": p.Owner, "module": p.Module, "create_time": p.Created, "digest": p.Digest},
		"acquisition.json": {"commit": p.Commit, "create_time": p.Created},
	}
	for name, fields := range metadata {
		if err := exactStrings(files[name], fields); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	mappings := map[string]string{
		"b5/module.yaml":       "version: v1\nfiles_dir: files\n",
		"shake256/module.yaml": "version: v1\nfiles_dir: files\nv1_buf_yaml_file: v1_buf_yaml/buf.yaml\nv1_buf_lock_file: v1_buf_lock/buf.lock\n",
	}
	for name, want := range mappings {
		if string(files[name]) != want {
			return fmt.Errorf("native mapping differs")
		}
	}
	seen := map[string]bool{}
	for name := range metadata {
		seen[name] = true
	}
	for name := range mappings {
		seen[name] = true
	}
	content := map[string][]byte{}
	for _, name := range []string{"buf.yaml", "buf.lock"} {
		key := "shake256/v1_" + strings.ReplaceAll(name, ".", "_") + "/" + name
		body, exists := files[key]
		if !exists {
			return fmt.Errorf("original v1 record missing")
		}
		content[name], seen[key] = body, true
	}
	for name, body := range files {
		if !strings.HasPrefix(name, "export/") {
			continue
		}
		rel := strings.TrimPrefix(name, "export/")
		if !inputName(rel) || rel == "buf.yaml" || rel == "buf.lock" {
			return fmt.Errorf("invalid exported source name")
		}
		content[rel], seen[name] = body, true
		for _, prefix := range []string{"b5/files/", "shake256/files/"} {
			key := prefix + rel
			native, exists := files[key]
			if !exists || !bytes.Equal(native, body) {
				return fmt.Errorf("native/export content differs")
			}
			seen[key] = true
		}
	}
	if len(content) < 3 || len(seen) != len(files) {
		return fmt.Errorf("incomplete or unselected module inputs")
	}
	if moduleDigest(content) != p.Digest {
		return fmt.Errorf("native SHAKE256 content digest differs")
	}
	return nil
}
