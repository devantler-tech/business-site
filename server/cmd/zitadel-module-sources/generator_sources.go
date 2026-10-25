package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/sumdb"
)

// This is Go 1.26.10's official verifier, not a key supplied by a capsule.
const officialGeneratorSumDBKey = "sum.golang.org+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"

func runGeneratorSources(operation string, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	build := flags.String("build", "", "completed owned native build")
	checkpoint := flags.String("checkpoint-sha256", "", "external generator checkpoint seal")
	source := flags.String("source-sha256", "", "external prepared-source seal")
	output := flags.String("output", "", "fresh selected source capsule")
	pack := flags.String("package", "", "retained selected source capsule")
	seal := flags.String("sha256", "", "external capsule seal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected generator-source arguments")
	}
	var result generatorSourceReceipt
	var err error
	switch {
	case operation == "capture-generator-sources" && *build != "" && *checkpoint != "" && *source != "" && *output != "" && *pack == "" && *seal == "":
		result, err = captureGeneratorSources(*build, *checkpoint, *source, *output, officialGeneratorSumDBKey)
	case operation == "verify-generator-sources" && *build == "" && *checkpoint == "" && *source != "" && *output == "" && *pack != "" && *seal != "":
		result, err = verifyGeneratorSources(*pack, *seal, *source, officialGeneratorSumDBKey)
	default:
		return fmt.Errorf("one complete explicit generator-source operation required")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// generatorProofOps supplies only already acquired, build-owned cache evidence.
// The maintained client validates signatures, inclusion and consistency. It can
// update its latest tree in memory; there is no network or persistent write path.
type generatorProofOps struct {
	key    string
	read   func(string, int64) ([]byte, error)
	mu     sync.Mutex
	latest []byte
	used   map[string][]byte
	bytes  int64
}

func (*generatorProofOps) ReadRemote(string) ([]byte, error) {
	return nil, fmt.Errorf("checksum networking is denied")
}
func (o *generatorProofOps) ReadConfig(name string) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch name {
	case "key":
		return []byte(o.key), nil
	case "sum.golang.org/latest":
		return bytes.Clone(o.latest), nil
	default:
		return nil, fmt.Errorf("unexpected checksum configuration")
	}
}
func (o *generatorProofOps) WriteConfig(name string, old, next []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if name != "sum.golang.org/latest" || !bytes.Equal(old, o.latest) {
		return sumdb.ErrWriteConflict
	}
	o.latest = bytes.Clone(next)
	return nil
}
func (o *generatorProofOps) ReadCache(name string) ([]byte, error) {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") || len(name) > 2048 || (!strings.HasPrefix(name, "sum.golang.org/lookup/") && !strings.HasPrefix(name, "sum.golang.org/tile/")) {
		return nil, fmt.Errorf("unexpected checksum proof path")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	limit := inputLimit - o.bytes
	if previous, ok := o.used[name]; ok {
		limit = int64(len(previous))
	} else if len(o.used) >= 8192 || limit <= 0 {
		return nil, fmt.Errorf("checksum proof set exceeds limits")
	}
	// Serialize and bound the actual descriptor read, before it can allocate.
	body, err := o.read(name, limit)
	if err != nil || len(body) == 0 || int64(len(body)) > limit {
		return nil, fmt.Errorf("missing or invalid checksum proof")
	}
	if previous, ok := o.used[name]; ok {
		if !bytes.Equal(previous, body) {
			return nil, fmt.Errorf("checksum proof changed")
		}
	} else {
		if len(o.used) >= 8192 || o.bytes+int64(len(body)) > inputLimit {
			return nil, fmt.Errorf("checksum proof set exceeds limits")
		}
		o.used[name] = bytes.Clone(body)
		o.bytes += int64(len(body))
	}
	return bytes.Clone(body), nil
}
func (*generatorProofOps) WriteCache(string, []byte) {}
func (*generatorProofOps) Log(string)                {}
func (*generatorProofOps) SecurityError(string)      {} // Client.Lookup returns ErrSecurity; never log inspected bytes.

