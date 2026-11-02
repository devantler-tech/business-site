package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func carrierFixture(t *testing.T) (string, string, map[string]any) {
	t.Helper()
	root, args := handoffFixture(t)
	if err := os.Mkdir(filepath.Join(root, "zitadel-handoff"), 0700); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = filepath.Join(root, "zitadel-handoff/handoff.json")
	if err := runRelease(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "carrier.tar")
	var out bytes.Buffer
	if err := runRelease(context.Background(), []string{"pack-carrier", "--input", root, "--output", archive}, &out); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	return root, archive, receipt
}

func carrierUnpackArgs(archive, output string, receipt map[string]any) []string {
	return []string{"unpack-carrier", "--input", archive, "--sha256", receipt["archiveSha256"].(string),
		"--manifest-sha256", receipt["handoffSha256"].(string), "--source-sha", strings.Repeat("a", 40),
		"--run-id", "123", "--run-attempt", "1", "--output", output}
}

func TestOriginalCarrierRoundTripPreservesEveryOriginalAndGrantsNoAuthority(t *testing.T) {
	root, archive, receipt := carrierFixture(t)
	out := filepath.Join(t.TempDir(), "native")
	var response bytes.Buffer
	if err := runRelease(context.Background(), carrierUnpackArgs(archive, out, receipt), &response); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(append([]string{}, handoffWitnesses...), "zitadel-offline-replay/inputs.tar", "zitadel-base-source/base-source.tar", "zitadel-image/candidate-image.tar", "zitadel-handoff/handoff.json") {
		before, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("original changed: %s %v", name, err)
		}
		original, e1 := os.Lstat(filepath.Join(root, name))
		restored, e2 := os.Lstat(filepath.Join(out, name))
		if e1 != nil || e2 != nil || original.Mode().Perm() != restored.Mode().Perm() {
			t.Fatalf("original mode changed: %s", name)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(response.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forgeAdmission", "sourceOfferPublished", "imagePublished", "signatureVerified", "portalActivated"} {
		if receipt[name] != false || got[name] != false {
			t.Fatalf("transport granted %s", name)
		}
	}
}

func TestOriginalCarrierRefusesWrongBindingsAndExistingDestinations(t *testing.T) {
	for _, kind := range []string{"archive", "manifest", "source", "run", "attempt", "truncated", "trailing", "link", "existing"} {
		t.Run(kind, func(t *testing.T) {
			_, archive, receipt := carrierFixture(t)
			out := filepath.Join(t.TempDir(), "native")
			args := carrierUnpackArgs(archive, out, receipt)
			switch kind {
			case "archive", "manifest", "source", "run", "attempt":
				flag := map[string]string{"archive": "--sha256", "manifest": "--manifest-sha256", "source": "--source-sha", "run": "--run-id", "attempt": "--run-attempt"}[kind]
				for i := range args {
					if args[i] == flag {
						args[i+1] = map[string]string{"archive": strings.Repeat("9", 64), "manifest": strings.Repeat("9", 64), "source": strings.Repeat("b", 40), "run": "124", "attempt": "2"}[kind]
					}
				}
			case "truncated", "trailing":
				args[4] = alterArchiveTail(t, archive, kind)
			case "link":
				linked := archive + ".link"
				if err := os.Symlink(archive, linked); err != nil {
					t.Fatal(err)
				}
				args[2] = linked
			case "existing":
				if err := os.Mkdir(out, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := runRelease(context.Background(), args, &bytes.Buffer{}); err == nil {
				t.Fatal("unbound or malformed carrier accepted")
			}
		})
	}
}
