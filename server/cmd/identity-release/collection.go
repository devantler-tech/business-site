package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

type sourceReader func(context.Context, sourceObject) ([]byte, error)

func collectBaseOriginals(ctx context.Context, p baseProfile, read sourceReader) (map[string][]byte, error) {
	raw := map[string][]byte{}
	for _, spec := range baseObjects(p) {
		b, err := read(ctx, spec)
		if err != nil {
			return nil, err
		}
		if int64(len(b)) != spec.Bytes || fmt.Sprintf("%x", sha256.Sum256(b)) != spec.SHA256 {
			return nil, fmt.Errorf("original source object differs")
		}
		if _, exists := raw[spec.Path]; exists {
			return nil, fmt.Errorf("duplicate original object")
		}
		raw[spec.Path] = b
	}
	derived, err := checkBaseContents(p, raw)
	if err != nil {
		return nil, err
	}
	for name, data := range derived {
		raw[name] = data
	}
	return raw, nil
}

// Each selected file is joined to its actual compressed layer, DiffID and
// original tar mode. Source archive bodies remain opaque, never extracted.
func checkBaseContents(p baseProfile, raw map[string][]byte) (map[string][]byte, error) {
	if len(raw) != len(baseObjects(p)) {
		return nil, fmt.Errorf("original source object membership differs")
	}
	for _, spec := range baseObjects(p) {
		b, ok := raw[spec.Path]
		if !ok || int64(len(b)) != spec.Bytes || fmt.Sprintf("%x", sha256.Sum256(b)) != spec.SHA256 {
			return nil, fmt.Errorf("original object digest differs")
		}
	}
	ids, err := checkBaseMetadata(p, raw)
	if err != nil {
		return nil, err
	}
	derived := map[string][]byte{}
	var expanded int64
	for i, spec := range p.Layers {
		result, err := checkBaseLayer(raw[spec.Path], spec, ids[i], p.Derived)
		if err != nil {
			return nil, err
		}
		expanded += result.ExpandedBytes
		if expanded > 32<<20 {
			return nil, fmt.Errorf("base layer aggregate expansion bound")
		}
		for _, want := range p.Derived {
			if want.LayerSHA256 != spec.SHA256 {
				continue
			}
			got, ok := result.Files[want.ImagePath]
			if !ok || got.ImageMode != want.ImageMode || derived[want.Path] != nil {
				return nil, fmt.Errorf("derived base identity differs")
			}
			derived[want.Path] = got.Data
		}
	}
	if len(derived) != len(p.Derived) {
		return nil, fmt.Errorf("derived base membership differs")
	}
	for _, spec := range p.Descriptors {
		if err := checkSourceDescriptor(raw[spec.Descriptor.Path], spec); err != nil {
			return nil, err
		}
	}
	for _, pkg := range p.Packages {
		if err := checkSourceStatus(derived["status/"+pkg.Name], pkg); err != nil {
			return nil, err
		}
	}
	return derived, nil
}

func checkSourceStatus(raw []byte, want sourcePackage) error {
	if len(raw) < 1 || len(raw) > 65536 || !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) {
		return fmt.Errorf("invalid installed package metadata")
	}
	fields := map[string]string{}
	previous, ended := "", false
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			ended = true
			continue
		}
		if ended || len(line) > 4096 {
			return fmt.Errorf("extra installed package paragraph")
		}
		if line[0] == ' ' || line[0] == '\t' {
			if previous == "" || previous == "Package" || previous == "Source" || previous == "Version" {
				return fmt.Errorf("ambiguous package identity continuation")
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || !sourceControlKey.MatchString(parts[0]) {
			return fmt.Errorf("malformed package identity field")
		}
		if _, exists := fields[parts[0]]; exists {
			return fmt.Errorf("duplicate installed package field")
		}
		previous = parts[0]
		fields[previous] = strings.TrimSpace(parts[1])
	}
	source := fields["Source"]
	if source == "" {
		source = fields["Package"]
	}
	if source != want.Source && source != want.Source+" ("+want.Version+")" {
		return fmt.Errorf("installed source package differs")
	}
	if fields["Package"] != want.Name || fields["Version"] != want.Version {
		return fmt.Errorf("installed binary package version differs")
	}
	return nil
}