func authenticateGeneratorSources(selected []identity, latest []byte, key string, read func(string, int64) ([]byte, error)) ([]identity, map[string][]byte, error) {
	if len(latest) == 0 || int64(len(latest)) > inputLimit {
		return nil, nil, fmt.Errorf("missing signed checksum tree")
	}
	ops := &generatorProofOps{key: key, latest: bytes.Clone(latest), read: read, used: map[string][]byte{}}
	client := sumdb.NewClient(ops)
	result := slices.Clone(selected)
	for i, id := range selected {
		if !validGeneratorModule(id) || id.GoModSum != "" {
			return nil, nil, fmt.Errorf("invalid selected generator identity")
		}
		lines, err := client.Lookup(id.Path, id.Version)
		if err != nil || len(lines) != 1 || lines[0] != id.Path+" "+id.Version+" "+id.Sum {
			return nil, nil, fmt.Errorf("generator ZIP checksum is not authenticated")
		}
		lines, err = client.Lookup(id.Path, id.Version+"/go.mod")
		prefix := id.Path + " " + id.Version + "/go.mod "
		if err != nil || len(lines) != 1 || !strings.HasPrefix(lines[0], prefix) || !contentSum(strings.TrimPrefix(lines[0], prefix)) {
			return nil, nil, fmt.Errorf("generator go.mod checksum is not authenticated")
		}
		result[i].GoModSum = strings.TrimPrefix(lines[0], prefix)
	}
	return result, ops.used, nil
}

type generatorSourceManifest struct {
	SchemaVersion        int          `json:"schemaVersion"`
	Profile              string       `json:"profile"`
	SourceManifestSHA256 string       `json:"sourceManifestSha256"`
	CheckpointSHA256     string       `json:"checkpointSha256"`
	Modules              []identity   `json:"modules"`
	Files                []fileRecord `json:"files"`
}

type generatorSourceReceipt struct {
	SchemaVersion                      int    `json:"schemaVersion"`
	ManifestSHA256                     string `json:"manifestSha256"`
	SourceManifestSHA256               string `json:"sourceManifestSha256"`
	CheckpointSHA256                   string `json:"checkpointSha256"`
	Modules                            int    `json:"modules"`
	ProofFiles                         int    `json:"proofFiles"`
	Files                              int    `json:"files"`
	Bytes                              int64  `json:"bytes"`
	SelectedGeneratorGoSourcesCaptured bool   `json:"selectedGeneratorGoSourcesCaptured"`
	DependencyInputsCaptured           bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved          bool   `json:"nativeOfflineReplayProved"`
}

func selectedGeneratorCheckpoint(body []byte, seal, source string) ([]identity, error) {
	if _, err := validateGeneratorCheckpoint(body, seal, source); err != nil {
		return nil, err
	}
	var c generatorCheckpoint
	if err := decodeStrict(body, &c); err != nil {
		return nil, err
	}
	return c.Modules, nil
}

func physicalGeneratorDirectory(name string) error {
	actual, err := filepath.EvalSymlinks(name)
	if err != nil || !filepath.IsAbs(name) || filepath.Clean(name) != name || actual != name {
		return fmt.Errorf("physical absolute source directory required")
	}
	return nil
}

