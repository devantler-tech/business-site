package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestGeneratorCheckpointSealAndCompleteMembership(t *testing.T) {
	tools := generatorFixture()
	modules, err := selectGeneratorModules(tools)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := generatorCheckpoint{SchemaVersion: 1, SourceManifestSHA256: strings.Repeat("a", 64), Tools: tools, Modules: modules}
	body := marshal(t, checkpoint)
	if _, err := validateGeneratorCheckpoint(body, digest(body), checkpoint.SourceManifestSHA256); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"changed-seal", "changed-source", "missing-tool", "missing-module", "extra-module", "claimed-offline", "claimed-closure", "unknown-field", "duplicate-field", "incomplete-json"} {
		t.Run(name, func(t *testing.T) {
			changedCheckpoint := checkpoint
			candidate := bytes.Clone(body)
			expected, source := digest(body), checkpoint.SourceManifestSHA256
			switch name {
			case "changed-seal":
				expected = strings.Repeat("b", 64)
			case "changed-source":
				source = strings.Repeat("b", 64)
			case "missing-tool":
				changedCheckpoint.Tools = changedCheckpoint.Tools[1:]
				candidate = marshal(t, changedCheckpoint)
			case "missing-module":
				changedCheckpoint.Modules = changedCheckpoint.Modules[1:]
				candidate = marshal(t, changedCheckpoint)
			case "extra-module":
				changedCheckpoint.Modules = append(slices.Clone(changedCheckpoint.Modules), changedCheckpoint.Modules[0])
				candidate = marshal(t, changedCheckpoint)
			case "claimed-offline":
				changedCheckpoint.NativeOfflineReplayProved = true
				candidate = marshal(t, changedCheckpoint)
			case "claimed-closure":
				changedCheckpoint.DependencyInputsCaptured = true
				candidate = marshal(t, changedCheckpoint)
			case "unknown-field":
				candidate = append([]byte(`{"authority":"on",`), body[1:]...)
			case "duplicate-field":
				candidate = append([]byte(`{"schemaVersion":2,`), body[1:]...)
			case "incomplete-json":
				candidate = candidate[:len(candidate)-1]
			}
			if name != "changed-seal" {
				expected = digest(candidate)
			}
			if _, err := validateGeneratorCheckpoint(candidate, expected, source); err == nil {
				t.Fatal("invalid checkpoint admitted")
			}
		})
	}
	file := filepath.Join(t.TempDir(), "checkpoint.json")
	writeFixture(t, file, body)
	var output bytes.Buffer
	if err := run([]string{"verify-generator-checkpoint", "--checkpoint", file, "--sha256", digest(body), "--source-sha256", checkpoint.SourceManifestSHA256}, &output); err != nil {
		t.Fatal(err)
	}
	var receipt generatorCheckpointReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.Tools != 14 || receipt.Modules != len(modules) || receipt.NativeOfflineReplayProved || receipt.DependencyInputsCaptured {
		t.Fatalf("wrong receipt: %+v %v", receipt, err)
	}
}

func TestGeneratorCheckpointRequiresActualNativeExecutables(t *testing.T) {
	for _, kind := range []string{"absent", "text", "symlink", "wrong-native-compiler"} {
		t.Run(kind, func(t *testing.T) {
			build := t.TempDir()
			if kind != "absent" {
				name := filepath.Join(build, "prepared/source/.artifacts/bin/linux/amd64/gci")
				if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "text":
					writeFixture(t, name, []byte("plausible text is not native metadata"))
				case "symlink":
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(executable, name); err != nil {
						t.Fatal(err)
					}
				case "wrong-native-compiler":
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					body, err := os.ReadFile(executable)
					if err != nil {
						t.Fatal(err)
					}
					for _, tool := range generatorTools {
						writeFixture(t, filepath.Join(filepath.Dir(name), tool.Name), body)
					}
				}
			}
			root, err := openRoot(build)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			})
			observations, err := observeGeneratorExecutables(root)
			if err == nil || observations != nil {
				t.Fatal("unobserved generators produced a checkpoint")
			}
			if strings.Contains(err.Error(), "frozen generator source") {
				t.Fatal("binary control failed at the source-profile prerequisite")
			}
			if kind == "wrong-native-compiler" && !strings.Contains(err.Error(), "native generator identity differs") {
				t.Fatalf("did not reach parsed native identity: %v", err)
			}
		})
	}
}

