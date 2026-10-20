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
	"sync/atomic"
	"testing"
	"time"
)

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(data []byte) (int, error) { return f(data) }

func publicArtifactDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "da"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "da/index.html"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("public page"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// configuredPublicArtifact binds startup to real validated public bytes and restores build identity.
func configuredPublicArtifact(t *testing.T) string {
	t.Helper()
	dir := publicArtifactDir(t)
	artifact, err := loadPublic(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, oldDigest, oldMode := revision, publicDigest, buildMode
	t.Cleanup(func() { revision, publicDigest, buildMode = oldRevision, oldDigest, oldMode })
	revision, publicDigest, buildMode = strings.Repeat("a", 40), artifact.Digest(), "development"
	return dir
}

// Exercise the real startup and cancellation path, including owned cleanup,
// without a production broker, database, fixed port or sleep-based readiness.
func TestPackagedPublicRuntimeStartsAndCancels(t *testing.T) {
	// Private migration configuration cannot turn ordinary web startup into DDL.
	t.Setenv("PORTAL_MIGRATION_DATABASE_FILE", "/missing/private-migration-file")
	for _, name := range []string{"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL", "PORTAL_CLIENT_SECRET_FILE", "PORTAL_CLIENT_SECRET_PROJECTION_FILE"} {
		t.Setenv(name, "")
	}
	dir := configuredPublicArtifact(t)
	ctx, cancel := context.WithCancel(context.Background())
	var listen string
	listenTCP := func(network, address string) (net.Listener, error) {
		listener, err := net.Listen(network, address)
		if err == nil {
			// The readiness channel synchronizes this address with the HTTP client.
			// The runtime retains this exact listener until serving shuts down.
			listen = listener.Addr().String()
		}
		return listener, err
	}
	ready := make(chan struct{})
	var once sync.Once
	var readiness string
	output := writeFunc(func(data []byte) (int, error) {
		once.Do(func() { readiness = string(data); close(ready) })
		return len(data), nil
	})
	done := make(chan error, 1)
	stopped := make(chan struct{})
	defer func() {
		if err := cancelAndJoinRuntime(cancel, stopped, 5*time.Second); err != nil {
			t.Error("runtime fixture cleanup did not join its worker")
		}
	}()
	go func() {
		defer close(stopped)
		done <- runWithListen(ctx, []string{"--public", dir, "--listen", "127.0.0.1:0"}, output, listenTCP)
	}()
	select {
	case <-ready:
	case <-done:
		t.Fatal("runtime stopped before public serving")
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not begin startup")
	}
	if !strings.HasPrefix(readiness, "runtime ready; listen="+listen+"; ") {
		t.Fatal("runtime readiness did not identify its owned listener")
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

// cancelAndJoinRuntime bounds teardown and joins before dependent fixture cleanup.
func cancelAndJoinRuntime(cancel context.CancelFunc, stopped <-chan struct{}, timeout time.Duration) error {
	cancel()
	// A completed worker succeeds even when the timeout is already expired.
	select {
	case <-stopped:
		return nil
	default:
	}
	select {
	case <-stopped:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

// Exercise the actual deferred cleanup on completed, active and stuck workers.
func TestRuntimeCancelAndJoin(t *testing.T) {
	t.Run("already stopped", func(t *testing.T) {
		stopped := make(chan struct{})
		close(stopped)
		called := false
		if err := cancelAndJoinRuntime(func() { called = true }, stopped, 0); err != nil || !called {
			t.Fatal("completed worker cleanup did not cancel and join")
		}
	})
	t.Run("active worker", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stopped := make(chan struct{})
		var finished atomic.Bool
		go func() { <-ctx.Done(); finished.Store(true); close(stopped) }()
		if err := cancelAndJoinRuntime(cancel, stopped, 5*time.Second); err != nil || !finished.Load() {
			t.Fatal("cleanup returned before worker resources were released")
		}
	})
	t.Run("bounded timeout", func(t *testing.T) {
		called := false
		if err := cancelAndJoinRuntime(func() { called = true }, make(chan struct{}), 0); err != context.DeadlineExceeded || !called {
			t.Fatal("unfinished worker was reported joined or cancellation was missed")
		}
	})
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
	dir := publicArtifactDir(t)
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
