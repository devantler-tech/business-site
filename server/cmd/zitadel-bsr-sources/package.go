package main

import (
	"bytes"
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
	"slices"
	"strings"
)

const frozenLock = "47029cbd1d10cd84291ffbdf1f84e1a1a8ccf9ebe5c4f098633b46ca7e4ffc6b"

type fileRecord struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type toolRecord struct {
	Phase   string `json:"phase"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type manifest struct {
	SchemaVersion        int             `json:"schemaVersion"`
	Profile              string          `json:"profile"`
	SourceManifestSHA256 string          `json:"sourceManifestSha256"`
	RecipeSHA256         string          `json:"recipeSha256"`
	Tools                []toolRecord    `json:"tools"`
	Modules              []moduleProfile `json:"modules"`
	Files                []fileRecord    `json:"files"`
}

type receipt struct {
	SchemaVersion              int    `json:"schemaVersion"`
	ManifestSHA256             string `json:"manifestSha256"`
	Modules                    int    `json:"modules"`
	Files                      int    `json:"files"`
	CacheFiles                 int    `json:"cacheFiles"`
	ExportFiles                int    `json:"exportFiles"`
	Bytes                      int64  `json:"bytes"`
	SelectedBSRSourcesCaptured bool   `json:"selectedBSRSourcesCaptured"`
	DependencyInputsCaptured   bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved  bool   `json:"nativeOfflineReplayProved"`
}

// digest identifies literal bytes independently from the native Buf content digest.
func digest(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

// canonical encodes only the verifier's closed schema in its own stable format.
func canonical(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	return append(raw, '\n'), err
}

// modulePrefix separates immutable module identity from its native cache spelling.
func modulePrefix(p moduleProfile) string {
	return "modules/" + p.Owner + "/" + p.Module + "/" + p.Commit + "/"
}

// collectModule reads complete selected native subtrees and the independent acquisition.
func collectModule(root *os.Root, p moduleProfile, remaining *int64) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, format := range []string{"b5", "shake256"} {
		prefix := "buf-cache/v3/modules/" + format + "/buf.build/" + p.Owner + "/" + p.Module + "/" + p.Commit
		tree, err := readTree(root, prefix, remaining)
		if err != nil {
			return nil, err
		}
		for name, body := range tree {
			files[format+"/"+name] = body
		}
	}
	exported, err := readTree(root, "bsr-exports/"+p.Owner+"/"+p.Module, remaining)
	if err != nil {
		return nil, err
	}
	for name, body := range exported {
		files["export/"+name] = body
	}
	for name, input := range map[string]string{
		"commit.json":      "buf-cache/v3/commits/shake256/buf.build/" + p.Commit + ".json",
		"acquisition.json": "bsr-records/" + p.Commit + ".json",
	} {
		body, err := readBudgeted(root, input, false, remaining)
		if err != nil {
			return nil, err
		}
		files[name] = body
	}
	return files, validateModule(p, files)
}

// capture retains only the frozen schema inputs; shell-owned commands acquire them.
func capture(build, recipe, sourceSeal, output string, profiles []moduleProfile, lockHash string) (result receipt, resultErr error) {
	root, err := checkedRoot(build)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	files := map[string][]byte{}
	remaining := packageLimit
	for _, p := range profiles {
		module, err := collectModule(root, p, &remaining)
		if err != nil {
			return result, err
		}
		for name, body := range module {
			files[modulePrefix(p)+name] = body
		}
	}
	for name, input := range map[string]string{
		"source-manifest.json": "source-snapshot/source-manifest.json",
		"buf.lock":             "prepared/source/proto/buf.lock",
		"api.version":          "bsr-tools/api.version",
		"console.version":      "bsr-tools/console.version",
	} {
		body, err := readBudgeted(root, input, false, &remaining)
		if err != nil {
			return result, err
		}
		files["records/"+name] = body
	}
	if digest(files["records/source-manifest.json"]) != sourceSeal {
		return result, fmt.Errorf("prepared-source external seal differs")
	}
	recipeRoot, err := checkedRoot(filepath.Dir(recipe))
	if err != nil {
		return result, err
	}
	files["records/build-recipe.sh"], err = readBudgeted(recipeRoot, filepath.Base(recipe), true, &remaining)
	if err := errors.Join(err, recipeRoot.Close()); err != nil {
		return result, err
	}
	m := manifest{SchemaVersion: 1, Profile: "selected-bsr-sources-v1", SourceManifestSHA256: sourceSeal, RecipeSHA256: digest(files["records/build-recipe.sh"]), Modules: profiles}
	for _, phase := range []string{"api", "console"} {
		info, err := inspectAt(root, "bsr-tools/"+phase, true)
		if err != nil {
			return result, err
		}
		if info.Size() == 0 || info.Mode()&0o111 == 0 {
			return result, fmt.Errorf("supplied native tool is empty or lacks execute permission")
		}
		binary, err := readInput(root, "bsr-tools/"+phase, true, 128<<20)
		if err != nil {
			return result, err
		}
		m.Tools = append(m.Tools, toolRecord{phase, strings.TrimSuffix(string(files["records/"+phase+".version"]), "\n"), digest(binary)})
	}
	for _, name := range sortedNames(files) {
		m.Files = append(m.Files, fileRecord{name, int64(len(files[name])), digest(files[name])})
	}
	if _, err := validatePackage(m, files, profiles, lockHash); err != nil {
		return result, err
	}
	raw, err := canonical(m)
	if err != nil {
		return result, err
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return result, err
	}
	if err := os.Chmod(output, 0o700); err != nil {
		return result, err
	}
	owned, err := checkedRoot(output)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, owned.Close()) }()
	for _, name := range sortedNames(files) {
		if err := writePayload(owned, name, files[name]); err != nil {
			return result, err
		}
	}
	if err := writePayload(owned, "manifest.json", raw); err != nil {
		return result, err
	}
	return verify(output, digest(raw), profiles, lockHash)
}

// sortedNames fixes inventory order without inheriting a map's iteration order.
func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// validatePackage refuses invented identities, extra records and incomplete input graphs.
func validatePackage(m manifest, files map[string][]byte, profiles []moduleProfile, lockHash string) (receipt, error) {
	result := receipt{SchemaVersion: 1, Modules: len(profiles), Files: len(files), SelectedBSRSourcesCaptured: true}
	if m.SchemaVersion != 1 || m.Profile != "selected-bsr-sources-v1" || !reflect.DeepEqual(m.Modules, profiles) || len(m.Tools) != 2 {
		return receipt{}, fmt.Errorf("package profile differs")
	}
	if digest(files["records/buf.lock"]) != lockHash || digest(files["records/source-manifest.json"]) != m.SourceManifestSHA256 || digest(files["records/build-recipe.sh"]) != m.RecipeSHA256 {
		return receipt{}, fmt.Errorf("build/source profile binding differs")
	}
	expected := map[string]bool{}
	for _, name := range []string{"buf.lock", "source-manifest.json", "build-recipe.sh", "api.version", "console.version"} {
		key := "records/" + name
		if _, ok := files[key]; !ok {
			return receipt{}, fmt.Errorf("missing build record")
		}
		expected[key] = true
	}
	for index, version := range []string{"1.67.0", "1.72.0"} {
		tool := m.Tools[index]
		phase := []string{"api", "console"}[index]
		rawHash, err := hex.DecodeString(tool.SHA256)
		if err != nil || len(rawHash) != 32 || strings.ToLower(tool.SHA256) != tool.SHA256 || tool.Phase != phase || tool.Version != version || string(files["records/"+phase+".version"]) != version+"\n" {
			return receipt{}, fmt.Errorf("native tool identity differs")
		}
	}
	for _, p := range profiles {
		module := map[string][]byte{}
		for name, body := range files {
			if !strings.HasPrefix(name, modulePrefix(p)) {
				continue
			}
			rel := strings.TrimPrefix(name, modulePrefix(p))
			module[rel], expected[name] = body, true
			if strings.HasPrefix(rel, "export/") {
				result.ExportFiles++
			} else if rel != "acquisition.json" {
				result.CacheFiles++
			}
		}
		if err := validateModule(p, module); err != nil {
			return receipt{}, err
		}
	}
	if len(expected) != len(files) || len(m.Files) != len(files) || len(files) > 512 {
		return receipt{}, fmt.Errorf("unselected or missing package member")
	}
	for index, name := range sortedNames(files) {
		body := files[name]
		result.Bytes += int64(len(body))
		if !inputName(name) || int64(len(body)) > fileLimit || m.Files[index] != (fileRecord{name, int64(len(body)), digest(body)}) {
			return receipt{}, fmt.Errorf("file inventory differs")
		}
	}
	if result.Bytes > packageLimit {
		return receipt{}, fmt.Errorf("package exceeds byte limit")
	}
	return result, nil
}

// verify checks the canonical closed tree without executing any retained input.
func verify(directory, seal string, profiles []moduleProfile, lockHash string) (result receipt, resultErr error) {
	return inspectPackage(directory, seal, profiles, lockHash, nil)
}

// inspectPackage supplies the already verified immutable byte snapshot to a consumer.
func inspectPackage(directory, seal string, profiles []moduleProfile, lockHash string, consume func(map[string][]byte) error) (result receipt, resultErr error) {
	sealBytes, err := hex.DecodeString(seal)
	if err != nil || len(sealBytes) != 32 || strings.ToLower(seal) != seal {
		return result, fmt.Errorf("invalid external seal")
	}
	root, err := checkedRoot(directory)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	raw, err := readInput(root, "manifest.json", false, fileLimit)
	if err != nil {
		return result, err
	}
	if digest(raw) != seal {
		return result, fmt.Errorf("external manifest seal differs")
	}
	var m manifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return result, err
	}
	canonicalRaw, err := canonical(m)
	if err != nil || !bytes.Equal(raw, canonicalRaw) {
		return result, errors.Join(fmt.Errorf("manifest encoding is ambiguous or not canonical"), err)
	}
	files := map[string][]byte{}
	var total int64
	for _, record := range m.Files {
		if len(files) >= 512 || !inputName(record.Path) {
			return result, fmt.Errorf("invalid manifest member")
		}
		if _, exists := files[record.Path]; exists {
			return result, fmt.Errorf("duplicate manifest member")
		}
		if record.Bytes < 0 || record.Bytes > fileLimit || record.Bytes > packageLimit-total {
			return result, fmt.Errorf("declared package exceeds remaining byte budget")
		}
		body, err := readInput(root, record.Path, false, min(fileLimit, packageLimit-total))
		if err != nil {
			return result, err
		}
		files[record.Path] = body
		total += int64(len(body))
	}
	allowed := map[string]bool{"manifest.json": true}
	for name := range files {
		allowed[name] = true
	}
	if err := privateMembership(root, allowed); err != nil {
		return result, err
	}
	result, err = validatePackage(m, files, profiles, lockHash)
	if err != nil {
		return receipt{}, err
	}
	if consume != nil {
		if err := consume(files); err != nil {
			return receipt{}, err
		}
	}
	result.ManifestSHA256 = seal
	return result, nil
}

// privateMembership requires the exact complete tree and canonical full file modes.
func privateMembership(root *os.Root, allowed map[string]bool) error {
	directories := map[string]bool{".": true}
	for name := range allowed {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if !directories[name] || info.Mode() != os.ModeDir|0o700 {
				return fmt.Errorf("unselected or wrong-mode directory")
			}
			return nil
		}
		if !allowed[name] {
			return fmt.Errorf("unselected package file")
		}
		if info.Mode() != 0o600 {
			return fmt.Errorf("wrong-mode package file")
		}
		return nil
	})
}

// restore creates a private writable native cache from verified retained source bytes.
func restore(directory, seal, output string, profiles []moduleProfile, lockHash string) (result receipt, resultErr error) {
	result, err := inspectPackage(directory, seal, profiles, lockHash, func(files map[string][]byte) error {
		return restoreSnapshot(files, profiles, output)
	})
	if err != nil {
		return receipt{}, err
	}
	if _, err := verify(directory, seal, profiles, lockHash); err != nil {
		return receipt{}, err
	}
	return result, nil
}

// restoreSnapshot copies only memory bytes validated against the external seal.
func restoreSnapshot(files map[string][]byte, profiles []moduleProfile, output string) (resultErr error) {
	if err := os.Mkdir(output, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(output, 0o700); err != nil {
		return err
	}
	cache, err := checkedRoot(output)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cache.Close()) }()
	native := nativeCacheFiles(files, profiles)
	for _, name := range sortedNames(native) {
		if err := writePayload(cache, name, native[name]); err != nil {
			return err
		}
	}
	return checkNativeSnapshot(cache, native)
}

// nativeCacheFiles maps only verified native members, excluding exports/acquisition records.
func nativeCacheFiles(files map[string][]byte, profiles []moduleProfile) map[string][]byte {
	native := map[string][]byte{}
	for _, p := range profiles {
		for _, key := range sortedNames(files) {
			if !strings.HasPrefix(key, modulePrefix(p)) {
				continue
			}
			rel := strings.TrimPrefix(key, modulePrefix(p))
			var name string
			switch {
			case rel == "commit.json":
				name = "v3/commits/shake256/buf.build/" + p.Commit + ".json"
			case strings.HasPrefix(rel, "b5/") || strings.HasPrefix(rel, "shake256/"):
				format, rest, _ := strings.Cut(rel, "/")
				name = "v3/modules/" + format + "/buf.build/" + p.Owner + "/" + p.Module + "/" + p.Commit + "/" + rest
			default:
				continue
			}
			native[name] = files[key]
		}
	}
	return native
}

// checkNativeSnapshot compares complete destination membership, modes and every literal byte.
func checkNativeSnapshot(cache *os.Root, native map[string][]byte) error {
	expected := map[string]bool{}
	for name := range native {
		expected[name] = true
	}
	if err := privateMembership(cache, expected); err != nil {
		return err
	}
	for _, name := range sortedNames(native) {
		readback, err := readInput(cache, name, false, fileLimit)
		if err != nil || !bytes.Equal(readback, native[name]) {
			return errors.Join(fmt.Errorf("native cache differs from sealed snapshot"), err)
		}
	}
	return nil
}

// checkCache refuses a mutated restored cache before a native consumer may open it.
func checkCache(directory, seal, cachePath string, profiles []moduleProfile, lockHash string) (receipt, error) {
	return inspectPackage(directory, seal, profiles, lockHash, func(files map[string][]byte) (resultErr error) {
		cache, err := checkedRoot(cachePath)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, cache.Close()) }()
		return checkNativeSnapshot(cache, nativeCacheFiles(files, profiles))
	})
}

// writeReceipt never suppresses an output failure after a successful operation.
func writeReceipt(writer io.Writer, value receipt) error {
	return json.NewEncoder(writer).Encode(value)
}
