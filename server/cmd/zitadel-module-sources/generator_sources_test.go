package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/note"
)

type primingChecksumOps struct {
	*generatorProofOps
	server *sumdb.Server
	mu     sync.Mutex
	cache  map[string][]byte
}

func (o *primingChecksumOps) ReadRemote(name string) ([]byte, error) {
	w := httptest.NewRecorder()
	o.server.ServeHTTP(w, httptest.NewRequest("GET", name, nil))
	if w.Code != 200 {
		return nil, fmt.Errorf("fixture checksum request failed")
	}
	return w.Body.Bytes(), nil
}
func (o *primingChecksumOps) WriteCache(name string, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cache[name] = append([]byte(nil), data...)
}

func generatorSourceFixture(t *testing.T) (build, checkpointSeal, sourceSeal, verifier string) {
	t.Helper()
	build = physicalGeneratorTestDirectory(t)
	tools := generatorFixture()
	modules, err := selectGeneratorModules(tools)
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[string]identity{}
	for _, id := range modules {
		zip, mod, sum, modSum := modulePayload(t, id.Path, id.Version, []string{"go.mod", "example.go"})
		id.Sum, id.GoModSum = sum, modSum
		payloads[id.Path+"@"+id.Version] = id
		prefix, err := modulePrefix(id)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(zip)
		if err != nil {
			t.Fatal(err)
		}
		for extension, body := range map[string][]byte{".zip": body, ".mod": mod, ".info": []byte(fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, id.Version))} {
			writeFixture(t, filepath.Join(build, "go-modules/cache/download", prefix+extension), body)
		}
	}
	for i := range tools {
		if tools[i].Build.Main.Version != "(devel)" {
			tools[i].Build.Main.Sum = payloads[tools[i].Build.Main.Path+"@"+tools[i].Build.Main.Version].Sum
		}
		for _, dep := range tools[i].Build.Deps {
			dep.Sum = payloads[dep.Path+"@"+dep.Version].Sum
		}
	}
	modules, err = selectGeneratorModules(tools)
	if err != nil {
		t.Fatal(err)
	}
	sourceSeal = strings.Repeat("a", 64)
	body := marshal(t, generatorCheckpoint{SchemaVersion: 1, SourceManifestSHA256: sourceSeal, Tools: tools, Modules: modules})
	writeFixture(t, filepath.Join(build, "generator-checkpoint.json"), body)
	checkpointSeal = hash(body)
	signer, key, err := note.GenerateKey(rand.Reader, "sum.golang.org")
	if err != nil {
		t.Fatal(err)
	}
	verifier = key
	priming := &primingChecksumOps{cache: map[string][]byte{}, server: sumdb.NewServer(sumdb.NewTestServer(signer, func(p, v string) ([]byte, error) {
		id, ok := payloads[p+"@"+v]
		if !ok {
			return nil, fmt.Errorf("unexpected module")
		}
		return []byte(fmt.Sprintf("%s %s %s\n%s %s/go.mod %s\n", p, v, id.Sum, p, v, id.GoModSum)), nil
	}))}
	priming.generatorProofOps = &generatorProofOps{key: key, latest: []byte{}, used: map[string][]byte{}, read: func(name string, limit int64) ([]byte, error) {
		priming.mu.Lock()
		defer priming.mu.Unlock()
		if b, ok := priming.cache[name]; ok {
			if int64(len(b)) > limit {
				return nil, fmt.Errorf("fixture proof exceeds limit")
			}
			return append([]byte(nil), b...), nil
		}
		return nil, os.ErrNotExist
	}}
	client := sumdb.NewClient(priming)
	for _, id := range modules {
		for _, version := range []string{id.Version, id.Version + "/go.mod"} {
			if _, err := client.Lookup(id.Path, version); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, used, err := authenticateGeneratorSources(modules, priming.latest, key, priming.read)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range used {
		writeFixture(t, filepath.Join(build, "go-modules/cache/download/sumdb", name), body)
	}
	writeFixture(t, filepath.Join(build, "go-path/pkg/sumdb/sum.golang.org/latest"), priming.latest)
	return
}

func capturedGeneratorSourceFixture(t *testing.T) (string, generatorSourceReceipt, string, string) {
	t.Helper()
	build, checkpoint, source, key := generatorSourceFixture(t)
	out := filepath.Join(physicalGeneratorTestDirectory(t), "captured")
	got, err := captureGeneratorSources(build, checkpoint, source, out, key)
	if err != nil {
		t.Fatal(err)
	}
	return out, got, source, key
}

func TestGeneratorSourceCaptureUsesAuthenticatedChecksumsAndPortableProofs(t *testing.T) {
	out, got, source, key := capturedGeneratorSourceFixture(t)
	if !got.SelectedGeneratorGoSourcesCaptured || got.Modules != 12 || got.DependencyInputsCaptured || got.NativeOfflineReplayProved {
		t.Fatalf("wrong receipt: %+v", got)
	}
	copyRoot := filepath.Join(physicalGeneratorTestDirectory(t), "moved")
	if err := os.CopyFS(copyRoot, os.DirFS(out)); err != nil {
		t.Fatal(err)
	}
	checked, err := verifyGeneratorSources(copyRoot, got.ManifestSHA256, source, key)
	if err != nil || got != checked {
		t.Fatalf("portable proof: %+v %v", checked, err)
	}
}

func TestGeneratorSourceCaptureRejectsMissingOrChangedProofAndPayload(t *testing.T) {
	for _, kind := range []string{"lookup", "tile", "latest", "mod", "zip", "info", "checkpoint", "source"} {
		t.Run(kind, func(t *testing.T) {
			build, checkpoint, source, key := generatorSourceFixture(t)
			switch kind {
			case "checkpoint":
				checkpoint = strings.Repeat("b", 64)
			case "source":
				source = strings.Repeat("b", 64)
			default:
				changed := false
				err := filepath.WalkDir(build, func(name string, d os.DirEntry, e error) error {
					if e != nil {
						return e
					}
					if d.IsDir() || changed {
						return nil
					}
					if (kind == "lookup" && strings.Contains(name, "/lookup/")) || (kind == "tile" && strings.Contains(name, "/tile/")) || (kind == "latest" && strings.HasSuffix(name, "/latest")) || (strings.HasSuffix(name, "."+kind)) {
						changed = true
						return os.WriteFile(name, []byte("forged but plausibly self-hashed"), 0o644)
					}
					return nil
				})
				if err != nil || !changed {
					t.Fatalf("mutation failed: %v", err)
				}
			}
			got, err := captureGeneratorSources(build, checkpoint, source, filepath.Join(physicalGeneratorTestDirectory(t), "refused"), key)
			if err == nil || got != (generatorSourceReceipt{}) {
				t.Fatal("changed authenticated input accepted")
			}
		})
	}
}

func TestGeneratorSourceProductionKeyCannotBeSelectedByArtifacts(t *testing.T) {
	build, checkpoint, source, key := generatorSourceFixture(t)
	if key == officialGeneratorSumDBKey {
		t.Fatal("fixture unexpectedly uses the official signer")
	}
	if _, err := captureGeneratorSources(build, checkpoint, source, filepath.Join(physicalGeneratorTestDirectory(t), "refused"), officialGeneratorSumDBKey); err == nil {
		t.Fatal("untrusted signing key accepted")
	}
	var output bytes.Buffer
	args := []string{"capture-generator-sources", "--build", build, "--checkpoint-sha256", checkpoint, "--source-sha256", source, "--output", filepath.Join(physicalGeneratorTestDirectory(t), "cli-refused")}
	if err := run(args, &output); err == nil || output.Len() != 0 {
		t.Fatal("CLI emitted a receipt for a different signing key")
	}
	if err := run(append(args, "--key", key), &output); err == nil || output.Len() != 0 {
		t.Fatal("CLI admitted an alternate verifier key")
	}
}

func TestGeneratorSourceRetainedMembershipIsClosed(t *testing.T) {
	out, got, source, key := capturedGeneratorSourceFixture(t)
	writeFixture(t, filepath.Join(out, "proofs/sum.golang.org/lookup/extra@v1.0.0"), []byte("extra"))
	if _, err := verifyGeneratorSources(out, got.ManifestSHA256, source, key); err == nil {
		t.Fatal("extra retained proof accepted")
	}
}

func physicalGeneratorTestDirectory(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGeneratorProofBudgetIsAdmittedBeforeReading(t *testing.T) {
	reads := 0
	ops := &generatorProofOps{bytes: inputLimit, used: map[string][]byte{}, read: func(string, int64) ([]byte, error) {
		reads++
		return []byte("unneeded allocation"), nil
	}}
	if _, err := ops.ReadCache("sum.golang.org/tile/8/0/000"); err == nil {
		t.Fatal("exhausted proof budget admitted")
	}
	if reads != 0 {
		t.Fatal("exhausted proof budget read bytes before refusal")
	}
}

func TestGeneratorProofReaderReceivesOnlyRemainingBudget(t *testing.T) {
	reads := 0
	ops := &generatorProofOps{bytes: inputLimit - 2, used: map[string][]byte{}, read: func(_ string, limit int64) ([]byte, error) {
		reads++
		if limit != 2 {
			t.Fatal("reader did not receive the remaining allocation budget")
		}
		return []byte("ok"), nil
	}}
	if _, err := ops.ReadCache("sum.golang.org/tile/8/0/000"); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.ReadCache("sum.golang.org/tile/8/0/001"); err == nil || reads != 1 {
		t.Fatal("exhausted budget performed another source read")
	}
}
