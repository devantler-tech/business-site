package main

import (
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"

	"golang.org/x/mod/module"
)

type generatorTool struct{ Name, Command, Module, Version string }

// These are the fourteen native generate-install commands at the frozen pin,
// including gci (missing from upstream's declared outputs) and the nested gRPC module.
var generatorTools = []generatorTool{
	{"gci", "github.com/daixiang0/gci", "github.com/daixiang0/gci", "v0.14.0"},
	{"enumer", "github.com/dmarkham/enumer", "github.com/dmarkham/enumer", "v1.6.3"},
	{"mockgen", "go.uber.org/mock/mockgen", "go.uber.org/mock", "v0.6.0"},
	{"stringer", "golang.org/x/tools/cmd/stringer", "golang.org/x/tools", "v0.43.0"},
	{"statik", "github.com/rakyll/statik", "github.com/rakyll/statik", "v0.1.8"},
	{"buf", "github.com/bufbuild/buf/cmd/buf", "github.com/bufbuild/buf", "v1.67.0"},
	{"protoc-gen-go", "google.golang.org/protobuf/cmd/protoc-gen-go", "google.golang.org/protobuf", "v1.36.11"},
	{"protoc-gen-go-grpc", "google.golang.org/grpc/cmd/protoc-gen-go-grpc", "google.golang.org/grpc/cmd/protoc-gen-go-grpc", "v1.6.1"},
	{"protoc-gen-grpc-gateway", "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway", "github.com/grpc-ecosystem/grpc-gateway/v2", "v2.28.0"},
	{"protoc-gen-openapiv2", "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2", "github.com/grpc-ecosystem/grpc-gateway/v2", "v2.28.0"},
	{"protoc-gen-validate", "github.com/envoyproxy/protoc-gen-validate", "github.com/envoyproxy/protoc-gen-validate", "v1.3.3"},
	{"protoc-gen-connect-go", "connectrpc.com/connect/cmd/protoc-gen-connect-go", "connectrpc.com/connect", "v1.19.1"},
	{"protoc-gen-authoption", "github.com/zitadel/zitadel/internal/protoc/protoc-gen-authoption", "github.com/zitadel/zitadel", "(devel)"},
	{"protoc-gen-zitadel", "github.com/zitadel/zitadel/internal/protoc/protoc-gen-zitadel", "github.com/zitadel/zitadel", "(devel)"},
}

type generatorObservation struct {
	Name         string          `json:"name"`
	BinarySHA256 string          `json:"binarySha256"`
	Bytes        int64           `json:"bytes"`
	Build        debug.BuildInfo `json:"build"`
}

type generatorCheckpoint struct {
	SchemaVersion             int                    `json:"schemaVersion"`
	SourceManifestSHA256      string                 `json:"sourceManifestSha256"`
	Tools                     []generatorObservation `json:"tools"`
	Modules                   []identity             `json:"modules"`
	DependencyInputsCaptured  bool                   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved bool                   `json:"nativeOfflineReplayProved"`
}

type generatorCheckpointReceipt struct {
	SchemaVersion                   int    `json:"schemaVersion"`
	SourceManifestSHA256            string `json:"sourceManifestSha256"`
	CheckpointSHA256                string `json:"checkpointSha256"`
	Tools                           int    `json:"tools"`
	Modules                         int    `json:"modules"`
	GeneratorBuildMetadataValidated bool   `json:"generatorBuildMetadataValidated"`
	DependencyInputsCaptured        bool   `json:"dependencyInputsCaptured"`
	NativeOfflineReplayProved       bool   `json:"nativeOfflineReplayProved"`
}

