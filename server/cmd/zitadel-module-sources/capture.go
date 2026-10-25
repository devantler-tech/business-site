package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/mod/module"
)

// The actual complete Linux compiler stream is about 20 MB at the frozen pin.
const inputLimit int64 = 32 << 20
const payloadLimit int64 = 512 << 20
const totalLimit int64 = 2 << 30

type receipt struct {
	SchemaVersion                    int    `json:"schemaVersion"`
	ManifestSHA256                   string `json:"manifestSha256"`
	Modules                          int    `json:"modules"`
	Files                            int    `json:"files"`
	Bytes                            int64  `json:"bytes"`
	SelectedServiceGoSourcesCaptured bool   `json:"selectedServiceGoSourcesCaptured"`
	DependencyInputsCaptured         bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved        bool   `json:"nativeOfflineReplayProved"`
}

type fileRecord struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type moduleRecord struct {
	Original  identity `json:"original"`
	Effective identity `json:"effective"`
	Prefix    string   `json:"prefix"`
}

type manifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	Profile         string         `json:"profile"`
	BuildRoot       string         `json:"buildRoot"`
	CatalogueSHA256 string         `json:"catalogueSha256"`
	Modules         []moduleRecord `json:"modules"`
	Files           []fileRecord   `json:"files"`
}

// capture retains selected module inputs from one build-owned cache.
func capture(build, cataloguePath, output, expected string) (result receipt, resultErr error) {
	if err := os.Mkdir(output, 0o700); err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			result = receipt{}
			resultErr = errors.Join(resultErr, os.RemoveAll(output))
		}
	}()
	root, err := openRoot(build)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	catalogueRoot, err := openRoot(filepath.Dir(cataloguePath))
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, catalogueRoot.Close()) }()
	raw, err := readAt(catalogueRoot, filepath.Base(cataloguePath), inputLimit)
	if err != nil {
		return result, err
	}
	mod, err := readAt(root, "prepared/source/go.mod", inputLimit)
	if err != nil {
		return result, err
	}
	sums, err := readAt(root, "prepared/source/go.sum", inputLimit)
	if err != nil {
		return result, err
	}
	packages, err := readAt(root, "packages.jsonl", inputLimit)
	if err != nil {
		return result, err
	}
	selected, err := selectModules(raw, mod, sums, packages, build, expected)
	if err != nil {
		return result, err
	}
	m := manifest{SchemaVersion: 1, Profile: "selected-service-go-sources-v1", BuildRoot: build, CatalogueSHA256: expected}
	for _, record := range []struct {
		name string
		body []byte
	}{{"records/catalogue.json", raw}, {"records/go.mod", mod}, {"records/go.sum", sums}, {"records/packages.jsonl", packages}} {
		if err := writeNew(output, record.name, record.body); err != nil {
			return result, err
		}
		m.Files = append(m.Files, fileRecord{record.name, int64(len(record.body)), hash(record.body)})
		result.Bytes += int64(len(record.body))
	}
	seen := map[string]identity{}
	for _, selected := range selected {
		prefix, err := modulePrefix(selected.Effective)
		if err != nil {
			return result, err
		}
		m.Modules = append(m.Modules, moduleRecord{selected.Original, selected.Effective, "modules/" + prefix})
		if old, ok := seen[prefix]; ok {
			if !reflect.DeepEqual(old, selected.Effective) {
				return result, fmt.Errorf("conflicting effective module")
			}
			continue
		}
		seen[prefix] = selected.Effective
		for _, extension := range []string{".zip", ".mod"} {
			name := "modules/" + prefix + extension
			source := "go-modules/cache/download/" + prefix + extension
			record, err := copyAt(root, source, output, name, totalLimit-result.Bytes)
			if err != nil {
				return result, err
			}
			m.Files = append(m.Files, record)
			result.Bytes += record.Bytes
		}
		retainedMod, err := os.ReadFile(filepath.Join(output, "modules/"+prefix+".mod"))
		if err != nil {
			return result, err
		}
		if err := verifyPayload(selected.Effective.Path, selected.Effective.Version, filepath.Join(output, "modules/"+prefix+".zip"), retainedMod, selected.Effective.Sum, selected.Effective.GoModSum); err != nil {
			return result, err
		}
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	if result.Bytes > totalLimit {
		return result, fmt.Errorf("selected module package exceeds limit")
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return result, err
	}
	body = append(body, '\n')
	if err := writeNew(output, "manifest.json", body); err != nil {
		return result, err
	}
	result.ManifestSHA256, result.Modules, result.Files = hash(body), len(m.Modules), len(m.Files)
	return verify(output, result.ManifestSHA256, expected)
}

