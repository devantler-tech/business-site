package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Location is transport configuration, never part of a portable source receipt.
func TestGoRestorationReceiptIsIndependentOfScratchLocation(t *testing.T) {
	inputs, policy := restorationFixture(t)
	parent := physicalGeneratorTestDirectory(t)
	first, err := restoreGoInputs(inputs, filepath.Join(parent, "first"), policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := restoreGoInputs(inputs, filepath.Join(parent, "second"), policy)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical retained sources produced location-dependent receipts")
	}
}

// A pinned install still performs Go's native deprecation/latest metadata query.
// The finite file proxy must answer it without a network or checksum bypass.
func TestRestoredGoInputsSupportPinnedNativeInstall(t *testing.T) {
	inputs, policy := restorationFixtureWithGraphOnlyVersion(t, true)
	sources := restorationGenerators(t, inputs)
	selected := sources.Modules[0]
	prefix, err := modulePrefix(selected)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"complete", "proxy-off", "missing-list", "missing-lookup", "advertise-graph-only"} {
		t.Run(fault, func(t *testing.T) {
			output := filepath.Join(physicalGeneratorTestDirectory(t), "restored space,|%#")
			receipt, err := restoreGoInputs(inputs, output, policy)
			if err != nil {
				t.Fatal(err)
			}
			proxy := (&url.URL{Scheme: "file", Path: filepath.Join(output, "go-proxy")}).String()
			proxy = strings.NewReplacer(",", "%2C", "|", "%7C").Replace(proxy)
			parsed, err := url.Parse(proxy)
			if err != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Path != filepath.Join(output, "go-proxy") || strings.ContainsAny(proxy, ",|") || receipt.ModuleProxyScope != "retained-snapshot" {
				t.Fatal("proxy URI or snapshot boundary differs", err)
			}
			if fault == "proxy-off" {
				proxy = "off"
			}
			if fault == "missing-list" {
				if err := os.Remove(filepath.Join(output, "go-proxy", filepath.Dir(prefix), "list")); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "missing-lookup" {
				removeRestoredLookup(t, output, selected)
			}
			if fault == "complete" || fault == "advertise-graph-only" {
				listPath := filepath.Join(output, "go-proxy", filepath.Dir(prefix), "list")
				list, err := os.ReadFile(listPath)
				if err != nil || string(list) != selected.Version+"\n" {
					t.Fatal("proxy advertised a graph-only version", err, string(list))
				}
				graphPrefix, err := modulePrefix(identity{Path: selected.Path, Version: "v1.999.0"})
				if err != nil {
					t.Fatal(err)
				}
				for _, extension := range []string{".mod", ".info"} {
					if _, err := os.Stat(filepath.Join(output, "go-proxy", graphPrefix+extension)); err != nil {
						t.Fatal("exact graph metadata was lost", err)
					}
				}
				if fault == "advertise-graph-only" {
					if err := os.WriteFile(listPath, append(list, []byte("v1.999.0\n")...), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			denied, requests := deniedChecksumServer(t)
			bin := filepath.Join(t.TempDir(), "bin")
			cmd := exec.CommandContext(t.Context(), "go", "install", selected.Path+"@"+selected.Version)
			cmd.Dir = physicalGeneratorTestDirectory(t)
			cmd.Env = append(restoredNativeGoEnvironment(t, output, policy, denied.URL), "GOPROXY="+proxy, "GOBIN="+bin)
			body, err := cmd.CombinedOutput()
			if fault != "complete" {
				if err == nil {
					t.Fatal("incomplete native resolver input was accepted")
				}
				if fault == "proxy-off" && !bytes.Contains(body, []byte("loading deprecation")) {
					t.Fatalf("original deprecation failure was not reproduced: %s", body)
				}
				if fault == "missing-list" && !bytes.Contains(body, []byte("/@v/list")) {
					t.Fatalf("missing proxy index did not cause refusal: %s", body)
				}
				if fault == "missing-lookup" && requests.Load() == 0 {
					t.Fatalf("checksum refusal did not attempt its unavailable proof: %s", body)
				}
				if fault == "advertise-graph-only" && (!bytes.Contains(body, []byte("loading deprecation")) || !bytes.Contains(body, []byte("v1.999.0/go.mod")) || requests.Load() == 0) {
					t.Fatalf("graph-only version did not reproduce its missing native proof: %s", body)
				}
				return
			}
			if err != nil || requests.Load() != 0 {
				t.Fatalf("pinned native install required uncaptured resolver inputs: %v %s", err, body)
			}
			program := filepath.Join(bin, filepath.Base(selected.Path))
			if err := exec.CommandContext(t.Context(), program).Run(); err != nil {
				t.Fatal("native generator fixture did not run", err)
			}
		})
	}
}

func restorationGenerators(t *testing.T, inputs goRestoreInputs) generatorSourceManifest {
	t.Helper()
	root, err := openRoot(inputs.Generators)
	if err != nil {
		t.Fatal(err)
	}
	var sources generatorSourceManifest
	err = loadGoRestoreManifest(root, inputs.GeneratorsSHA256, &sources)
	if closeErr := root.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return sources
}

func removeRestoredLookup(t *testing.T, output string, selected identity) {
	t.Helper()
	prefix, err := modulePrefix(selected)
	if err != nil {
		t.Fatal(err)
	}
	lookup := "go-modules/cache/download/sumdb/sum.golang.org/lookup/" + strings.Replace(prefix, "/@v/", "@", 1)
	if err := os.Remove(filepath.Join(output, lookup)); err != nil {
		t.Fatal(err)
	}
}

func deniedChecksumServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	requests := new(atomic.Int64)
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "offline fixture", http.StatusForbidden)
	}))
	t.Cleanup(denied.Close)
	return denied, requests
}

