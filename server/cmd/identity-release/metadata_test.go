package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func baseMetadataFixture(t *testing.T) (baseProfile, map[string][]byte) {
	t.Helper()
	var p baseProfile
	if err := sourceJSON(baseProfileBytes, &p); err != nil {
		t.Fatal(err)
	}
	raw := map[string][]byte{}
	for _, n := range []string{"index.json", "amd64.json", "config.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", n+".raw"))
		if err != nil {
			t.Fatal(err)
		}
		raw["metadata/"+n] = b
	}
	return p, raw
}
func TestBaseMetadataBindsTheOriginalAMD64Chain(t *testing.T) {
	p, raw := baseMetadataFixture(t)
	ids, err := checkBaseMetadata(p, raw)
	if err != nil || len(ids) != 13 {
		t.Fatalf("complete original chain differs: %v", err)
	}
}
func TestBaseMetadataRefusesInconsistentChain(t *testing.T) {
	for _, kind := range []string{"index-sha", "duplicate-amd64", "no-amd64", "child-digest", "child-size", "config-digest", "layers-missing", "layer-digest", "architecture", "rootfs-type", "diffid-count", "invalid-diffid"} {
		t.Run(kind, func(t *testing.T) {
			p, raw := baseMetadataFixture(t)
			file := "metadata/index.json"
			if kind == "config-digest" || kind == "layers-missing" || kind == "layer-digest" {
				file = "metadata/amd64.json"
			}
			if kind == "architecture" || kind == "rootfs-type" || kind == "diffid-count" || kind == "invalid-diffid" {
				file = "metadata/config.json"
			}
			var doc map[string]any
			if err := json.Unmarshal(raw[file], &doc); err != nil {
				t.Fatal(err)
			}
			if kind == "index-sha" {
				p.Metadata[0].SHA256 = "bad"
			} else {
				switch kind {
				case "duplicate-amd64":
					for _, v := range doc["manifests"].([]any) {
						m := v.(map[string]any)
						if m["platform"].(map[string]any)["architecture"] == "amd64" {
							doc["manifests"] = append(doc["manifests"].([]any), v)
							break
						}
					}
				case "no-amd64":
					for _, v := range doc["manifests"].([]any) {
						v.(map[string]any)["platform"].(map[string]any)["architecture"] = "other"
					}
				case "child-digest", "child-size":
					for _, v := range doc["manifests"].([]any) {
						m := v.(map[string]any)
						if m["platform"].(map[string]any)["architecture"] == "amd64" {
							if kind == "child-digest" {
								m["digest"] = "sha256:wrong"
							} else {
								m["size"] = 1
							}
						}
					}
				case "config-digest":
					doc["config"].(map[string]any)["digest"] = "sha256:wrong"
				case "layers-missing":
					doc["layers"] = doc["layers"].([]any)[:12]
				case "layer-digest":
					doc["layers"].([]any)[0].(map[string]any)["digest"] = "sha256:wrong"
				case "architecture":
					doc["architecture"] = "arm64"
				case "rootfs-type":
					doc["rootfs"].(map[string]any)["type"] = "other"
				case "diffid-count":
					r := doc["rootfs"].(map[string]any)
					r["diff_ids"] = r["diff_ids"].([]any)[:12]
				case "invalid-diffid":
					doc["rootfs"].(map[string]any)["diff_ids"].([]any)[0] = "sha256:wrong"
				}
				b, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				raw[file] = b
				for i := range p.Metadata {
					if p.Metadata[i].Path == file {
						p.Metadata[i].Bytes = int64(len(b))
						p.Metadata[i].SHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
					}
				}
			}
			if _, err := checkBaseMetadata(p, raw); err == nil {
				t.Fatal("inconsistent base chain accepted")
			}
		})
	}
}