// Validation proves a sealed metadata join. Only observation reads executables;
// neither mode is source capture, signature verification or a cold-build proof.
func validateGeneratorCheckpoint(body []byte, expected, source string) (generatorCheckpointReceipt, error) {
	if !generatorDigest(expected) || !generatorDigest(source) || hash(body) != expected {
		return generatorCheckpointReceipt{}, fmt.Errorf("generator checkpoint seal differs")
	}
	var checkpoint generatorCheckpoint
	if err := decodeStrict(body, &checkpoint); err != nil {
		return generatorCheckpointReceipt{}, err
	}
	if checkpoint.SchemaVersion != 1 || checkpoint.SourceManifestSHA256 != source || checkpoint.DependencyInputsCaptured || checkpoint.NativeOfflineReplayProved {
		return generatorCheckpointReceipt{}, fmt.Errorf("generator checkpoint profile differs")
	}
	modules, err := selectGeneratorModules(checkpoint.Tools)
	if err != nil {
		return generatorCheckpointReceipt{}, err
	}
	if !reflect.DeepEqual(modules, checkpoint.Modules) {
		return generatorCheckpointReceipt{}, fmt.Errorf("generator module union differs")
	}
	return generatorCheckpointReceipt{SchemaVersion: 1, SourceManifestSHA256: source, CheckpointSHA256: expected, Tools: len(checkpoint.Tools), Modules: len(modules), GeneratorBuildMetadataValidated: true}, nil
}

func runGeneratorVerification(args []string, stdout io.Writer) (resultErr error) {
	flags := flag.NewFlagSet("verify-generator-checkpoint", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("checkpoint", "", "sealed native checkpoint")
	seal := flags.String("sha256", "", "external checkpoint digest")
	source := flags.String("source-sha256", "", "external prepared-source digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*file) || filepath.Clean(*file) != *file {
		return fmt.Errorf("explicit checkpoint required")
	}
	root, err := openRoot(filepath.Dir(*file))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	body, err := readAt(root, filepath.Base(*file), inputLimit)
	if err != nil {
		return err
	}
	receipt, err := validateGeneratorCheckpoint(body, *seal, *source)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(receipt)
}

func runGeneratorObservation(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("observe-generators", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	build := flags.String("build", "", "explicit build root at the API generation checkpoint")
	seal := flags.String("source-sha256", "", "verified prepared-source manifest SHA-256")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*build) || filepath.Clean(*build) != *build || !generatorDigest(*seal) {
		return fmt.Errorf("one complete explicit native generator checkpoint is required")
	}
	observations, err := observeGenerators(*build)
	if err != nil {
		return err
	}
	modules, err := selectGeneratorModules(observations)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(generatorCheckpoint{SchemaVersion: 1, SourceManifestSHA256: *seal, Tools: observations, Modules: modules})
}

func generatorDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

// selectGeneratorModules retains each tool's complete original build information
// while selecting a path AND version union of effective linked modules. It does
// not claim to enumerate graph-loading-only metadata or complete offline inputs.
func selectGeneratorModules(observations []generatorObservation) ([]identity, error) {
	if len(observations) != len(generatorTools) {
		return nil, fmt.Errorf("native generator set differs")
	}
	selected := map[string]identity{}
	for i, observation := range observations {
		tool, b := generatorTools[i], observation.Build
		if !generatorDigest(observation.BinarySHA256) || observation.Bytes <= 0 || observation.Bytes > payloadLimit || observation.Name != tool.Name || b.GoVersion != "go1.26.10" || b.Path != tool.Command || b.Main.Path != tool.Module || b.Main.Version != tool.Version || b.Main.Replace != nil {
			return nil, fmt.Errorf("native generator identity differs")
		}
		settings := map[string]string{}
		for _, setting := range b.Settings {
			if _, exists := settings[setting.Key]; exists {
				return nil, fmt.Errorf("duplicate generator build setting")
			}
			settings[setting.Key] = setting.Value
		}
		if settings["GOOS"] != "linux" || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" {
			return nil, fmt.Errorf("generator platform differs")
		}
		modules := b.Deps
		if tool.Version != "(devel)" {
			modules = append(slices.Clone(modules), &b.Main)
		} else if b.Main.Sum != "" {
			return nil, fmt.Errorf("local generator main has an external checksum")
		}
		seen := map[string]bool{}
		for _, original := range modules {
			if original == nil || seen[original.Path] {
				return nil, fmt.Errorf("missing or duplicate generator dependency")
			}
			if module.Check(original.Path, original.Version) != nil || module.CanonicalVersion(original.Version) != original.Version {
				return nil, fmt.Errorf("invalid original generator module")
			}
			seen[original.Path] = true
			effective := original
			if original.Replace != nil {
				effective = original.Replace
			}
			id := identity{Path: effective.Path, Version: effective.Version, Sum: effective.Sum}
			if effective.Replace != nil || !validGeneratorModule(id) {
				return nil, fmt.Errorf("invalid effective generator module")
			}
			key := id.Path + "@" + id.Version
			if previous, exists := selected[key]; exists && previous != id {
				return nil, fmt.Errorf("conflicting generator checksum")
			}
			selected[key] = id
		}
	}
	result := make([]identity, 0, len(selected))
	for _, id := range selected {
		result = append(result, id)
	}
	slices.SortFunc(result, func(a, b identity) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return strings.Compare(a.Version, b.Version)
	})
	if len(result) == 0 || len(result) > 2048 {
		return nil, fmt.Errorf("unbounded generator module set")
	}
	return result, nil
}