func restoredNativeGoEnvironment(t *testing.T, output string, policy goRestorePolicy, endpoint string) []string {
	t.Helper()
	return append(os.Environ(), "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOROOT=", "GOFLAGS=-modcacherw", "GOPROXY=off",
		"GOVCS=*:off", "GONOSUMDB=", "GONOPROXY=", "GOPRIVATE=", "GOCACHE="+t.TempDir(),
		"GOPATH="+filepath.Join(output, "go-path"), "GOMODCACHE="+filepath.Join(output, "go-modules"), "GOSUMDB="+policy.checksumKey+" "+endpoint)
}

// restorationFixture joins real source capsules through one fictional native build.
func restorationFixture(t *testing.T) (goRestoreInputs, goRestorePolicy) {
	t.Helper()
	return restorationFixtureWithGraphOnlyVersion(t, false)
}

func restorationFixtureWithGraphOnlyVersion(t *testing.T, graphOnly bool) (goRestoreInputs, goRestorePolicy) {
	t.Helper()
	return restorationFixtureOptions(t, graphOnly, false)
}

func restorationFixtureOptions(t *testing.T, graphOnly, requireService bool) (goRestoreInputs, goRestorePolicy) {
	t.Helper()
	service, cataloguePath := materializeSelection(t)
	if requireService {
		service.mod = append(service.mod, []byte("\nrequire (\n example.com/Example v1.2.3\n example.com/original v1.0.0\n)\nreplace example.com/original v1.0.0 => gopkg.in/ini.v1 v1.2.3\n")...)
		var catalog catalogue
		if err := json.Unmarshal(service.cat, &catalog); err != nil {
			t.Fatal(err)
		}
		catalog.GoModSHA256 = hash(service.mod)
		service.cat = marshal(t, catalog)
		writeFixture(t, cataloguePath, service.cat)
	}
	build, checkpoint, source, key := generatorSourceFixture(t)
	if err := os.CopyFS(filepath.Join(build, "go-modules"), os.DirFS(filepath.Join(service.build, "go-modules"))); err != nil {
		t.Fatal(err)
	}
	if graphOnly {
		body, err := os.ReadFile(filepath.Join(build, "generator-checkpoint.json"))
		if err != nil {
			t.Fatal(err)
		}
		var observed generatorCheckpoint
		if err := json.Unmarshal(body, &observed); err != nil || len(observed.Modules) == 0 {
			t.Fatal("missing fixture generator identity", err)
		}
		id := identity{Path: observed.Modules[0].Path, Version: "v1.999.0"}
		_, mod, _, modSum := modulePayload(t, id.Path, id.Version, []string{"go.mod", "main.go"})
		prefix, err := modulePrefix(id)
		if err != nil {
			t.Fatal(err)
		}
		// Original graph metadata is valid against the frozen project go.sum,
		// but no source ZIP, generator checkpoint entry or signed lookup exists.
		writeFixture(t, filepath.Join(build, "go-modules/cache/download", prefix+".mod"), mod)
		writeFixture(t, filepath.Join(build, "go-modules/cache/download", prefix+".info"), marshal(t, map[string]string{"Version": id.Version}))
		service.sums = fmt.Appendf(service.sums, "%s %s/go.mod %s\n", id.Path, id.Version, modSum)
		var catalog catalogue
		if err := json.Unmarshal(service.cat, &catalog); err != nil {
			t.Fatal(err)
		}
		catalog.GoSumSHA256 = hash(service.sums)
		service.cat = marshal(t, catalog)
		writeFixture(t, cataloguePath, service.cat)
	}
	packages := bytes.ReplaceAll(service.packages, []byte(service.build), []byte(build))
	writeFixture(t, filepath.Join(build, "prepared/source/go.mod"), service.mod)
	writeFixture(t, filepath.Join(build, "prepared/source/go.sum"), service.sums)
	writeFixture(t, filepath.Join(build, "packages.jsonl"), packages)
	for _, selected := range service.entries {
		prefix, err := modulePrefix(selected.Effective)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(build, "go-modules/cache/download", prefix+".info"), marshal(t, map[string]string{"Version": selected.Effective.Version}))
	}
	serviceRoot, serviceReceipt := successfulSourceCapture(t, func(out string) (receipt, error) { return capture(build, cataloguePath, out, hash(service.cat)) })
	generatorRoot, generatorReceipt := successfulSourceCapture(t, func(out string) (generatorSourceReceipt, error) {
		return captureGeneratorSources(build, checkpoint, source, out, key)
	})
	metadataRoot, metadataReceipt := successfulSourceCapture(t, func(out string) (graphMetadataReceipt, error) {
		return captureGoGraphMetadata(build, source, out, key, hash(service.sums))
	})
	return goRestoreInputs{Service: serviceRoot, ServiceSHA256: serviceReceipt.ManifestSHA256,
			Generators: generatorRoot, GeneratorsSHA256: generatorReceipt.ManifestSHA256,
			Metadata: metadataRoot, MetadataSHA256: metadataReceipt.ManifestSHA256, SourceSHA256: source},
		goRestorePolicy{catalogue: hash(service.cat), checksumKey: key, goSum: hash(service.sums)}
}

