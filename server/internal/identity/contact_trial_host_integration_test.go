//go:build rendererintegration

package identity

import (
	"os"
	"path/filepath"
	"testing"
)

// This selection uses the real compiled worker through the real command, then
// restarts plain Go at the same origin with the same session and saved data.
func TestCompiledContactHost(t *testing.T) {
	node, worker := os.Getenv("RENDERER_TEST_NODE"), os.Getenv("RENDERER_TEST_ARTIFACT")
	if !filepath.IsAbs(node) || !filepath.IsAbs(worker) {
		t.Fatal("actual Node executable and compiled worker are required")
	}
	contactTrialHostProcess(t, node, worker)
}
