// Command zitadel-source-bundle seals explicit identity build source inputs.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const sourceMemberLimit int64 = 128 << 20
const sourceTreeLimit int64 = 1 << 30
const sourceMemberCount = 50000
const sourceManifestLimit = 16 << 20
const sourceArchiveLimit = sourceTreeLimit + sourceManifestLimit + int64(sourceMemberCount)*2048

// This complete tree identity is independently collected from the frozen
// preparer after the native API serialization change, before installation.
const sourcePreparedProfile = "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d"

// sourceModifiedOn dates the frozen modification set, not its later capture.
// Update it with that set; identical inputs must retain the same source seal.
const sourceModifiedOn = "2026-10-24"

type sourceCapturePolicy struct{ Prepared, OriginalAPI, SerialAPI, Preparation string }
type sourceCaptureSummary struct {
	ManifestSHA256        string `json:"manifestSha256"`
	PreparedProfileSHA256 string `json:"preparedProfileSha256"`
	Files                 int    `json:"files"`
	Records               int    `json:"records"`
	Bytes                 int64  `json:"bytes"`
	RecipeSHA256          string `json:"recipeSha256"`
}

// sourceRestoreBuild restores declared bytes only. Like unpack, it authenticates
// a closed snapshot against the caller's external seal; the native builder still
// has to admit its frozen profile and literal recipe/transformation identities.
// No command or destination supplied by a source record is executed.
func sourceRestoreBuild(snapshot, seal, output string) (summary sourceCaptureSummary, result error) {
	manifest, err := sourceVerifyTree(snapshot, seal)
	if err != nil || manifest.PreparedProfile == "" {
		return summary, fmt.Errorf("verified prepared build snapshot required")
	}
	recordNames := map[string]bool{
		"source-preparation.json":                       true,
		"api-project.original.json":                     true,
		"api-project.serial.json":                       true,
		"inputs.sha256":                                 true,
		"recipes/prepare-zitadel-source.sh":             true,
		"recipes/zitadel-source-inputs.sha256":          true,
		"recipes/build-zitadel-linux.sh":                true,
		"recipes/zitadel-source-bundle.go":              true,
		"recipes/zitadel-console-assets.go":             true,
		"recipes/zitadel-pnpm-source/index.mjs":         true,
		"recipes/zitadel-pnpm-source/package.json":      true,
		"recipes/zitadel-pnpm-source/package-lock.json": true,
		"ZITADEL-SOURCE.md":                             true,
		"modifications.json":                            true,
	}
	if len(manifest.Records) != len(recordNames) {
		return summary, fmt.Errorf("prepared build records are incomplete or unexpected")
	}
	for _, member := range manifest.Records {
		if !recordNames[member.Path] {
			return summary, fmt.Errorf("unexpected prepared build record")
		}
	}
	physicalInput, err := filepath.EvalSymlinks(snapshot)
	if err != nil {
		return summary, err
	}
	physicalInput, err = filepath.Abs(physicalInput)
	if err != nil {
		return summary, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return summary, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return summary, err
	}
	output = filepath.Join(parent, filepath.Base(output))
	if output == physicalInput || strings.HasPrefix(output, physicalInput+string(filepath.Separator)) {
		return summary, fmt.Errorf("build reservation must be outside the sealed snapshot")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return summary, err
	}
	defer func() {
		if result != nil {
			summary = sourceCaptureSummary{}
			result = errors.Join(result, os.RemoveAll(output))
		}
	}()
	copyMember := func(member sourceMember, inputName, outputName string) error {
		body, err := sourceReadAt(snapshot, inputName)
		if err != nil || int64(len(body)) != member.Bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return fmt.Errorf("prepared member changed during restoration: %s", inputName)
		}
		name := filepath.Join(output, filepath.FromSlash(outputName))
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return err
		}
		return sourceWrite(name, body, os.FileMode(member.Mode))
	}
	for _, member := range manifest.Files {
		if err := copyMember(member, "source/"+member.Path, "prepared/source/"+member.Path); err != nil {
			return summary, err
		}
		summary.Bytes += member.Bytes
	}
	for _, member := range manifest.Records {
		// Retain all literal records as audit inputs. Only the four fixed native
		// build records additionally enter the locations the original path uses.
		if err := copyMember(member, "records/"+member.Path, "prepared/records/"+member.Path); err != nil {
			return summary, err
		}
		target := ""
		switch member.Path {
		case "source-preparation.json":
			target = "prepared/source-preparation.json"
		case "api-project.original.json", "api-project.serial.json", "inputs.sha256":
			target = member.Path
		case "recipes/build-zitadel-linux.sh":
			summary.RecipeSHA256 = member.SHA256
		}
		if target != "" {
			if err := copyMember(member, "records/"+member.Path, target); err != nil {
				return summary, err
			}
		}
	}
	if _, err := sourceVerifyTree(snapshot, seal); err != nil {
		return summary, err
	}
	summary.ManifestSHA256, summary.PreparedProfileSHA256 = seal, manifest.PreparedProfile
	summary.Files, summary.Records = len(manifest.Files), len(manifest.Records)
	return summary, nil
}

