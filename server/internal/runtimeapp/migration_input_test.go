package runtimeapp

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Owned fixtures exercise path integrity without changing another UID's files.
func TestMigrationInputPathIntegrity(t *testing.T) {
	dir := t.TempDir()
	version := filepath.Join(dir, "version")
	if err := os.Mkdir(version, 0700); err != nil {
		t.Fatal("owned projection directory failed")
	}
	file := filepath.Join(version, "certificate")
	if err := os.WriteFile(file, []byte("trust material"), 0644); err != nil {
		t.Fatal("owned trust material failed")
	}
	if err := os.Symlink("version", filepath.Join(dir, "..data")); err != nil {
		t.Fatal("owned projection generation failed")
	}
	projected := filepath.Join(dir, "certificate")
	if err := os.Symlink("..data/certificate", projected); err != nil {
		t.Fatal("owned projection link failed")
	}
	for _, mode := range []os.FileMode{0700, 0770, 0777, 0777 | os.ModeSticky} {
		if err := os.Chmod(version, mode); err != nil {
			t.Fatal("owned ancestor permission control failed")
		}
		certificate, err := migrationCertificate(projected)
		if mode == 0700 || mode&os.ModeSticky != 0 {
			if err != nil || string(certificate) != "trust material" {
				t.Fatal("protected selected entry or projected certificate was refused")
			}
		} else if err != ErrMigration || certificate != nil {
			t.Fatal("writable symlink-target ancestor was accepted")
		}
	}
	if err := os.Chmod(version, 0700); err != nil {
		t.Fatal("owned ancestor restoration failed")
	}
	outside := filepath.Join(t.TempDir(), "certificate")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal("owned escape fixture failed")
	}
	escape := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal("owned escape link failed")
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal("owned cycle link failed")
	}
	for _, name := range []string{escape, loop, dir + "/version/../certificate"} {
		if certificate, err := migrationCertificate(name); err != ErrMigration || certificate != nil {
			t.Fatal("escaped, cyclic or noncanonical migration path was accepted")
		}
	}
}

type migrationOwnershipFixture struct {
	os.FileInfo
	metadata any
}

// Sys supplies controlled ownership metadata without a privileged chown.
func (info migrationOwnershipFixture) Sys() any { return info.metadata }

// Mode bits alone cannot authorize a foreign owner or unknown metadata. Fake
// stat metadata avoids privileges or modifying files belonging to another user.
func TestMigrationInputOwnership(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal("owned metadata fixture unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		t.Fatal("owned Unix ownership metadata unavailable")
	}
	foreign := uint32(42)
	if os.Geteuid() == 42 {
		foreign = 43
	}
	for _, tc := range []struct {
		metadata any
		allowed  bool
	}{
		{&syscall.Stat_t{Uid: 0}, true},
		{&syscall.Stat_t{Uid: stat.Uid}, true},
		{&syscall.Stat_t{Uid: foreign}, false},
		{(*syscall.Stat_t)(nil), false},
		{nil, false},
	} {
		if migrationOperatorOwns(migrationOwnershipFixture{FileInfo: info, metadata: tc.metadata}) != tc.allowed {
			t.Fatal("migration ownership did not bind to root or the operator UID")
		}
	}
}