// verify rechecks a closed retained package against its external seal.
func verify(directory, expected, catalogueHash string) (result receipt, resultErr error) {
	root, err := openRoot(directory)
	if err != nil {
		return receipt{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = receipt{}
		}
	}()
	raw, err := readAt(root, "manifest.json", inputLimit)
	if err != nil {
		return receipt{}, err
	}
	if hash(raw) != expected {
		return receipt{}, fmt.Errorf("external manifest seal differs")
	}
	var m manifest
	if err := decodeStrict(raw, &m); err != nil {
		return receipt{}, err
	}
	if m.SchemaVersion != 1 || m.Profile != "selected-service-go-sources-v1" || m.CatalogueSHA256 != catalogueHash || len(m.Files) < 6 || len(m.Files) > 1028 {
		return receipt{}, fmt.Errorf("unsupported retained module manifest")
	}
	records := map[string][]byte{}
	wanted := map[string]bool{"records/catalogue.json": true, "records/go.mod": true, "records/go.sum": true, "records/packages.jsonl": true}
	for name := range wanted {
		body, err := readAt(root, name, inputLimit)
		if err != nil {
			return receipt{}, err
		}
		records[name] = body
	}
	selected, err := selectModules(records["records/catalogue.json"], records["records/go.mod"], records["records/go.sum"], records["records/packages.jsonl"], m.BuildRoot, catalogueHash)
	if err != nil {
		return receipt{}, err
	}
	var modules []moduleRecord
	for _, selected := range selected {
		prefix, err := modulePrefix(selected.Effective)
		if err != nil {
			return receipt{}, err
		}
		prefix = "modules/" + prefix
		modules = append(modules, moduleRecord{selected.Original, selected.Effective, prefix})
		wanted[prefix+".zip"], wanted[prefix+".mod"] = true, true
	}
	if !reflect.DeepEqual(m.Modules, modules) || len(m.Files) != len(wanted) {
		return receipt{}, fmt.Errorf("retained module membership differs")
	}
	wantedDirectories := map[string]bool{".": true}
	for name := range wanted {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			wantedDirectories[dir] = true
		}
	}
	wanted["manifest.json"] = true
	if err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !wantedDirectories[name] {
				return fmt.Errorf("extra retained directory")
			}
			return nil
		}
		if !wanted[name] || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return fmt.Errorf("extra, linked, special or wrong-mode retained file")
		}
		return nil
	}); err != nil {
		return receipt{}, err
	}
	result = receipt{SchemaVersion: 1, ManifestSHA256: expected, Modules: len(modules), Files: len(m.Files), SelectedServiceGoSourcesCaptured: true}
	previous := ""
	for _, record := range m.Files {
		decoded, decodeErr := hex.DecodeString(record.SHA256)
		if !wanted[record.Path] || record.Path == "manifest.json" || record.Path <= previous || record.Bytes < 0 || record.Bytes > payloadLimit || decodeErr != nil || len(decoded) != sha256.Size {
			return receipt{}, fmt.Errorf("invalid retained file manifest")
		}
		previous = record.Path
		result.Bytes += record.Bytes
		if result.Bytes > totalLimit {
			return receipt{}, fmt.Errorf("retained package exceeds limit")
		}
		f, err := openAt(root, record.Path)
		if err != nil {
			return receipt{}, err
		}
		n, sha, copyErr := copyStream(io.Discard, f, record.Bytes)
		if err := errors.Join(copyErr, f.Close()); err != nil {
			return receipt{}, err
		}
		if n != record.Bytes || sha != record.SHA256 {
			return receipt{}, fmt.Errorf("retained file bytes differ")
		}
	}
	for _, selected := range selected {
		prefix, err := modulePrefix(selected.Effective)
		if err != nil {
			return receipt{}, err
		}
		prefix = "modules/" + prefix
		mod, err := readAt(root, prefix+".mod", inputLimit)
		if err != nil {
			return receipt{}, err
		}
		if err := verifyPayload(selected.Effective.Path, selected.Effective.Version, filepath.Join(directory, prefix+".zip"), mod, selected.Effective.Sum, selected.Effective.GoModSum); err != nil {
			return receipt{}, err
		}
	}
	return result, nil
}

