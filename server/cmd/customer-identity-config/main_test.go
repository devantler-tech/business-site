package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestExplicitPreparation(t *testing.T) {
	for _, args := range [][]string{nil, {"--prepare=false"}} {
		var out bytes.Buffer
		if err := run(args, &out); err != nil || out.Len() != 0 {
			t.Fatalf("default-off command emitted configuration: %q %v", out.String(), err)
		}
	}
	args := []string{"--prepare", "--dex-issuer=https://login.example.invalid", "--keycloak-issuer=https://accounts.example.invalid/realms/customers", "--portal-callback=https://site.example.invalid/portal/callback"}
	var out bytes.Buffer
	if err := run(args, &out); err != nil || !json.Valid(out.Bytes()) {
		t.Fatalf("explicit preparation failed: %v %q", err, out.String())
	}
	if err := run(args, brokenWriter{}); err == nil {
		t.Fatal("failed output must not report successful preparation")
	}
	if err := run(args, shortWriter{}); err == nil {
		t.Fatal("partial output must not report successful preparation")
	}
}

func TestInvalidArgumentsProduceNoPackage(t *testing.T) {
	for _, args := range [][]string{{"--prepare"}, {"--unknown"}, {"unexpected"}, {"--prepare=not-a-boolean"}} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("invalid request emitted a package: %q %v", out.String(), err)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("fictional writer failure") }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 1, nil }
