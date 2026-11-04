package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestSourceJSONRejectsAmbiguity(t *testing.T) {
	for _, input := range []string{`{"sha256":"original","sha256":"substituted"}`, `{"a":{"x":1,"x":2}}`, `{"a":[{"x":1,"x":2}]}`, strings.Repeat("[", 30) + strings.Repeat("]", 30)} {
		t.Run(fmt.Sprintf("case-%d", len(input)), func(t *testing.T) {
			var out any
			if err := sourceJSON([]byte(input), &out); err == nil {
				t.Fatal("ambiguous or excessively nested source metadata accepted")
			}
		})
	}
}
func sourceDSCFixture() ([]byte, sourceDescriptor) {
	source := sourceFetchFixture()
	source.Path = "sources/fixture_1.tar.xz"
	text := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\nFormat: 3.0 (native)\nSource: fixture\nVersion: 1\nChecksums-Sha256:\n " + source.SHA256 + " 4 fixture_1.tar.xz\nFiles:\n 098f6bcd4621d373cade4e832627b4f6 4 fixture_1.tar.xz\n\n-----BEGIN PGP SIGNATURE-----\nretained opaque signature\n-----END PGP SIGNATURE-----\n"
	return []byte(text), sourceDescriptor{Name: "fixture", Version: "1", Archives: []sourceObject{source}}
}
func TestSourceDescriptorBindsOnlyItsSHA256Section(t *testing.T) {
	data, want := sourceDSCFixture()
	if err := checkSourceDescriptor(data, want); err != nil {
		t.Fatal(err)
	}
}
func TestSourceDescriptorRefusesInconsistentOriginals(t *testing.T) {
	for _, kind := range []string{"source", "version", "missing-sha", "wrong-sha", "wrong-size", "wrong-name", "extra-member", "duplicate-member", "duplicate-source", "unsigned", "trailing-content"} {
		t.Run(kind, func(t *testing.T) {
			data, want := sourceDSCFixture()
			s := string(data)
			switch kind {
			case "source":
				s = strings.Replace(s, "Source: fixture", "Source: other", 1)
			case "version":
				s = strings.Replace(s, "Version: 1", "Version: 2", 1)
			case "missing-sha":
				s = strings.Replace(s, "Checksums-Sha256:", "Checksums-Sha1:", 1)
			case "wrong-sha":
				s = strings.Replace(s, want.Archives[0].SHA256, strings.Repeat("a", 64), 1)
			case "wrong-size":
				s = strings.Replace(s, " 4 fixture_1.tar.xz", " 5 fixture_1.tar.xz", 1)
			case "wrong-name":
				s = strings.Replace(s, "fixture_1.tar.xz", "other.tar.xz", 1)
			case "extra-member":
				s = strings.Replace(s, "\nFiles:", "\n "+strings.Repeat("a", 64)+" 4 extra.tar.xz\nFiles:", 1)
			case "duplicate-member":
				s = strings.Replace(s, "\nFiles:", "\n "+want.Archives[0].SHA256+" 4 fixture_1.tar.xz\nFiles:", 1)
			case "duplicate-source":
				s = strings.Replace(s, "Source: fixture", "Source: fixture\nSource: fixture", 1)
			case "unsigned":
				s = strings.TrimPrefix(s, "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA512\n\n")
			case "trailing-content":
				s += "unexpected trailing record\n"
			}
			if err := checkSourceDescriptor([]byte(s), want); err == nil {
				t.Fatal("inconsistent original descriptor accepted")
			}
		})
	}
}
