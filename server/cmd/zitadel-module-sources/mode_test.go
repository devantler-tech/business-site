package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// TestCaptureUnderRestrictiveUmask isolates process-global permissions in a child.
func TestCaptureUnderRestrictiveUmask(t *testing.T) {
	if os.Getenv("MODULE_CAPTURE_UMASK_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestCaptureUnderRestrictiveUmask$")
		command.Env = append(os.Environ(), "MODULE_CAPTURE_UMASK_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("restrictive umask capture: %v\n%s", err, output)
		}
		return
	}
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	capturedSelection(t)
}
