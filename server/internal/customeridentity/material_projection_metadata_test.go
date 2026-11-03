package customeridentity

import (
	"os"
	"syscall"
	"testing"
)

type projectionMetadata struct {
	os.FileInfo
	metadata syscall.Stat_t
}

func (info projectionMetadata) Sys() any { return &info.metadata }

// TestMaterialProjectionOwnership checks both supported owners and primary-group-only reads.
func TestMaterialProjectionOwnership(t *testing.T) {
	input, _ := materialProjectionFixture(t, "serve")
	info, err := os.Stat(input + "/database-password")
	projectionMust(t, err)
	original, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("native Unix metadata missing")
	}
	for _, test := range []struct {
		name     string
		uid, gid uint32
		want     bool
	}{
		{"current owner", uint32(os.Geteuid()), uint32(os.Getegid()), true},
		{"volume writer owner", 0, uint32(os.Getegid()), true},
		{"foreign owner", uint32(os.Geteuid() + 1), uint32(os.Getegid()), false},
		{"foreign read group", 0, uint32(os.Getegid() + 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := *original
			metadata.Uid = test.uid
			metadata.Gid = test.gid
			if materialProjectionOwned(projectionMetadata{FileInfo: info, metadata: metadata}, false) != test.want {
				t.Fatal("projection ownership boundary differs")
			}
		})
	}
}
