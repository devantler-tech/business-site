package main

import (
	_ "embed"
)

const frozenBaseImage = "gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3"

//go:embed base-profile.json
var baseProfileBytes []byte

type sourcePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
}
type sourceDescriptor struct {
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Descriptor sourceObject   `json:"descriptor"`
	Archives   []sourceObject `json:"archives"`
}
type sourceDerived struct {
	ImagePath   string `json:"imagePath"`
	Path        string `json:"path"`
	LayerSHA256 string `json:"layerSha256"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ImageMode   uint32 `json:"imageMode"`
}
type baseProfile struct {
	SchemaVersion int                `json:"schemaVersion"`
	Profile       string             `json:"profile"`
	Image         string             `json:"image"`
	Metadata      []sourceObject     `json:"metadata"`
	Layers        []sourceObject     `json:"layers"`
	Packages      []sourcePackage    `json:"packages"`
	Descriptors   []sourceDescriptor `json:"descriptors"`
	Derived       []sourceDerived    `json:"derived"`
}