// sourceCaptureBuild seals the pre-install source and its complete literal
// preparation/build records. Only its caller's fresh reservation is removed.
func sourceCaptureBuild(build, repository, output string, policy sourceCapturePolicy) (summary sourceCaptureSummary, result error) {
	seal, err := sourceSealTree(filepath.Join(build, "prepared/source"), output)
	if err != nil {
		return summary, err
	}
	defer func() {
		if result != nil {
			summary = sourceCaptureSummary{}
			result = errors.Join(result, os.RemoveAll(output))
		}
	}()
	if seal != policy.Prepared {
		return summary, fmt.Errorf("prepared source differs from frozen complete profile")
	}
	manifestBytes, err := sourceReadAt(output, "source-manifest.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(manifestBytes)) != seal {
		return summary, fmt.Errorf("prepared source manifest unavailable")
	}
	prepared, err := sourceDecodeManifest(manifestBytes)
	if err != nil {
		return summary, err
	}
	inputHashes := map[string]string{}
	for _, member := range prepared.Files {
		inputHashes[member.Path] = member.SHA256
	}
	var inputRecord strings.Builder
	for _, name := range []string{"go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "console/package.json", "proto/buf.lock"} {
		if !sourceDigest(inputHashes[name]) {
			return summary, fmt.Errorf("frozen build input missing")
		}
		fmt.Fprintf(&inputRecord, "%s  %s\n", inputHashes[name], name)
	}
	records := map[string][]byte{}
	for _, entry := range []struct{ root, name, retained string }{
		{build, "prepared/source-preparation.json", "source-preparation.json"},
		{build, "api-project.original.json", "api-project.original.json"},
		{build, "api-project.serial.json", "api-project.serial.json"},
		{build, "inputs.sha256", "inputs.sha256"},
		{repository, "scripts/prepare-zitadel-source.sh", "recipes/prepare-zitadel-source.sh"},
		{repository, "scripts/zitadel-source-inputs.sha256", "recipes/zitadel-source-inputs.sha256"},
		{repository, "scripts/build-zitadel-linux.sh", "recipes/build-zitadel-linux.sh"},
		{repository, "scripts/zitadel-source-bundle.go", "recipes/zitadel-source-bundle.go"},
		{repository, "scripts/zitadel-console-assets.go", "recipes/zitadel-console-assets.go"},
		{repository, "tools/zitadel-pnpm-source/index.mjs", "recipes/zitadel-pnpm-source/index.mjs"},
		{repository, "tools/zitadel-pnpm-source/package.json", "recipes/zitadel-pnpm-source/package.json"},
		{repository, "tools/zitadel-pnpm-source/package-lock.json", "recipes/zitadel-pnpm-source/package-lock.json"},
		{repository, "docs/ZITADEL-SOURCE.md", "ZITADEL-SOURCE.md"},
	} {
		body, err := sourceReadAt(entry.root, entry.name)
		if err != nil || len(body) == 0 || len(body) > sourceManifestLimit {
			return summary, fmt.Errorf("build source record unavailable: %s", entry.retained)
		}
		records[entry.retained] = body
	}
	if string(records["inputs.sha256"]) != inputRecord.String() {
		return summary, fmt.Errorf("build input record differs from the complete prepared source")
	}
	digest := func(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }
	if digest(records["source-preparation.json"]) != policy.Preparation || digest(records["api-project.original.json"]) != policy.OriginalAPI || digest(records["api-project.serial.json"]) != policy.SerialAPI {
		return summary, fmt.Errorf("frozen source transformation record differs")
	}
	var prep struct {
		UpstreamCommit      string `json:"upstreamCommit"`
		RecipeSHA256        string `json:"recipeSha256"`
		InputManifestSHA256 string `json:"inputManifestSha256"`
	}
	if err := json.Unmarshal(records["source-preparation.json"], &prep); err != nil || prep.UpstreamCommit != "8afa5dd37a8e723efb9eace8d09fd5c1cccef694" || prep.RecipeSHA256 != digest(records["recipes/prepare-zitadel-source.sh"]) || prep.InputManifestSHA256 != digest(records["recipes/zitadel-source-inputs.sha256"]) {
		return summary, fmt.Errorf("preparation is not bound to the retained recipes")
	}
	modifications := struct {
		SchemaVersion         int      `json:"schemaVersion"`
		ModifiedOn            string   `json:"modifiedOn"`
		UpstreamCommit        string   `json:"upstreamCommit"`
		PreparedProfileSHA256 string   `json:"preparedProfileSha256"`
		Modifications         []string `json:"modifications"`
	}{1, sourceModifiedOn, prep.UpstreamCommit, seal, []string{
		"Remove the four Ailerons font/style paths and their two imports.",
		"Refuse the optional JSONPath machine-ID metadata selector; preserve raw response and host/IP identification.",
		"Set apps/api/project.json generate-install.options.parallel to false; preserve all fourteen native install commands.",
	}}
	raw, err := json.MarshalIndent(modifications, "", "  ")
	if err != nil {
		return summary, err
	}
	records["modifications.json"] = append(raw, '\n')
	sealed, err := sourceAddRecords(output, seal, records)
	if err != nil {
		return summary, err
	}
	manifest, err := sourceVerifyTree(output, sealed)
	if err != nil {
		return summary, err
	}
	var total int64
	for _, member := range manifest.Files {
		total += member.Bytes
	}
	return sourceCaptureSummary{ManifestSHA256: sealed, PreparedProfileSHA256: seal, Files: len(manifest.Files), Records: len(manifest.Records), Bytes: total, RecipeSHA256: digest(records["recipes/build-zitadel-linux.sh"])}, nil
}

