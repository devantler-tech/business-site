package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type goRestoreInputs struct {
	Service, ServiceSHA256, Generators, GeneratorsSHA256 string
	Metadata, MetadataSHA256, SourceSHA256               string
}
type goRestorePolicy struct{ catalogue, checksumKey, goSum string }
type goRestoreReceipt struct {
	SchemaVersion             int    `json:"schemaVersion"`
	ManifestSHA256            string `json:"manifestSha256"`
	SourceManifestSHA256      string `json:"sourceManifestSha256"`
	Modules                   int    `json:"modules"`
	ObservedModules           int    `json:"observedModules"`
	ModuleProxyScope          string `json:"moduleProxyScope"`
	Files                     int    `json:"files"`
	Bytes                     int64  `json:"bytes"`
	SelectedGoInputsRestored  bool   `json:"selectedGoInputsRestored"`
	DependencyInputsCaptured  bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved bool   `json:"nativeOfflineReplayProved"`
}
type goRestoreManifest struct {
	SchemaVersion        int               `json:"schemaVersion"`
	Profile              string            `json:"profile"`
	SourceManifestSHA256 string            `json:"sourceManifestSha256"`
	ServiceSHA256        string            `json:"serviceSha256"`
	GeneratorsSHA256     string            `json:"generatorsSha256"`
	MetadataSHA256       string            `json:"metadataSha256"`
	ModuleProxyScope     string            `json:"moduleProxyScope"`
	Modules              []goRestoreModule `json:"modules"`
	Files                []fileRecord      `json:"files"`
}

type goRestoreModule struct {
	Path, Version, Sum, GoModSum, Prefix string
}
type goRestoreFile struct {
	root   *os.Root
	source string
	record fileRecord
}

