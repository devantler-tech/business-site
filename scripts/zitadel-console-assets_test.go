package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func assetFixture() []consoleAsset {
	var result []consoleAsset
	for _, name := range []string{"grpc-web", "javascript.zip", "openapi"} {
		body := []byte("fixture-original-" + name)
		hash := sha256.Sum256(body)
		result = append(result, consoleAsset{Name: name, URL: "https://github.com/fictional/releases/" + name, SHA256: hex.EncodeToString(hash[:]), Limit: 1024})
	}
	return result
}

func fixtureDownload(url string, output io.Writer) error {
	_, err := output.Write([]byte("fixture-original-" + filepath.Base(url)))
	return err
}

func nativeAssetFixture(t *testing.T) (string, string, []consoleAsset, string, string, string) {
	t.Helper()
	directory := assetDirectory(t)
	profile := assetFixture()
	seal, err := captureConsoleAssets(directory, profile, fixtureDownload)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(directory)
	tmp, records := filepath.Join(parent, "native-tmp"), filepath.Join(parent, "requests")
	leaf := filepath.Join(tmp, "zitadel-console-proto-plugins.AbC123")
	for _, path := range []string{tmp, records, leaf} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return directory, seal, profile, tmp, records, leaf
}

func TestConsoleNativeRequestsConsumeTheCompleteOriginalSet(t *testing.T) {
	directory, seal, profile, tmp, records, leaf := nativeAssetFixture(t)
	for i, asset := range profile {
		name := asset.Name
		if i == 1 {
			name = "p.zip"
		}
		destination := filepath.Join(leaf, name)
		if err := serveConsoleRequest(directory, seal, profile, tmp, records, []string{"-fsSL", asset.URL, "-o", destination}); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(destination)
		if err != nil || string(body) != "fixture-original-"+asset.Name {
			t.Fatalf("native destination did not receive original bytes: %v", err)
		}
		receipt, err := verifyConsoleConsumption(directory, seal, profile, records)
		if i != 2 {
			if err == nil || len(receipt) != 0 {
				t.Fatal("incomplete native request set reported complete consumption")
			}
			continue
		}
		var result struct {
			Manifest string `json:"manifestSha256"`
			Requests int    `json:"requests"`
			Consumed bool   `json:"originalConsoleDownloadsConsumed"`
			Offline  bool   `json:"nativeOfflineReplayProved"`
		}
		if err != nil || json.Unmarshal(receipt, &result) != nil || result.Manifest != seal || result.Requests != 3 || !result.Consumed || result.Offline {
			t.Fatalf("complete native consumption was not recorded accurately: %s %v", receipt, err)
		}
	}
}