// modulePrefix uses Go's canonical cache escaping, including uppercase paths.
func modulePrefix(id identity) (string, error) {
	p, err := module.EscapePath(id.Path)
	if err != nil {
		return "", err
	}
	v, err := module.EscapeVersion(id.Version)
	if err != nil {
		return "", err
	}
	return p + "/@v/" + v, nil
}

// openRoot requires a real explicit directory, never a linked root.
func openRoot(directory string) (*os.Root, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("root is not a regular directory")
	}
	return os.OpenRoot(directory)
}

// openAt rejects linked parents and binds the inspected regular file to its descriptor.
func openAt(root *os.Root, name string) (*os.File, error) {
	if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
		return nil, fmt.Errorf("invalid member path")
	}
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		info, err := root.Lstat(strings.Join(parts[:i], "/"))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked or special parent")
		}
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("nonregular input")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.Join(fmt.Errorf("input changed while opening"), err, f.Close())
	}
	return f, nil
}

// readAt bounds complete metadata reads and propagates close/read failures.
func readAt(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := openAt(root, name)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	err = errors.Join(readErr, f.Close())
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("input exceeds limit")
	}
	return body, nil
}

// copyStream preserves complete bytes and detects partial reads/writes and excess input.
func copyStream(dst io.Writer, src io.Reader, limit int64) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, limit+1))
	if err != nil {
		return n, "", err
	}
	if n > limit {
		return n, "", fmt.Errorf("payload exceeds limit")
	}
	return n, fmt.Sprintf("%x", h.Sum(nil)), nil
}

// copyAt creates a fresh retained file from the confined build cache.
func copyAt(root *os.Root, source, output, name string, remaining int64) (fileRecord, error) {
	f, err := openAt(root, source)
	if err != nil {
		return fileRecord{}, err
	}
	if err := os.MkdirAll(filepath.Join(output, path.Dir(name)), 0o755); err != nil {
		return fileRecord{}, errors.Join(err, f.Close())
	}
	dst, err := os.OpenFile(filepath.Join(output, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fileRecord{}, errors.Join(err, f.Close())
	}
	limit := min(payloadLimit, remaining)
	if strings.HasSuffix(name, ".mod") {
		limit = min(limit, inputLimit)
	}
	if limit < 0 {
		return fileRecord{}, errors.Join(fmt.Errorf("package size limit exceeded"), dst.Close(), f.Close())
	}
	n, sha, copyErr := copyStream(dst, f, limit)
	if err := errors.Join(copyErr, dst.Chmod(0o644), dst.Close(), f.Close()); err != nil {
		return fileRecord{}, err
	}
	return fileRecord{name, n, sha}, nil
}

// writeNew creates one complete fresh metadata file without replacing existing data.
func writeNew(output, name string, body []byte) error {
	if err := os.MkdirAll(filepath.Join(output, path.Dir(name)), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(output, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, _, writeErr := copyStream(f, strings.NewReader(string(body)), inputLimit)
	return errors.Join(writeErr, f.Chmod(0o644), f.Close())
}