func generatorFixture() []generatorObservation {
	var observations []generatorObservation
	for _, tool := range generatorTools {
		main := debug.Module{Path: tool.Module, Version: tool.Version, Sum: "h1:" + strings.Repeat("A", 43) + "="}
		if tool.Version == "(devel)" {
			main.Sum = ""
		}
		observations = append(observations, generatorObservation{
			Name: tool.Name, BinarySHA256: strings.Repeat("a", 64), Bytes: 100,
			Build: debug.BuildInfo{GoVersion: "go1.26.10", Path: tool.Command, Main: main,
				Deps:     []*debug.Module{{Path: "example.com/dependency", Version: "v1.2.3", Sum: "h1:" + strings.Repeat("B", 43) + "="}},
				Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"}}},
		})
	}
	return observations
}

func TestGeneratorSelectionRetainsAllNativeTools(t *testing.T) {
	observations := generatorFixture()
	// Two versions of one module really can occur across independent go installs.
	observations[0].Build.Deps[0] = &debug.Module{Path: "example.com/dependency", Version: "v1.3.0", Sum: "h1:" + strings.Repeat("C", 43) + "="}
	got, err := selectGeneratorModules(observations)
	if err != nil || len(got) != 13 {
		t.Fatalf("selected=%v err=%v", got, err)
	}
	if !slices.ContainsFunc(got, func(id identity) bool {
		return id.Path == "google.golang.org/grpc/cmd/protoc-gen-go-grpc" && id.Version == "v1.6.1"
	}) {
		t.Fatal("nested generator module lost")
	}
	if !slices.ContainsFunc(got, func(id identity) bool { return id.Path == "github.com/daixiang0/gci" }) {
		t.Fatal("native gci install omitted because upstream outputs list omits it")
	}
}

func TestGeneratorSelectionRejectsIncompleteOrChangedObservations(t *testing.T) {
	for _, name := range []string{"missing", "extra", "reordered", "wrong-command", "wrong-main", "wrong-version", "wrong-go", "wrong-os", "wrong-arch", "cgo", "duplicate-setting", "missing-setting", "empty-digest", "short-digest", "oversized-binary", "bad-sum", "missing-dependency-sum", "local-dependency", "nested-replacement", "conflicting-sum", "duplicate-dependency", "changed-local-main"} {
		t.Run(name, func(t *testing.T) {
			observations := generatorFixture()
			switch name {
			case "missing":
				observations = observations[1:]
			case "extra":
				observations = append(observations, observations[0])
			case "reordered":
				observations[0], observations[1] = observations[1], observations[0]
			case "wrong-command":
				observations[0].Build.Path += "/other"
			case "wrong-main":
				observations[0].Build.Main.Path += "/other"
			case "wrong-version":
				observations[0].Build.Main.Version = "v9.0.0"
			case "wrong-go":
				observations[0].Build.GoVersion = "go1.27.3"
			case "wrong-os":
				observations[0].Build.Settings[0].Value = "darwin"
			case "wrong-arch":
				observations[0].Build.Settings[1].Value = "arm64"
			case "cgo":
				observations[0].Build.Settings[2].Value = "1"
			case "duplicate-setting":
				observations[0].Build.Settings = append(observations[0].Build.Settings, observations[0].Build.Settings[0])
			case "missing-setting":
				observations[0].Build.Settings = observations[0].Build.Settings[1:]
			case "empty-digest":
				observations[0].BinarySHA256 = ""
			case "short-digest":
				observations[0].BinarySHA256 = "aa"
			case "oversized-binary":
				observations[0].Bytes = payloadLimit + 1
			case "bad-sum":
				observations[0].Build.Main.Sum = "h1:short"
			case "missing-dependency-sum":
				observations[0].Build.Deps[0].Sum = ""
			case "local-dependency":
				observations[0].Build.Deps[0].Replace = &debug.Module{Path: "../local"}
			case "nested-replacement":
				observations[0].Build.Deps[0].Replace = &debug.Module{Path: "example.com/other", Version: "v1.0.0", Sum: observations[0].Build.Deps[0].Sum, Replace: &debug.Module{Path: "elsewhere"}}
			case "conflicting-sum":
				observations[1].Build.Deps[0].Sum = observations[1].Build.Main.Sum
			case "duplicate-dependency":
				observations[0].Build.Deps = append(observations[0].Build.Deps, observations[0].Build.Deps[0])
			case "changed-local-main":
				observations[13].Build.Main.Version = "v4.20.0"
			}
			if _, err := selectGeneratorModules(observations); err == nil {
				t.Fatal("invalid generator observation was accepted")
			}
		})
	}
}

func TestGeneratorSelectionPreservesEffectiveReplacement(t *testing.T) {
	observations := generatorFixture()
	replacement := &debug.Module{Path: "example.com/replacement", Version: "v1.0.1", Sum: observations[0].Build.Deps[0].Sum}
	for i := range observations {
		observations[i].Build.Deps[0].Replace = replacement
	}
	got, err := selectGeneratorModules(observations)
	if err != nil || !slices.ContainsFunc(got, func(id identity) bool { return id.Path == replacement.Path && id.Version == replacement.Version }) {
		t.Fatalf("effective replacement lost: %v %v", got, err)
	}
	if slices.ContainsFunc(got, func(id identity) bool { return id.Path == "example.com/dependency" }) {
		t.Fatal("original replacement captured as effective source")
	}
	if observations[0].Build.Deps[0].Replace != replacement {
		t.Fatal("original build metadata changed")
	}
}

func TestGeneratorReplacementCannotHideInvalidOriginalIdentity(t *testing.T) {
	observations := generatorFixture()
	observations[0].Build.Deps[0] = &debug.Module{Path: "../outside", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/legitimate", Version: "v1.0.0", Sum: observations[0].Build.Main.Sum}}
	if _, err := selectGeneratorModules(observations); err == nil {
		t.Fatal("replacement hid invalid original module")
	}
}

func TestGeneratorFileReadsActualEmbeddedBuildMetadata(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observeGeneratorFile(file, "actual-test-command")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if observation.BinarySHA256 != digest(body) || observation.Bytes != int64(len(body)) || observation.Build.GoVersion == "" || observation.Build.Path == "" {
		t.Fatalf("incomplete actual metadata: %+v", observation)
	}
	if _, err := file.Stat(); err == nil {
		t.Fatal("native executable descriptor remained open")
	}
}
