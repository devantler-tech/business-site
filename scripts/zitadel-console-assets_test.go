package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
