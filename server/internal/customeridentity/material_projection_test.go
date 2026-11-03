package customeridentity

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestMaterialProjectionRefusals mutates actual files, permissions and native links independently.
func TestMaterialProjectionRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string, string)
	}{
		{"missing key", func(t *testing.T, _, g string) { projectionMust(t, os.Remove(filepath.Join(g, "database-password"))) }},
		{"extra key", func(t *testing.T, _, g string) { materialWrite(t, g, "extra-secret", []byte("fictional")) }},
		{"extra mount member", func(t *testing.T, p, _ string) { materialWrite(t, p, "extra-secret", []byte("fictional")) }},
		{"world readable", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0644))
		}},
		{"group writable", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0660))
		}},
		{"executable", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0700))
		}},
		{"empty", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0600))
			materialWrite(t, g, "database-password", nil)
		}},
		{"oversized", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0600))
			materialWrite(t, g, "database-password", []byte(strings.Repeat("x", 65537)))
		}},
		{"hard link", func(t *testing.T, p, g string) {
			projectionMust(t, os.Link(filepath.Join(g, "database-password"), filepath.Join(filepath.Dir(p), "outside-link")))
		}},
		{"file symlink", func(t *testing.T, _, g string) {
			projectionMust(t, os.Remove(filepath.Join(g, "database-password")))
			projectionMust(t, os.Symlink("settings.json", filepath.Join(g, "database-password")))
		}},
		{"fifo", func(t *testing.T, _, g string) {
			projectionMust(t, os.Remove(filepath.Join(g, "database-password")))
			projectionMust(t, syscall.Mkfifo(filepath.Join(g, "database-password"), 0600))
		}},
		{"alias escape", func(t *testing.T, p, g string) {
			projectionMust(t, os.Remove(filepath.Join(p, "database-password")))
			projectionMust(t, os.Symlink(filepath.Join(g, "database-password"), filepath.Join(p, "database-password")))
		}},
		{"generation escape", func(t *testing.T, p, g string) {
			projectionMust(t, os.Remove(filepath.Join(p, "..data")))
			projectionMust(t, os.Symlink(g, filepath.Join(p, "..data")))
		}},
		{"writable directory", func(t *testing.T, _, g string) { projectionMust(t, os.Chmod(g, 0770)) }},
		{"other-user-only read", func(t *testing.T, _, g string) {
			projectionMust(t, os.Chmod(filepath.Join(g, "database-password"), 0004))
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, output := materialProjectionFixture(t, "serve")
			generation, e := os.Readlink(filepath.Join(input, "..data"))
			projectionMust(t, e)
			test.mutate(t, input, filepath.Join(input, generation))
			if err := MaterializeProjection("serve", input, output); err == nil {
				t.Fatal("invalid projection accepted")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("invalid projection reserved output")
			}
		})
	}
}

func projectionMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestMaterialProjectionSnapshotRefusesChanges uses the same retained snapshot as output creation.
func TestMaterialProjectionSnapshotRefusesChanges(t *testing.T) {
	for _, mutation := range []string{"rotation", "same bytes new generation link", "member replacement", "changed bytes", "mount replacement"} {
		t.Run(mutation, func(t *testing.T) {
			input, output := materialProjectionFixture(t, "serve")
			names, _, err := materialProfile("serve")
			projectionMust(t, err)
			projection, err := materialOpenProjection(input, names)
			projectionMust(t, err)
			defer func() { _ = projection.root.Close() }()
			generation := filepath.Join(input, projection.generation)
			snapshot, err := materialReadOwned(generation, names, materialProjectionOwned)
			projectionMust(t, err)
			defer snapshot.close()
			snapshot.projection = projection
			projectionMust(t, snapshot.unchanged())
			switch mutation {
			case "rotation":
				next := "..2026_11_03_12_00_01.123456789"
				projectionMust(t, os.Rename(generation, filepath.Join(input, next)))
				projectionMust(t, os.Remove(filepath.Join(input, "..data")))
				projectionMust(t, os.Symlink(next, filepath.Join(input, "..data")))
			case "same bytes new generation link":
				projectionMust(t, os.Symlink(projection.generation, filepath.Join(input, "..next")))
				projectionMust(t, os.Rename(filepath.Join(input, "..next"), filepath.Join(input, "..data")))
			case "member replacement":
				data, e := os.ReadFile(filepath.Join(generation, "database-password"))
				projectionMust(t, e)
				materialWrite(t, generation, "replacement", data)
				projectionMust(t, os.Rename(filepath.Join(generation, "replacement"), filepath.Join(generation, "database-password")))
			case "changed bytes":
				projectionMust(t, os.Chmod(filepath.Join(generation, "database-password"), 0600))
				materialWrite(t, generation, "database-password", materialPassword(t))
			case "mount replacement":
				projectionMust(t, os.Rename(input, input+"-displaced"))
				projectionMust(t, os.Mkdir(input, 0700))
			}
			if snapshot.unchanged() == nil {
				t.Fatal("changed projection retained snapshot authority")
			}
			if materialWriteOutputs(output, map[string][]byte{"config.json": []byte("fictional")}, snapshot) == nil {
				t.Fatal("changed projection produced successful output")
			}
			entries, e := os.ReadDir(output)
			if e != nil || len(entries) != 0 {
				t.Fatal("failed output retained usable credentials")
			}
		})
	}
}
