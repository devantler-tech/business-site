package customeridentity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMaterialProjectionReadOnlyMount uses an owned disk image and actual kernel mount flags.
func TestMaterialProjectionReadOnlyMount(t *testing.T) {
	source := t.TempDir()
	outputs := map[string]string{}
	for _, phase := range []string{"init", "setup", "serve"} {
		input, output := materialProjectionFixture(t, phase)
		projectionMust(t, os.Chmod(input, 0777))
		projectionMust(t, os.Rename(input, filepath.Join(source, phase)))
		outputs[phase] = output
	}
	image := filepath.Join(t.TempDir(), "identity-fixture.dmg")
	if identityDiskCommand("create", "-srcfolder", source, "-format", "UDRO", "-fs", "HFS+", "-volname", "IdentityFixture", image) != nil {
		t.Fatal("could not create owned read-only fixture")
	}
	mount, err := filepath.EvalSymlinks(t.TempDir())
	projectionMust(t, err)
	if identityDiskCommand("attach", "-readonly", "-nobrowse", "-mountpoint", mount, image) != nil {
		t.Fatal("could not mount owned read-only fixture")
	}
	t.Cleanup(func() {
		if identityDiskCommand("detach", mount) != nil {
			t.Error("owned read-only fixture detach failed")
		}
	})
	for _, phase := range []string{"init", "setup", "serve"} {
		t.Run(phase, func(t *testing.T) {
			projected := filepath.Join(mount, phase)
			info, e := os.Lstat(projected)
			projectionMust(t, e)
			if info.Mode().Perm()&0022 == 0 {
				t.Fatal("fixture lost the broad directory mode")
			}
			if MaterializeProjection(phase, projected, outputs[phase]) != nil {
				t.Fatal("kernel read-only projection root was refused")
			}
			materialAssertOutput(t, outputs[phase], phase)
		})
	}
}

// identityDiskCommand bounds each native operation and never emits fixture paths or values.
func identityDiskCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/hdiutil", args...)
	command.WaitDelay = time.Second
	return command.Run()
}