// TestRestoreGoInputsFailureRemovesOnlyItsReservation performs real writes before failure.
func TestRestoreGoInputsFailureRemovesOnlyItsReservation(t *testing.T) {
	inputs, policy := restorationFixture(t)
	output := filepath.Join(physicalGeneratorTestDirectory(t), "restored")
	writes := 0
	got, err := restoreGoInputsWithCopier(inputs, output, policy, func(root *os.Root, source, output, name string, remaining int64) (fileRecord, error) {
		record, err := copyAt(root, source, output, name, remaining)
		if err != nil {
			return record, err
		}
		writes++
		if writes == 3 {
			return record, fmt.Errorf("fixture write failure")
		}
		return record, nil
	})
	if err == nil || writes != 3 || got != (goRestoreReceipt{}) {
		t.Fatal("partial output received a success receipt")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("failed restoration left partial output", err)
	}
}

// TestGoRestorationCLICannotChooseTrustRoots preserves the fixed production policy.
func TestGoRestorationCLICannotChooseTrustRoots(t *testing.T) {
	inputs, _ := restorationFixture(t)
	output := filepath.Join(physicalGeneratorTestDirectory(t), "restored")
	complete := []string{"restore-go-inputs", "--service", inputs.Service, "--service-sha256", inputs.ServiceSHA256,
		"--generators", inputs.Generators, "--generators-sha256", inputs.GeneratorsSHA256,
		"--metadata", inputs.Metadata, "--metadata-sha256", inputs.MetadataSHA256,
		"--source-sha256", inputs.SourceSHA256, "--output", output}
	for _, args := range [][]string{complete, {"restore-go-inputs", "--output", output},
		append(append([]string{}, complete...), "--key", "fixture"),
		append(append([]string{}, complete...), "--catalogue-sha256", strings.Repeat("a", 64)),
		append(append([]string{}, complete...), "--capture")} {
		var stdout bytes.Buffer
		if err := run(args, &stdout); err == nil || stdout.Len() != 0 {
			t.Fatal("CLI admitted caller-selected or incomplete authority")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("invalid CLI created output", err)
		}
	}
}