// sourceReadAt propagates checked file and root close failures together.
func sourceReadAt(root, name string) (body []byte, result error) {
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("linked or unavailable build record root")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			body = nil
			result = errors.Join(result, err)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(st, opened) {
		return nil, fmt.Errorf("build record root changed")
	}
	info, err := r.Lstat(name)
	if err != nil {
		return nil, err
	}
	return sourceRead(r, name, info)
}

type sourceMember struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type sourceManifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	Phase           string         `json:"phase"`
	Files           []sourceMember `json:"files"`
	PreparedProfile string         `json:"preparedProfileSha256,omitempty"`
	Records         []sourceMember `json:"records,omitempty"`
}

// sourceAllMembers retains separate source and build-record namespaces.
func sourceAllMembers(manifest sourceManifest) []sourceMember {
	var all []sourceMember
	for _, group := range []struct {
		prefix  string
		members []sourceMember
	}{{"source/", manifest.Files}, {"records/", manifest.Records}} {
		for _, member := range group.members {
			member.Path = group.prefix + member.Path
			all = append(all, member)
		}
	}
	return all
}

// sourceAddRecords extends a newly owned snapshot with literal checked build
// inputs. The prepared file identity remains separate from this external seal.
func sourceAddRecords(root, seal string, records map[string][]byte) (string, error) {
	manifest, err := sourceVerifyTree(root, seal)
	if err != nil || len(manifest.Records) != 0 || len(records) == 0 || len(records) > 64 {
		return "", fmt.Errorf("invalid build record input")
	}
	if err := os.Mkdir(filepath.Join(root, "records"), 0755); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(records))
	for name := range records {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	manifest.PreparedProfile = seal
	for _, name := range keys {
		body := records[name]
		if !sourcePath(name) || len(body) > sourceManifestLimit {
			return "", fmt.Errorf("invalid build record")
		}
		destination := filepath.Join(root, "records", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return "", err
		}
		if err := sourceWrite(destination, body, 0644); err != nil {
			return "", err
		}
		manifest.Records = append(manifest.Records, sourceMember{Path: name, Bytes: int64(len(body)), SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Mode: 0644})
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if _, err := sourceDecodeManifest(raw); err != nil {
		return "", err
	}
	if err := sourceWrite(filepath.Join(root, "source-manifest.pending"), raw, 0444); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(root, "source-manifest.pending"), filepath.Join(root, "source-manifest.json")); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

// main preserves the explicit command's checked success or refusal status.
func main() { os.Exit(sourceBundleRun(os.Args[1:], os.Stdout, os.Stderr)) }

// sourceBundleRun is inert until one supported explicit operation is selected.
func sourceBundleRun(args []string, out, diagnostic io.Writer) int {
	if len(args) == 0 {
		return 0
	}
	var value any
	var err error
	switch {
	case len(args) == 4 && args[0] == "--restore-build":
		value, err = sourceRestoreBuild(args[1], args[2], args[3])
	case len(args) == 3 && args[0] == "--seal-inputs":
		var seal string
		seal, err = sourceSealTreePhase(args[1], args[2], "retained-build-inputs")
		value = struct {
			ManifestSHA256 string `json:"manifestSha256"`
		}{seal}
	case len(args) == 4 && args[0] == "--capture-build":
		value, err = sourceCaptureBuild(args[1], args[2], args[3], sourceCapturePolicy{
			sourcePreparedProfile,
			"bf332001875b5075845459450c72d98523c1db0923df40a04525740892c0e0e6",
			"6f1f58f338a3e3fd6c65bcc42c5b2bc56b71a7affc758e14e096c122f28a5455",
			"2ca8257ddee934a97d72793ade409cbe9eb14b9824fc610710c0485ed86beca2",
		})
	case len(args) == 3 && args[0] == "--seal-prepared":
		var seal string
		seal, err = sourceSealTree(args[1], args[2])
		if err == nil && seal != sourcePreparedProfile {
			err = errors.Join(fmt.Errorf("prepared source differs from frozen complete profile"), os.RemoveAll(args[2]))
		}
		value = struct {
			ManifestSHA256 string `json:"manifestSha256"`
		}{seal}
	case len(args) == 3 && args[0] == "--verify":
		value, err = sourceVerifyTree(args[1], args[2])
	case len(args) == 4 && args[0] == "--pack":
		var digest string
		digest, err = sourcePackTree(args[1], args[2], args[3])
		value = struct {
			ArchiveSHA256  string `json:"archiveSha256"`
			ManifestSHA256 string `json:"manifestSha256"`
		}{digest, args[2]}
	case len(args) == 5 && args[0] == "--unpack":
		err = sourceUnpackTree(args[1], args[2], args[3], args[4])
		value = struct {
			ArchiveSHA256  string `json:"archiveSha256"`
			ManifestSHA256 string `json:"manifestSha256"`
		}{args[2], args[3]}
	default:
		err = fmt.Errorf("unsupported explicit operation")
	}
	if err == nil {
		err = json.NewEncoder(out).Encode(value)
	}
	if err != nil {
		fmt.Fprintf(diagnostic, "source-bundle: %v\n", err)
		return 1
	}
	return 0
}

// sourceDigest accepts a complete lowercase SHA-256 identity only.
func sourceDigest(value string) bool {
	return len(value) == 64 && !strings.ContainsFunc(value, func(c rune) bool {
		return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f')
	})
}

// sourcePackTree emits a closed, deterministic USTAR stream. Its digest is
// external to the archive and never changes the captured member manifest.
func sourcePackTree(root, seal, archive string) (digest string, result error) {
	owned := false
	defer func() {
		if owned && result != nil {
			digest = ""
			result = errors.Join(result, os.Remove(archive))
		}
	}()
	manifest, err := sourceVerifyTree(root, seal)
	if err != nil {
		return "", err
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	physicalRoot, err = filepath.Abs(physicalRoot)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(archive))
	if err != nil {
		return "", err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	archive = filepath.Join(parent, filepath.Base(archive))
	if archive == physicalRoot || strings.HasPrefix(archive, physicalRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("archive output must be outside the sealed tree")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := r.Close(); err != nil {
			digest = ""
			result = errors.Join(result, err)
		}
	}()
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	owned = true
	defer func() {
		result = errors.Join(result, f.Close())
		if result != nil {
			digest = ""
		}
	}()
	hash := sha256.New()
	w := tar.NewWriter(io.MultiWriter(f, hash))
	names := []sourceMember{{Path: "source-manifest.json", Mode: 0444}}
	names = append(names, sourceAllMembers(manifest)...)
	for _, member := range names {
		info, err := r.Lstat(member.Path)
		if err != nil {
			return "", err
		}
		body, err := sourceRead(r, member.Path, info)
		if err != nil {
			return "", err
		}
		if member.Path == "source-manifest.json" {
			if fmt.Sprintf("%x", sha256.Sum256(body)) != seal {
				return "", fmt.Errorf("source manifest changed during packing")
			}
		} else if info.Size() != member.Bytes || uint32(info.Mode()) != member.Mode || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return "", fmt.Errorf("source changed during packing")
		}
		header := sourceHeader(member.Path, int64(len(body)), int64(member.Mode))
		if err := w.WriteHeader(header); err != nil {
			return "", err
		}
		if n, err := w.Write(body); err != nil || n != len(body) {
			return "", errors.Join(fmt.Errorf("incomplete source archive write"), err)
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// sourceHeader fixes metadata that cannot select files or execute commands.
func sourceHeader(name string, size, mode int64) *tar.Header {
	return &tar.Header{Name: name, Mode: mode, Size: size, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
}

// sourceUnpackTree admits only the ordered manifest and its exact regular
// members into a new reservation. Any failure removes only that reservation.
func sourceUnpackTree(archive, digest, seal, output string) (result error) {
	owned := false
	defer func() {
		if owned && result != nil {
			result = errors.Join(result, os.RemoveAll(output))
		}
	}()
	if !sourceDigest(digest) || !sourceDigest(seal) {
		return fmt.Errorf("invalid external source identity")
	}
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1024 || info.Size() > sourceArchiveLimit {
		return fmt.Errorf("invalid source archive")
	}
	f, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != info.Size() {
		return fmt.Errorf("source archive changed")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	owned = true
	hash := sha256.New()
	stream := io.TeeReader(io.LimitReader(f, sourceArchiveLimit+1), hash)
	r := tar.NewReader(stream)
	header, err := r.Next()
	if err != nil || header.Name != "source-manifest.json" || header.Size < 1 || header.Size > sourceManifestLimit {
		return fmt.Errorf("source manifest must be the first bounded archive member")
	}
	if err := sourceCheckHeader(header, "source-manifest.json", header.Size, 0444); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(r, sourceManifestLimit+1))
	if err != nil || int64(len(raw)) != header.Size || fmt.Sprintf("%x", sha256.Sum256(raw)) != seal {
		return fmt.Errorf("source manifest archive identity differs")
	}
	manifest, err := sourceDecodeManifest(raw)
	if err != nil {
		return err
	}
	// Reconstruct the admitted stream independently of the tar reader. Readers
	// skip padding and unused header bytes; those cannot carry hidden payload.
	canonicalHash := sha256.New()
	canonical := tar.NewWriter(canonicalHash)
	if err := canonical.WriteHeader(sourceHeader("source-manifest.json", int64(len(raw)), 0444)); err != nil {
		return err
	}
	if n, err := canonical.Write(raw); err != nil || n != len(raw) {
		return errors.Join(fmt.Errorf("incomplete canonical source manifest"), err)
	}
	if err := sourceWrite(filepath.Join(output, "source-manifest.json"), raw, 0444); err != nil {
		return err
	}
	for _, member := range sourceAllMembers(manifest) {
		name := member.Path
		header, err := r.Next()
		if err != nil {
			return fmt.Errorf("missing source archive member: %s: %w", name, err)
		}
		if err := sourceCheckHeader(header, name, member.Bytes, int64(member.Mode)); err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(r, member.Bytes+1))
		if err != nil || int64(len(body)) != member.Bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return fmt.Errorf("incomplete or altered archived source: %s", name)
		}
		if err := canonical.WriteHeader(sourceHeader(name, member.Bytes, int64(member.Mode))); err != nil {
			return err
		}
		if n, err := canonical.Write(body); err != nil || n != len(body) {
			return errors.Join(fmt.Errorf("incomplete canonical source member"), err)
		}
		destination := filepath.Join(output, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), sourceDirectoryMode(manifest.Phase)); err != nil {
			return err
		}
		if err := sourceWrite(destination, body, os.FileMode(member.Mode)); err != nil {
			return err
		}
	}
	if _, err := r.Next(); err != io.EOF {
		return fmt.Errorf("source archive has extra members or an invalid terminator")
	}
	var trailing [1]byte
	if n, err := stream.Read(trailing[:]); n != 0 || err != io.EOF {
		return fmt.Errorf("source archive has trailing input or a failed final read")
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != digest {
		return fmt.Errorf("source archive digest differs")
	}
	if err := canonical.Close(); err != nil {
		return err
	}
	if fmt.Sprintf("%x", canonicalHash.Sum(nil)) != digest {
		return fmt.Errorf("source archive has noncanonical headers or padding")
	}
	if _, err := sourceVerifyTree(output, seal); err != nil {
		return err
	}
	return nil
}

// sourceCheckHeader rejects metadata that the archive's member identity did not
// declare, including links, extended headers, permissions and ownership.
func sourceCheckHeader(got *tar.Header, name string, size, mode int64) error {
	if got.Name != name || got.Size != size || got.Mode != mode || got.Typeflag != tar.TypeReg || got.Linkname != "" || got.Uid != 0 || got.Gid != 0 || got.Uname != "" || got.Gname != "" || !got.ModTime.Equal(time.Unix(0, 0)) || !got.AccessTime.IsZero() || !got.ChangeTime.IsZero() || got.Devmajor != 0 || got.Devminor != 0 || len(got.PAXRecords) != 0 || len(got.Xattrs) != 0 || got.Format != tar.FormatUSTAR {
		return fmt.Errorf("undeclared source archive member or metadata: %s", name)
	}
	return nil
}

// sourceVerifyTree rechecks the complete closed snapshot without modifying it.
// Canonical JSON equality refuses duplicate/case-aliased keys and hidden fields.
func sourceVerifyTree(root, seal string) (verified sourceManifest, result error) {
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || !sourceDigest(seal) {
		return sourceManifest{}, fmt.Errorf("invalid sealed source root or digest")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return sourceManifest{}, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			verified = sourceManifest{}
			result = errors.Join(result, err)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(st, opened) {
		return sourceManifest{}, fmt.Errorf("sealed source root changed")
	}
	meta, err := r.Lstat("source-manifest.json")
	if err != nil || !meta.Mode().IsRegular() || meta.Mode() != 0444 || meta.Size() < 1 || meta.Size() > sourceManifestLimit {
		return sourceManifest{}, fmt.Errorf("invalid source manifest file")
	}
	raw, err := sourceRead(r, "source-manifest.json", meta)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != seal {
		return sourceManifest{}, fmt.Errorf("source manifest digest differs or read failed")
	}
	manifest, err := sourceDecodeManifest(raw)
	if err != nil {
		return sourceManifest{}, err
	}
	wanted := map[string]sourceMember{}
	directories := map[string]bool{".": true, "source": true}
	for _, member := range sourceAllMembers(manifest) {
		name := member.Path
		wanted[name] = member
		for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
			directories[directory] = true
		}
	}
	seen := 0
	if err := fs.WalkDir(r.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() && directories[name] {
			if manifest.Phase == "retained-build-inputs" && info.Mode() != os.ModeDir|0700 {
				return fmt.Errorf("retained input directory is not private")
			}
			return nil
		}
		if name == "source-manifest.json" {
			body, err := sourceRead(r, name, meta)
			if err != nil || !bytes.Equal(body, raw) {
				return fmt.Errorf("source manifest changed during readback")
			}
			return nil
		}
		member, found := wanted[name]
		if !found || !info.Mode().IsRegular() || uint32(info.Mode()) != member.Mode || info.Size() != member.Bytes {
			return fmt.Errorf("extra or changed sealed source member: %s", name)
		}
		body, err := sourceRead(r, name, info)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return fmt.Errorf("sealed source bytes differ or are unavailable: %s", name)
		}
		seen++
		return nil
	}); err != nil {
		return sourceManifest{}, err
	}
	if seen != len(wanted) {
		return sourceManifest{}, fmt.Errorf("sealed source membership is incomplete")
	}
	return manifest, nil
}

// sourceDecodeManifest admits a canonical complete member declaration before
// any archive-controlled name is used as an output path.
func sourceDecodeManifest(raw []byte) (sourceManifest, error) {
	var manifest sourceManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&manifest); err != nil {
		return sourceManifest{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return sourceManifest{}, fmt.Errorf("source manifest has trailing input")
	}
	canonical, err := json.MarshalIndent(manifest, "", "  ")
	retained := manifest.Phase == "retained-build-inputs"
	if err != nil || !bytes.Equal(append(canonical, '\n'), raw) || manifest.SchemaVersion != 1 || (!retained && manifest.Phase != "prepared-before-install") || len(manifest.Files) == 0 || len(manifest.Files) > sourceMemberCount {
		return sourceManifest{}, fmt.Errorf("noncanonical or unsupported source manifest")
	}
	if retained && (len(manifest.Records) != 0 || manifest.PreparedProfile != "") {
		return sourceManifest{}, fmt.Errorf("input transport cannot declare prepared build records")
	}
	components := sourceInputComponents()
	previous := ""
	var total int64
	if len(manifest.Records) > 64 || (len(manifest.Records) == 0 && manifest.PreparedProfile != "") {
		return sourceManifest{}, fmt.Errorf("invalid source build record profile")
	}
	if len(manifest.Records) > 0 {
		original := sourceManifest{SchemaVersion: 1, Phase: "prepared-before-install", Files: manifest.Files}
		base, err := json.MarshalIndent(original, "", "  ")
		if err != nil || !sourceDigest(manifest.PreparedProfile) || fmt.Sprintf("%x", sha256.Sum256(append(base, '\n'))) != manifest.PreparedProfile {
			return sourceManifest{}, fmt.Errorf("build records differ from prepared source identity")
		}
	}
	for _, member := range sourceAllMembers(manifest) {
		// Each namespace has its own sorted order, while archive order remains
		// source then records. Neither can alias the other.
		if strings.HasPrefix(member.Path, "records/") && strings.HasPrefix(previous, "source/") {
			previous = ""
		}
		if strings.HasPrefix(member.Path, "records/") && member.Mode != 0644 {
			return sourceManifest{}, fmt.Errorf("executable build record is unsupported")
		}
		mode := member.Mode == 0644 || member.Mode == 0755 || (retained && (member.Mode == 0600 || member.Mode == 0444))
		if !sourcePath(member.Path) || member.Path <= previous || member.Bytes < 0 || member.Bytes > sourceMemberLimit || member.Bytes > sourceTreeLimit-total || !sourceDigest(member.SHA256) || !mode {
			return sourceManifest{}, fmt.Errorf("invalid or unordered source member")
		}
		if retained {
			name, nested, _ := strings.Cut(strings.TrimPrefix(member.Path, "source/"), "/")
			if _, found := components[name]; !found || (name == "build-receipt.json") != (nested == "") {
				return sourceManifest{}, fmt.Errorf("unexpected retained input component")
			}
			components[name] = true
		}
		previous = member.Path
		total += member.Bytes
	}
	if retained {
		for _, found := range components {
			if !found {
				return sourceManifest{}, fmt.Errorf("retained input component is missing")
			}
		}
	}
	return manifest, nil
}

// sourceInputComponents is the transport allow-list, not dependency admission.
// Each sealed capsule still requires its maintained native verifier before use.
func sourceInputComponents() map[string]bool {
	return map[string]bool{
		"build-receipt.json": false, "source-snapshot": false,
		"selected-go-sources": false, "selected-generator-go-sources": false,
		"go-graph-metadata": false, "selected-pnpm-sources": false,
		"selected-bsr-sources": false, "selected-console-originals": false,
	}
}

// sourceDirectoryMode reconstructs the native private capsule layout without
// changing the existing prepared-source profile or its executable file modes.
func sourceDirectoryMode(phase string) fs.FileMode {
	if phase == "retained-build-inputs" {
		return 0700
	}
	return 0755
}

// sourcePath admits a canonical archive-relative identity without interpreting it.
func sourcePath(name string) bool {
	return name != "" && len(name) <= 4096 && name != "." && name != ".." && path.Clean(name) == name &&
		!path.IsAbs(name) && !strings.HasPrefix(name, "../") && !strings.Contains(name, "\\") &&
		utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl)
}

