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
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
)

const graphMetadataLimit int64 = 64 << 20
const frozenIdentityGoSum = "ecb0f1cc67843b22a16c3e7a5fee39a5ec930acd787051a8d697e119282b0646"

type graphMetadataModule struct {
	Path, Version, GoModSum string
	HasInfo                 bool
}

type graphMetadataManifest struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	Profile              string                `json:"profile"`
	SourceManifestSHA256 string                `json:"sourceManifestSha256"`
	Modules              []graphMetadataModule `json:"modules"`
	Files                []fileRecord          `json:"files"`
}

type graphMetadataReceipt struct {
	SchemaVersion             int    `json:"schemaVersion"`
	ManifestSHA256            string `json:"manifestSha256"`
	SourceManifestSHA256      string `json:"sourceManifestSha256"`
	Modules                   int    `json:"modules"`
	MetadataFiles             int    `json:"metadataFiles"`
	ProofFiles                int    `json:"proofFiles"`
	Files                     int    `json:"files"`
	Bytes                     int64  `json:"bytes"`
	GoGraphMetadataCaptured   bool   `json:"goGraphMetadataCaptured"`
	DependencyInputsCaptured  bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved bool   `json:"nativeOfflineReplayProved"`
}

// runGoGraphMetadata fixes checksum authority independently of inspected artifacts.
func runGoGraphMetadata(operation string, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	build := flags.String("build", "", "completed private native build")
	source := flags.String("source-sha256", "", "external prepared source seal")
	output := flags.String("output", "", "fresh metadata package")
	pack := flags.String("package", "", "retained metadata package")
	seal := flags.String("sha256", "", "external metadata seal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected graph metadata arguments")
	}
	var result graphMetadataReceipt
	var err error
	switch {
	case operation == "capture-graph-metadata" && *build != "" && *output != "" && *pack == "" && *seal == "":
		result, err = captureGoGraphMetadata(*build, *source, *output, officialGeneratorSumDBKey, frozenIdentityGoSum)
	case operation == "verify-graph-metadata" && *build == "" && *output == "" && *pack != "" && *seal != "":
		result, err = verifyGoGraphMetadata(*pack, *seal, *source, officialGeneratorSumDBKey, frozenIdentityGoSum)
	default:
		return fmt.Errorf("one complete graph metadata operation required")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// graphMetadataInventory observes only metadata in this build's native download cache.
func graphMetadataInventory(root *os.Root) ([]graphMetadataModule, error) {
	const prefix = "go-modules/cache/download"
	for _, name := range []string{"go-modules", "go-modules/cache", prefix} {
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("physical download cache required")
		}
	}
	entries := map[string]graphMetadataModule{}
	seen := 0
	err := fs.WalkDir(root.FS(), prefix, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		seen++
		if seen > 100000 || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unbounded or linked metadata cache")
		}
		if name == prefix+"/sumdb" && entry.IsDir() {
			return fs.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(name)
		if ext != ".mod" && ext != ".info" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular metadata")
		}
		rel := strings.TrimPrefix(name, prefix+"/")
		parts := strings.Split(rel, "/@v/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid metadata cache path")
		}
		p, pErr := module.UnescapePath(parts[0])
		v, vErr := module.UnescapeVersion(strings.TrimSuffix(parts[1], ext))
		if pErr != nil || vErr != nil || module.Check(p, v) != nil || module.CanonicalVersion(v) != v {
			return fmt.Errorf("invalid metadata identity")
		}
		canonical, err := modulePrefix(identity{Path: p, Version: v})
		if err != nil || rel != canonical+ext {
			return fmt.Errorf("metadata identity is not canonical")
		}
		id := entries[canonical]
		id.Path, id.Version = p, v
		if ext == ".info" {
			id.HasInfo = true
		} else {
			id.GoModSum = "observed"
		}
		entries[canonical] = id
		if len(entries) > 8192 {
			return fmt.Errorf("too many metadata identities")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("missing metadata inventory")
	}
	keys := make([]string, 0, len(entries))
	for name := range entries {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	result := make([]graphMetadataModule, 0, len(keys))
	for _, name := range keys {
		id := entries[name]
		if id.GoModSum == "" {
			return nil, fmt.Errorf("info lacks graph module input")
		}
		id.GoModSum = ""
		result = append(result, id)
	}
	return result, nil
}

