package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
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
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
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

type verifierBootstrapInputs struct {
	ToolSource, Cache, Composition, SourceSHA256, ToolSourceSHA256 string
}

type verifierBootstrapManifest struct {
	SchemaVersion        int          `json:"schemaVersion"`
	Profile              string       `json:"profile"`
	SourceManifestSHA256 string       `json:"sourceManifestSha256"`
	ToolSourceSHA256     string       `json:"toolSourceSha256"`
	ToolGoModSHA256      string       `json:"toolGoModSha256"`
	ToolGoSumSHA256      string       `json:"toolGoSumSha256"`
	Dependency           identity     `json:"dependency"`
	Files                []fileRecord `json:"files"`
}

type verifierBootstrapReceipt struct {
	SchemaVersion             int    `json:"schemaVersion"`
	ManifestSHA256            string `json:"manifestSha256"`
	SourceManifestSHA256      string `json:"sourceManifestSha256"`
	ToolSourceSHA256          string `json:"toolSourceSha256"`
	Files                     int    `json:"files"`
	VerifierBootstrapCaptured bool   `json:"verifierBootstrapCaptured"`
	VerifierBootstrapRestored bool   `json:"verifierBootstrapRestored"`
	GoVerifiersRebuilt        bool   `json:"goVerifiersRebuilt"`
}

type verifierComposition struct {
	ImportPath, Name string
	GoFiles          []string
}

// The observed native package membership must equal these reviewed commands.
// Captured JSON never supplies a command or a source destination.
func verifierCompositions() []verifierComposition {
	return []verifierComposition{
		{"github.com/devantler-tech/business-site/server/cmd/zitadel-module-sources", "main", []string{"capture.go", "generator_sources.go", "generators.go", "graph_metadata.go", "json_fields.go", "main.go", "payload.go", "restore.go", "selection.go"}},
		{"github.com/devantler-tech/business-site/server/cmd/zitadel-bsr-sources", "main", []string{"files.go", "main.go", "package.go", "validation.go"}},
	}
}

func verifyVerifierComposition(inputs verifierBootstrapInputs, body []byte) (resultErr error) {
	var observed []verifierComposition
	if err := decodeStrict(body, &observed); err != nil {
		return err
	}
	wanted := verifierCompositions()
	if !reflect.DeepEqual(observed, wanted) {
		return fmt.Errorf("original native verifier compositions differ")
	}
	root, err := openRoot(inputs.ToolSource)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	for _, command := range wanted {
		for _, file := range command.GoFiles {
			name := "server/cmd/" + path.Base(command.ImportPath) + "/" + file
			body, err := readAt(root, name, inputLimit)
			if err != nil || len(body) == 0 {
				return fmt.Errorf("complete verifier command source required")
			}
			info, err := root.Lstat(name)
			if err != nil || info.Mode() != 0644 {
				return fmt.Errorf("inert original verifier command source required")
			}
		}
	}
	return nil
}

