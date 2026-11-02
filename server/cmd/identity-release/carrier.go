package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const carrierLimit int64 = 5 << 28
const carrierHandoff = "zitadel-handoff/handoff.json"

type carrierReceipt struct {
	SchemaVersion        int    `json:"schemaVersion"`
	Profile              string `json:"profile"`
	Operation            string `json:"operation"`
	SourceSHA            string `json:"sourceSha"`
	OriginalCIRunID      string `json:"originalCIRunId"`
	OriginalCIRunAttempt uint64 `json:"originalCIRunAttempt"`
	ArchiveSHA256        string `json:"archiveSha256"`
	ArchiveBytes         int64  `json:"archiveBytes"`
	HandoffSHA256        string `json:"handoffSha256"`
	OriginalFiles        int    `json:"originalFiles"`
	ForgeAdmission       bool   `json:"forgeAdmission"`
	SourceOfferPublished bool   `json:"sourceOfferPublished"`
	ImagePublished       bool   `json:"imagePublished"`
	SignatureVerified    bool   `json:"signatureVerified"`
	PortalActivated      bool   `json:"portalActivated"`
}

func carrierPaths() []string {
	return append(append([]string{}, handoffWitnesses...), "zitadel-offline-replay/inputs.tar", "zitadel-base-source/base-source.tar", "zitadel-image/candidate-image.tar")
}

func carrierManifest(body []byte) (handoffDocument, error) {
	var doc handoffDocument
	if err := sourceJSON(body, &doc); err != nil {
		return doc, err
	}
	canonical, err := json.MarshalIndent(doc, "", "  ")
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || doc.SchemaVersion != 1 ||
		doc.Profile != "identity-publication-handoff-v1" || doc.SourceRepository != "devantler-tech/business-site" ||
		doc.WorkflowPath != ".github/workflows/ci.yaml" || !handoffRevision.MatchString(doc.SourceSHA) ||
		!handoffRun.MatchString(doc.OriginalCIRunID) || doc.OriginalCIRunAttempt == 0 || doc.OriginalCIRunAttempt > 1000000 ||
		doc.ForgeAdmission || doc.SourceOfferPublished || doc.ImagePublished || doc.SignatureVerified || doc.PortalActivated {
		return doc, fmt.Errorf("closed original handoff required")
	}
	paths := carrierPaths()
	if len(doc.Files) != len(paths) {
		return doc, fmt.Errorf("complete original handoff files required")
	}
	total := int64(len(body))
	for i, f := range doc.Files {
		bound := int64(8 << 20)
		switch i {
		case len(paths) - 3:
			bound = carrierLimit
		case len(paths) - 2:
			bound = 16 << 20
		case len(paths) - 1:
			bound = 1 << 30
		}
		if f.Path != paths[i] || f.Bytes < 1 || f.Bytes > bound || !sourceDigest.MatchString(f.SHA256) ||
			(f.Mode != 0444 && f.Mode != 0600 && f.Mode != 0644) {
			return doc, fmt.Errorf("original carrier role differs")
		}
		if i == len(paths)-3 && f.Mode != 0600 || i >= len(paths)-2 && f.Mode != 0444 {
			return doc, fmt.Errorf("original archive mode differs")
		}
		total += f.Bytes + 1024
		if total > carrierLimit-4096 {
			return doc, fmt.Errorf("original carrier exceeds 1.25 GiB")
		}
	}
	return doc, nil
}

func carrierJoinRoot(input string, doc handoffDocument, body []byte) error {
	observed, err := observeHandoff(input)
	if err != nil {
		return err
	}
	observed.SourceSHA, observed.OriginalCIRunID, observed.OriginalCIRunAttempt = doc.SourceSHA, doc.OriginalCIRunID, doc.OriginalCIRunAttempt
	canonical, err := json.MarshalIndent(observed, "", "  ")
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) {
		return fmt.Errorf("original archive/witness observation differs")
	}
	return nil
}

