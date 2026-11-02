package main

import (
	"crypto/sha256"
	"fmt"
)

type baseObjectDescriptor struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}
type baseIndex struct {
	SchemaVersion int `json:"schemaVersion"`
	Manifests     []struct {
		baseObjectDescriptor
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
			Variant      string `json:"variant"`
		} `json:"platform"`
	} `json:"manifests"`
}
type baseManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Config        baseObjectDescriptor   `json:"config"`
	Layers        []baseObjectDescriptor `json:"layers"`
}
type baseImageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	RootFS       struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

func checkBaseMetadata(p baseProfile, raw map[string][]byte) ([]string, error) {
	if len(p.Metadata) != 3 || len(p.Layers) != 13 {
		return nil, fmt.Errorf("closed base metadata membership required")
	}
	for _, spec := range p.Metadata {
		b, ok := raw[spec.Path]
		if !ok || int64(len(b)) != spec.Bytes || fmt.Sprintf("%x", sha256.Sum256(b)) != spec.SHA256 {
			return nil, fmt.Errorf("base metadata digest differs")
		}
	}
	var index baseIndex
	var manifest baseManifest
	var config baseImageConfig
	if err := sourceJSON(raw[p.Metadata[0].Path], &index); err != nil {
		return nil, err
	}
	if err := sourceJSON(raw[p.Metadata[1].Path], &manifest); err != nil {
		return nil, err
	}
	if err := sourceJSON(raw[p.Metadata[2].Path], &config); err != nil {
		return nil, err
	}
	if index.SchemaVersion != 2 || manifest.SchemaVersion != 2 || len(index.Manifests) < 1 || len(index.Manifests) > 32 {
		return nil, fmt.Errorf("base descriptor schema differs")
	}
	selected := 0
	for _, descriptor := range index.Manifests {
		if descriptor.Platform.Architecture == "amd64" && descriptor.Platform.OS == "linux" && descriptor.Platform.Variant == "" {
			selected++
			if descriptor.Digest != "sha256:"+p.Metadata[1].SHA256 || descriptor.Size != p.Metadata[1].Bytes {
				return nil, fmt.Errorf("amd64 child descriptor differs")
			}
		}
	}
	if selected != 1 || manifest.Config.Digest != "sha256:"+p.Metadata[2].SHA256 || manifest.Config.Size != p.Metadata[2].Bytes || len(manifest.Layers) != len(p.Layers) {
		return nil, fmt.Errorf("base manifest/config chain differs")
	}
	for i, layer := range manifest.Layers {
		if layer.Digest != "sha256:"+p.Layers[i].SHA256 || layer.Size != p.Layers[i].Bytes {
			return nil, fmt.Errorf("ordered base layer differs")
		}
	}
	if config.Architecture != "amd64" || config.OS != "linux" || config.RootFS.Type != "layers" || len(config.RootFS.DiffIDs) != len(p.Layers) {
		return nil, fmt.Errorf("base platform or RootFS differs")
	}
	for _, id := range config.RootFS.DiffIDs {
		if len(id) != 71 || id[:7] != "sha256:" || !sourceDigest.MatchString(id[7:]) {
			return nil, fmt.Errorf("invalid base DiffID")
		}
	}
	return config.RootFS.DiffIDs, nil
}
