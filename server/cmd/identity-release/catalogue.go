package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
)

func baseObjects(p baseProfile) []sourceObject {
	all := append([]sourceObject{}, p.Metadata...)
	all = append(all, p.Layers...)
	for _, d := range p.Descriptors {
		all = append(all, d.Descriptor)
		all = append(all, d.Archives...)
	}
	return all
}

func loadBaseProfile() (baseProfile, error) {
	var p baseProfile
	if err := sourceJSON(baseProfileBytes, &p); err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(baseProfileBytes))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	return p, validateBaseProfile(p)
}

func validateBaseProfile(p baseProfile) error {
	if p.SchemaVersion != 1 || p.Profile != "frozen-identity-base-source-v1" || p.Image != frozenBaseImage || len(p.Metadata) != 3 || len(p.Layers) != 13 || len(p.Packages) != 6 || len(p.Descriptors) != 5 || len(p.Derived) != 13 {
		return fmt.Errorf("closed frozen base profile required")
	}
	if p.Metadata[0].SHA256 != strings.TrimPrefix(strings.Split(frozenBaseImage, "@")[1], "sha256:") {
		return fmt.Errorf("base index pin differs")
	}
	objects := baseObjects(p)
	if len(objects) != 28 {
		return fmt.Errorf("complete original object membership required")
	}
	paths := map[string]bool{}
	var total int64
	for _, o := range objects {
		if _, err := validateSourceObject(o); err != nil {
			return err
		}
		if paths[o.Path] {
			return fmt.Errorf("duplicate base payload path")
		}
		paths[o.Path] = true
		total += o.Bytes
	}
	if total > 32<<20 {
		return fmt.Errorf("base input aggregate bound")
	}
	registry := "https://gcr.io/v2/distroless/static-debian13/"
	for i, name := range []string{"index.json", "amd64.json", "config.json"} {
		kind := "manifests"
		if i == 2 {
			kind = "blobs"
		}
		o := p.Metadata[i]
		if o.Path != "metadata/"+name || o.URL != registry+kind+"/sha256:"+o.SHA256 {
			return fmt.Errorf("base metadata endpoint differs")
		}
	}
	layerIDs := map[string]bool{}
	for _, o := range p.Layers {
		if o.Path != "layers/"+o.SHA256+".tar.gz" || o.URL != registry+"blobs/sha256:"+o.SHA256 || layerIDs[o.SHA256] {
			return fmt.Errorf("base layer identity differs")
		}
		layerIDs[o.SHA256] = true
	}
	folders := map[string]string{"base-files": "b/base-files", "netbase": "n/netbase", "tzdata": "t/tzdata", "media-types": "m/media-types", "ca-certificates": "c/ca-certificates"}
	sources := map[string]string{}
	for _, d := range p.Descriptors {
		folder, ok := folders[d.Name]
		if !ok || sources[d.Name] != "" || d.Version == "" || len(d.Version) > 80 || strings.ContainsAny(d.Version, "/\\\x00\n\r\t ") {
			return fmt.Errorf("source package identity differs")
		}
		sources[d.Name] = d.Version
		prefix := "https://deb.debian.org/debian/pool/main/" + folder + "/"
		base := d.Name + "_" + d.Version + ".dsc"
		if d.Descriptor.Path != "sources/"+base || d.Descriptor.URL != prefix+base || d.Descriptor.Bytes > 65536 || len(d.Archives) < 1 || len(d.Archives) > 3 {
			return fmt.Errorf("source descriptor endpoint differs")
		}
		for _, a := range d.Archives {
			if a.Path != "sources/"+path.Base(a.Path) || a.URL != prefix+path.Base(a.Path) {
				return fmt.Errorf("source archive endpoint differs")
			}
		}
	}
	names := map[string]bool{"base-files": false, "netbase": false, "tzdata": false, "tzdata-legacy": false, "media-types": false, "ca-certificates": false}
	wanted := map[string]string{"usr/share/common-licenses/GPL-2": "notices/GPL-2.txt"}
	for _, pkg := range p.Packages {
		used, ok := names[pkg.Name]
		if !ok || used || pkg.Version != sources[pkg.Source] {
			return fmt.Errorf("binary/source package membership differs")
		}
		names[pkg.Name] = true
		if (pkg.Name == "tzdata-legacy" && pkg.Source != "tzdata") || (pkg.Name != "tzdata-legacy" && pkg.Source != pkg.Name) {
			return fmt.Errorf("binary source-package mapping differs")
		}
		wanted["var/lib/dpkg/status.d/"+pkg.Name] = "status/" + pkg.Name
		wanted["usr/share/doc/"+pkg.Name+"/copyright"] = "notices/" + pkg.Name + "-copyright.txt"
	}
	seen := map[string]bool{}
	for _, v := range p.Derived {
		if wanted[v.ImagePath] != v.Path || seen[v.ImagePath] || paths[v.Path] || !layerIDs[v.LayerSHA256] || v.Bytes < 1 || v.Bytes > 65536 || !sourceDigest.MatchString(v.SHA256) || (v.ImageMode != 0644 && v.ImageMode != 0555) {
			return fmt.Errorf("closed base file observation differs")
		}
		seen[v.ImagePath] = true
		paths[v.Path] = true
	}
	// URL parsing here is an independent declaration check; no request is made.
	for _, d := range p.Descriptors {
		u, err := url.Parse(d.Descriptor.URL)
		if err != nil || u.Host != "deb.debian.org" {
			return fmt.Errorf("descriptor origin differs")
		}
	}
	return nil
}