// runVerifierBootstrap exposes only the fixed source/dependency profile. It
// never executes tools, chooses a checksum authority or enables distribution.
func runVerifierBootstrap(mode string, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputs verifierBootstrapInputs
	flags.StringVar(&inputs.ToolSource, "tool-source", "", "separately verified restored tool layout")
	flags.StringVar(&inputs.SourceSHA256, "source-sha256", "", "external prepared-source seal")
	flags.StringVar(&inputs.ToolSourceSHA256, "tool-source-sha256", "", "external complete tool-source member seal")
	flags.StringVar(&inputs.Cache, "cache", "", "original private verifier module cache")
	flags.StringVar(&inputs.Composition, "composition", "", "original successful native Go package observation")
	directory := flags.String("package", "", "sealed bootstrap capsule")
	seal := flags.String("sha256", "", "external bootstrap capsule seal")
	output := flags.String("output", "", "fresh private output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || inputs.ToolSource == "" || inputs.SourceSHA256 == "" || inputs.ToolSourceSHA256 == "" {
		return fmt.Errorf("complete explicit verifier bootstrap operation required")
	}
	var result verifierBootstrapReceipt
	var err error
	switch {
	case mode == "capture-verifier-bootstrap" && inputs.Cache != "" && inputs.Composition != "" && *output != "" && *directory == "" && *seal == "":
		result, err = captureVerifierBootstrap(inputs, *output, frozenVerifierDependency())
	case mode == "verify-verifier-bootstrap" && inputs.Cache == "" && inputs.Composition == "" && *output == "" && *directory != "" && *seal != "":
		result, err = verifyVerifierBootstrap(inputs, *directory, *seal, frozenVerifierDependency())
	case mode == "restore-verifier-bootstrap" && inputs.Cache == "" && inputs.Composition == "" && *output != "" && *directory != "" && *seal != "":
		result, err = restoreVerifierBootstrap(inputs, *directory, *seal, *output, frozenVerifierDependency())
	default:
		return fmt.Errorf("one complete fixed verifier bootstrap operation required")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// frozenVerifierDependency is separate from the service/generator selections.
// Neither the CLI nor captured metadata can select its version or checksums.
func frozenVerifierDependency() identity {
	return identity{Path: "golang.org/x/mod", Version: "v0.42.0", Sum: "h1:ICeL3t2L30ljSPHHxDnR4fqVcBt/zR2zJAb9juavmQg=", GoModSum: "h1:u5fzIrRInkL8MaO1cbRkUHT64v303YIT5KEqpNr7jRs="}
}

// verifierBootstrapMetadata joins the exact dependency to the restored helper
// module, while retaining unrelated module declarations without acquiring them.
func verifierBootstrapMetadata(inputs verifierBootstrapInputs, dep identity) (modHash, sumHash string, resultErr error) {
	if !generatorDigest(inputs.SourceSHA256) || !generatorDigest(inputs.ToolSourceSHA256) || dep.Path != "golang.org/x/mod" || dep.Version != "v0.42.0" || !contentSum(dep.Sum) || !contentSum(dep.GoModSum) || dep.Main || dep.Replace != nil {
		return "", "", fmt.Errorf("fixed verifier bootstrap identity required")
	}
	root, err := openRoot(inputs.ToolSource)
	if err != nil {
		return "", "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			modHash, sumHash = "", ""
		}
	}()
	mod, err := readAt(root, "server/go.mod", inputLimit)
	if err != nil {
		return "", "", err
	}
	sums, err := readAt(root, "server/go.sum", inputLimit)
	if err != nil {
		return "", "", err
	}
	m, err := modfile.Parse("go.mod", mod, nil)
	if err != nil || m.Module == nil || m.Module.Mod.Path != "github.com/devantler-tech/business-site/server" || m.Go == nil || m.Go.Version != "1.27.3" || len(m.Replace) != 0 || len(m.Exclude) != 0 {
		return "", "", fmt.Errorf("verifier module or compiler declaration differs")
	}
	found := 0
	for _, r := range m.Require {
		if r.Mod.Path == dep.Path {
			if r.Mod.Version != dep.Version {
				return "", "", fmt.Errorf("verifier dependency version differs")
			}
			found++
		}
	}
	if found != 1 {
		return "", "", fmt.Errorf("one fixed verifier dependency required")
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || values[fields[0]+" "+fields[1]] != "" {
			return "", "", fmt.Errorf("incomplete or duplicate verifier checksum")
		}
		values[fields[0]+" "+fields[1]] = fields[2]
	}
	if values[dep.Path+" "+dep.Version] != dep.Sum || values[dep.Path+" "+dep.Version+"/go.mod"] != dep.GoModSum {
		return "", "", fmt.Errorf("original verifier checksums differ")
	}
	return hash(mod), hash(sums), nil
}

// verifierBootstrapReservation uses a new private root outside every input. Its
// caller removes only this exact reservation on failure, never a replacement.
func verifierBootstrapReservation(output string, inputs ...string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return "", nil, fmt.Errorf("absolute fresh bootstrap output required")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return "", nil, err
	}
	output = filepath.Join(parent, filepath.Base(output))
	for _, input := range inputs {
		if input == "" {
			continue
		}
		physical, err := filepath.EvalSymlinks(input)
		if err != nil {
			return "", nil, err
		}
		physical, err = filepath.Abs(physical)
		if err != nil {
			return "", nil, err
		}
		if output == physical || strings.HasPrefix(output, physical+string(filepath.Separator)) {
			return "", nil, fmt.Errorf("bootstrap output overlaps input")
		}
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(output)
	return output, info, err
}

// verifierBootstrapCleanup preserves a different root installed by another
// writer rather than treating an earlier pathname reservation as perpetual ownership.
func verifierBootstrapCleanup(output string, owned os.FileInfo, err error) error {
	if err == nil {
		return nil
	}
	current, statErr := os.Lstat(output)
	if statErr != nil || owned == nil || !os.SameFile(owned, current) {
		return errors.Join(err, fmt.Errorf("bootstrap reservation changed; replacement retained"), statErr)
	}
	return errors.Join(err, os.RemoveAll(output))
}

// captureVerifierBootstrap retains only original ZIP/mod/info, with no extracted
// tree, compiled cache, ziphash marker, or unrelated service dependency.
func captureVerifierBootstrap(inputs verifierBootstrapInputs, output string, dep identity) (result verifierBootstrapReceipt, resultErr error) {
	var owned os.FileInfo
	var roots []*os.Root
	defer func() {
		for _, root := range roots {
			resultErr = errors.Join(resultErr, root.Close())
		}
		if resultErr != nil {
			result = verifierBootstrapReceipt{}
			if owned != nil {
				resultErr = verifierBootstrapCleanup(output, owned, resultErr)
			}
		}
	}()
	modHash, sumHash, err := verifierBootstrapMetadata(inputs, dep)
	if err != nil {
		return result, err
	}
	observations, err := openRoot(filepath.Dir(inputs.Composition))
	if err != nil {
		return result, err
	}
	roots = append(roots, observations)
	composition, err := readAt(observations, filepath.Base(inputs.Composition), inputLimit)
	if err != nil {
		return result, err
	}
	if err := verifyVerifierComposition(inputs, composition); err != nil {
		return result, err
	}
	cache, err := openRoot(inputs.Cache)
	if err != nil {
		return result, err
	}
	roots = append(roots, cache)
	output, owned, err = verifierBootstrapReservation(output, inputs.ToolSource, inputs.Cache, inputs.Composition)
	if err != nil {
		return result, err
	}
	prefix, err := modulePrefix(dep)
	if err != nil {
		return result, err
	}
	m := verifierBootstrapManifest{SchemaVersion: 1, Profile: "selected-verifier-bootstrap-v1", SourceManifestSHA256: inputs.SourceSHA256, ToolSourceSHA256: inputs.ToolSourceSHA256, ToolGoModSHA256: modHash, ToolGoSumSHA256: sumHash, Dependency: dep}
	for _, suffix := range []string{".info", ".mod", ".zip"} {
		record, err := copyAt(cache, "cache/download/"+prefix+suffix, output, "modules/"+prefix+suffix, payloadLimit)
		if err != nil {
			return result, err
		}
		m.Files = append(m.Files, record)
	}
	if err := writeNew(output, "records/composition.json", composition); err != nil {
		return result, err
	}
	m.Files = append(m.Files, fileRecord{"records/composition.json", int64(len(composition)), hash(composition)})
	seal, err := writeSourceManifest(output, m)
	if err != nil {
		return result, err
	}
	result, err = verifyVerifierBootstrap(inputs, output, seal, dep)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	for _, record := range m.Files[:3] {
		body, err := readAt(cache, "cache/download/"+strings.TrimPrefix(record.Path, "modules/"), payloadLimit)
		if err != nil || int64(len(body)) != record.Bytes || hash(body) != record.SHA256 {
			return verifierBootstrapReceipt{}, fmt.Errorf("original verifier cache changed during capture")
		}
	}
	finalComposition, err := readAt(observations, filepath.Base(inputs.Composition), inputLimit)
	if err != nil || hash(finalComposition) != hash(composition) {
		return verifierBootstrapReceipt{}, fmt.Errorf("original compiler composition changed during capture")
	}
	return result, nil
}

// verifyVerifierBootstrap authenticates closed transport, original module
// checksums and complete original terms, independently of the manifest's claims.
func verifyVerifierBootstrap(inputs verifierBootstrapInputs, directory, seal string, dep identity) (result verifierBootstrapReceipt, resultErr error) {
	modHash, sumHash, err := verifierBootstrapMetadata(inputs, dep)
	if err != nil {
		return result, err
	}
	if !generatorDigest(seal) {
		return result, fmt.Errorf("external bootstrap seal required")
	}
	root, err := openRoot(directory)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = verifierBootstrapReceipt{}
		}
	}()
	meta, err := root.Lstat(".")
	if err != nil || meta.Mode() != os.ModeDir|0700 {
		return result, fmt.Errorf("private bootstrap root required")
	}
	var m verifierBootstrapManifest
	if err := loadGoRestoreManifest(root, seal, &m); err != nil {
		return result, err
	}
	canonical, err := sourceManifestBytes(m)
	if err != nil || hash(canonical) != seal || m.SchemaVersion != 1 || m.Profile != "selected-verifier-bootstrap-v1" || m.SourceManifestSHA256 != inputs.SourceSHA256 || m.ToolSourceSHA256 != inputs.ToolSourceSHA256 || m.ToolGoModSHA256 != modHash || m.ToolGoSumSHA256 != sumHash || !reflect.DeepEqual(m.Dependency, dep) || len(m.Files) != 4 {
		return result, fmt.Errorf("bootstrap source or profile differs")
	}
	prefix, err := modulePrefix(dep)
	if err != nil {
		return result, err
	}
	wanted := map[string]bool{"manifest.json": true}
	for i, suffix := range []string{".info", ".mod", ".zip"} {
		record := m.Files[i]
		if record.Path != "modules/"+prefix+suffix {
			return result, fmt.Errorf("bootstrap member selection differs")
		}
		if err := verifySourceRecord(root, record); err != nil {
			return result, err
		}
		wanted[record.Path] = true
	}
	if m.Files[3].Path != "records/composition.json" {
		return result, fmt.Errorf("original verifier composition missing")
	}
	if err := verifySourceRecord(root, m.Files[3]); err != nil {
		return result, err
	}
	wanted[m.Files[3].Path] = true
	composition, err := readAt(root, "records/composition.json", inputLimit)
	if err != nil {
		return result, err
	}
	if err := verifyVerifierComposition(inputs, composition); err != nil {
		return result, err
	}
	if err := verifyClosedSourceMembers(root, wanted); err != nil {
		return result, err
	}
	mod, err := readAt(root, "modules/"+prefix+".mod", inputLimit)
	if err != nil {
		return result, err
	}
	zipPath := filepath.Join(directory, "modules/"+prefix+".zip")
	if err := verifyPayload(dep.Path, dep.Version, zipPath, mod, dep.Sum, dep.GoModSum); err != nil {
		return result, err
	}
	info, err := readAt(root, "modules/"+prefix+".info", inputLimit)
	if err != nil {
		return result, err
	}
	var version struct{ Version string }
	if err := validateJSONFields(info, &version); err != nil {
		return result, err
	}
	if err := json.Unmarshal(info, &version); err != nil || version.Version != dep.Version {
		return result, fmt.Errorf("bootstrap info version differs")
	}
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return result, err
	}
	terms := map[string]bool{"LICENSE": false, "PATENTS": false}
	for _, f := range archive.File {
		name := strings.TrimPrefix(f.Name, dep.Path+"@"+dep.Version+"/")
		if _, ok := terms[name]; ok {
			terms[name] = f.UncompressedSize64 > 0
		}
	}
	if err := archive.Close(); err != nil {
		return result, err
	}
	for _, present := range terms {
		if !present {
			return result, fmt.Errorf("complete original bootstrap terms required")
		}
	}
	// Re-read after ZIP parsing so the result cannot rely on earlier membership.
	if err := verifyClosedSourceMembers(root, wanted); err != nil {
		return result, err
	}
	for _, record := range m.Files {
		if err := verifySourceRecord(root, record); err != nil {
			return result, err
		}
	}
	finalMod, finalSum, err := verifierBootstrapMetadata(inputs, dep)
	if err != nil || finalMod != modHash || finalSum != sumHash {
		return result, fmt.Errorf("verifier source metadata changed")
	}
	current, err := os.Lstat(directory)
	if err != nil || !os.SameFile(meta, current) || current.Mode() != os.ModeDir|0700 {
		return result, fmt.Errorf("bootstrap root changed")
	}
	return verifierBootstrapReceipt{SchemaVersion: 1, ManifestSHA256: seal, SourceManifestSHA256: inputs.SourceSHA256, ToolSourceSHA256: inputs.ToolSourceSHA256, Files: 4, VerifierBootstrapCaptured: true}, nil
}