// TestRestoreGoInputsRejectsConflictingLiteralInfo permits no arbitrary cache winner.
func TestRestoreGoInputsRejectsConflictingLiteralInfo(t *testing.T) {
	inputs, policy := restorationFixture(t)
	root, err := openRoot(inputs.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	var metadata graphMetadataManifest
	err = loadGoRestoreManifest(root, inputs.MetadataSHA256, &metadata)
	if closeErr := root.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	// Generator entries already contain Time; changing only it remains individually
	// valid literal metadata, but cannot overwrite a different retained input.
	changed := false
	for i, record := range metadata.Files {
		if !strings.HasPrefix(record.Path, "metadata/") || !strings.HasSuffix(record.Path, ".info") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(inputs.Metadata, record.Path))
		if err != nil {
			t.Fatal(err)
		}
		var info map[string]any
		if err := json.Unmarshal(body, &info); err != nil {
			t.Fatal(err)
		}
		if _, ok := info["Time"]; !ok {
			continue
		}
		info["Time"] = "2030-01-01T00:00:00Z"
		body = marshal(t, info)
		writeFixture(t, filepath.Join(inputs.Metadata, record.Path), body)
		metadata.Files[i] = fileRecord{record.Path, int64(len(body)), hash(body)}
		changed = true
		break
	}
	if !changed {
		t.Fatal("fixture has no shared literal info")
	}
	body, err := sourceManifestBytes(metadata)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(inputs.Metadata, "manifest.json"), body)
	inputs.MetadataSHA256 = hash(body)
	if _, err := verifyGoGraphMetadata(inputs.Metadata, inputs.MetadataSHA256, inputs.SourceSHA256, policy.checksumKey, policy.goSum); err != nil {
		t.Fatal("conflict fixture must retain individually valid metadata", err)
	}
	output := filepath.Join(physicalGeneratorTestDirectory(t), "restored")
	got, err := restoreGoInputs(inputs, output, policy)
	if err == nil || got != (goRestoreReceipt{}) {
		t.Fatal("conflicting retained bytes were chosen arbitrarily")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("conflict created output", err)
	}
}

