package main

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
)

// TestIdentityPreparationRefusesUnboundAndCancelledCommands never opens a web listener.
func TestIdentityPreparationRefusesUnboundAndCancelledCommands(t *testing.T) {
	args := []string{"--identity-materialize", "--identity-phase", "serve", "--identity-inputs", "/private/not-read", "--identity-output", "/private/not-written"}
	listen := func(string, string) (net.Listener, error) {
		t.Fatal("identity preparation fell through to web startup")
		return nil, nil
	}
	var output strings.Builder
	if err := runWithListen(context.Background(), args, &output, listen); err == nil || err.Error() != "customer identity phase preparation refused" || output.Len() != 0 {
		t.Fatal("unbound identity command did not fail privately")
	}
	configuredPublicArtifact(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWithListen(ctx, args, io.Discard, listen); err == nil || err.Error() != "customer identity phase preparation refused" {
		t.Fatal("cancelled identity preparation did not fail closed")
	}
}