// authenticateGraphMetadata authenticates complete go.mod content, never cache markers.
// The key and frozen checksum-file hash are private test seams, fixed by the CLI.
func authenticateGraphMetadata(ids []graphMetadataModule, sums, latest []byte, key, sumsHash string, readMetadata func(string, int64) ([]byte, error), readProof func(string, int64) ([]byte, error)) ([]graphMetadataModule, map[string][]byte, error) {
	if hash(sums) != sumsHash || len(ids) == 0 || len(ids) > 8192 {
		return nil, nil, fmt.Errorf("frozen graph input differs")
	}
	frozen := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !contentSum(fields[2]) {
			return nil, nil, fmt.Errorf("malformed frozen checksum input")
		}
		if strings.HasSuffix(fields[1], "/go.mod") {
			name := fields[0] + " " + fields[1]
			if frozen[name] != "" {
				return nil, nil, fmt.Errorf("duplicate frozen checksum")
			}
			frozen[name] = fields[2]
		}
	}
	if len(latest) == 0 || int64(len(latest)) > inputLimit {
		return nil, nil, fmt.Errorf("missing graph checksum tree")
	}
	ops := &generatorProofOps{key: key, latest: bytes.Clone(latest), read: readProof, used: map[string][]byte{}}
	client := sumdb.NewClient(ops)
	result := slices.Clone(ids)
	previous := ""
	var total int64
	for i, id := range ids {
		prefix, err := modulePrefix(identity{Path: id.Path, Version: id.Version})
		if err != nil || prefix <= previous || module.Check(id.Path, id.Version) != nil || module.CanonicalVersion(id.Version) != id.Version {
			return nil, nil, fmt.Errorf("invalid or unordered graph identity")
		}
		previous = prefix
		mod, err := readMetadata(prefix+".mod", min(1<<20, graphMetadataLimit-total))
		if err != nil {
			return nil, nil, err
		}
		total += int64(len(mod))
		actual, err := dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(mod)), nil })
		if err != nil {
			return nil, nil, err
		}
		name := id.Path + " " + id.Version + "/go.mod"
		want := frozen[name]
		if want == "" {
			lines, err := client.Lookup(id.Path, id.Version+"/go.mod")
			if err != nil || len(lines) != 1 || !strings.HasPrefix(lines[0], name+" ") {
				return nil, nil, fmt.Errorf("graph checksum is not authenticated")
			}
			want = strings.TrimPrefix(lines[0], name+" ")
		}
		if !contentSum(want) || actual != want || (id.GoModSum != "" && id.GoModSum != want) {
			return nil, nil, fmt.Errorf("graph go.mod content differs")
		}
		result[i].GoModSum = want
		if id.HasInfo {
			body, err := readMetadata(prefix+".info", min(64<<10, graphMetadataLimit-total))
			if err != nil {
				return nil, nil, err
			}
			total += int64(len(body))
			var info struct{ Version string }
			if decodeObservation(body, &info) != nil || info.Version != id.Version {
				return nil, nil, fmt.Errorf("literal graph info version differs")
			}
		}
	}
	return result, ops.used, nil
}

// captureGoGraphMetadata retains only observed resolver metadata in a new private capsule.
func captureGoGraphMetadata(build, source, output, key, sumsHash string) (graphMetadataReceipt, error) {
	return captureGoGraphMetadataWithWriter(build, source, output, key, sumsHash, writeNew)
}

// captureGoGraphMetadataWithWriter keeps the file-write failure seam internal to this package.
func captureGoGraphMetadataWithWriter(build, source, output, key, sumsHash string, write func(string, string, []byte) error) (result graphMetadataReceipt, resultErr error) {
	if !generatorDigest(source) || physicalGeneratorDirectory(build) != nil || physicalGeneratorDirectory(filepath.Dir(output)) != nil || !filepath.IsAbs(output) || filepath.Clean(output) != output || output == build || strings.HasPrefix(build, output+string(filepath.Separator)) || strings.HasPrefix(output, filepath.Join(build, "go-modules")+string(filepath.Separator)) || strings.HasPrefix(output, filepath.Join(build, "go-path")+string(filepath.Separator)) || strings.HasPrefix(output, filepath.Join(build, "prepared")+string(filepath.Separator)) {
		return result, fmt.Errorf("distinct owned metadata paths and source seal required")
	}
	root, err := openRoot(build)
	if err != nil {
		return result, err
	}
	reserved := false
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = graphMetadataReceipt{}
			if reserved {
				resultErr = errors.Join(resultErr, os.RemoveAll(output))
			}
		}
	}()
	ids, err := graphMetadataInventory(root)
	if err != nil {
		return result, err
	}
	sums, err := readAt(root, "prepared/source/go.sum", inputLimit)
	if err != nil {
		return result, err
	}
	latest, err := readAt(root, "go-path/pkg/sumdb/sum.golang.org/latest", inputLimit)
	if err != nil {
		return result, err
	}
	metadata := map[string][]byte{}
	ids, proofs, err := authenticateGraphMetadata(ids, sums, latest, key, sumsHash, func(name string, limit int64) ([]byte, error) {
		body, err := readAt(root, "go-modules/cache/download/"+name, limit)
		if err == nil {
			metadata[name] = body
		}
		return body, err
	}, func(name string, limit int64) ([]byte, error) {
		return readAt(root, "go-modules/cache/download/sumdb/"+name, limit)
	})
	if err != nil {
		return result, err
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return result, err
	}
	reserved = true
	m := graphMetadataManifest{SchemaVersion: 1, Profile: "identity-go-graph-metadata-v1", SourceManifestSHA256: source, Modules: ids}
	var total int64
	appendFile := func(name string, body []byte) error {
		if int64(len(body)) > graphMetadataLimit-total {
			return fmt.Errorf("graph metadata capsule exceeds limit")
		}
		if err := write(output, name, body); err != nil {
			return err
		}
		total += int64(len(body))
		m.Files = append(m.Files, fileRecord{name, int64(len(body)), hash(body)})
		return nil
	}
	if err := appendFile("records/go.sum", sums); err != nil {
		return result, err
	}
	if err := appendFile("records/sumdb-latest", latest); err != nil {
		return result, err
	}
	for name, body := range metadata {
		if err := appendFile("metadata/"+name, body); err != nil {
			return result, err
		}
	}
	for name, body := range proofs {
		if err := appendFile("proofs/"+name, body); err != nil {
			return result, err
		}
	}
	seal, err := writeOrderedSourceManifest(output, m.Files, m)
	if err != nil {
		return result, err
	}
	return verifyGoGraphMetadata(output, seal, source, key, sumsHash)
}

