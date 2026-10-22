// Command runtime-receipt admits a bounded source-bound publication archive without extraction.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"
)

const archiveLimit = 4 * 1024 * 1024
const memberLimit = 2 * 1024 * 1024
const totalLimit = 8 * 1024 * 1024
const receiptLimit = 16 * 1024

var errRefused = errors.New("runtime-receipt refused")
var sourcePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func positiveID(value string) bool {
	if value == "" || value[0] < '1' || value[0] > '9' {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n <= 9007199254740991
}

func arguments(args []string) (map[string]string, error) {
	if len(args) != 12 {
		return nil, errRefused
	}
	options := map[string]string{"--archive": "", "--sha256": "", "--source": "", "--run": "", "--attempt": "", "--artifact": ""}
	for i := 0; i < len(args); i += 2 {
		value, known := options[args[i]]
		if !known || value != "" || args[i+1] == "" {
			return nil, errRefused
		}
		options[args[i]] = args[i+1]
	}
	if !digestPattern.MatchString(options["--sha256"]) || !sourcePattern.MatchString(options["--source"]) {
		return nil, errRefused
	}
	for _, key := range []string{"--run", "--attempt", "--artifact"} {
		if !positiveID(options[key]) {
			return nil, errRefused
		}
	}
	return options, nil
}

// readArchive hashes the same bounded byte snapshot later inspected by archive/zip.
func readArchive(path, expected string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errRefused
	}
	file := os.NewFile(uintptr(fd), "publication archive")
	// Read-only descriptor cleanup cannot commit a partially admitted receipt.
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > archiveLimit {
		return nil, errRefused
	}
	data, err := io.ReadAll(io.LimitReader(file, archiveLimit+1))
	if err != nil || len(data) > archiveLimit || int64(len(data)) != info.Size() {
		return nil, errRefused
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expected {
		return nil, errRefused
	}
	return data, nil
}

// archiveReceipt examines complete member streams, including CRC failures, without extracting paths.
func archiveReceipt(data []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(r.File) != 5 {
		return nil, errRefused
	}
	remaining := map[string]bool{"runtime.json": true, "signature.json": true, "provenance.json": true, "attestation.json": true, "receipt.json": true}
	var receipt []byte
	total := 0
	for _, f := range r.File {
		if !remaining[f.Name] || !f.Mode().IsRegular() || f.UncompressedSize64 > memberLimit {
			return nil, errRefused
		}
		delete(remaining, f.Name)
		limit := memberLimit
		if f.Name == "receipt.json" {
			limit = receiptLimit
		}
		stream, err := f.Open()
		if err != nil {
			return nil, errRefused
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, int64(limit)+1))
		closeErr := stream.Close()
		total += len(body)
		if readErr != nil || closeErr != nil || len(body) > limit || total > totalLimit || !utf8.Valid(body) || !json.Valid(body) {
			return nil, errRefused
		}
		if f.Name == "receipt.json" {
			receipt = body
		}
	}
	if len(remaining) != 0 {
		return nil, errRefused
	}
	return receipt, nil
}

// decodeReceipt admits exactly eleven string fields and rejects JSON key aliases and duplicates.
func decodeReceipt(data []byte, options map[string]string) (map[string]string, error) {
	fields := map[string]bool{"repository": true, "sourceRevision": true, "publicDigest": true, "publicProfile": true, "image": true, "signerIdentity": true, "workflowRevision": true, "ciRunId": true, "ciRunAttempt": true, "publicationRunId": true, "publicationRunAttempt": true}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errRefused
	}
	receipt := make(map[string]string)
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, errRefused
		}
		key, ok := token.(string)
		if !ok || !fields[key] {
			return nil, errRefused
		}
		delete(fields, key)
		token, err = d.Token()
		if err != nil {
			return nil, errRefused
		}
		value, ok := token.(string)
		if !ok || value == "" {
			return nil, errRefused
		}
		receipt[key] = value
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') || len(fields) != 0 {
		return nil, errRefused
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errRefused
	}
	expected := map[string]string{
		"repository": "devantler-tech/business-site", "sourceRevision": options["--source"], "workflowRevision": options["--source"],
		"ciRunId": options["--run"], "ciRunAttempt": options["--attempt"], "publicationRunId": options["--run"], "publicationRunAttempt": options["--attempt"],
		"publicProfile": "production-template", "signerIdentity": "https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main",
	}
	for key, value := range expected {
		if receipt[key] != value {
			return nil, errRefused
		}
	}
	const imagePrefix = "ghcr.io/devantler-tech/business-site@sha256:"
	image := receipt["image"]
	if !digestPattern.MatchString(receipt["publicDigest"]) || len(image) != len(imagePrefix)+64 || image[:len(imagePrefix)] != imagePrefix || !digestPattern.MatchString(image[len(imagePrefix):]) {
		return nil, errRefused
	}
	return receipt, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if ctx.Err() != nil {
		return errRefused
	}
	options, err := arguments(args)
	if err != nil {
		return err
	}
	data, err := readArchive(options["--archive"], options["--sha256"])
	if err != nil {
		return err
	}
	body, err := archiveReceipt(data)
	if err != nil {
		return err
	}
	receipt, err := decodeReceipt(body, options)
	if err != nil || ctx.Err() != nil {
		return errRefused
	}
	return json.NewEncoder(output).Encode(receipt)
}

func execute() int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		// Paths, source-controlled text and inspected archive contents are never diagnostics.
		_, _ = fmt.Fprintln(os.Stderr, errRefused)
		return 1
	}
	return 0
}

func main() { os.Exit(execute()) }
