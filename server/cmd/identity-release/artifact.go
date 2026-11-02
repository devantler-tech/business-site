package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// restoreArtifact consumes the digest from the authenticated Actions artifact
// record. ZIP metadata never chooses a destination or an executable.
func restoreArtifact(input, seal, source, run string, attempt uint64, output string) (receipt carrierReceipt, resultErr error) {
	if !filepath.IsAbs(input) || filepath.Clean(input) != input || !sourceDigest.MatchString(seal) ||
		!handoffRevision.MatchString(source) || !handoffRun.MatchString(run) || attempt == 0 || attempt > 1000000 {
		return receipt, fmt.Errorf("external artifact digest and original caller binding required")
	}
	sealed, err := openSealedArchive(input, seal, carrierLimit+8<<20)
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, sealed.close()) }()
	file, st := sealed.file, sealed.original.info
	zr, err := zip.NewReader(file, st.Size())
	if err != nil || len(zr.File) != 2 {
		return receipt, fmt.Errorf("closed two-file Actions artifact required")
	}
	members := map[string]*zip.File{}
	for _, member := range zr.File {
		limit := uint64(carrierLimit)
		if member.Name == "carrier.json" {
			limit = 1 << 20
		} else if member.Name != "carrier.tar" {
			return receipt, fmt.Errorf("unexpected artifact role")
		}
		if members[member.Name] != nil || !member.Mode().IsRegular() || member.UncompressedSize64 == 0 || member.UncompressedSize64 > limit ||
			member.CompressedSize64 > uint64(st.Size()) || (member.Method != zip.Store && member.Method != zip.Deflate) {
			return receipt, fmt.Errorf("invalid artifact member")
		}
		members[member.Name] = member
	}
	metadata, err := members["carrier.json"].Open()
	if err != nil {
		return receipt, err
	}
	body, readErr := io.ReadAll(io.LimitReader(metadata, 1<<20+1))
	closeErr := metadata.Close()
	if readErr != nil || closeErr != nil || uint64(len(body)) != members["carrier.json"].UncompressedSize64 {
		return receipt, fmt.Errorf("incomplete artifact receipt")
	}
	if err = sourceJSON(body, &receipt); err != nil {
		return receipt, err
	}
	// Reject unknown fields as well as forged authority. The Actions digest binds
	// this receipt; the caller independently supplies its run and source identity.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil {
		return receipt, err
	}
	for _, key := range []string{"schemaVersion", "profile", "operation", "sourceSha", "originalCIRunId", "originalCIRunAttempt", "archiveSha256", "archiveBytes", "handoffSha256", "originalFiles", "forgeAdmission", "sourceOfferPublished", "imagePublished", "signatureVerified", "portalActivated"} {
		if fields[key] == nil {
			return receipt, fmt.Errorf("missing original artifact receipt field")
		}
	}
	for _, key := range []string{"forgeAdmission", "sourceOfferPublished", "imagePublished", "signatureVerified", "portalActivated"} {
		if string(fields[key]) != "false" {
			return receipt, fmt.Errorf("explicit false transport authority required")
		}
	}
	if err = json.Unmarshal(body, &fields); err != nil || len(fields) != 15 || receipt.SchemaVersion != 1 || receipt.Profile != "identity-original-carrier-v1" ||
		receipt.Operation != "pack-carrier" || receipt.SourceSHA != source || receipt.OriginalCIRunID != run || receipt.OriginalCIRunAttempt != attempt ||
		receipt.ArchiveBytes != int64(members["carrier.tar"].UncompressedSize64) || !sourceDigest.MatchString(receipt.ArchiveSHA256) ||
		!sourceDigest.MatchString(receipt.HandoffSHA256) || receipt.OriginalFiles != 17 || receipt.ForgeAdmission || receipt.SourceOfferPublished || receipt.ImagePublished || receipt.SignatureVerified || receipt.PortalActivated {
		return receipt, fmt.Errorf("original artifact receipt differs")
	}
	name, root, err := reserveSourceRoot(output)
	if err != nil {
		return receipt, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	archive, err := root.OpenFile("carrier.tar", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return receipt, err
	}
	member, err := members["carrier.tar"].Open()
	if err != nil {
		return receipt, errors.Join(err, archive.Close())
	}
	hash := sha256.New()
	n, copyErr := io.CopyBuffer(io.MultiWriter(archive, hash), io.LimitReader(member, carrierLimit+1), make([]byte, 1<<20))
	memberErr, syncErr, modeErr, archiveErr := member.Close(), archive.Sync(), archive.Chmod(0444), archive.Close()
	if copyErr != nil || memberErr != nil || syncErr != nil || modeErr != nil || archiveErr != nil || n != receipt.ArchiveBytes || fmt.Sprintf("%x", hash.Sum(nil)) != receipt.ArchiveSHA256 {
		return receipt, fmt.Errorf("incomplete or changed original carrier")
	}
	if err = sealed.unchanged(); err != nil {
		return receipt, err
	}
	return unpackCarrier(filepath.Join(name, "carrier.tar"), receipt.ArchiveSHA256, receipt.HandoffSHA256, source, run, attempt, filepath.Join(name, "native"))
}

func runArtifact(args []string, out io.Writer) error {
	f := flag.NewFlagSet("restore-artifact", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	input, seal := f.String("input", "", "original Actions ZIP"), f.String("sha256", "", "authenticated Actions digest")
	source, run := f.String("source-sha", "", "original source"), f.String("run-id", "", "original CI run")
	attempt, output := f.Uint64("run-attempt", 0, "original attempt"), f.String("output", "", "new private destination")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected artifact argument")
	}
	receipt, err := restoreArtifact(*input, *seal, *source, *run, *attempt, *output)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(receipt)
}