// verifyGoGraphMetadata repeats authentication and exact membership checks without networking.
func verifyGoGraphMetadata(directory, seal, source, key, sumsHash string) (result graphMetadataReceipt, resultErr error) {
	if !generatorDigest(seal) || !generatorDigest(source) || physicalGeneratorDirectory(directory) != nil {
		return result, fmt.Errorf("physical capsule and external seals required")
	}
	root, err := openRoot(directory)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = graphMetadataReceipt{}
		}
	}()
	body, err := readAt(root, "manifest.json", inputLimit)
	if err != nil {
		return result, err
	}
	if hash(body) != seal {
		return result, fmt.Errorf("graph manifest seal differs")
	}
	var m graphMetadataManifest
	if err := decodeStrict(body, &m); err != nil {
		return result, err
	}
	canonical, err := sourceManifestBytes(m)
	if err != nil || !bytes.Equal(canonical, body) || m.SchemaVersion != 1 || m.Profile != "identity-go-graph-metadata-v1" || m.SourceManifestSHA256 != source || len(m.Files) > 20000 {
		return result, fmt.Errorf("graph metadata profile differs")
	}
	sums, err := readAt(root, "records/go.sum", inputLimit)
	if err != nil {
		return result, err
	}
	latest, err := readAt(root, "records/sumdb-latest", inputLimit)
	if err != nil {
		return result, err
	}
	ids, proofs, err := authenticateGraphMetadata(m.Modules, sums, latest, key, sumsHash, func(name string, limit int64) ([]byte, error) { return readAt(root, "metadata/"+name, limit) }, func(name string, limit int64) ([]byte, error) { return readAt(root, "proofs/"+name, limit) })
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(ids, m.Modules) {
		return result, fmt.Errorf("authenticated graph identity differs")
	}
	wanted := map[string]bool{"records/go.sum": true, "records/sumdb-latest": true}
	metadataCount := 0
	for _, id := range ids {
		if !contentSum(id.GoModSum) {
			return result, fmt.Errorf("missing authenticated graph checksum")
		}
		prefix, err := modulePrefix(identity{Path: id.Path, Version: id.Version})
		if err != nil {
			return result, err
		}
		wanted["metadata/"+prefix+".mod"] = true
		metadataCount++
		if id.HasInfo {
			wanted["metadata/"+prefix+".info"] = true
			metadataCount++
		}
	}
	for name := range proofs {
		wanted["proofs/"+name] = true
	}
	if len(wanted) != len(m.Files) {
		return result, fmt.Errorf("graph metadata membership differs")
	}
	previous := ""
	var total int64
	for _, record := range m.Files {
		if !wanted[record.Path] || record.Path <= previous || record.Bytes < 0 || record.Bytes > inputLimit || !generatorDigest(record.SHA256) || record.Bytes > graphMetadataLimit-total {
			return result, fmt.Errorf("invalid graph metadata member")
		}
		previous = record.Path
		total += record.Bytes
		if err := verifySourceRecord(root, record); err != nil {
			return result, err
		}
	}
	wanted["manifest.json"] = true
	if err := verifyClosedSourceMembers(root, wanted); err != nil {
		return result, err
	}
	return graphMetadataReceipt{SchemaVersion: 1, ManifestSHA256: seal, SourceManifestSHA256: source, Modules: len(ids), MetadataFiles: metadataCount, ProofFiles: len(proofs), Files: len(m.Files), Bytes: total, GoGraphMetadataCaptured: true}, nil
}