// Capture is a selected component, not the graph-loading closure or cold build.
// The private key argument is an internal test seam; the CLI always fixes the
// official key, and neither the manifest nor checkpoint can select one.
func captureGeneratorSources(build, checkpointSeal, source, output, key string) (result generatorSourceReceipt, resultErr error) {
	if err := physicalGeneratorDirectory(build); err != nil {
		return result, err
	}
	if err := physicalGeneratorDirectory(filepath.Dir(output)); err != nil {
		return result, err
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || build == output || strings.HasPrefix(build, output+string(filepath.Separator)) {
		return result, fmt.Errorf("distinct physical output required")
	}
	for _, name := range []string{"go-modules", "go-path", "prepared"} {
		if strings.HasPrefix(output, filepath.Join(build, name)+string(filepath.Separator)) || output == filepath.Join(build, name) {
			return result, fmt.Errorf("output overlaps acquisition inputs")
		}
	}
	root, err := openRoot(build)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = generatorSourceReceipt{}
		}
	}()
	checkpoint, err := readAt(root, "generator-checkpoint.json", inputLimit)
	if err != nil {
		return result, err
	}
	selected, err := selectedGeneratorCheckpoint(checkpoint, checkpointSeal, source)
	if err != nil {
		return result, err
	}
	latest, err := readAt(root, "go-path/pkg/sumdb/sum.golang.org/latest", inputLimit)
	if err != nil {
		return result, err
	}
	modules, proofs, err := authenticateGeneratorSources(selected, latest, key, func(name string, limit int64) ([]byte, error) {
		return readAt(root, "go-modules/cache/download/sumdb/"+name, limit)
	})
	if err != nil {
		return result, err
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return result, err
	}
	// A failed reservation remains private and unsealed; never recursively remove
	// a pathname that another process could have replaced.
	m := generatorSourceManifest{SchemaVersion: 1, Profile: "selected-generator-go-sources-v1", SourceManifestSHA256: source, CheckpointSHA256: checkpointSeal, Modules: modules}
	appendRecord := func(name string, body []byte) error {
		if err := writeNew(output, name, body); err != nil {
			return err
		}
		m.Files = append(m.Files, fileRecord{name, int64(len(body)), hash(body)})
		return nil
	}
	if err := appendRecord("records/checkpoint.json", checkpoint); err != nil {
		return result, err
	}
	if err := appendRecord("records/sumdb-latest", latest); err != nil {
		return result, err
	}
	for name, body := range proofs {
		if err := appendRecord("proofs/"+name, body); err != nil {
			return result, err
		}
	}
	var total int64
	for _, record := range m.Files {
		total += record.Bytes
	}
	for _, id := range modules {
		prefix, err := modulePrefix(id)
		if err != nil {
			return result, err
		}
		for _, extension := range []string{".zip", ".mod", ".info"} {
			record, err := copyAt(root, "go-modules/cache/download/"+prefix+extension, output, "modules/"+prefix+extension, totalLimit-total)
			if err != nil {
				return result, err
			}
			m.Files = append(m.Files, record)
			total += record.Bytes
		}
		if err := verifyGeneratorPayload(output, id, prefix); err != nil {
			return result, err
		}
	}
	slices.SortFunc(m.Files, func(a, b fileRecord) int { return strings.Compare(a.Path, b.Path) })
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return result, err
	}
	body = append(body, '\n')
	if err := writeNew(output, "manifest.json", body); err != nil {
		return result, err
	}
	return verifyGeneratorSources(output, hash(body), source, key)
}

