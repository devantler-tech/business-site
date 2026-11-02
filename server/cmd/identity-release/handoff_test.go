package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func handoffFixture(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	digest := strings.Repeat("1", 64)
	binary := strings.Repeat("2", 64)
	replayBinary := strings.Repeat("3", 64)
	imageID := "sha256:" + strings.Repeat("4", 64)
	write := func(name string, body []byte, mode os.FileMode) string {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, mode); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(body))
	}
	put := func(name string, value any) string {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return write(name, body, 0600)
	}
	sourceArchive := write("zitadel-offline-replay/inputs.tar", []byte("opaque source archive fixture"), 0600)
	baseArchive := write("zitadel-base-source/base-source.tar", []byte("opaque base archive fixture"), 0444)
	imageBody := []byte("opaque saved image fixture")
	imageArchive := write("zitadel-image/candidate-image.tar", imageBody, 0444)
	p, err := loadBaseProfile()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := baseManifestBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	baseManifest := write("zitadel-base-source/evidence/base-source.json", manifest, 0600)
	for _, op := range []string{"capture", "verify", "pack"} {
		v := map[string]any{"schemaVersion": 1, "operation": op + "-base-source", "baseImage": frozenBaseImage,
			"manifestSha256": baseManifest, "payloadFiles": 41, "baseImageSignatureVerified": false,
			"openpgpVerified": false, "sourceReleaseVerified": false, "imageAdmission": false}
		if op == "pack" {
			v["archiveSha256"] = baseArchive
		}
		put("zitadel-base-source/evidence/"+op+".json", v)
	}
	build := map[string]any{"schemaVersion": 1, "profile": "api-login-v1-console", "platform": "linux/amd64",
		"upstreamCommit": "8afa5dd37a8e723efb9eace8d09fd5c1cccef694", "version": "4.20.0",
		"recipeSha256": digest, "binarySha256": binary, "reproducible": false,
		"sourceSnapshot": map[string]any{"manifestSha256": digest, "preparedProfileSha256": "6f1a041fb83c337d5fd0619e33c305497e64be3bdf87c0f8e57c23deffd38181",
			"files": 9528, "bytes": 261325366, "records": 58}}
	buildSHA := put("zitadel-build/build-receipt.json", build)
	put("zitadel-offline-replay/native/build-receipt.json", map[string]any{"schemaVersion": 1,
		"profile": "api-login-v1-console", "platform": "linux/amd64", "binarySha256": replayBinary,
		"recipeSha256": digest, "sourceInput": map[string]any{"manifestSha256": digest}})
	for _, name := range []string{"input-retention", "input-archive", "input-roundtrip"} {
		v := map[string]any{"manifestSha256": digest}
		if name == "input-archive" || name == "input-roundtrip" {
			v["archiveSha256"] = sourceArchive
		}
		put("zitadel-offline-replay/"+name+".json", v)
	}
	put("zitadel-offline-replay/replay-receipt.json", map[string]any{"schemaVersion": 1,
		"profile": "native-retained-input-replay-v1", "platform": "linux/amd64", "inputManifestSha256": digest,
		"inputArchiveSha256": sourceArchive, "sourceManifestSha256": digest, "recipeSha256": digest,
		"binarySha256": replayBinary, "dependencyInputsCaptured": true, "nativeOfflineReplayProved": true,
		"networkDenied": true, "capabilitiesDropped": true, "nonRoot": true, "noNewPrivileges": true,
		"reproducible": false, "imageAdmission": "unknown", "licenseClosure": "unknown"})
	layers := make([]string, 16)
	for i := range layers {
		layers[i] = "sha256:" + digest
	}
	put("zitadel-image/image-smoke.json", map[string]any{"schemaVersion": 1, "profile": "cli-version-smoke",
		"base": frozenBaseImage, "localImageId": imageID, "contextSha256": digest, "candidateReloaded": true,
		"candidateArchive": map[string]any{"path": "candidate-image.tar", "bytes": len(imageBody), "sha256": imageArchive, "mode": 0444},
		"baseRootFS":       map[string]any{"type": "layers", "layers": layers},
		"files":            []any{map[string]any{"path": "zitadel", "sha256": binary}, map[string]any{"path": "build-receipt.json", "sha256": buildSHA}},
		"cleanup":          "complete", "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false})
	put("zitadel-image/service-smoke.json", map[string]any{"schemaVersion": 1, "profile": "isolated-service-smoke",
		"localImageId": imageID, "readOnlyRoot": true, "cleanup": "complete", "authenticationProof": false,
		"browserProof": false, "imageAdmission": "unknown", "licenseClosure": "unknown"})
	browserSHA := put("zitadel-image/browser-smoke.json", map[string]any{"schemaVersion": 1,
		"browserSourceSha256": digest, "cleanup": "complete", "portalFederation": false,
		"productionTLS": false, "invitationProof": false, "mfaProof": false, "browserSandboxDisabled": false,
		"imageAdmission": "unknown", "licenseClosure": "unknown"})
	put("zitadel-image/browser-binding.json", map[string]any{"schemaVersion": 1, "localImageId": imageID,
		"browserSourceSha256": digest, "browserReceiptSha256": browserSHA, "cleanup": "complete",
		"imageAdmission": "unknown", "licenseClosure": "unknown"})
	return root, []string{"prepare-handoff", "--input", root, "--source-sha", strings.Repeat("a", 40),
		"--run-id", "123", "--run-attempt", "1", "--output", filepath.Join(root, "handoff.json")}
}

func TestPublicationHandoff(t *testing.T) {
	root, args := handoffFixture(t)
	var out bytes.Buffer
	if err := runRelease(context.Background(), args, &out); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "handoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result["profile"] != "identity-publication-handoff-v1" || result["originalCIRunId"] != "123" ||
		result["forgeAdmission"] != false || result["imagePublished"] != false || result["portalActivated"] != false {
		t.Fatalf("unexpected observation: %s", body)
	}
	if result["originalBinarySha256"] == result["replayBinarySha256"] {
		t.Fatal("fixture must retain the distinct connected/replay binary identities")
	}
	if err = runRelease(context.Background(), args, &out); err == nil {
		t.Fatal("existing output accepted")
	}
}

func TestPublicationHandoffReadOnlyWitnesses(t *testing.T) {
	root, args := handoffFixture(t)
	for _, name := range []string{"zitadel-image/image-smoke.json", "zitadel-image/service-smoke.json", "zitadel-image/browser-binding.json", "zitadel-base-source/evidence/base-source.json"} {
		if err := os.Chmod(filepath.Join(root, name), 0444); err != nil {
			t.Fatal(err)
		}
	}
	if err := runRelease(context.Background(), args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationHandoffRefuses(t *testing.T) {
	for _, kind := range []string{"missing-archive", "changed-archive", "linked-archive", "linked-root", "writable-image", "missing-witness", "partial-json", "duplicate-json", "failed-replay", "changed-binary", "claimed-admission", "missing-false-field", "extra-rootfs-field", "changed-roundtrip-archive", "claimed-browser-admission", "bad-source", "zero-run", "unknown-option"} {
		t.Run(kind, func(t *testing.T) {
			root, args := handoffFixture(t)
			image := filepath.Join(root, "zitadel-image/candidate-image.tar")
			replay := filepath.Join(root, "zitadel-offline-replay/replay-receipt.json")
			edit := func(file, before, after string) {
				t.Helper()
				body, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				next := strings.Replace(string(body), before, after, 1)
				if next == string(body) {
					t.Fatal("mutation not applied")
				}
				if err = os.WriteFile(file, []byte(next), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			switch kind {
			case "missing-archive":
				err = os.Remove(image)
			case "changed-archive":
				if err = os.Chmod(image, 0600); err == nil {
					err = os.WriteFile(image, []byte("substitution"), 0600)
				}
			case "linked-archive":
				if err = os.Rename(image, image+".original"); err == nil {
					err = os.Symlink(image+".original", image)
				}
			case "linked-root":
				err = os.Symlink(root, root+".link")
				if err == nil {
					t.Cleanup(func() { _ = os.Remove(root + ".link") })
					args[2] = root + ".link"
				}
			case "writable-image":
				err = os.Chmod(image, 0644)
			case "missing-witness":
				err = os.Remove(replay)
			case "partial-json":
				err = os.WriteFile(replay, []byte(`{"schemaVersion":1`), 0600)
			case "duplicate-json":
				edit(replay, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`)
			case "failed-replay":
				edit(replay, `"networkDenied":true`, `"networkDenied":false`)
			case "changed-binary":
				edit(replay, strings.Repeat("3", 64), strings.Repeat("9", 64))
			case "claimed-admission":
				edit(replay, `"imageAdmission":"unknown"`, `"imageAdmission":"admitted"`)
			case "missing-false-field":
				edit(replay, `"reproducible":false,`, "")
			case "extra-rootfs-field":
				edit(filepath.Join(root, "zitadel-image/image-smoke.json"), `"baseRootFS":{`, `"baseRootFS":{"unexpected":"private detail",`)
			case "changed-roundtrip-archive":
				file := filepath.Join(root, "zitadel-offline-replay/input-roundtrip.json")
				body, readErr := os.ReadFile(file)
				if readErr != nil {
					t.Fatal(readErr)
				}
				var value map[string]string
				if readErr = json.Unmarshal(body, &value); readErr != nil {
					t.Fatal(readErr)
				}
				edit(file, value["archiveSha256"], strings.Repeat("9", 64))
			case "claimed-browser-admission":
				file := filepath.Join(root, "zitadel-image/browser-smoke.json")
				before, readErr := os.ReadFile(file)
				if readErr != nil {
					t.Fatal(readErr)
				}
				edit(file, `"imageAdmission":"unknown"`, `"imageAdmission":"admitted"`)
				after, readErr := os.ReadFile(file)
				if readErr != nil {
					t.Fatal(readErr)
				}
				edit(filepath.Join(root, "zitadel-image/browser-binding.json"), fmt.Sprintf("%x", sha256.Sum256(before)), fmt.Sprintf("%x", sha256.Sum256(after)))
			case "bad-source":
				args[4] = "main"
			case "zero-run":
				args[6] = "0"
			case "unknown-option":
				args = append(args, "--publish")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = runRelease(context.Background(), args, &bytes.Buffer{}); err == nil {
				t.Fatal("invalid handoff accepted")
			}
			if _, err = os.Lstat(filepath.Join(root, "handoff.json")); !os.IsNotExist(err) {
				t.Fatal("failed input left a handoff")
			}
		})
	}
}
