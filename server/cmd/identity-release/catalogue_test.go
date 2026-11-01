package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFrozenBaseProfileIsComplete(t *testing.T) {
	p, err := loadBaseProfile()
	if err != nil || len(baseObjects(p)) != 28 {
		t.Fatalf("frozen original catalogue refused: %v", err)
	}
}

func TestFrozenBaseProfileRejectsChangedBoundaries(t *testing.T) {
	for name, change := range map[string]func(*baseProfile){
		"schema":             func(p *baseProfile) { p.SchemaVersion++ },
		"profile":            func(p *baseProfile) { p.Profile += "-other" },
		"image":              func(p *baseProfile) { p.Image += "-other" },
		"index":              func(p *baseProfile) { p.Metadata[0].SHA256 = strings.Repeat("0", 64) },
		"missing-metadata":   func(p *baseProfile) { p.Metadata = p.Metadata[:2] },
		"missing-layer":      func(p *baseProfile) { p.Layers = p.Layers[:12] },
		"duplicate-layer":    func(p *baseProfile) { p.Layers[1] = p.Layers[0] },
		"layer-endpoint":     func(p *baseProfile) { p.Layers[0].URL += "?token=unexpected" },
		"layer-path":         func(p *baseProfile) { p.Layers[0].Path = "layers/other.tar.gz" },
		"metadata-endpoint":  func(p *baseProfile) { p.Metadata[0].URL = p.Metadata[1].URL },
		"missing-package":    func(p *baseProfile) { p.Packages = p.Packages[:5] },
		"duplicate-package":  func(p *baseProfile) { p.Packages[1] = p.Packages[0] },
		"package-version":    func(p *baseProfile) { p.Packages[0].Version += "-other" },
		"package-source":     func(p *baseProfile) { p.Packages[0].Source = "unrecorded" },
		"missing-descriptor": func(p *baseProfile) { p.Descriptors = p.Descriptors[:4] },
		"descriptor-origin":  func(p *baseProfile) { p.Descriptors[0].Descriptor.URL = "https://outside.invalid/source.dsc" },
		"descriptor-version": func(p *baseProfile) { p.Descriptors[0].Version = "../other" },
		"extra-archive": func(p *baseProfile) {
			p.Descriptors[0].Archives = append(p.Descriptors[0].Archives, p.Descriptors[1].Archives[0])
		},
		"archive-origin":     func(p *baseProfile) { p.Descriptors[0].Archives[0].URL = p.Descriptors[1].Archives[0].URL },
		"missing-derived":    func(p *baseProfile) { p.Derived = p.Derived[:12] },
		"derived-path":       func(p *baseProfile) { p.Derived[0].Path = "../outside" },
		"derived-image-path": func(p *baseProfile) { p.Derived[0].ImagePath = "etc/passwd" },
		"derived-layer":      func(p *baseProfile) { p.Derived[0].LayerSHA256 = strings.Repeat("0", 64) },
		"derived-mode":       func(p *baseProfile) { p.Derived[0].ImageMode = 0777 },
		"derived-size":       func(p *baseProfile) { p.Derived[0].Bytes = 65537 },
		"derived-digest":     func(p *baseProfile) { p.Derived[0].SHA256 = "wrong" },
		"duplicate-derived":  func(p *baseProfile) { p.Derived[1] = p.Derived[0] },
	} {
		t.Run(name, func(t *testing.T) {
			var p baseProfile
			if err := json.Unmarshal(baseProfileBytes, &p); err != nil {
				t.Fatal(err)
			}
			change(&p)
			if err := validateBaseProfile(p); err == nil {
				t.Fatal("changed frozen boundary admitted")
			}
		})
	}
}
