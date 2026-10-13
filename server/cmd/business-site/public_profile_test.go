package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readPublicReceipt decodes the actual command output for both package profiles.
func readPublicReceipt(t *testing.T, output string) map[string]string {
	t.Helper()
	var receipt map[string]string
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

// TestPackageReceiptNamesPublicProfile binds the actual executable's receipt,
// rather than assuming that a successful ordinary build contains company pages.
func TestPackageReceiptNamesPublicProfile(t *testing.T) {
	oldRevision, oldDigest, oldMode := revision, publicDigest, buildMode
	t.Cleanup(func() { revision, publicDigest, buildMode = oldRevision, oldDigest, oldMode })
	revision, publicDigest, buildMode = strings.Repeat("a", 40), strings.Repeat("b", 64), "development"
	var output strings.Builder
	if err := run(context.Background(), []string{"--version"}, &output); err != nil {
		t.Fatal(err)
	}
	receipt := readPublicReceipt(t, output.String())
	if receipt["PublicProfile"] != "default" {
		t.Fatal("the actual package receipt does not bind its default public-content profile")
	}
}

// TestProductionDisclosureReceipt exercises the actual startup command with a
// fictional private projection. Validation must not need a listener or portal.
func TestProductionDisclosureReceipt(t *testing.T) {
	dir := publicArtifactDir(t)
	for _, name := range []string{"company/index.html", "da/company/index.html"} {
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		data := `<html><body><address data-company-address data-pagefind-ignore><span>__BUSINESS_ADDRESS_LINE_1__</span><span>__BUSINESS_ADDRESS_LINE_2__</span></address></body></html>`
		if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	artifact, err := loadPublic(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, oldDigest, oldMode, oldProfile := revision, publicDigest, buildMode, publicProfile
	t.Cleanup(func() { revision, publicDigest, buildMode, publicProfile = oldRevision, oldDigest, oldMode, oldProfile })
	revision, publicDigest, buildMode, publicProfile = strings.Repeat("a", 40), artifact.Digest(), "development", "production-template"
	projection := filepath.Join(t.TempDir(), "disclosure")
	if err := os.WriteFile(projection, []byte("Example & Partners Road 1\n1234 Exampletown"), 0600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	listen := func(string, string) (net.Listener, error) {
		t.Fatal("public validation opened a listener")
		return nil, nil
	}
	if err := runWithListen(context.Background(), []string{"--public", dir, "--company-disclosure-file", projection, "--check-serving-public"}, &output, listen); err != nil {
		t.Fatal(err)
	}
	receipt := readPublicReceipt(t, output.String())
	if receipt["PublicProfile"] != "production-template" || receipt["PublicDigest"] != artifact.Digest() || receipt["EffectivePublicDigest"] == artifact.Digest() || len(receipt["EffectivePublicDigest"]) != 64 {
		t.Fatal("template and served public identities were not distinguished")
	}
	if strings.Contains(output.String(), "Example") || strings.Contains(output.String(), projection) {
		t.Fatal("validation receipt exposed disclosure input")
	}
	for _, args := range [][]string{{"--public", dir, "--check-serving-public"}, {"--public", dir, "--company-disclosure-file", projection + "-missing", "--check-serving-public"}} {
		if err := runWithListen(context.Background(), args, &output, listen); err == nil || strings.Contains(err.Error(), projection) {
			t.Fatal("absent input was accepted or disclosed")
		}
	}
}
