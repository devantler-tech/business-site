// Explicit retention of the frozen console generator's original downloads.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
)

type consoleAsset struct {
	Name, URL, SHA256 string
	Limit             int64
}
type assetDownloader func(string, io.Writer) error

type capturedConsoleAsset struct {
	Name, URL, SHA256 string
	Bytes             int64
}
type consoleCapsule struct {
	SchemaVersion                                       int
	Kind, Platform                                      string
	Assets                                              []capturedConsoleAsset
	DependencyInputsCaptured, NativeOfflineReplayProved bool
}

var nativeConsoleAssets = []consoleAsset{
	{"protoc-gen-grpc-web", "https://github.com/grpc/grpc-web/releases/download/1.5.0/protoc-gen-grpc-web-1.5.0-linux-x86_64", "2e6e074497b221045a14d5a54e9fc910945bfdd1198b12b9fc23686a95671d64", 32 << 20},
	{"protobuf-javascript.zip", "https://github.com/protocolbuffers/protobuf-javascript/releases/download/v4.0.2/protobuf-javascript-4.0.2-linux-x86_64.zip", "e4b0bc2c9fe32a21167c6d84a50df21c2f405552f9a6ed7d5e858d92cac46d22", 32 << 20},
	{"protoc-gen-openapiv2", "https://github.com/grpc-ecosystem/grpc-gateway/releases/download/v2.22.0/protoc-gen-openapiv2-v2.22.0-linux-x86_64", "72a6fc6a6d130189c549a6d88cbdef407d3ed1c95ab101ffb3d223d8b3778c90", 32 << 20},
}

func consoleDigest(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func consoleProfile(profile []consoleAsset) error {
	if len(profile) != 3 {
		return fmt.Errorf("incomplete console archive profile")
	}
	seen := map[string]bool{}
	for _, asset := range profile {
		u, err := url.Parse(asset.URL)
		decoded, hashErr := hex.DecodeString(asset.SHA256)
		if asset.Name == "" || filepath.Base(asset.Name) != asset.Name || asset.Name == "." || asset.Name == "manifest.json" || strings.ContainsAny(asset.Name, "\\\x00\r\n") || seen[asset.Name] || seen[asset.URL] || err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || hashErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != asset.SHA256 || asset.Limit <= 0 || asset.Limit > 32<<20 {
			return fmt.Errorf("invalid console archive profile")
		}
		seen[asset.Name], seen[asset.URL] = true, true
	}
	return nil
}

func consoleDirectory(directory string) error {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return fmt.Errorf("unclean console capsule path")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return fmt.Errorf("linked or unavailable console capsule path")
	}
	stat, err := os.Lstat(directory)
	if err != nil || stat.Mode() != os.ModeDir|0700 {
		return fmt.Errorf("console capsule must be a private directory")
	}
	return nil
}

type consoleBoundedWriter struct {
	output    io.Writer
	remaining int64
}

func (w *consoleBoundedWriter) Write(body []byte) (int, error) {
	if int64(len(body)) > w.remaining {
		return 0, fmt.Errorf("console archive exceeds byte bound")
	}
	n, err := w.output.Write(body)
	w.remaining -= int64(n)
	return n, err
}

