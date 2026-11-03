package customeridentity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var projectionGeneration = regexp.MustCompile(`^\.\.[0-9]{4}_[0-9]{2}_[0-9]{2}_[0-9]{2}_[0-9]{2}_[0-9]{2}\.[0-9]+$`)

type materialProjection struct {
	root             *os.Root
	path, generation string
	identity, link   os.FileInfo
	names            []string
}

// MaterializeProjection prepares one phase from a single atomic-writer generation.
// It never changes source permissions or copies inputs to an intermediate directory.
func MaterializeProjection(phase, inputDirectory, outputDirectory string) error {
	names, fields, err := materialProfile(phase)
	if err != nil || os.Geteuid() <= 0 || os.Getegid() <= 0 || !materialPath(inputDirectory) || outputDirectory == inputDirectory || strings.HasPrefix(outputDirectory, inputDirectory+string(filepath.Separator)) {
		return errMaterial
	}
	projection, err := materialOpenProjection(inputDirectory, names)
	if err != nil {
		return errMaterial
	}
	defer func() { _ = projection.root.Close() }() // Read-only input descriptor owns no mutable state.
	snapshot, err := materialReadOwned(filepath.Join(inputDirectory, projection.generation), names, materialProjectionOwned)
	if err != nil {
		return errMaterial
	}
	defer snapshot.close()
	snapshot.projection = projection
	// Both retained descriptors remain open through the final output readback.
	if snapshot.unchanged() != nil {
		return errMaterial
	}
	return materializeSnapshot(phase, outputDirectory, fields, snapshot)
}

// materialProjectionOwned permits the volume writer owner and only the process primary read group.
func materialProjectionOwned(info os.FileInfo, directory bool) bool {
	return materialProjectionPermissions(info, directory, false)
}

// materialProjectionPermissions preserves file restrictions even on a read-only mount.
func materialProjectionPermissions(info os.FileInfo, directory, readOnly bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && uint64(stat.Uid) != uint64(os.Geteuid())) {
		return false
	}
	mode := info.Mode()
	if directory {
		return info.IsDir() && mode&os.ModeSetuid == 0 && (readOnly || mode&(os.ModeSticky|0022) == 0) && mode.Perm()&0500 == 0500 && (mode.Perm()&0070 == 0 || uint64(stat.Gid) == uint64(os.Getegid()))
	}
	return mode.IsRegular() && mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && mode.Perm()&0137 == 0 && stat.Nlink == 1 && info.Size() > 0 && info.Size() <= 65536 && (mode.Perm()&0040 == 0 || uint64(stat.Gid) == uint64(os.Getegid()))
}

// materialProjectionRoot admits broad directory bits only when this descriptor's mount is read-only.
func materialProjectionRoot(root *os.Root, info os.FileInfo) bool {
	if materialProjectionOwned(info, true) {
		return true
	}
	if !materialProjectionPermissions(info, true, true) {
		return false
	}
	directory, err := root.Open(".")
	if err != nil {
		return false
	}
	var status syscall.Statfs_t
	statErr := syscall.Fstatfs(int(directory.Fd()), &status)
	closeErr := directory.Close()
	// Linux ST_RDONLY and Darwin MNT_RDONLY both use this kernel ABI bit.
	const readOnlyMount = 1
	return statErr == nil && closeErr == nil && uint64(status.Flags)&readOnlyMount != 0
}

// materialOpenProjection pins the mount root, its generation link and complete aliases.
func materialOpenProjection(path string, names []string) (*materialProjection, error) {
	identity, err := os.Lstat(path)
	if err != nil || !identity.IsDir() {
		return nil, errMaterial
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errMaterial
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}() // Failed reads create no source state.
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(identity, actual) || !materialProjectionRoot(root, actual) {
		return nil, errMaterial
	}
	generation, err := root.Readlink("..data")
	if err != nil || !projectionGeneration.MatchString(generation) {
		return nil, errMaterial
	}
	link, err := root.Lstat("..data")
	if err != nil || link.Mode()&os.ModeSymlink == 0 {
		return nil, errMaterial
	}
	projection := &materialProjection{root: root, path: path, generation: generation, identity: identity, link: link, names: names}
	if projection.unchanged() != nil {
		return nil, errMaterial
	}
	success = true
	return projection, nil
}

// unchanged refuses rotation, replacement and unexpected members without following arbitrary links.
func (projection *materialProjection) unchanged() error {
	identity, err := os.Lstat(projection.path)
	if err != nil || !os.SameFile(identity, projection.identity) || !materialProjectionRoot(projection.root, identity) {
		return errMaterial
	}
	link, err := projection.root.Lstat("..data")
	if err != nil || !os.SameFile(link, projection.link) {
		return errMaterial
	}
	generation, err := projection.root.Readlink("..data")
	if err != nil || generation != projection.generation {
		return errMaterial
	}
	names := append([]string{"..data", projection.generation}, projection.names...)
	if materialMembers(projection.root, names) != nil {
		return errMaterial
	}
	for _, name := range projection.names {
		target, e := projection.root.Readlink(name)
		if e != nil || target != "..data/"+name {
			return errMaterial
		}
	}
	return nil
}
