package identity

import (
	"os/exec"
	"strings"
	"testing"
)

// The unconditional database CI job supplies Node 24 before running this suite.
// Exercise the served browser asset's counter, not a translated Go replica.
func TestRequestBrowserCharacterScript(t *testing.T) {
	version, err := exec.CommandContext(t.Context(), "node", "--version").Output()
	if err != nil || !strings.HasPrefix(string(version), "v24.") {
		t.Fatal("Node 24 is required for the actual browser-script controls")
	}
	output, err := exec.CommandContext(t.Context(), "node", "--test", "../../../scripts/portal-request-limits.test.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("actual browser character controls: %v\n%s", err, output)
	}
}
