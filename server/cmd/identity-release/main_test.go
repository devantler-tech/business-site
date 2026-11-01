package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestReleaseCommandIsInertWithoutAnExplicitCompleteOperation(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"capture-base-source"}, {"capture-base-source", "--output", "relative"}, {"capture-base-source", "--output", "/new", "--url", "https://outside.invalid"}, {"verify-base-source", "--input", "/missing"}, {"verify-base-source", "--input", "/missing", "--sha256", "wrong"}, {"pack-base-source", "--input", "/missing", "--sha256", "wrong", "--output", "/new"}, {"publish"}} {
		var out bytes.Buffer
		err := runRelease(context.Background(), args, &out)
		if len(args) == 0 || args[0] == "--help" {
			if err != nil || out.Len() == 0 {
				t.Fatalf("help failed: %v", err)
			}
		} else if err == nil {
			t.Fatalf("incomplete or unknown operation accepted: %v", args)
		}
	}
}

func TestReleaseCommandVerifiesAnExternallySealedCapsule(t *testing.T) {
	// The real command uses only the embedded frozen profile; a synthetic capsule
	// cannot enter its verification boundary even with its correct synthetic seal.
	p, raw := syntheticBaseCollection(t)
	root := filepath.Join(t.TempDir(), "capsule")
	seal, err := captureBaseCapsule(context.Background(), p, func(_ context.Context, s sourceObject) ([]byte, error) { return raw[s.Path], nil }, root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = runRelease(context.Background(), []string{"verify-base-source", "--input", root, "--sha256", seal}, &out); err == nil || out.Len() != 0 {
		t.Fatal("synthetic profile impersonated original frozen base")
	}
}
