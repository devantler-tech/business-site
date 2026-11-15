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
			t.Fatal("escaped, cyclic or non-canonical migration path was accepted")
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

// A Kubernetes Secret projection is root-owned and, with the pod's fsGroup,
// readable by that group: the kubelet writes 0440 root:<fsGroup>. Only the
// projected transport admits that group read, and only for this process's own
// primary group; the strict private-file transport keeps refusing it.
func TestMigrationProjectedConnection(t *testing.T) {
	dir, version := kubeletProjection(t, "database-url")
	file := filepath.Join(version, "database-url")
	if err := os.WriteFile(file, []byte("postgres://fixture\n"), 0400); err != nil {
		t.Fatal("owned projected connection failed")
	}
	if err := os.Chown(file, -1, os.Getegid()); err != nil {
		t.Fatal("owned projected connection group failed")
	}
	projected := filepath.Join(dir, "database-url")
	nonRoot := os.Geteuid() > 0 && os.Getegid() > 0
	for _, tc := range []struct {
		mode              os.FileMode
		projected, strict bool
	}{
		{0400, true, true},
		{0440, true, false},
		{0460, false, false},
		{0450, false, false},
		{0444, false, false},
		{0404, false, false},
	} {
		if err := os.Chmod(file, tc.mode); err != nil {
			t.Fatal("owned projected connection mode failed")
		}
		value, err := migrationProjectedConnection(projected)
		if accepted := err == nil && value == "postgres://fixture"; accepted != (tc.projected && nonRoot) || !accepted && err != ErrMigration {
			t.Fatalf("projected transport decided mode %o wrongly", tc.mode)
		}
		value, err = migrationConnection(projected)
		if accepted := err == nil && value == "postgres://fixture"; accepted != tc.strict {
			t.Fatalf("strict transport decided mode %o wrongly", tc.mode)
		}
	}
}

// A group-read bit is this process's only when the file's group is its own
// primary group; another group or unknown metadata is refused.
func TestMigrationOwnGroup(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal("owned metadata fixture unavailable")
	}
	own := uint32(os.Getegid())
	for _, tc := range []struct {
		metadata any
		allowed  bool
	}{
		{&syscall.Stat_t{Gid: own}, own > 0},
		{&syscall.Stat_t{Gid: own + 1}, false},
		{(*syscall.Stat_t)(nil), false},
		{nil, false},
	} {
		if migrationOwnGroup(migrationOwnershipFixture{FileInfo: info, metadata: tc.metadata}) != tc.allowed {
			t.Fatal("projected group read did not bind to the process's own group")
		}
	}
}

// kubeletProjection lays out a Secret projection the way the kubelet does: a
// timestamped generation directory, the ..data link to it and one link per
// entry. It returns the projection directory and the generation directory.
func kubeletProjection(t *testing.T, names ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	version := filepath.Join(dir, "..2026_11_15_00_00_00.1")
	if err := os.Mkdir(version, 0755); err != nil {
		t.Fatal("owned projection generation failed")
	}
	if err := os.Symlink(filepath.Base(version), filepath.Join(dir, "..data")); err != nil {
		t.Fatal("owned projection generation link failed")
	}
	for _, name := range names {
		if err := os.Symlink("..data/"+name, filepath.Join(dir, name)); err != nil {
			t.Fatal("owned projection entry link failed")
		}
	}
	return dir, version
}