// restoreVerifierBootstrap populates only the fixed native download-cache paths.
// Native Go still authenticates them against the unchanged original go.sum.
func restoreVerifierBootstrap(inputs verifierBootstrapInputs, directory, seal, output string, dep identity) (result verifierBootstrapReceipt, resultErr error) {
	return restoreVerifierBootstrapWithCopier(inputs, directory, seal, output, dep, copyAt)
}

// The copier seam exercises interrupted consumption without changing CLI policy.
func restoreVerifierBootstrapWithCopier(inputs verifierBootstrapInputs, directory, seal, output string, dep identity, copier func(*os.Root, string, string, string, int64) (fileRecord, error)) (result verifierBootstrapReceipt, resultErr error) {
	var owned os.FileInfo
	result, err := verifyVerifierBootstrap(inputs, directory, seal, dep)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	root, err := openRoot(directory)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			result = verifierBootstrapReceipt{}
			if owned != nil {
				resultErr = verifierBootstrapCleanup(output, owned, resultErr)
			}
		}
	}()
	var manifest verifierBootstrapManifest
	if err := loadGoRestoreManifest(root, seal, &manifest); err != nil {
		return verifierBootstrapReceipt{}, err
	}
	output, owned, err = verifierBootstrapReservation(output, directory, inputs.ToolSource)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	prefix, err := modulePrefix(dep)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	var records []fileRecord
	for i, suffix := range []string{".info", ".mod", ".zip"} {
		record, err := copier(root, "modules/"+prefix+suffix, output, "cache/download/"+prefix+suffix, payloadLimit)
		if err != nil {
			return verifierBootstrapReceipt{}, err
		}
		expected := manifest.Files[i]
		expected.Path = "cache/download/" + prefix + suffix
		if record != expected {
			return verifierBootstrapReceipt{}, fmt.Errorf("copied bootstrap input differs from its authenticated original")
		}
		records = append(records, record)
	}
	if _, err := verifyVerifierBootstrap(inputs, directory, seal, dep); err != nil {
		return verifierBootstrapReceipt{}, err
	}
	native, err := openRoot(output)
	if err != nil {
		return verifierBootstrapReceipt{}, err
	}
	wanted := map[string]bool{}
	for _, record := range records {
		wanted[record.Path] = true
		if err := verifySourceRecord(native, record); err != nil {
			return verifierBootstrapReceipt{}, errors.Join(err, native.Close())
		}
	}
	err = verifyClosedSourceMembers(native, wanted)
	meta, statErr := native.Lstat(".")
	if err = errors.Join(err, statErr, native.Close()); err != nil {
		return verifierBootstrapReceipt{}, err
	}
	current, err := os.Lstat(output)
	if err != nil || !os.SameFile(owned, current) || !os.SameFile(owned, meta) || current.Mode() != os.ModeDir|0700 {
		return verifierBootstrapReceipt{}, fmt.Errorf("native bootstrap output changed")
	}
	result.VerifierBootstrapRestored = true
	return result, nil
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
	result.ManifestSHA256, err = writeSourceManifest(output, m)
	if err != nil {
		return result, err
	}
	result.Modules, result.Files = len(m.Modules), len(m.Files)
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
	wanted["manifest.json"] = true
	if err := verifyClosedSourceMembers(root, wanted); err != nil {
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

// writeSourceManifest uses the same canonical sealed encoding for source profiles.
func writeSourceManifest(output string, value any) (string, error) {
	body, err := sourceManifestBytes(value)
	if err != nil {
		return "", err
	}
	if err := writeNew(output, "manifest.json", body); err != nil {
		return "", err
	}
	return hash(body), nil
}

// writeOrderedSourceManifest gives retained capsules one canonical file ordering.
func writeOrderedSourceManifest(output string, files []fileRecord, value any) (string, error) {
	slices.SortFunc(files, func(a, b fileRecord) int { return strings.Compare(a.Path, b.Path) })
	return writeSourceManifest(output, value)
}

// verifySourceRecord checks the complete confined file through the opened descriptor.
func verifySourceRecord(root *os.Root, record fileRecord) error {
	if record.Bytes < 0 || record.Bytes > payloadLimit || !generatorDigest(record.SHA256) {
		return fmt.Errorf("invalid retained file record")
	}
	f, err := openAt(root, record.Path)
	if err != nil {
		return err
	}
	size, sha, readErr := copyStream(io.Discard, f, record.Bytes)
	if err := errors.Join(readErr, f.Close()); err != nil {
		return err
	}
	if size != record.Bytes || sha != record.SHA256 {
		return fmt.Errorf("retained source file changed")
	}
	return nil
}

// sourceManifestBytes is shared by writing and canonical readback checks.
func sourceManifestBytes(value any) ([]byte, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	return append(body, '\n'), err
}

// verifyClosedSourceMembers enforces exact files and their implied directories.
// Profile-specific selection and full byte/hash validation stay with the caller.
func verifyClosedSourceMembers(root *os.Root, wanted map[string]bool) error {
	directories := map[string]bool{".": true}
	for name := range wanted {
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
			return fmt.Errorf("invalid retained source member plan")
		}
		// Shared parent chains need to be visited only once, even for a large
		// selected graph. The root is already present and terminates the walk.
		for dir := path.Dir(name); !directories[dir]; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	observed := 0
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !directories[name] {
				return fmt.Errorf("extra retained source directory")
			}
			return nil
		}
		if !wanted[name] || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return fmt.Errorf("extra, linked, special or wrong-mode retained source file")
		}
		observed++
		return nil
	})
	if err != nil {
		return err
	}
	if observed != len(wanted) {
		return fmt.Errorf("missing retained source member")
	}
	return nil
}