func TestConsoleNativeRequestsRefuseUnsupportedOrUnsafeInputs(t *testing.T) {
	for _, mutation := range []string{"flags", "URL", "order", "basename", "escape", "relative", "wrong reservation", "linked parent", "existing file", "changed original", "extra original", "wrong seal", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			directory, seal, profile, tmp, records, leaf := nativeAssetFixture(t)
			destination := filepath.Join(leaf, profile[0].Name)
			args := []string{"-fsSL", profile[0].URL, "-o", destination}
			switch mutation {
			case "flags":
				args[0] = "--insecure"
			case "URL":
				args[1] += "?override=1"
			case "order":
				args[1] = profile[1].URL
			case "basename":
				args[3] += ".unexpected"
			case "escape":
				args[3] = filepath.Join(filepath.Dir(tmp), profile[0].Name)
			case "relative":
				args[3] = profile[0].Name
			case "wrong reservation":
				wrong := filepath.Join(tmp, "AbC123")
				if err := os.Mkdir(wrong, 0700); err != nil {
					t.Fatal(err)
				}
				args[3] = filepath.Join(wrong, profile[0].Name)
			case "linked parent":
				alias := filepath.Join(tmp, "zitadel-console-proto-plugins.XyZ987")
				if err := os.Symlink(leaf, alias); err != nil {
					t.Fatal(err)
				}
				args[3] = filepath.Join(alias, profile[0].Name)
			case "existing file":
				if err := os.WriteFile(destination, []byte("preserve-existing"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed original":
				if err := os.WriteFile(filepath.Join(directory, profile[0].Name), []byte("substituted"), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra original":
				if err := os.WriteFile(filepath.Join(directory, "extra"), []byte("extra"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong seal":
				seal = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "duplicate":
				if err := serveConsoleRequest(directory, seal, profile, tmp, records, args); err != nil {
					t.Fatal(err)
				}
			}
			if err := serveConsoleRequest(directory, seal, profile, tmp, records, args); err == nil {
				t.Fatal("unsupported native request succeeded")
			}
			if mutation == "existing file" {
				body, err := os.ReadFile(destination)
				if err != nil || string(body) != "preserve-existing" {
					t.Fatal("existing destination was overwritten")
				}
			}
			if receipt, err := verifyConsoleConsumption(directory, seal, profile, records); err == nil || len(receipt) != 0 {
				t.Fatal("rejected request produced complete consumption evidence")
			}
		})
	}
}

func TestConsoleNativeConsumptionRejectsCorruptedRequestRecords(t *testing.T) {
	for _, mutation := range []string{"missing", "extra", "changed", "linked", "special mode"} {
		t.Run(mutation, func(t *testing.T) {
			directory, seal, profile, tmp, records, leaf := nativeAssetFixture(t)
			for i, asset := range profile {
				name := asset.Name
				if i == 1 {
					name = "p.zip"
				}
				if err := serveConsoleRequest(directory, seal, profile, tmp, records, []string{"-fsSL", asset.URL, "-o", filepath.Join(leaf, name)}); err != nil {
					t.Fatal(err)
				}
			}
			first := filepath.Join(records, "request-1.json")
			var err error
			switch mutation {
			case "missing":
				err = os.Remove(first)
			case "extra":
				err = os.WriteFile(filepath.Join(records, "request-4.json"), []byte("unexpected"), 0600)
			case "changed":
				err = os.WriteFile(first, []byte("{}\n"), 0600)
			case "linked":
				err = os.Link(first, filepath.Join(filepath.Dir(records), "request-alias"))
			case "special mode":
				err = os.Chmod(first, 0600|os.ModeSetgid)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result, err := verifyConsoleConsumption(directory, seal, profile, records); err == nil || len(result) != 0 {
				t.Fatal("invalid native request evidence produced success")
			}
		})
	}
}

func assetDirectory(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "capsule")
}

func TestConsoleFailedCapturePreservesReplacementDestination(t *testing.T) {
	directory := assetDirectory(t)
	profile := assetFixture()
	held := directory + "-failed-original"
	sentinel := filepath.Join(directory, "unrelated")
	download := func(_ string, _ io.Writer) error {
		if err := os.Rename(directory, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("preserve this"), 0600); err != nil {
			t.Fatal(err)
		}
		return io.ErrUnexpectedEOF
	}
	if seal, err := captureConsoleAssets(directory, profile, download); err == nil || seal != "" {
		t.Fatal("failed download produced a seal")
	}
	if body, err := os.ReadFile(sentinel); err != nil || string(body) != "preserve this" {
		t.Fatalf("failed capture removed a replacement destination: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(held, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("failed original retained a success manifest")
	}
}

func TestConsoleCaptureRefusesRenamedReservation(t *testing.T) {
	directory := assetDirectory(t)
	profile := assetFixture()
	first := true
	download := func(url string, output io.Writer) error {
		if first {
			first = false
			if err := os.Rename(directory, directory+"-moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return fixtureDownload(url, output)
	}
	if seal, err := captureConsoleAssets(directory, profile, download); err == nil || seal != "" {
		t.Fatal("capture sealed a reservation no longer at its destination")
	}
	if _, err := os.Lstat(directory); err != nil {
		t.Fatalf("capture removed the replacement reservation: %v", err)
	}
}

func TestConsoleOriginalAssetsSurviveAndReplayWithoutDownloader(t *testing.T) {
	directory := assetDirectory(t)
	profile := assetFixture()
	seal, err := captureConsoleAssets(directory, profile, fixtureDownload)
	if err != nil || len(seal) != 64 {
		t.Fatalf("capture: %s %v", seal, err)
	}
	for _, asset := range profile {
		var output bytes.Buffer
		if err := replayConsoleAsset(directory, seal, profile, asset.URL, &output); err != nil {
			t.Fatal(err)
		}
		if output.String() != "fixture-original-"+asset.Name {
			t.Fatal("replay did not retain the original archive bytes")
		}
	}
}

func TestConsoleCaptureRechecksTheCompleteFinalPackage(t *testing.T) {
	for _, mutation := range []string{"extra member", "changed prior member", "linked prior member", "wrong prior mode"} {
		t.Run(mutation, func(t *testing.T) {
			directory := assetDirectory(t)
			profile := assetFixture()
			download := func(url string, output io.Writer) error {
				if url == profile[2].URL {
					var err error
					switch mutation {
					case "extra member":
						err = os.WriteFile(filepath.Join(directory, "extra"), []byte("unexpected"), 0600)
					case "changed prior member":
						err = os.WriteFile(filepath.Join(directory, profile[0].Name), []byte("substituted"), 0600)
					case "linked prior member":
						err = os.Link(filepath.Join(directory, profile[0].Name), filepath.Join(filepath.Dir(directory), "second-name"))
					case "wrong prior mode":
						err = os.Chmod(filepath.Join(directory, profile[0].Name), 0644)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				return fixtureDownload(url, output)
			}
			if seal, err := captureConsoleAssets(directory, profile, download); err == nil || seal != "" {
				t.Fatal("capture emitted success for a package its own verifier rejects")
			}
		})
	}
}

func TestConsolePackageRejectsSpecialModeBits(t *testing.T) {
	for _, target := range []string{"directory", "member"} {
		t.Run(target, func(t *testing.T) {
			directory := assetDirectory(t)
			profile := assetFixture()
			seal, err := captureConsoleAssets(directory, profile, fixtureDownload)
			if err != nil {
				t.Fatal(err)
			}
			name, mode, special := directory, os.FileMode(0700), os.ModeSticky
			if target == "member" {
				name, mode, special = filepath.Join(directory, profile[0].Name), 0600, os.ModeSetgid
			}
			if err := os.Chmod(name, mode|special); err != nil {
				t.Fatal(err)
			}
			stat, err := os.Lstat(name)
			if err != nil || stat.Mode()&special == 0 {
				t.Fatal("special-mode negative control was not established")
			}
			if _, err := verifiedConsoleAssets(directory, seal, profile); err == nil {
				t.Fatal("special permission bits were admitted")
			}
		})
	}
}

func TestConsoleFrozenProfileRetainsCompleteGRPCWebRelease(t *testing.T) {
	// The frozen upstream release asset is 33,079,312 bytes. A smaller
	// transport cap truncates that legitimate input before checksum validation.
	body := make([]byte, 33079312)
	profile := assetFixture()
	profile[0].Limit = nativeConsoleAssets[0].Limit
	profile[0].SHA256 = consoleDigest(body)
	download := func(url string, output io.Writer) error {
		if url == profile[0].URL {
			_, err := io.Copy(output, bytes.NewReader(body))
			return err
		}
		return fixtureDownload(url, output)
	}
	directory := assetDirectory(t)
	seal, err := captureConsoleAssets(directory, profile, download)
	if err != nil {
		t.Fatalf("frozen release length was refused: %v", err)
	}
	var output bytes.Buffer
	if err := replayConsoleAsset(directory, seal, profile, profile[0].URL, &output); err != nil || !bytes.Equal(body, output.Bytes()) {
		t.Fatalf("frozen release length did not survive complete replay: %v", err)
	}
}

func TestConsoleCapsuleRefusesIncompleteChangedAndExtraInputs(t *testing.T) {
	for _, mutation := range []string{"wrong seal", "changed archive", "missing archive", "extra archive", "linked archive", "hardlinked archive", "public archive", "changed manifest", "unknown URL"} {
		t.Run(mutation, func(t *testing.T) {
			directory := assetDirectory(t)
			profile := assetFixture()
			seal, err := captureConsoleAssets(directory, profile, fixtureDownload)
			if err != nil {
				t.Fatal(err)
			}
			url := profile[0].URL
			switch mutation {
			case "wrong seal":
				seal = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "changed archive":
				err = os.WriteFile(filepath.Join(directory, profile[0].Name), []byte("changed"), 0600)
			case "missing archive":
				err = os.Remove(filepath.Join(directory, profile[1].Name))
			case "extra archive":
				err = os.WriteFile(filepath.Join(directory, "extra"), []byte("extra"), 0600)
			case "linked archive":
				err = os.Remove(filepath.Join(directory, profile[1].Name))
				if err == nil {
					err = os.Symlink(profile[0].Name, filepath.Join(directory, profile[1].Name))
				}
			case "hardlinked archive":
				err = os.Link(filepath.Join(directory, profile[0].Name), filepath.Join(filepath.Dir(directory), "second-link"))
			case "public archive":
				err = os.Chmod(filepath.Join(directory, profile[0].Name), 0644)
			case "changed manifest":
				err = os.WriteFile(filepath.Join(directory, "manifest.json"), []byte("{}"), 0600)
			case "unknown URL":
				url += "?override=1"
			}
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := replayConsoleAsset(directory, seal, profile, url, &output); err == nil {
				t.Fatal("admitted an unsealed or incomplete capsule")
			}
			if output.Len() != 0 {
				t.Fatal("published bytes before the complete capsule was verified")
			}
		})
	}
}

func TestConsoleCommandIsInertUnlessExplicitlyRequested(t *testing.T) {
	var output bytes.Buffer
	if err := consoleAssetsCommand(nil, &output); err != nil || output.Len() != 0 {
		t.Fatalf("ordinary invocation was not inert: %v", err)
	}
	for _, args := range [][]string{
		{"capture"}, {"fetch"}, {"verify"}, {"unknown"},
		{"fetch", "--url", nativeConsoleAssets[0].URL, "--curl", "/usr/bin/curl"},
		{"verify", "--url", nativeConsoleAssets[0].URL},
		{"verify", "unexpected-argument"},
	} {
		if err := consoleAssetsCommand(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("incomplete or ambiguous command produced success: %q: %v", args, err)
		}
	}
}

func TestConsoleCaptureDoesNotSealFailedOrWrongOriginals(t *testing.T) {
	for _, mutation := range []string{"checksum", "oversize", "download", "existing", "profile"} {
		t.Run(mutation, func(t *testing.T) {
			directory := assetDirectory(t)
			profile := assetFixture()
			download := fixtureDownload
			switch mutation {
			case "checksum":
				download = func(_ string, w io.Writer) error { _, err := w.Write([]byte("wrong")); return err }
			case "oversize":
				profile[0].Limit = 2
			case "download":
				download = func(_ string, _ io.Writer) error { return io.ErrUnexpectedEOF }
			case "existing":
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			case "profile":
				profile[0].Name = "../escaped"
			}
			if seal, err := captureConsoleAssets(directory, profile, download); err == nil || seal != "" {
				t.Fatal("sealed failed capture")
			}
			if _, err := os.Lstat(filepath.Join(directory, "manifest.json")); err == nil {
				t.Fatal("left a success manifest after failed capture")
			}
		})
	}
}
