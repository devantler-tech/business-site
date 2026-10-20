package runtimeapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// projectedIdentityFixture creates real atomic-volume links and matching-group metadata.
func projectedIdentityFixture(t *testing.T, data []byte, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "..fixture")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal("projection directory creation failed")
	}
	file := filepath.Join(target, "secret")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal("projection fixture write failed")
	}
	if err := os.Chown(file, -1, os.Getegid()); err != nil {
		t.Fatal("projection group assignment failed")
	}
	if err := os.Chmod(file, mode); err != nil {
		t.Fatal("projection permission assignment failed")
	}
	if err := os.Symlink("..fixture", filepath.Join(dir, "..data")); err != nil {
		t.Fatal("projection data link failed")
	}
	if err := os.Symlink("..data/secret", filepath.Join(dir, "secret")); err != nil {
		t.Fatal("projection leaf link failed")
	}
	return filepath.Join(dir, "secret")
}

// TestIdentityProjectionBoundary retains strict private input checks on real descriptors.
func TestIdentityProjectionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		mode              os.FileMode
	}{
		{"atomic group read", "synthetic-secret", "synthetic-secret", 0440},
		{"one CRLF", "synthetic-secret\r\n", "synthetic-secret", 0440},
		{"preserved whitespace", " synthetic-secret \n", " synthetic-secret ", 0640},
		{"empty", "", "", 0440},
		{"only ending", "\n", "", 0440},
		{"oversized", strings.Repeat("x", 16385), "", 0440},
		{"other read", "synthetic-secret", "", 0444},
		{"group write", "synthetic-secret", "", 0660},
		{"execute", "synthetic-secret", "", 0540},
		{"special mode", "synthetic-secret", "", 0440 | os.ModeSetuid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := projectedIdentityFixture(t, []byte(tc.input), tc.mode)
			got, err := readIdentitySecret(Config{secretFile: path, secretProjected: true})
			if tc.want == "" {
				if got != "" || !errors.Is(err, ErrConfig) {
					t.Fatal("unsafe projection did not fail generically")
				}
			} else if err != nil || got != tc.want {
				t.Fatal("valid projection bytes were not preserved")
			}
			if _, err := readSecret(path); err != ErrConfig {
				t.Fatal("ordinary reader accepted group-readable input")
			}
		})
	}
	t.Run("confined links only", func(t *testing.T) {
		outside := projectedIdentityFixture(t, []byte("synthetic-secret"), 0440)
		dir := t.TempDir()
		path := filepath.Join(dir, "escape")
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal("escape fixture creation failed")
		}
		for _, path := range []string{path, filepath.Join(dir, "missing"), dir, "relative-secret"} {
			if value, err := readIdentitySecret(Config{secretFile: path, secretProjected: true}); value != "" || err != ErrConfig {
				t.Fatal("unconfined or nonregular projection accepted")
			}
		}
	})
	t.Run("descriptor ownership policy", func(t *testing.T) {
		path := projectedIdentityFixture(t, []byte("synthetic-secret"), 0440)
		file, err := os.Open(path)
		if err != nil {
			t.Fatal("ownership fixture open failed")
		}
		info, err := file.Stat()
		if closeErr := file.Close(); err != nil || closeErr != nil {
			t.Fatal("ownership fixture metadata failed")
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !projectionOwnerAllowed(owner, info.Mode()) {
			t.Fatal("real matching ownership was not accepted")
		}
		// Vary the captured metadata, not host membership or privileged fixture setup.
		wrongGroup := *owner
		wrongGroup.Gid ^= 1
		foreignOwner := *owner
		foreignOwner.Uid = 1
		if os.Geteuid() == 1 {
			foreignOwner.Uid = 2
		}
		rootOwner := *owner
		rootOwner.Uid = 0
		for _, tc := range []struct {
			name  string
			owner *syscall.Stat_t
			mode  os.FileMode
			want  bool
		}{
			{"root owner", &rootOwner, 0440, true},
			{"foreign owner", &foreignOwner, 0440, false},
			{"wrong group", &wrongGroup, 0440, false},
			{"owner-only unrelated group", &wrongGroup, 0600, true},
			{"missing Unix metadata", nil, 0440, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if projectionOwnerAllowed(tc.owner, tc.mode) != tc.want {
					t.Fatal("projection ownership policy changed")
				}
			})
		}
	})
	t.Run("FIFO is bounded", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "fifo")
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal("FIFO fixture creation failed")
		}
		result := make(chan error, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := readIdentitySecret(Config{secretFile: path, secretProjected: true})
			result <- err
		}()
		t.Cleanup(func() {
			if fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				if err := syscall.Close(fd); err != nil {
					t.Error("FIFO fixture cleanup failed")
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("FIFO reader cleanup did not finish")
			}
		})
		select {
		case err := <-result:
			if err != ErrConfig {
				t.Fatal("FIFO projection did not fail generically")
			}
		case <-time.After(time.Second):
			t.Fatal("projection waited for a FIFO writer")
		}
	})
}
