package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(data []byte) (int, error) { return f(data) }

// Exercise the real startup and cancellation path, including owned cleanup,
// without a production broker, database, fixed port or sleep-based readiness.
func TestPackagedPublicRuntimeStartsAndCancels(t *testing.T) {
	for _, name := range []string{"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL", "PORTAL_CLIENT_SECRET_FILE"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "da"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "da/index.html"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("public page"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	artifact, err := loadPublic(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, oldDigest, oldMode := revision, publicDigest, buildMode
	t.Cleanup(func() { revision, publicDigest, buildMode = oldRevision, oldDigest, oldMode })
	revision, publicDigest, buildMode = strings.Repeat("a", 40), artifact.Digest(), "development"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	address, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := address.Addr().String()
	if err := address.Close(); err != nil {
		t.Fatal("fixture port reservation cleanup failed")
	}
	ready := make(chan struct{})
	var once sync.Once
	output := writeFunc(func(data []byte) (int, error) {
		once.Do(func() { close(ready) })
		return len(data), nil
	})
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--public", dir, "--listen", listen}, output) }()
	select {
	case <-ready:
	case <-done:
		t.Fatal("runtime stopped before public serving")
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not begin startup")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://" + listen + "/")
	if err != nil {
		t.Fatal("runtime announced readiness without serving the public page")
	}
	body, readErr := io.ReadAll(res.Body)
	closeErr := res.Body.Close()
	if readErr != nil || closeErr != nil || res.StatusCode != http.StatusOK || string(body) != "public page" {
		t.Fatal("runtime did not serve the verified public snapshot")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("verified public runtime did not stop cleanly")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop after an actual public request")
	}
}

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
