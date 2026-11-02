package main

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func carrierRestoreMember(root *os.Root, tr io.Reader, canonical io.Writer, spec sourceFile) (resultErr error) {
	file, err := root.OpenFile(spec.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(file, canonical, hash), io.LimitReader(tr, spec.Bytes+1), make([]byte, 1<<20))
	if err != nil || n != spec.Bytes || fmt.Sprintf("%x", hash.Sum(nil)) != spec.SHA256 {
		return fmt.Errorf("original carrier member bytes differ")
	}
	if err = file.Sync(); err != nil {
		return err
	}
	return file.Chmod(os.FileMode(spec.Mode))
}

func carrierMatchesHeader(header *tar.Header, spec sourceFile) bool {
	return header != nil && header.Name == spec.Path && header.Typeflag == tar.TypeReg && header.Size == spec.Bytes && header.Mode == int64(spec.Mode)
}

func unpackCarrier(input, archiveSeal, manifestSeal, source, run string, attempt uint64, output string) (receipt carrierReceipt, resultErr error) {
	if !filepath.IsAbs(input) || filepath.Clean(input) != input || !sourceDigest.MatchString(archiveSeal) ||
		!sourceDigest.MatchString(manifestSeal) || !handoffRevision.MatchString(source) || !handoffRun.MatchString(run) || attempt == 0 || attempt > 1000000 {
		return receipt, fmt.Errorf("external carrier/handoff seals and original caller identity required")
	}
	parent, err := os.OpenRoot(filepath.Dir(input))
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	original, err := readHandoffSnapshot(parent, filepath.Base(input), carrierLimit, false, 0444)
	if err != nil || original.file.SHA256 != archiveSeal {
		return receipt, fmt.Errorf("original carrier seal differs")
	}
	file, err := sourceOpenRegular(parent, filepath.Base(input))
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	opened, err := file.Stat()
	if err != nil || !sameHandoffSnapshot(original.info, opened) {
		return receipt, fmt.Errorf("carrier changed before restoration")
	}
	tr := tar.NewReader(file)
	header, err := tr.Next()
	if err != nil || header.Name != carrierHandoff || header.Typeflag != tar.TypeReg || header.Mode != 0444 || header.Size < 1 || header.Size > 1<<20 {
		return receipt, fmt.Errorf("original handoff must be the first carrier member")
	}
	body, err := io.ReadAll(io.LimitReader(tr, header.Size+1))
	if err != nil || int64(len(body)) != header.Size || sourceSHA(body) != manifestSeal {
		return receipt, fmt.Errorf("external original handoff seal differs")
	}
	doc, err := carrierManifest(body)
	if err != nil || doc.SourceSHA != source || doc.OriginalCIRunID != run || doc.OriginalCIRunAttempt != attempt {
		return receipt, fmt.Errorf("original handoff caller binding differs")
	}
	name, err := sourceOutputPath(output)
	if err != nil {
		return receipt, err
	}
	if err = os.Mkdir(name, 0700); err != nil {
		return receipt, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	for _, dir := range []string{"zitadel-handoff", "zitadel-build", "zitadel-image", "zitadel-offline-replay", "zitadel-offline-replay/native", "zitadel-base-source", "zitadel-base-source/evidence"} {
		if err = root.Mkdir(dir, 0700); err != nil {
			return receipt, err
		}
	}
	canonical := sha256.New()
	tw := tar.NewWriter(canonical)
	manifest := sourceFile{Path: carrierHandoff, Bytes: int64(len(body)), SHA256: manifestSeal, Mode: 0444}
	if err = tw.WriteHeader(carrierHeader(manifest)); err != nil {
		return receipt, err
	}
	if _, err = tw.Write(body); err != nil {
		return receipt, err
	}
	if err = writeSourceFile(root, carrierHandoff, body); err != nil {
		return receipt, err
	}
	for _, spec := range doc.Files {
		header, err = tr.Next()
		if err != nil || !carrierMatchesHeader(header, spec) {
			return receipt, fmt.Errorf("closed original carrier role differs: %s", spec.Path)
		}
		if err = tw.WriteHeader(carrierHeader(spec)); err != nil {
			return receipt, err
		}
		if err = carrierRestoreMember(root, tr, tw, spec); err != nil {
			return receipt, err
		}
	}
	if _, err = tr.Next(); err != io.EOF {
		return receipt, fmt.Errorf("extra or malformed original carrier member")
	}
	if err = tw.Close(); err != nil {
		return receipt, err
	}
	if fmt.Sprintf("%x", canonical.Sum(nil)) != archiveSeal {
		return receipt, fmt.Errorf("noncanonical original carrier")
	}
	current, err := file.Stat()
	entry, entryErr := parent.Lstat(filepath.Base(input))
	if err != nil || entryErr != nil || !sameHandoffSnapshot(original.info, current) || !sameHandoffSnapshot(original.info, entry) {
		return receipt, fmt.Errorf("carrier changed during restoration")
	}
	if err = carrierJoinRoot(name, doc, body); err != nil {
		return receipt, err
	}
	receipt = carrierReceipt{SchemaVersion: 1, Profile: "identity-original-carrier-v1", Operation: "unpack-carrier",
		SourceSHA: source, OriginalCIRunID: run, OriginalCIRunAttempt: attempt, ArchiveSHA256: archiveSeal,
		ArchiveBytes: original.file.Bytes, HandoffSHA256: manifestSeal, OriginalFiles: 17}
	return receipt, nil
}