// TestRestoreGoInputsReconstructsOnlyAuthenticatedInputs covers the native cache layout.
func TestRestoreGoInputsReconstructsOnlyAuthenticatedInputs(t *testing.T) {
	inputs, policy := restorationFixture(t)
	output := filepath.Join(physicalGeneratorTestDirectory(t), "restored")
	got, err := restoreGoInputs(inputs, output, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got.Modules != 14 || got.Files < 42 || !got.SelectedGoInputsRestored || got.NativeOfflineReplayProved || got.DependencyInputsCaptured {
		t.Fatalf("incomplete or overstated restoration receipt: %+v", got)
	}
	for _, directory := range []string{"go-modules/cache/download", "go-path/pkg/sumdb/sum.golang.org"} {
		if info, err := os.Stat(filepath.Join(output, directory)); err != nil || !info.IsDir() {
			t.Fatal("native cache layout missing", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(output, "go-path/pkg/sumdb/sum.golang.org/latest")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(output, "go-modules/cache/download/example.com/!example/@v/v1.2.3.ziphash")); !os.IsNotExist(err) {
		t.Fatal("unchecked ZIP marker was reconstructed")
	}
}

// TestRestoredGoInputsServeTheNativeChecksumClient isolates every native Go cache.
func TestRestoredGoInputsServeTheNativeChecksumClient(t *testing.T) {
	inputs, policy := restorationFixture(t)
	for _, missingProof := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "missing-lookup"}[missingProof], func(t *testing.T) {
			output := filepath.Join(physicalGeneratorTestDirectory(t), "restored")
			if _, err := restoreGoInputs(inputs, output, policy); err != nil {
				t.Fatal(err)
			}
			sources := restorationGenerators(t, inputs)
			selected := sources.Modules[0]
			if missingProof {
				removeRestoredLookup(t, output, selected)
			}
			denied, requests := deniedChecksumServer(t)
			cmd := exec.CommandContext(t.Context(), "go", "mod", "download", "-json", selected.Path+"@"+selected.Version)
			cmd.Dir = physicalGeneratorTestDirectory(t)
			// The standard writable-cache flag permits this fixture's own TempDir
			// cleanup after native extraction; checksum verification is unchanged.
			cmd.Env = restoredNativeGoEnvironment(t, output, policy, denied.URL)
			body, err := cmd.CombinedOutput()
			var got struct{ Path, Version, Sum, GoModSum, Error string }
			if decodeErr := json.Unmarshal(body, &got); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if missingProof {
				if err == nil || requests.Load() == 0 || got.Error == "" || got.Path != selected.Path || got.Version != selected.Version {
					t.Fatalf("incomplete cache was accepted: %s", body)
				}
				return
			}
			if err != nil || requests.Load() != 0 {
				t.Fatalf("native Go needed uncaptured inputs: %v %s", err, body)
			}
			if got.Path != selected.Path || got.Version != selected.Version || got.Sum != selected.Sum || got.GoModSum != selected.GoModSum {
				t.Fatal("native Go did not independently match the selected input checksums")
			}
		})
	}
}

// TestRestoreGoInputsRefusesUnsealedOrOverlappingInputs preserves existing data.
func TestRestoreGoInputsRefusesUnsealedOrOverlappingInputs(t *testing.T) {
	for _, scenario := range []string{"service-seal", "generator-seal", "metadata-seal", "source-seal", "wrong-key", "extra-input", "existing-output", "linked-output", "nested-output", "aliased-parent-output"} {
		t.Run(scenario, func(t *testing.T) {
			inputs, policy := restorationFixture(t)
			parent := physicalGeneratorTestDirectory(t)
			output := filepath.Join(parent, "restored")
			preserved := ""
			switch scenario {
			case "service-seal":
				inputs.ServiceSHA256 = strings.Repeat("0", 64)
			case "generator-seal":
				inputs.GeneratorsSHA256 = strings.Repeat("0", 64)
			case "metadata-seal":
				inputs.MetadataSHA256 = strings.Repeat("0", 64)
			case "source-seal":
				inputs.SourceSHA256 = strings.Repeat("0", 64)
			case "wrong-key":
				policy.checksumKey = officialGeneratorSumDBKey
			case "extra-input":
				writeFixture(t, filepath.Join(inputs.Metadata, "extra"), []byte("unselected"))
			case "existing-output", "linked-output":
				preserved = filepath.Join(parent, "preserved")
				writeFixture(t, filepath.Join(preserved, "sentinel"), []byte("keep"))
				if scenario == "existing-output" {
					output = preserved
				} else if err := os.Symlink(preserved, output); err != nil {
					t.Fatal(err)
				}
			case "nested-output":
				output = filepath.Join(inputs.Service, "restored")
			case "aliased-parent-output":
				alias := filepath.Join(parent, "capsule-parent")
				if err := os.Symlink(filepath.Dir(inputs.Service), alias); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(alias, filepath.Base(inputs.Service), "restored")
			}
			got, err := restoreGoInputs(inputs, output, policy)
			if err == nil || got != (goRestoreReceipt{}) {
				t.Fatal("invalid restoration emitted a receipt")
			}
			if preserved != "" {
				body, err := os.ReadFile(filepath.Join(preserved, "sentinel"))
				if err != nil || string(body) != "keep" {
					t.Fatal("preexisting output changed", err)
				}
			} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("failed restoration created output", err)
			}
		})
	}
}