func verifyGeneratorPayload(directory string, id identity, prefix string) (resultErr error) {
	root, err := openRoot(directory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	mod, err := readAt(root, "modules/"+prefix+".mod", inputLimit)
	if err != nil {
		return err
	}
	if err := verifyPayload(id.Path, id.Version, filepath.Join(directory, "modules", prefix+".zip"), mod, id.Sum, id.GoModSum); err != nil {
		return err
	}
	info, err := readAt(root, "modules/"+prefix+".info", inputLimit)
	if err != nil {
		return err
	}
	if err := uniqueJSON(info); err != nil {
		return err
	}
	// .info's version is an equality check only. Its timestamp/Origin are retained
	// literal data, not sumdb-authenticated facts, URLs or execution authority.
	var value map[string]json.RawMessage
	if err := json.Unmarshal(info, &value); err != nil {
		return err
	}
	for name := range value {
		if name != "Version" && name != "Time" && name != "Origin" {
			return fmt.Errorf("unsupported generator info field")
		}
	}
	var version string
	if err := json.Unmarshal(value["Version"], &version); err != nil || version != id.Version {
		return fmt.Errorf("generator info version differs")
	}
	return nil
}

func verifyGeneratorSources(directory, seal, source, key string) (result generatorSourceReceipt, resultErr error) {
	if !generatorDigest(seal) || !generatorDigest(source) {
		return result, fmt.Errorf("complete external generator-source seals required")
	}
	if err := physicalGeneratorDirectory(directory); err != nil {
		return result, err
	}
	root, err := openRoot(directory)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = generatorSourceReceipt{}
		}
	}()
	body, err := readAt(root, "manifest.json", inputLimit)
	if err != nil || hash(body) != seal {
		return result, fmt.Errorf("generator-source manifest seal differs")
	}
	var m generatorSourceManifest
	if err := decodeStrict(body, &m); err != nil {
		return result, err
	}
	canonical, err := json.MarshalIndent(m, "", "  ")
	if err != nil || !bytes.Equal(body, append(canonical, '\n')) {
		return result, fmt.Errorf("generator-source manifest is not canonical")
	}
	if m.SchemaVersion != 1 || m.Profile != "selected-generator-go-sources-v1" || m.SourceManifestSHA256 != source || !generatorDigest(m.CheckpointSHA256) || len(m.Files) < 5 || len(m.Files) > 20000 {
		return result, fmt.Errorf("generator-source profile differs")
	}
	checkpoint, err := readAt(root, "records/checkpoint.json", inputLimit)
	if err != nil {
		return result, err
	}
	selected, err := selectedGeneratorCheckpoint(checkpoint, m.CheckpointSHA256, source)
	if err != nil {
		return result, err
	}
	latest, err := readAt(root, "records/sumdb-latest", inputLimit)
	if err != nil {
		return result, err
	}
	modules, proofs, err := authenticateGeneratorSources(selected, latest, key, func(name string, limit int64) ([]byte, error) { return readAt(root, "proofs/"+name, limit) })
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(modules, m.Modules) {
		return result, fmt.Errorf("authenticated generator membership differs")
	}
	wanted := map[string]bool{"records/checkpoint.json": true, "records/sumdb-latest": true}
	for name := range proofs {
		wanted["proofs/"+name] = true
	}
	for _, id := range modules {
		prefix, err := modulePrefix(id)
		if err != nil {
			return result, err
		}
		for _, extension := range []string{".zip", ".mod", ".info"} {
			wanted["modules/"+prefix+extension] = true
		}
	}
	if len(m.Files) != len(wanted) {
		return result, fmt.Errorf("generator-source file membership differs")
	}
	previous := ""
	var total int64
	for _, record := range m.Files {
		if !wanted[record.Path] || record.Path <= previous || record.Bytes < 0 || record.Bytes > payloadLimit || !generatorDigest(record.SHA256) {
			return result, fmt.Errorf("invalid generator-source file table")
		}
		previous = record.Path
		total += record.Bytes
		if total > totalLimit {
			return result, fmt.Errorf("generator-source package exceeds limit")
		}
		f, err := openAt(root, record.Path)
		if err != nil {
			return result, err
		}
		size, sha, readErr := copyStream(io.Discard, f, record.Bytes)
		if err := errors.Join(readErr, f.Close()); err != nil {
			return result, err
		}
		if size != record.Bytes || sha != record.SHA256 {
			return result, fmt.Errorf("generator-source file changed")
		}
	}
	wanted["manifest.json"] = true
	directories := map[string]bool{".": true}
	for name := range wanted {
		for d := path.Dir(name); d != "."; d = path.Dir(d) {
			directories[d] = true
		}
	}
	if err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !directories[name] {
				return fmt.Errorf("extra generator-source directory")
			}
			return nil
		}
		if !wanted[name] || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return fmt.Errorf("extra, linked or unsafe generator-source member")
		}
		return nil
	}); err != nil {
		return result, err
	}
	for _, id := range modules {
		prefix, err := modulePrefix(id)
		if err != nil {
			return result, err
		}
		if err := verifyGeneratorPayload(directory, id, prefix); err != nil {
			return result, err
		}
	}
	return generatorSourceReceipt{SchemaVersion: 1, ManifestSHA256: seal, SourceManifestSHA256: source, CheckpointSHA256: m.CheckpointSHA256, Modules: len(modules), ProofFiles: len(proofs), Files: len(m.Files), Bytes: total, SelectedGeneratorGoSourcesCaptured: true}, nil
}