func carrierHeader(f sourceFile) *tar.Header {
	return &tar.Header{Name: f.Path, Mode: int64(f.Mode), Size: f.Bytes, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
}

func carrierOriginal(root *os.Root, f sourceFile, writer io.Writer) (resultErr error) {
	st, err := root.Lstat(f.Path)
	if err != nil || !st.Mode().IsRegular() || !sourceSingleLink(st) || st.Size() != f.Bytes || uint32(st.Mode().Perm()) != f.Mode {
		return fmt.Errorf("original carrier member metadata differs")
	}
	file, err := sourceOpenRegular(root, f.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	opened, err := file.Stat()
	if err != nil || !sameHandoffSnapshot(st, opened) {
		return fmt.Errorf("original changed before carrier copy")
	}
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(writer, hash), io.LimitReader(file, f.Bytes+1), make([]byte, 1<<20))
	current, statErr := file.Stat()
	entry, entryErr := root.Lstat(f.Path)
	if err != nil || statErr != nil || entryErr != nil || n != f.Bytes || fmt.Sprintf("%x", hash.Sum(nil)) != f.SHA256 ||
		!sameHandoffSnapshot(st, current) || !sameHandoffSnapshot(st, entry) {
		return fmt.Errorf("original changed or failed during carrier copy")
	}
	return nil
}

func packCarrier(input, output string) (receipt carrierReceipt, resultErr error) {
	if !filepath.IsAbs(input) || filepath.Clean(input) != input {
		return receipt, fmt.Errorf("physical original root required")
	}
	before, err := os.Lstat(input)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return receipt, fmt.Errorf("physical original root required")
	}
	root, err := os.OpenRoot(input)
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	manifest, err := readHandoffSnapshot(root, carrierHandoff, 1<<20, true, 0444)
	if err != nil {
		return receipt, err
	}
	doc, err := carrierManifest(manifest.data)
	if err != nil {
		return receipt, err
	}
	// Refuse excessive or substituted sizes before streaming the large originals.
	for _, f := range doc.Files {
		st, err := root.Lstat(f.Path)
		if err != nil || !st.Mode().IsRegular() || st.Size() != f.Bytes {
			return receipt, fmt.Errorf("original carrier length differs")
		}
	}
	if err = carrierJoinRoot(input, doc, manifest.data); err != nil {
		return receipt, err
	}
	name, err := sourceOutputPath(output)
	if err != nil {
		return receipt, err
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return receipt, err
	}
	created, err := file.Stat()
	if err != nil {
		return receipt, errors.Join(err, file.Close())
	}
	closed, success := false, false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if !success || resultErr != nil {
			resultErr = errors.Join(resultErr, removeSourceOutput(name, created))
		}
	}()
	hash := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(file, hash))
	files := append([]sourceFile{manifest.file}, doc.Files...)
	for _, f := range files {
		if err = tw.WriteHeader(carrierHeader(f)); err != nil {
			return receipt, err
		}
		if err = carrierOriginal(root, f, tw); err != nil {
			return receipt, err
		}
	}
	if err = tw.Close(); err != nil {
		return receipt, err
	}
	if err = carrierJoinRoot(input, doc, manifest.data); err != nil {
		return receipt, err
	}
	if err = file.Sync(); err != nil {
		return receipt, err
	}
	if err = file.Chmod(0444); err != nil {
		return receipt, err
	}
	final, err := file.Stat()
	if err != nil || final.Size() > carrierLimit {
		return receipt, fmt.Errorf("completed carrier bound differs")
	}
	err = file.Close()
	closed = true
	if err != nil {
		return receipt, err
	}
	parent, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	actual, err := readHandoffSnapshot(parent, filepath.Base(name), carrierLimit, false, 0444)
	if err != nil || !sameHandoffSnapshot(final, actual.info) || actual.file.SHA256 != fmt.Sprintf("%x", hash.Sum(nil)) {
		return receipt, fmt.Errorf("actual carrier differs from written bytes")
	}
	current, err := os.Lstat(input)
	if err != nil || !os.SameFile(before, current) {
		return receipt, fmt.Errorf("original root changed during carrier observation")
	}
	receipt = carrierReceipt{SchemaVersion: 1, Profile: "identity-original-carrier-v1", Operation: "pack-carrier",
		SourceSHA: doc.SourceSHA, OriginalCIRunID: doc.OriginalCIRunID, OriginalCIRunAttempt: doc.OriginalCIRunAttempt,
		ArchiveSHA256: actual.file.SHA256, ArchiveBytes: actual.file.Bytes, HandoffSHA256: manifest.file.SHA256, OriginalFiles: 17}
	success = true
	return receipt, nil
}

func runCarrier(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "pack-carrier" && args[0] != "unpack-carrier" {
		return fmt.Errorf("explicit carrier operation required")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	input, output := f.String("input", "", "physical original or carrier"), f.String("output", "", "new private destination")
	var archive, manifest, source, run string
	var attempt uint64
	if args[0] == "unpack-carrier" {
		f.StringVar(&archive, "sha256", "", "external carrier seal")
		f.StringVar(&manifest, "manifest-sha256", "", "external original handoff seal")
		f.StringVar(&source, "source-sha", "", "caller-bound original source")
		f.StringVar(&run, "run-id", "", "caller-bound original run")
		f.Uint64Var(&attempt, "run-attempt", 0, "caller-bound original attempt")
	}
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected carrier argument")
	}
	var receipt carrierReceipt
	var err error
	if args[0] == "pack-carrier" {
		receipt, err = packCarrier(*input, *output)
	} else {
		receipt, err = unpackCarrier(*input, archive, manifest, source, run, attempt, *output)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(receipt)
}