// runGoInputRestoration exposes no caller-selected trust root or dependency policy.
func runGoInputRestoration(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("restore-go-inputs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputs goRestoreInputs
	flags.StringVar(&inputs.Service, "service", "", "verified selected service source capsule")
	flags.StringVar(&inputs.ServiceSHA256, "service-sha256", "", "external service capsule seal")
	flags.StringVar(&inputs.Generators, "generators", "", "verified selected generator source capsule")
	flags.StringVar(&inputs.GeneratorsSHA256, "generators-sha256", "", "external generator capsule seal")
	flags.StringVar(&inputs.Metadata, "metadata", "", "verified observed graph metadata capsule")
	flags.StringVar(&inputs.MetadataSHA256, "metadata-sha256", "", "external graph metadata seal")
	flags.StringVar(&inputs.SourceSHA256, "source-sha256", "", "external prepared source seal")
	output := flags.String("output", "", "fresh native input directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || inputs.Service == "" || inputs.ServiceSHA256 == "" || inputs.Generators == "" || inputs.GeneratorsSHA256 == "" || inputs.Metadata == "" || inputs.MetadataSHA256 == "" || inputs.SourceSHA256 == "" || *output == "" {
		return fmt.Errorf("complete explicit Go source input restoration required")
	}
	result, err := restoreGoInputs(inputs, *output, goRestorePolicy{frozenCatalogue, officialGeneratorSumDBKey, frozenIdentityGoSum})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// verifyGoRestoreInputs authenticates the three independent capsules before any write.
func verifyGoRestoreInputs(inputs goRestoreInputs, policy goRestorePolicy) error {
	if _, err := verify(inputs.Service, inputs.ServiceSHA256, policy.catalogue); err != nil {
		return err
	}
	if _, err := verifyGeneratorSources(inputs.Generators, inputs.GeneratorsSHA256, inputs.SourceSHA256, policy.checksumKey); err != nil {
		return err
	}
	_, err := verifyGoGraphMetadata(inputs.Metadata, inputs.MetadataSHA256, inputs.SourceSHA256, policy.checksumKey, policy.goSum)
	return err
}

// loadGoRestoreManifest binds each typed manifest to its caller-supplied transport seal.
func loadGoRestoreManifest(root *os.Root, seal string, target any) error {
	body, err := readAt(root, "manifest.json", inputLimit)
	if err != nil {
		return err
	}
	if hash(body) != seal {
		return fmt.Errorf("go restoration input manifest changed")
	}
	return decodeStrict(body, target)
}

// goRestoreTarget maps only admitted inputs to the native Go cache's documented paths.
func goRestoreTarget(name string) string {
	for _, prefix := range []string{"modules/", "metadata/"} {
		if strings.HasPrefix(name, prefix) {
			return "go-modules/cache/download/" + strings.TrimPrefix(name, prefix)
		}
	}
	if strings.HasPrefix(name, "proofs/") {
		return "go-modules/cache/download/sumdb/" + strings.TrimPrefix(name, "proofs/")
	}
	if name == "records/sumdb-latest" {
		return "go-path/pkg/sumdb/sum.golang.org/latest"
	}
	return ""
}

// restoreGoInputs reconstructs explicit native inputs after complete capsule verification.
// Inputs and the new output's parent are exclusively owned throughout the operation.
func restoreGoInputs(inputs goRestoreInputs, output string, policy goRestorePolicy) (result goRestoreReceipt, resultErr error) {
	return restoreGoInputsWithCopier(inputs, output, policy, copyAt)
}

// restoreGoInputsWithCopier keeps failed-write lifetime controls separate from CLI authority.
func restoreGoInputsWithCopier(inputs goRestoreInputs, output string, policy goRestorePolicy, copier func(*os.Root, string, string, string, int64) (fileRecord, error)) (result goRestoreReceipt, resultErr error) {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return result, fmt.Errorf("absolute canonical output required")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return result, err
	}
	output = filepath.Join(parent, filepath.Base(output))
	for _, input := range []string{inputs.Service, inputs.Generators, inputs.Metadata} {
		if !filepath.IsAbs(input) || filepath.Clean(input) != input {
			return result, fmt.Errorf("absolute canonical input required")
		}
		physical, err := filepath.EvalSymlinks(input)
		if err != nil {
			return result, err
		}
		rel, err := filepath.Rel(physical, output)
		if err != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return result, fmt.Errorf("go restoration output overlaps an input capsule")
		}
	}
	if err := verifyGoRestoreInputs(inputs, policy); err != nil {
		return result, err
	}
	var roots []*os.Root
	reserved := false
	defer func() {
		for _, root := range roots {
			resultErr = errors.Join(resultErr, root.Close())
		}
		if resultErr != nil {
			result = goRestoreReceipt{}
			if reserved {
				resultErr = errors.Join(resultErr, os.RemoveAll(output))
			}
		}
	}()
	open := func(directory string) (*os.Root, error) {
		root, err := openRoot(directory)
		if err == nil {
			roots = append(roots, root)
		}
		return root, err
	}
	serviceRoot, err := open(inputs.Service)
	if err != nil {
		return result, err
	}
	generatorRoot, err := open(inputs.Generators)
	if err != nil {
		return result, err
	}
	metadataRoot, err := open(inputs.Metadata)
	if err != nil {
		return result, err
	}
	var service manifest
	var generators generatorSourceManifest
	var metadata graphMetadataManifest
	for _, item := range []struct {
		root   *os.Root
		seal   string
		target any
	}{
		{serviceRoot, inputs.ServiceSHA256, &service}, {generatorRoot, inputs.GeneratorsSHA256, &generators}, {metadataRoot, inputs.MetadataSHA256, &metadata},
	} {
		if err := loadGoRestoreManifest(item.root, item.seal, item.target); err != nil {
			return result, err
		}
	}
	selected := map[string]identity{}
	addIdentity := func(id identity) error {
		key := id.Path + "@" + id.Version
		if old, exists := selected[key]; exists && (old.Sum != id.Sum || old.GoModSum != id.GoModSum) {
			return fmt.Errorf("compiled Go input identities conflict")
		}
		selected[key] = id
		return nil
	}
	for _, record := range service.Modules {
		if err := addIdentity(record.Effective); err != nil {
			return result, err
		}
	}
	for _, id := range generators.Modules {
		if err := addIdentity(id); err != nil {
			return result, err
		}
	}
	observed := map[string]graphMetadataModule{}
	for _, id := range metadata.Modules {
		observed[id.Path+"@"+id.Version] = id
	}
	for key, id := range selected {
		graph, ok := observed[key]
		if !ok || !graph.HasInfo || graph.GoModSum != id.GoModSum {
			return result, fmt.Errorf("compiled Go input lacks matching native metadata")
		}
	}
	plan := map[string]goRestoreFile{}
	var total int64
	for _, component := range []struct {
		root  *os.Root
		files []fileRecord
	}{
		{serviceRoot, service.Files}, {generatorRoot, generators.Files}, {metadataRoot, metadata.Files},
	} {
		for _, record := range component.files {
			target := goRestoreTarget(record.Path)
			if target == "" {
				continue
			}
			targets := []string{target}
			if relative, ok := strings.CutPrefix(target, "go-modules/cache/download/"); ok && !strings.HasPrefix(relative, "sumdb/") {
				targets = append(targets, "go-proxy/"+relative)
			}
			for _, target := range targets {
				if old, exists := plan[target]; exists {
					if old.record.Bytes != record.Bytes || old.record.SHA256 != record.SHA256 {
						return result, fmt.Errorf("native Go input contents conflict")
					}
					continue
				}
				if record.Bytes < 0 || record.Bytes > totalLimit-total {
					return result, fmt.Errorf("native Go input size limit exceeded")
				}
				total += record.Bytes
				copyRecord := record
				copyRecord.Path = target
				plan[target] = goRestoreFile{component.root, record.Path, copyRecord}
			}
		}
	}
	// This proxy enumerates only the verified retained snapshot. Go itself orders
	// its versions and performs deprecation/retraction/checksum checks. No @latest
	// response, current upstream version or unauthenticated timestamp is invented.
	lists := map[string][]string{}
	for _, id := range metadata.Modules {
		if !id.HasInfo {
			continue
		}
		prefix, err := modulePrefix(identity{Path: id.Path, Version: id.Version})
		if err != nil {
			return result, err
		}
		for _, extension := range []string{".mod", ".info"} {
			if _, ok := plan["go-proxy/"+prefix+extension]; !ok {
				return result, fmt.Errorf("retained proxy metadata missing")
			}
		}
		name := "go-proxy/" + path.Dir(prefix) + "/list"
		lists[name] = append(lists[name], id.Version)
	}
	var modules []goRestoreModule
	for _, id := range selected {
		prefix, err := modulePrefix(id)
		if err != nil {
			return result, err
		}
		for _, ext := range []string{".zip", ".mod", ".info"} {
			if _, ok := plan["go-modules/cache/download/"+prefix+ext]; !ok {
				return result, fmt.Errorf("native Go input member missing")
			}
		}
		modules = append(modules, goRestoreModule{id.Path, id.Version, id.Sum, id.GoModSum, prefix})
	}
	slices.SortFunc(modules, func(a, b goRestoreModule) int { return strings.Compare(a.Path+"@"+a.Version, b.Path+"@"+b.Version) })
	if err := os.Mkdir(output, 0700); err != nil {
		return result, err
	}
	reserved = true
	names := make([]string, 0, len(plan))
	for name := range plan {
		names = append(names, name)
	}
	slices.Sort(names)
	restored := goRestoreManifest{SchemaVersion: 1, Profile: "identity-go-inputs-v1", SourceManifestSHA256: inputs.SourceSHA256,
		ServiceSHA256: inputs.ServiceSHA256, GeneratorsSHA256: inputs.GeneratorsSHA256, MetadataSHA256: inputs.MetadataSHA256,
		ModuleProxyScope: "retained-snapshot", Modules: modules}
	var copied int64
	for _, name := range names {
		file := plan[name]
		record, err := copier(file.root, file.source, output, name, totalLimit-copied)
		if err != nil {
			return result, err
		}
		if record != file.record {
			return result, fmt.Errorf("native Go input changed during restoration")
		}
		copied += record.Bytes
		restored.Files = append(restored.Files, record)
	}
	for name, versions := range lists {
		slices.Sort(versions)
		body := []byte(strings.Join(versions, "\n") + "\n")
		if int64(len(body)) > totalLimit-copied {
			return result, fmt.Errorf("retained proxy index exceeds size bound")
		}
		if err := writeNew(output, name, body); err != nil {
			return result, err
		}
		copied += int64(len(body))
		restored.Files = append(restored.Files, fileRecord{name, int64(len(body)), hash(body)})
	}
	seal, err := writeOrderedSourceManifest(output, restored.Files, restored)
	if err != nil {
		return result, err
	}
	outputRoot, err := open(output)
	if err != nil {
		return result, err
	}
	var readback goRestoreManifest
	if err := loadGoRestoreManifest(outputRoot, seal, &readback); err != nil {
		return result, err
	}
	wanted := map[string]bool{"manifest.json": true}
	for _, record := range readback.Files {
		if err := verifySourceRecord(outputRoot, record); err != nil {
			return result, err
		}
		wanted[record.Path] = true
	}
	if err := verifyClosedSourceMembers(outputRoot, wanted); err != nil {
		return result, err
	}
	return goRestoreReceipt{SchemaVersion: 1, ManifestSHA256: seal, SourceManifestSHA256: inputs.SourceSHA256, Modules: len(selected),
		ModuleProxyScope: "retained-snapshot",
		ObservedModules:  len(metadata.Modules), Files: len(restored.Files), Bytes: copied, SelectedGoInputsRestored: true}, nil
}
