package runtimeapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSecretFIFOIsRejectedWithoutWaitingForWriter prevents unbounded startup before validation.
func TestSecretFIFOIsRejectedWithoutWaitingForWriter(t *testing.T) {
	name := filepath.Join(t.TempDir(), "secret-fifo")
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := readSecret(name); result <- err }()
	t.Cleanup(func() {
		// Release a blocked reader on the pre-fix implementation, without
		// leaving a test goroutine or requiring a blocking writer.
		if fd, err := syscall.Open(name, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			syscall.Close(fd)
		}
	})
	select {
	case err := <-result:
		if err != ErrConfig {
			t.Fatal("nonregular secret did not fail generically")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("secret open waited for a FIFO writer")
	}
}

// TestSecretFileUsesOneConfinedPrivateDescriptor retains projected-secret support without escapes.
func TestSecretFileUsesOneConfinedPrivateDescriptor(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "private")
	if err := os.WriteFile(private, []byte("synthetic-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSecret(private); err != nil || got != "synthetic-secret" {
		t.Fatal("private regular secret rejected")
	}
	projected := filepath.Join(dir, "projected")
	if err := os.Symlink("private", projected); err != nil {
		t.Fatal(err)
	}
	if got, err := readSecret(projected); err != nil || got != "synthetic-secret" {
		t.Fatal("confined projected secret rejected")
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("outside-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"escape", "missing"} {
		if _, err := readSecret(filepath.Join(dir, name)); err != ErrConfig {
			t.Fatal("unsafe secret did not fail generically")
		}
	}
	for _, tc := range []struct {
		data string
		mode os.FileMode
	}{{"", 0600}, {strings.Repeat("x", 16385), 0600}, {"synthetic-secret", 0644}} {
		if err := os.WriteFile(private, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(private, tc.mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readSecret(private); err != ErrConfig {
			t.Fatal("empty, oversized or public secret accepted")
		}
	}
}

// One file line ending is presentation, while all other secret bytes remain exact.
func TestSecretRemovesOnlyOneTrailingLineTerminator(t *testing.T) {
	name := filepath.Join(t.TempDir(), "secret")
	for _, tc := range []struct{ input, want string }{
		{"secret\n", "secret"}, {"secret\r\n", "secret"},
		{"secret", "secret"}, {" secret ", " secret "},
		{"secret\r", "secret\r"}, {"secret\n\n", "secret\n"},
		{"\n", ""}, {"\r\n", ""},
	} {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			if err := os.WriteFile(name, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readSecret(name)
			if tc.want == "" {
				if err != ErrConfig {
					t.Fatal("line-ending-only secret accepted")
				}
			} else if err != nil || got != tc.want {
				t.Fatal("one line terminator was not handled without changing other bytes")
			}
		})
	}
}