// sourceRead retains a complete regular file, including a valid empty source.
// Every path component and the opened file must agree with the inspected tree.
func sourceRead(root *os.Root, name string, expected fs.FileInfo) ([]byte, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		st, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || st.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !st.IsDir()) {
			return nil, fmt.Errorf("linked or missing source component: %s", name)
		}
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, statErr := f.Stat()
	if statErr != nil || !st.Mode().IsRegular() || !os.SameFile(st, expected) || st.Mode() != expected.Mode() || st.Size() != expected.Size() || st.Size() < 0 || st.Size() > sourceMemberLimit {
		return nil, errors.Join(fmt.Errorf("source type, identity, mode or size changed: %s", name), statErr, f.Close())
	}
	return sourceReadBody(f, st.Size())
}

// sourceReadBody keeps complete-read and close errors on the same observation.
func sourceReadBody(reader io.ReadCloser, expected int64) ([]byte, error) {
	if expected < 0 || expected > sourceMemberLimit {
		return nil, errors.Join(fmt.Errorf("unsupported source size"), reader.Close())
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, sourceMemberLimit+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(body)) != expected {
		return nil, errors.Join(fmt.Errorf("incomplete source read"), readErr, closeErr)
	}
	return body, nil
}

// sourceWrite creates one owned output, preserving exact bytes and declared mode.
func sourceWrite(name string, body []byte, mode os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	return sourceWriteComplete(f, body, mode)
}