func validGeneratorModule(id identity) bool {
	return module.Check(id.Path, id.Version) == nil && module.CanonicalVersion(id.Version) == id.Version && contentSum(id.Sum)
}

// observeGenerators reads real native executables through confined descriptors.
// Hashing before and after metadata parsing detects a changed executable; no
// inspected command is executed. The caller owns the otherwise idle build root.
func observeGenerators(build string) (observations []generatorObservation, resultErr error) {
	root, err := openRoot(build)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			observations = nil
		}
	}()
	// These literal inputs are independently fixed by the reviewed source profile.
	// A checkpoint cannot silently bind locally changed tool-install instructions.
	for name, expected := range map[string]string{
		"prepared/source/go.mod":                "d93f15da9568e8845430997cf6a1f5831decb2c3a526d71ffa8a0d869fe65b50",
		"prepared/source/go.sum":                frozenIdentityGoSum,
		"prepared/source/apps/api/project.json": "6f1f58f338a3e3fd6c65bcc42c5b2bc56b71a7affc758e14e096c122f28a5455",
	} {
		body, err := readAt(root, name, inputLimit)
		if err != nil {
			return nil, err
		}
		if hash(body) != expected {
			return nil, fmt.Errorf("frozen generator source input differs")
		}
	}
	return observeGeneratorExecutables(root)
}

// The source-profile gate and executable reader are separate so adverse binary
// controls exercise this boundary without first failing an unrelated source gate.
func observeGeneratorExecutables(root *os.Root) ([]generatorObservation, error) {
	var observations []generatorObservation
	for _, tool := range generatorTools {
		f, err := openAt(root, "prepared/source/.artifacts/bin/linux/amd64/"+tool.Name)
		if err != nil {
			return nil, err
		}
		observation, err := observeGeneratorFile(f, tool.Name)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	if _, err := selectGeneratorModules(observations); err != nil {
		return nil, err
	}
	return observations, nil
}

func observeGeneratorFile(f *os.File, name string) (result generatorObservation, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			result = generatorObservation{}
		}
	}()
	n, before, err := copyStream(io.Discard, f, payloadLimit)
	if err != nil {
		return result, err
	}
	info, err := buildinfo.Read(f)
	if err != nil {
		return result, err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return result, err
	}
	afterN, after, err := copyStream(io.Discard, f, payloadLimit)
	if err != nil || after != before || afterN != n {
		return result, errors.Join(fmt.Errorf("generator changed during metadata read"), err)
	}
	return generatorObservation{Name: name, BinarySHA256: before, Bytes: n, Build: *info}, nil
}
