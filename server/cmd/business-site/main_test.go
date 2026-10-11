package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnpackagedBuildCannotStartAndErrorsArePrivate refuses unbound binaries without path leaks.
func TestUnpackagedBuildCannotStartAndErrorsArePrivate(t *testing.T) {
	if err := run(context.Background(), nil, io.Discard); err == nil || strings.Contains(err.Error(), "/Users") {
		t.Fatal("unpackaged binary was runnable or error disclosed a path")
	}
	if err := run(context.Background(), []string{"--public", "/private/missing/credential", "--check-public"}, io.Discard); err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal("missing artifact did not fail generically")
	}
}

// TestCheckCommandUsesActualPublicArtifactAndRejectsTampering checks the real startup digest gate.
func TestCheckCommandUsesActualPublicArtifactAndRejectsTampering(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "da"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "da/index.html"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("public page"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var output strings.Builder
	if err := run(context.Background(), []string{"--check-public", "--public", dir}, &output); err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(output.String())) != 64 {
		t.Fatal("actual public digest absent")
	}
	oldRevision, oldDigest, oldMode := revision, publicDigest, buildMode
	t.Cleanup(func() { revision, publicDigest, buildMode = oldRevision, oldDigest, oldMode })
	revision = strings.Repeat("a", 40)
	publicDigest = strings.TrimSpace(output.String())
	buildMode = "development"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--public", dir}, io.Discard); err == nil || err.Error() != "public artifact does not match this runtime package" {
		t.Fatal("tampered artifact accepted")
	}
}