type sourceOutput interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

// sourceWriteComplete preserves every write, metadata, flush and close error.
func sourceWriteComplete(output sourceOutput, body []byte, mode os.FileMode) error {
	n, writeErr := output.Write(body)
	modeErr := output.Chmod(mode)
	syncErr := output.Sync()
	closeErr := output.Close()
	if n != len(body) || writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("incomplete source output"), writeErr, modeErr, syncErr, closeErr)
	}
	return nil
}

// sourceSealTree copies only inspected regular members into a fresh reservation.
// Its manifest seals tree membership, original bytes and executable modes; the
// caller separately binds this mechanism to the frozen preparation profile.
func sourceSealTree(root, output string) (seal string, result error) {
	return sourceSealTreePhase(root, output, "prepared-before-install")
}

// sourceSealTreePhase shares the closed archive engine while keeping retained
// capsule modes separate from the immutable prepared-source profile.
func sourceSealTreePhase(root, output, phase string) (seal string, result error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source root is unavailable or linked")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return "", err
	}
	output = filepath.Join(parent, filepath.Base(output))
	if output == root || strings.HasPrefix(output, root+string(filepath.Separator)) {
		return "", fmt.Errorf("source reservation must be outside the input tree")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return "", err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, os.RemoveAll(output))
		}
	}()
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := r.Close(); err != nil {
			seal = ""
			result = errors.Join(result, err)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(st, opened) {
		return "", fmt.Errorf("source root changed")
	}
	members := map[string]fs.FileInfo{}
	components := sourceInputComponents()
	var total int64
	if err := fs.WalkDir(r.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if phase == "retained-build-inputs" {
			component, _, _ := strings.Cut(name, "/")
			if _, selected := components[component]; !selected {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if !sourcePath(name) {
			return fmt.Errorf("unclean source path")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		mode := info.Mode() == 0644 || info.Mode() == 0755 || (phase == "retained-build-inputs" && (info.Mode() == 0600 || info.Mode() == 0444))
		if !info.Mode().IsRegular() || !mode || info.Size() < 0 || info.Size() > sourceMemberLimit || len(members) >= sourceMemberCount || info.Size() > sourceTreeLimit-total {
			return fmt.Errorf("unsupported source member or tree size: %s", name)
		}
		members[name] = info
		total += info.Size()
		return nil
	}); err != nil {
		return "", err
	}
	if len(members) == 0 {
		return "", fmt.Errorf("empty source tree")
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	manifest := sourceManifest{SchemaVersion: 1, Phase: phase}
	for _, name := range names {
		body, err := sourceRead(r, name, members[name])
		if err != nil {
			return "", err
		}
		destination := filepath.Join(output, "source", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), sourceDirectoryMode(phase)); err != nil {
			return "", err
		}
		mode := members[name].Mode()
		if err := sourceWrite(destination, body, mode); err != nil {
			return "", err
		}
		manifest.Files = append(manifest.Files, sourceMember{Path: name, Bytes: int64(len(body)), SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Mode: uint32(mode)})
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > sourceManifestLimit {
		return "", fmt.Errorf("source manifest exceeds limit")
	}
	if _, err := sourceDecodeManifest(encoded); err != nil {
		return "", err
	}
	if err := sourceWrite(filepath.Join(output, "source-manifest.json"), encoded, 0444); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