func captureConsoleAssets(directory string, profile []consoleAsset, download assetDownloader) (seal string, err error) {
	if err = consoleProfile(profile); err != nil {
		return "", err
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return "", fmt.Errorf("unclean capture destination")
	}
	if err = consoleDirectory(filepath.Dir(directory)); err != nil {
		return "", err
	}
	if err = os.Mkdir(directory, 0700); err != nil {
		return "", err
	}
	// Failed captures remain private and unsealed. Never remove by pathname:
	// another process may have replaced this reservation while downloading.
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	reserved, err := root.Stat(".")
	if err != nil {
		return "", err
	}
	manifest := consoleCapsule{SchemaVersion: 1, Kind: "original-console-generator-downloads", Platform: "linux/amd64"}
	for _, asset := range profile {
		file, createErr := root.OpenFile(asset.Name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if createErr != nil {
			return "", createErr
		}
		err = download(asset.URL, &consoleBoundedWriter{output: file, remaining: asset.Limit})
		if err == nil {
			_, err = file.Seek(0, io.SeekStart)
		}
		var body []byte
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(file, asset.Limit+1))
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return "", err
		}
		if len(body) == 0 || int64(len(body)) > asset.Limit || consoleDigest(body) != asset.SHA256 {
			return "", fmt.Errorf("console original checksum differs")
		}
		manifest.Assets = append(manifest.Assets, capturedConsoleAsset{asset.Name, asset.URL, asset.SHA256, int64(len(body))})
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	body = append(body, '\n')
	file, err := root.OpenFile("manifest.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(body)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	current, err := os.Lstat(directory)
	if err != nil || current.Mode() != os.ModeDir|0700 || !os.SameFile(reserved, current) {
		return "", fmt.Errorf("console capture reservation changed")
	}
	seal = consoleDigest(body)
	if _, err := verifiedConsoleAssets(directory, seal, profile); err != nil {
		return "", err
	}
	return seal, nil
}

func consoleRead(root *os.Root, name string, limit int64) ([]byte, error) {
	stat, err := root.Lstat(name)
	if err != nil || stat.Mode() != 0600 || stat.Size() < 1 || stat.Size() > limit {
		return nil, fmt.Errorf("invalid console capsule member")
	}
	if native, ok := stat.Sys().(*syscall.Stat_t); !ok || native.Nlink != 1 {
		return nil, fmt.Errorf("linked console capsule member")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(stat, opened) {
		return nil, fmt.Errorf("console capsule member changed during open")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	after, afterErr := file.Stat()
	if err != nil || afterErr != nil || int64(len(body)) != stat.Size() || !os.SameFile(stat, after) || after.Size() != stat.Size() || after.ModTime() != stat.ModTime() {
		return nil, fmt.Errorf("console capsule member changed during read")
	}
	return body, nil
}

func verifiedConsoleAssets(directory, seal string, profile []consoleAsset) (map[string][]byte, error) {
	if err := consoleProfile(profile); err != nil {
		return nil, err
	}
	if len(seal) != 64 {
		return nil, fmt.Errorf("explicit external capsule seal required")
	}
	if err := consoleDirectory(directory); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	listing, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	names, err := listing.Readdirnames(6)
	closeErr := listing.Close()
	if err != nil && err != io.EOF {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	wanted := map[string]bool{"manifest.json": true}
	for _, asset := range profile {
		wanted[asset.Name] = true
	}
	if len(names) != len(wanted) {
		return nil, fmt.Errorf("console capsule is not complete and closed")
	}
	for _, name := range names {
		if !wanted[name] {
			return nil, fmt.Errorf("extra console capsule member")
		}
	}
	body, err := consoleRead(root, "manifest.json", 16384)
	if err != nil || consoleDigest(body) != seal {
		return nil, fmt.Errorf("console capsule external seal differs")
	}
	var manifest consoleCapsule
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, err
	}
	want := consoleCapsule{SchemaVersion: 1, Kind: "original-console-generator-downloads", Platform: "linux/amd64"}
	result := map[string][]byte{}
	for _, asset := range profile {
		original, err := consoleRead(root, asset.Name, asset.Limit)
		if err != nil || consoleDigest(original) != asset.SHA256 {
			return nil, fmt.Errorf("console archive checksum differs")
		}
		want.Assets = append(want.Assets, capturedConsoleAsset{asset.Name, asset.URL, asset.SHA256, int64(len(original))})
		result[asset.URL] = original
	}
	canonical, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	canonical = append(canonical, '\n')
	if !reflect.DeepEqual(manifest, want) || !bytes.Equal(canonical, body) {
		return nil, fmt.Errorf("console capsule identity differs")
	}
	return result, nil
}

func replayConsoleAsset(directory, seal string, profile []consoleAsset, url string, output io.Writer) error {
	originals, err := verifiedConsoleAssets(directory, seal, profile)
	if err != nil {
		return err
	}
	body, ok := originals[url]
	if !ok {
		return fmt.Errorf("console request is outside frozen profile")
	}
	n, err := output.Write(body)
	if err == nil && n != len(body) {
		return io.ErrShortWrite
	}
	return err
}

func serveConsoleRequest(directory, seal string, profile []consoleAsset, tempRoot, recordsRoot string, args []string) error {
	if len(args) != 4 || args[0] != "-fsSL" || args[2] != "-o" {
		return fmt.Errorf("unsupported native console download arguments")
	}
	originals, err := verifiedConsoleAssets(directory, seal, profile)
	if err != nil {
		return err
	}
	if err := consoleDirectory(tempRoot); err != nil {
		return err
	}
	if err := consoleDirectory(recordsRoot); err != nil {
		return err
	}
	records, err := os.OpenRoot(recordsRoot)
	if err != nil {
		return err
	}
	defer records.Close()
	previous, err := consoleRequestRecords(records, profile, originals)
	if err != nil {
		return err
	}
	index := len(previous)
	if index >= len(profile) || args[1] != profile[index].URL {
		return fmt.Errorf("unexpected, repeated or out-of-order console download")
	}
	asset := profile[index]
	name := asset.Name
	if index == 1 {
		name = "p.zip"
	}
	destination := args[3]
	parent := filepath.Dir(destination)
	suffix := strings.TrimPrefix(filepath.Base(parent), "zitadel-console-proto-plugins.")
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || filepath.Dir(parent) != tempRoot || filepath.Base(destination) != name || !strings.HasPrefix(filepath.Base(parent), "zitadel-console-proto-plugins.") || len(suffix) != 6 {
		return fmt.Errorf("console destination escaped its native temporary directory")
	}
	for _, char := range suffix {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
			return fmt.Errorf("invalid native console temporary reservation")
		}
	}
	if err := consoleDirectory(parent); err != nil {
		return err
	}
	output, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer output.Close()
	// Reserve the ordered request before writing. A failed request remains
	// incomplete and cannot be mistaken for a successful native transaction.
	record, err := records.OpenFile(fmt.Sprintf("request-%d.json", index+1), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer record.Close()
	file, err := output.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	body := originals[asset.URL]
	written, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != len(body) {
		return io.ErrShortWrite
	}
	actual, err := consoleRead(output, name, asset.Limit)
	if err != nil || !bytes.Equal(body, actual) {
		return fmt.Errorf("native console destination differs from the sealed original")
	}
	if err := consoleSameDirectory(output, parent); err != nil {
		return err
	}
	if err := consoleSameDirectory(records, recordsRoot); err != nil {
		return err
	}
	raw, err := json.Marshal(capturedConsoleAsset{asset.Name, asset.URL, asset.SHA256, int64(len(body))})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	n, err := record.Write(raw)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	return record.Close()
}

func verifyConsoleConsumption(directory, seal string, profile []consoleAsset, recordsRoot string) ([]byte, error) {
	originals, err := verifiedConsoleAssets(directory, seal, profile)
	if err != nil {
		return nil, err
	}
	if err := consoleDirectory(recordsRoot); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(recordsRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	records, err := consoleRequestRecords(root, profile, originals)
	if err != nil || len(records) != 3 {
		return nil, fmt.Errorf("native console download set is incomplete or invalid")
	}
	if err := consoleSameDirectory(root, recordsRoot); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"schemaVersion": 1, "manifestSha256": seal, "requests": 3, "requestsSha256": consoleDigest(raw), "originalConsoleDownloadsConsumed": true, "dependencyInputsCaptured": false, "nativeOfflineReplayProved": false})
}

func consoleSameDirectory(root *os.Root, name string) error {
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	current, err := os.Lstat(name)
	if err != nil || current.Mode() != os.ModeDir|0700 || !os.SameFile(opened, current) {
		return fmt.Errorf("console directory reservation changed")
	}
	return nil
}

func consoleRequestRecords(root *os.Root, profile []consoleAsset, originals map[string][]byte) ([]capturedConsoleAsset, error) {
	listing, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	names, err := listing.Readdirnames(5)
	closeErr := listing.Close()
	if err != nil && err != io.EOF {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(names) > 3 {
		return nil, fmt.Errorf("extra native console request records")
	}
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	var result []capturedConsoleAsset
	for i := range names {
		name := fmt.Sprintf("request-%d.json", i+1)
		if !seen[name] {
			return nil, fmt.Errorf("native console request sequence is incomplete")
		}
		asset := profile[i]
		want := capturedConsoleAsset{asset.Name, asset.URL, asset.SHA256, int64(len(originals[asset.URL]))}
		raw, err := consoleRead(root, name, 2048)
		if err != nil {
			return nil, err
		}
		expected, err := json.Marshal(want)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(raw, append(expected, '\n')) {
			return nil, fmt.Errorf("native console request record differs")
		}
		result = append(result, want)
	}
	return result, nil
}

func consoleAssetsCommand(args []string, output io.Writer) error {
	if len(args) == 0 {
		return nil
	}
	flags := flag.NewFlagSet("console-assets", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("capsule", "", "private capsule")
	seal := flags.String("sha256", "", "external seal")
	curl := flags.String("curl", "", "explicit native downloader")
	url := flags.String("url", "", "one frozen original")
	tempRoot := flags.String("temp-root", "", "owned native temporary root")
	recordsRoot := flags.String("requests", "", "owned native request records")
	if err := flags.Parse(args[1:]); err != nil || (args[0] != "native-request" && flags.NArg() != 0) {
		return fmt.Errorf("invalid console archive command")
	}
	if args[0] != "native-request" && args[0] != "consumption" && (*tempRoot != "" || *recordsRoot != "") {
		return fmt.Errorf("native request options require an explicit native operation")
	}
	switch args[0] {
	case "capture":
		if *seal != "" || *url != "" || !filepath.IsAbs(*curl) || filepath.Clean(*curl) != *curl {
			return fmt.Errorf("capture requires an explicit native downloader")
		}
		stat, err := os.Lstat(*curl)
		if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("native downloader is not a regular executable")
		}
		digest, err := captureConsoleAssets(*directory, nativeConsoleAssets, func(url string, writer io.Writer) error {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, *curl, "-q", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--connect-timeout", "15", "--max-time", "110", url)
			command.Stdout = writer
			var stderr bytes.Buffer
			command.Stderr = &consoleBoundedWriter{output: &stderr, remaining: 4096}
			if err := command.Run(); err != nil {
				return fmt.Errorf("native original download failed: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"manifestSha256": digest, "originalConsoleAssetsCaptured": true, "dependencyInputsCaptured": false, "nativeOfflineReplayProved": false})
	case "fetch":
		if *curl != "" || *url == "" {
			return fmt.Errorf("replay cannot accept a downloader")
		}
		return replayConsoleAsset(*directory, *seal, nativeConsoleAssets, *url, output)
	case "verify":
		if *curl != "" || *url != "" {
			return fmt.Errorf("verification has no downloader or URL")
		}
		if _, err := verifiedConsoleAssets(*directory, *seal, nativeConsoleAssets); err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"manifestSha256": *seal, "originalConsoleAssetsCaptured": true, "dependencyInputsCaptured": false, "nativeOfflineReplayProved": false})
	case "native-request":
		if *curl != "" || *url != "" || *tempRoot == "" || *recordsRoot == "" {
			return fmt.Errorf("native console requests require owned roots and no downloader")
		}
		return serveConsoleRequest(*directory, *seal, nativeConsoleAssets, *tempRoot, *recordsRoot, flags.Args())
	case "consumption":
		if *curl != "" || *url != "" || *tempRoot != "" || *recordsRoot == "" {
			return fmt.Errorf("consumption verification requires records and no downloader")
		}
		raw, err := verifyConsoleConsumption(*directory, *seal, nativeConsoleAssets, *recordsRoot)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, string(raw))
		return err
	default:
		return fmt.Errorf("unsupported console archive command")
	}
}

func main() {
	if err := consoleAssetsCommand(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "console-assets:", err)
		os.Exit(1)
	}
}
