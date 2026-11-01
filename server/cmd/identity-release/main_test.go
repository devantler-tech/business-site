package main

import (
	"bytes"
	"context"
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
	_, root, seal := capturedBaseFixture(t)
	var out bytes.Buffer
	if err := runRelease(context.Background(), []string{"verify-base-source", "--input", root, "--sha256", seal}, &out); err == nil || out.Len() != 0 {
		t.Fatal("synthetic profile impersonated original frozen base")
	}
}
