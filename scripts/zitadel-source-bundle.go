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
const sourcePreparedProfile = "99857abf9171ae0dd25438a1006297f012de4a637be58fd41ff7af70852f9298"

// sourceModifiedOn dates the frozen modification set, not its later capture.
// Update it with that set; identical inputs must retain the same source seal.
const sourceModifiedOn = "2026-10-29"

type sourceCapturePolicy struct{ Prepared, OriginalAPI, SerialAPI, Preparation string }
type sourceCaptureSummary struct {
	ManifestSHA256        string `json:"manifestSha256"`
	PreparedProfileSHA256 string `json:"preparedProfileSha256"`
	Files                 int    `json:"files"`
	Records               int    `json:"records"`
	Bytes                 int64  `json:"bytes"`
	RecipeSHA256          string `json:"recipeSha256"`
}

type sourceRecordInput struct {
	repository     bool
	name, retained string
}

// sourceBuildRecordInputs is the closed capture map. Its records are inert audit
// data; neither the grant document nor archived bytes may add a path or command.
func sourceBuildRecordInputs() []sourceRecordInput {
	return []sourceRecordInput{
		{false, "prepared/source-preparation.json", "source-preparation.json"},
		{false, "api-project.original.json", "api-project.original.json"},
		{false, "api-project.serial.json", "api-project.serial.json"},
		{false, "inputs.sha256", "inputs.sha256"},
		{true, "scripts/prepare-zitadel-source.sh", "recipes/prepare-zitadel-source.sh"},
		{true, "scripts/zitadel-source-inputs.sha256", "recipes/zitadel-source-inputs.sha256"},
		{true, "scripts/build-zitadel-linux.sh", "recipes/build-zitadel-linux.sh"},
		{true, "scripts/zitadel-source-bundle.go", "recipes/zitadel-source-bundle.go"},
		{true, "scripts/zitadel-console-assets.go", "recipes/zitadel-console-assets.go"},
		{true, "scripts/zitadel-login-icons.go", "recipes/zitadel-login-icons.go"},
		{true, "scripts/zitadel-text-fonts.go", "recipes/zitadel-text-fonts.go"},
		{true, "scripts/zitadel-copied-assets.go", "recipes/zitadel-copied-assets.go"},
		{true, "third_party/zitadel-copied-assets/catalogue.json", "third_party/zitadel-copied-assets/catalogue.json"},
		{true, "third_party/zitadel-copied-assets/Line-Awesome-LICENSE.md", "third_party/zitadel-copied-assets/Line-Awesome-LICENSE.md"},
		{true, "third_party/zitadel-copied-assets/Material-Icons-LICENSE", "third_party/zitadel-copied-assets/Material-Icons-LICENSE"},
		{true, "third_party/zitadel-copied-assets/Flag-Icons-LICENSE", "third_party/zitadel-copied-assets/Flag-Icons-LICENSE"},
		{true, "third_party/zitadel-copied-assets/ZITADEL-LICENSE", "third_party/zitadel-copied-assets/ZITADEL-LICENSE"},
		{true, "third_party/zitadel-copied-assets/ZITADEL-LICENSING.md", "third_party/zitadel-copied-assets/ZITADEL-LICENSING.md"},
		{true, "third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md", "third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md"},
		{true, "third_party/zitadel-copied-assets/original-line-awesome.css", "third_party/zitadel-copied-assets/original-line-awesome.css"},
		{true, "third_party/zitadel-copied-assets/original-line-awesome.min.css", "third_party/zitadel-copied-assets/original-line-awesome.min.css"},
		{true, "third_party/zitadel-text-fonts/catalogue.json", "third_party/zitadel-text-fonts/catalogue.json"},
		{true, "third_party/zitadel-text-fonts/Apache-2.0.txt", "third_party/zitadel-text-fonts/Apache-2.0.txt"},
		{true, "third_party/zitadel-console-notices/catalogue.json", "third_party/zitadel-console-notices/catalogue.json"},
		{true, "third_party/zitadel-go-module-notices/incorporated/publicsuffix/public_suffix_list.dat", "third_party/zitadel-go-module-notices/incorporated/publicsuffix/public_suffix_list.dat"},
		{true, "tools/zitadel-pnpm-source/index.mjs", "recipes/zitadel-pnpm-source/index.mjs"},
		{true, "tools/zitadel-pnpm-source/package.json", "recipes/zitadel-pnpm-source/package.json"},
		{true, "tools/zitadel-pnpm-source/package-lock.json", "recipes/zitadel-pnpm-source/package-lock.json"},
		{true, "docs/ZITADEL-SOURCE.md", "ZITADEL-SOURCE.md"},
		{true, "scripts/replay-zitadel-inputs.sh", "scripts/replay-zitadel-inputs.sh"},
		{true, "scripts/zitadel-console-notices.go", "scripts/zitadel-console-notices.go"},
		{true, "server/go.mod", "server/go.mod"},
		{true, "server/go.sum", "server/go.sum"},
		{true, "server/cmd/zitadel-module-sources/capture.go", "server/cmd/zitadel-module-sources/capture.go"},
		{true, "server/cmd/zitadel-module-sources/generator_sources.go", "server/cmd/zitadel-module-sources/generator_sources.go"},
		{true, "server/cmd/zitadel-module-sources/generators.go", "server/cmd/zitadel-module-sources/generators.go"},
		{true, "server/cmd/zitadel-module-sources/graph_metadata.go", "server/cmd/zitadel-module-sources/graph_metadata.go"},
		{true, "server/cmd/zitadel-module-sources/json_fields.go", "server/cmd/zitadel-module-sources/json_fields.go"},
		{true, "server/cmd/zitadel-module-sources/main.go", "server/cmd/zitadel-module-sources/main.go"},
		{true, "server/cmd/zitadel-module-sources/payload.go", "server/cmd/zitadel-module-sources/payload.go"},
		{true, "server/cmd/zitadel-module-sources/restore.go", "server/cmd/zitadel-module-sources/restore.go"},
		{true, "server/cmd/zitadel-module-sources/selection.go", "server/cmd/zitadel-module-sources/selection.go"},
		{true, "server/cmd/zitadel-bsr-sources/files.go", "server/cmd/zitadel-bsr-sources/files.go"},
		{true, "server/cmd/zitadel-bsr-sources/main.go", "server/cmd/zitadel-bsr-sources/main.go"},
		{true, "server/cmd/zitadel-bsr-sources/package.go", "server/cmd/zitadel-bsr-sources/package.go"},
		{true, "server/cmd/zitadel-bsr-sources/validation.go", "server/cmd/zitadel-bsr-sources/validation.go"},
		{true, "Dockerfile.zitadel", "Dockerfile.zitadel"},
		{true, "scripts/zitadel-image-package.go", "scripts/zitadel-image-package.go"},
		{true, "scripts/zitadel-image-smoke.go", "scripts/zitadel-image-smoke.go"},
		{true, "scripts/zitadel-service-smoke.go", "scripts/zitadel-service-smoke.go"},
		{true, "scripts/zitadel-browser.go", "scripts/zitadel-browser.go"},
		{true, "server/cmd/zitadel-module-sources/README.md", "server/cmd/zitadel-module-sources/README.md"},
		{true, "server/cmd/zitadel-bsr-sources/README.md", "server/cmd/zitadel-bsr-sources/README.md"},
		{true, "tools/zitadel-pnpm-source/README.md", "tools/zitadel-pnpm-source/README.md"},
		{true, "third_party/zitadel-build-tools/COPYING", "third_party/zitadel-build-tools/COPYING"},
		{true, "third_party/zitadel-build-tools/README.md", "third_party/zitadel-build-tools/README.md"},
		{true, "third_party/zitadel-build-tools/scope.json", "third_party/zitadel-build-tools/scope.json"},
	}
}

// sourceBuildRecordNames also refuses duplicate sources and destination aliases,
// before either capture or restoration can silently replace a retained member.
func sourceBuildRecordNames() (map[string]bool, error) {
	names := map[string]bool{"modifications.json": true}
	seen := map[struct {
		repository bool
		name       string
	}]bool{}
	for _, entry := range sourceBuildRecordInputs() {
		key := struct {
			repository bool
			name       string
		}{entry.repository, entry.name}
		if !sourcePath(entry.name) || !sourcePath(entry.retained) || names[entry.retained] || seen[key] {
			return nil, fmt.Errorf("duplicate or invalid literal build record")
		}
		names[entry.retained], seen[key] = true, true
	}
	return names, nil
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
	recordNames, err := sourceBuildRecordNames()
	if err != nil {
		return summary, err
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

type sourceToolMember struct {
	input  sourceRecordInput
	member sourceMember
}

type sourceToolSummary struct {
	SchemaVersion            int            `json:"schemaVersion"`
	Profile                  string         `json:"profile"`
	SourceManifestSHA256     string         `json:"sourceManifestSha256"`
	ToolSourceSHA256         string         `json:"toolSourceSha256"`
	Members                  []sourceMember `json:"members"`
	ToolSourceReadbackPassed bool           `json:"toolSourceReadbackPassed"`
	GoVerifiersRebuilt       bool           `json:"goVerifiersRebuilt"`
}

// sourceToolPlan selects only the reviewed own-source entries in the literal
// capture table. Third-party source/notice inputs keep their separate owners.
// The independent grant control pins this set to 37 paths and three terms.
func sourceToolPlan(snapshot, seal string) ([]sourceToolMember, error) {
	manifest, err := sourceVerifyTree(snapshot, seal)
	if err != nil || manifest.PreparedProfile == "" {
		return nil, fmt.Errorf("verified prepared tool-source snapshot required")
	}
	names, err := sourceBuildRecordNames()
	if err != nil || len(manifest.Records) != len(names) {
		return nil, fmt.Errorf("complete literal tool-source records required")
	}
	records := map[string]sourceMember{}
	for _, member := range manifest.Records {
		if !names[member.Path] {
			return nil, fmt.Errorf("unexpected tool-source record")
		}
		records[member.Path] = member
	}
	var plan []sourceToolMember
	for _, entry := range sourceBuildRecordInputs() {
		if !entry.repository || (strings.HasPrefix(entry.name, "third_party/") && !strings.HasPrefix(entry.name, "third_party/zitadel-build-tools/")) {
			continue
		}
		member, ok := records[entry.retained]
		if !ok || member.Mode != 0644 {
			return nil, fmt.Errorf("approved tool-source input missing")
		}
		member.Path = entry.name
		plan = append(plan, sourceToolMember{entry, member})
	}
	if len(plan) != 40 {
		return nil, fmt.Errorf("the exact approved tool-source scope is required")
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].member.Path < plan[j].member.Path })
	return plan, nil
}

// sourceToolFiles checks closed membership and complete bodies through a
// confined root. Its output stays inert; it does not compile or run source.
func sourceToolFiles(root *os.Root, plan []sourceToolMember) error {
	wanted, directories := map[string]sourceMember{}, map[string]bool{".": true}
	for _, item := range plan {
		wanted[item.member.Path] = item.member
		for dir := path.Dir(item.member.Path); !directories[dir]; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	seen := 0
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			mode := os.FileMode(0755)
			if name == "." {
				mode = 0700
			}
			if !directories[name] || info.Mode() != os.ModeDir|mode {
				return fmt.Errorf("unexpected tool-source directory")
			}
			return nil
		}
		member, ok := wanted[name]
		if !ok || info.Mode() != 0644 {
			return fmt.Errorf("unexpected or non-inert tool-source member")
		}
		body, err := sourceRead(root, name, info)
		if err != nil || int64(len(body)) != member.Bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return fmt.Errorf("restored tool-source body differs")
		}
		seen++
		return nil
	})
	if err != nil || seen != len(wanted) {
		return errors.Join(fmt.Errorf("incomplete tool-source readback"), err)
	}
	return nil
}

// sourceToolSource uses fixed original paths in a new owned layout, separate
// from both audit records and the four native service restoration destinations.
func sourceToolSource(snapshot, seal, output string, restore bool) (summary sourceToolSummary, result error) {
	plan, err := sourceToolPlan(snapshot, seal)
	if err != nil {
		return summary, err
	}
	input, err := filepath.EvalSymlinks(snapshot)
	if err != nil {
		return summary, err
	}
	input, err = filepath.Abs(input)
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
	if output == input || strings.HasPrefix(output, input+string(filepath.Separator)) {
		return summary, fmt.Errorf("tool source must remain outside its sealed input")
	}
	if restore {
		if err := os.Mkdir(output, 0700); err != nil {
			return summary, err
		}
	}
	owned, err := os.Lstat(output)
	if err != nil || !owned.IsDir() || owned.Mode().Perm() != 0700 {
		return summary, fmt.Errorf("physical private tool-source root required")
	}
	if restore {
		defer func() {
			if result != nil {
				current, err := os.Lstat(output)
				if err == nil && os.SameFile(owned, current) {
					result = errors.Join(result, os.RemoveAll(output))
				} else {
					result = errors.Join(result, fmt.Errorf("tool-source reservation changed; replacement retained"))
				}
			}
		}()
	}
	r, err := os.OpenRoot(output)
	if err != nil {
		return summary, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			summary = sourceToolSummary{}
			result = errors.Join(result, err)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(owned, opened) {
		return summary, fmt.Errorf("tool-source reservation changed")
	}
	if restore {
		for _, item := range plan {
			body, err := sourceReadAt(snapshot, "records/"+item.input.retained)
			if err != nil || int64(len(body)) != item.member.Bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != item.member.SHA256 {
				return summary, fmt.Errorf("approved tool source changed during restoration")
			}
			if err := r.MkdirAll(path.Dir(item.member.Path), 0755); err != nil {
				return summary, err
			}
			f, err := r.OpenFile(item.member.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if err != nil {
				return summary, err
			}
			if err := sourceWriteComplete(f, body, 0644); err != nil {
				return summary, err
			}
		}
	}
	if _, err := sourceVerifyTree(snapshot, seal); err != nil {
		return summary, err
	}
	if err := sourceToolFiles(r, plan); err != nil {
		return summary, err
	}
	current, err := os.Lstat(output)
	if err != nil || !os.SameFile(owned, current) || current.Mode() != os.ModeDir|0700 {
		return summary, fmt.Errorf("tool-source root changed during readback")
	}
	members := make([]sourceMember, len(plan))
	for i, item := range plan {
		members[i] = item.member
	}
	raw, err := json.Marshal(members)
	if err != nil {
		return summary, err
	}
	return sourceToolSummary{SchemaVersion: 1, Profile: "identity-build-tool-source-v1", SourceManifestSHA256: seal,
		ToolSourceSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Members: members, ToolSourceReadbackPassed: true}, nil
}

// sourceCaptureBuild seals the pre-install source and its complete literal
// preparation/build records. Only its caller's fresh reservation is removed.
func sourceCaptureBuild(build, repository, output string, policy sourceCapturePolicy) (summary sourceCaptureSummary, result error) {
	return sourceCaptureBuildWithNotices(build, repository, output, "", policy)
}

// sourceCaptureBuildWithNotices binds the prepared tree, literal build records
// and original notice layout. Capture alone does not establish offline replay.
func sourceCaptureBuildWithNotices(build, repository, output, notices string, policy sourceCapturePolicy) (summary sourceCaptureSummary, result error) {
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
	if _, err := sourceBuildRecordNames(); err != nil {
		return summary, err
	}
	for _, entry := range sourceBuildRecordInputs() {
		root := build
		if entry.repository {
			root = repository
			if notices != "" && sourceIsNoticeOriginal(entry.name) {
				root = notices
			}
		}
		body, err := sourceReadAt(root, entry.name)
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
		"Update go-jose/v4 to 4.1.6 with its native generated checksums; preserve the upstream key-size limit.",
		"Update golang.org/x/net to 0.62.0 with its native generated checksums; preserve upstream HTML protections.",
		"Update gorilla/csrf to 1.7.3 with its native generated checksums; mark only explicitly configured plaintext login requests.",
		"Replace provider image decoration with existing neutral arrow icons in eighteen consumers; remove sixteen original provider image paths.",
		"Replace SMTP image decoration with the existing neutral envelope glyph in five consumers; remove nine original SMTP image paths while preserving all nine connection defaults.",
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
	if notices != "" {
		if _, err := sourceNoticeVerify(notices, output, sealed); err != nil {
			return summary, err
		}
	}
	var total int64
	for _, member := range manifest.Files {
		total += member.Bytes
	}
	return sourceCaptureSummary{ManifestSHA256: sealed, PreparedProfileSHA256: seal, Files: len(manifest.Files), Records: len(manifest.Records), Bytes: total, RecipeSHA256: digest(records["recipes/build-zitadel-linux.sh"])}, nil
}

// sourceVerifyBuildRecords rechecks the current repository against every literal
// retained repository record. Restored audit data never chooses the input paths.
func sourceVerifyBuildRecords(snapshot, seal, repository string) (sourceManifest, error) {
	return sourceVerifyBuildRecordsWithNotices(snapshot, seal, repository, "")
}

func sourceVerifyBuildRecordsWithNotices(snapshot, seal, repository, notices string) (sourceManifest, error) {
	manifest, err := sourceVerifyTree(snapshot, seal)
	if err != nil || manifest.PreparedProfile == "" {
		return sourceManifest{}, fmt.Errorf("verified prepared build snapshot required")
	}
	names, err := sourceBuildRecordNames()
	if err != nil || len(manifest.Records) != len(names) {
		return sourceManifest{}, fmt.Errorf("incomplete build record membership")
	}
	members := map[string]sourceMember{}
	for _, member := range manifest.Records {
		if !names[member.Path] {
			return sourceManifest{}, fmt.Errorf("unexpected build record")
		}
		members[member.Path] = member
	}
	if notices != "" {
		if _, err := sourceNoticeVerify(notices, snapshot, seal); err != nil {
			return sourceManifest{}, err
		}
	}
	for _, entry := range sourceBuildRecordInputs() {
		if !entry.repository {
			continue
		}
		root := repository
		if notices != "" && sourceIsNoticeOriginal(entry.name) {
			root = notices
		}
		body, err := sourceReadAt(root, entry.name)
		member := members[entry.retained]
		if err != nil || int64(len(body)) != member.Bytes || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
			return sourceManifest{}, fmt.Errorf("repository differs from retained build record: %s", entry.retained)
		}
	}
	if notices != "" {
		if _, err := sourceNoticeVerify(notices, snapshot, seal); err != nil {
			return sourceManifest{}, err
		}
	}
	return sourceVerifyTree(snapshot, seal)
}

func sourceIsNoticeOriginal(name string) bool {
	for _, member := range sourceNoticeMembers() {
		if name == member.Path {
			return true
		}
	}
	return false
}

// sourceNoticeManifest binds one closed original-input capsule to an already
// sealed prepared snapshot. It is separate from the snapshot it authenticates.
type sourceNoticeManifest struct {
	SchemaVersion         int            `json:"schemaVersion"`
	Profile               string         `json:"profile"`
	SourceManifestSHA256  string         `json:"sourceManifestSha256"`
	PreparedProfileSHA256 string         `json:"preparedProfileSha256"`
	RecipeSHA256          string         `json:"recipeSha256"`
	Members               []sourceMember `json:"members"`
}

type sourceNoticeSummary struct {
	SchemaVersion                int    `json:"schemaVersion"`
	Profile                      string `json:"profile"`
	SourceManifestSHA256         string `json:"sourceManifestSha256"`
	PreparedProfileSHA256        string `json:"preparedProfileSha256"`
	RecipeSHA256                 string `json:"recipeSha256"`
	ManifestSHA256               string `json:"manifestSha256"`
	Files                        int    `json:"files"`
	Bytes                        int64  `json:"bytes"`
	OriginalInputsReadbackPassed bool   `json:"originalInputsReadbackPassed"`
}

// sourceNoticeMembers is the reviewed original-byte boundary, not a licence
// verdict. Identical bytes at different required paths remain separate inputs.
func sourceNoticeMembers() []sourceMember {
	return []sourceMember{
		{Path: "third_party/zitadel-console-notices/catalogue.json", Bytes: 12536, SHA256: "4f964bd659a7656f7951c363535120af0fe881be37b1ac522e0964dd41647206", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/connect-LICENSE", Bytes: 11354, SHA256: "445e3398edab2a40d8d5e5c1a1d533af6464fbae8378ecc0caaf2668e9bbea73", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/connect-attribution.txt", Bytes: 602, SHA256: "e01fde43e84618ece70bd9401756fd7baa2c04c18787d0332f0c7ac472e74624", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/connect-web-attribution.txt", Bytes: 602, SHA256: "e01fde43e84618ece70bd9401756fd7baa2c04c18787d0332f0c7ac472e74624", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/google-protobuf-apache-license.txt", Bytes: 10173, SHA256: "a6cba85bc92e0cff7a450b1d873c0eaa2e9fc96bf472df0247a26bec77bf3ff9", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/google-protobuf-bsd-license.txt", Bytes: 1519, SHA256: "103441a2584cb720426c60320bcba53807024aab3080b00e0da841c305757a34", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/google-protobuf-closure-attribution.txt", Bytes: 84, SHA256: "6f451d925cddd9d9f35c5c112aebb058c6ac610515a04ae82aff2c1f966a3ac7", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/grpc-web-LICENSE", Bytes: 11357, SHA256: "c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/grpc-web-attribution.txt", Bytes: 84, SHA256: "6f451d925cddd9d9f35c5c112aebb058c6ac610515a04ae82aff2c1f966a3ac7", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/grpc-web-google-source-attribution.txt", Bytes: 602, SHA256: "224506478be0f26af4120dbeab425efc529b544151d4e0cca267f09f478e2b50", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/ng-icons-LICENSE.md", Bytes: 1053, SHA256: "13fb1d1ebc52e14fce36d8250232d559fc47c8a91636ebf16697835efb6da1f0", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/opentype-LICENSE", Bytes: 1085, SHA256: "a5b87c9bc191e7f9c06b07bd7cbbed3ed846dfdb50e859484b2c245aa1ab2e61", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/protobuf-LICENSE", Bytes: 11357, SHA256: "c04b4216f1cd4c5a4f7fb2f2a1b0ae70d847e9e0cac7c9dee9bf8cc03177c449", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/protobuf-buf-attribution.txt", Bytes: 605, SHA256: "cbc300ed2b08a4853eb3b953cc60ebe2f6f739ce0dfa3c82735eb2715e204f16", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/protobuf-google-bsd-license.txt", Bytes: 1812, SHA256: "0e952cb110ff6944789fbe47c25b38819a3866c04c7c5cbea8d0c0d8f3a92f0e", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/qrcode-license.txt", Bytes: 1076, SHA256: "8df47c6ad9ac2c41eb9b2a72def9908959da0ede8dadd56c7b249c1bdb0c5ce6", Mode: 0644},
		{Path: "third_party/zitadel-console-notices/tiny-inflate-license.txt", Bytes: 1084, SHA256: "2af3d4c93529722bd5083f8f8b51c185423c06fb86d62ca00cd7c16182eebf39", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/Flag-Icons-LICENSE", Bytes: 1087, SHA256: "8f1195d55a2fd315a07d812328470ca9ba2abb78c8d317ff19619d5125e00cea", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/Line-Awesome-LICENSE.md", Bytes: 1809, SHA256: "55a4cc3d450a9dfa4d12c7de238442562899868299b800dbac6c70ca6d99c490", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/Material-Icons-LICENSE", Bytes: 11358, SHA256: "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/ZITADEL-CONTRIBUTING.md", Bytes: 38585, SHA256: "9ffa1bda0bca61af695a8de5c4f24410b731a2561584b8a0c5d1c209f1e4d5cc", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/ZITADEL-LICENSE", Bytes: 34522, SHA256: "4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/ZITADEL-LICENSING.md", Bytes: 1877, SHA256: "97a02685ee62bfd02b2e696580189807a193b81fcc37909818f9d19cef45394e", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/catalogue.json", Bytes: 962009, SHA256: "b457cbb2df1ecc95fd91a015d2d2def35298ef0cb445768104d128be9c062186", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/original-line-awesome.css", Bytes: 110644, SHA256: "194db85f3ea5dd6c3864270bd81cacb137b4e1f1c1473f64d9a65545d8f4c66e", Mode: 0644},
		{Path: "third_party/zitadel-copied-assets/original-line-awesome.min.css", Bytes: 89729, SHA256: "ce61a18cf084f15003798340044643f329ac5f90045acb2d9e778368bd799854", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/catalogue.json", Bytes: 207607, SHA256: "bb27ca58e6797c4122d70b2db9f9aea7002791b30de5d6323bf35356335b80dd", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/external/armon-go-radix-LICENSE.txt", Bytes: 1079, SHA256: "831892cd31b9eef0311bb1de9014527ef5d3592eed7add1f9f829510d2065e62", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/external/jub0bs-cors-LICENSE.txt", Bytes: 1063, SHA256: "35266411be2f2a792cfac1ed4211d20610e33bc1921ae4a68ff01c12b95764e8", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/external/zlib-crc32.c.header.txt", Bytes: 574, SHA256: "e5c4e9c78aa77e8868f4ea0735e5519226109943b45e7092db956ec9821f8c0a", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/external/zlib-zlib.h.header.txt", Bytes: 1326, SHA256: "b09e2a43e9960d3d28d37d24f10dfbc462a4cb376da54ba571ac9a8874911ed5", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/publicsuffix/MPL-2.0.txt", Bytes: 16726, SHA256: "3f3d9e0024b1921b067d6f7f88deb4a60cbe7a78e76c64e3f1d7fc3b779b9d04", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/publicsuffix/public_suffix_list.dat", Bytes: 330467, SHA256: "a5638281157e8c902b127a5376f9ea2d024bf2ee11524133a6a76cd1d14ee7be", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/unicode/BidiPBAReference-2013.java.rights.txt", Bytes: 221, SHA256: "b1e1ff06002e8881b6c0daf36f13f3e555e6ae8fbdeb966b9920bc7a9cc069be", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/unicode/BidiReference-2013.java.rights.txt", Bytes: 529, SHA256: "c47d586592db8e7efd2ed7d5095f2b9b4828da7dfd61618f362732fa83e76a35", Mode: 0644},
		{Path: "third_party/zitadel-go-module-notices/incorporated/unicode/LICENSE-V3.txt", Bytes: 1995, SHA256: "e7a93b009565cfce55919a381437ac4db883e9da2126fa28b91d12732bc53d96", Mode: 0644},
		{Path: "third_party/zitadel-login-icons/LICENSE.md", Bytes: 1809, SHA256: "55a4cc3d450a9dfa4d12c7de238442562899868299b800dbac6c70ca6d99c490", Mode: 0644},
		{Path: "third_party/zitadel-login-icons/NOTICE.md", Bytes: 1251, SHA256: "bbbf1549408fef9c90199ac0859a49a110683cf3b89394e0402d030a7cfe5155", Mode: 0644},
		{Path: "third_party/zitadel-login-icons/clipboard.svg", Bytes: 390, SHA256: "9a526c626cdb157f44c29831d5c99eba3d719a92758a257891164538090fd739", Mode: 0644},
		{Path: "third_party/zitadel-text-fonts/Apache-2.0.txt", Bytes: 11358, SHA256: "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", Mode: 0644},
		{Path: "third_party/zitadel-text-fonts/catalogue.json", Bytes: 30322, SHA256: "04619cd3cd0de555538bc81dd8d70e3ff0ccf575bde586cd6a32a0a924fa73cb", Mode: 0644},
	}
}

// sourceNoticeContext joins all thirteen existing original records without
// extending the prepared snapshot's record set or making its seal recursive.
func sourceNoticeContext(snapshot, seal string) (sourceNoticeManifest, []byte, error) {
	manifest, err := sourceVerifyTree(snapshot, seal)
	if err != nil || !sourceDigest(manifest.PreparedProfile) {
		return sourceNoticeManifest{}, nil, fmt.Errorf("verified prepared source required for notice originals")
	}
	names, err := sourceBuildRecordNames()
	if err != nil || len(manifest.Records) != len(names) {
		return sourceNoticeManifest{}, nil, fmt.Errorf("complete build records required for notice originals")
	}
	records := map[string]sourceMember{}
	for _, member := range manifest.Records {
		if !names[member.Path] {
			return sourceNoticeManifest{}, nil, fmt.Errorf("unexpected notice build record")
		}
		records[member.Path] = member
	}
	members := sourceNoticeMembers()
	originals := map[string]sourceMember{}
	for _, member := range members {
		originals[member.Path] = member
	}
	joins := 0
	for _, entry := range sourceBuildRecordInputs() {
		original, ok := originals[entry.name]
		if !entry.repository || !ok {
			continue
		}
		if records[entry.retained] != original {
			return sourceNoticeManifest{}, nil, fmt.Errorf("notice original differs from retained build record: %s", entry.name)
		}
		joins++
	}
	if joins != 13 {
		return sourceNoticeManifest{}, nil, fmt.Errorf("notice original record join is incomplete")
	}
	recipe := records["recipes/build-zitadel-linux.sh"]
	if !sourceDigest(recipe.SHA256) || recipe.Bytes <= 0 || recipe.Mode != 0644 {
		return sourceNoticeManifest{}, nil, fmt.Errorf("notice recipe binding missing")
	}
	value := sourceNoticeManifest{1, "identity-notice-originals-v1", seal, manifest.PreparedProfile, recipe.SHA256, members}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return sourceNoticeManifest{}, nil, err
	}
	return value, append(raw, '\n'), nil
}

// sourceNoticeObserve reads the same complete closed observation it classifies.
// Checkout capture may omit only five README documents; a capsule has no extras.
func sourceNoticeObserve(root string, raw []byte, capture bool) (bodies map[string][]byte, result error) {
	owned, err := os.Lstat(root)
	if err != nil || (owned.Mode() != os.ModeDir|0700 && (!capture || owned.Mode() != os.ModeDir|0755)) {
		return nil, fmt.Errorf("physical notice root with safe mode required")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			bodies = nil
			result = errors.Join(result, closeErr)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(owned, opened) {
		return nil, fmt.Errorf("notice root changed")
	}
	wanted := map[string]sourceMember{}
	directories := map[string]bool{".": true}
	for _, member := range sourceNoticeMembers() {
		wanted[member.Path] = member
		for parent := path.Dir(member.Path); parent != "."; parent = path.Dir(parent) {
			directories[parent] = true
		}
	}
	roots := []string{"third_party/zitadel-console-notices", "third_party/zitadel-copied-assets", "third_party/zitadel-go-module-notices", "third_party/zitadel-login-icons", "third_party/zitadel-text-fonts"}
	readmes := map[string]bool{}
	for _, name := range roots {
		readmes[name+"/README.md"] = true
	}
	for name := range directories {
		info, err := r.Lstat(name)
		if err != nil || (info.Mode() != os.ModeDir|0700 && (!capture || info.Mode() != os.ModeDir|0755)) {
			return nil, fmt.Errorf("missing, linked or unsafe notice parent")
		}
	}
	if !capture {
		roots = []string{"."}
	}
	bodies = map[string][]byte{}
	manifestSeen := false
	for _, base := range roots {
		if err := fs.WalkDir(r.FS(), base, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				if !directories[name] || (info.Mode() != os.ModeDir|0700 && (!capture || info.Mode() != os.ModeDir|0755)) {
					return fmt.Errorf("unexpected or unsafe notice directory")
				}
				return nil
			}
			if capture && readmes[name] {
				if info.Mode() != 0644 {
					return fmt.Errorf("unsafe excluded notice guidance")
				}
				return nil
			}
			if !capture && name == "notice-originals.json" {
				if manifestSeen || info.Mode() != 0644 || info.Size() != int64(len(raw)) {
					return fmt.Errorf("invalid notice manifest")
				}
				body, err := sourceRead(r, name, info)
				if err != nil || !bytes.Equal(body, raw) {
					return fmt.Errorf("notice manifest differs from source and original identities")
				}
				manifestSeen = true
				return nil
			}
			member, ok := wanted[name]
			if !ok || info.Mode() != 0644 || info.Size() != member.Bytes {
				return fmt.Errorf("unexpected, linked or substituted notice original: %s", name)
			}
			if _, exists := bodies[name]; exists {
				return fmt.Errorf("duplicate notice original")
			}
			body, err := sourceRead(r, name, info)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(body)) != member.SHA256 {
				return fmt.Errorf("notice original bytes differ: %s", name)
			}
			bodies[name] = body
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if len(bodies) != 41 || (!capture && !manifestSeen) {
		return nil, fmt.Errorf("incomplete notice original observation")
	}
	current, err := os.Lstat(root)
	if err != nil || !os.SameFile(owned, current) || owned.Mode() != current.Mode() {
		return nil, fmt.Errorf("notice root changed during readback")
	}
	return bodies, nil
}

func sourceNoticeSummaryFor(value sourceNoticeManifest, raw []byte) sourceNoticeSummary {
	var total int64
	for _, member := range value.Members {
		total += member.Bytes
	}
	return sourceNoticeSummary{1, value.Profile, value.SourceManifestSHA256, value.PreparedProfileSHA256, value.RecipeSHA256, fmt.Sprintf("%x", sha256.Sum256(raw)), len(value.Members), total, true}
}

// sourceNoticeMaterialize creates only its own private output, rechecks both
// inputs after writing, and never rolls back over a replaced output root.
func sourceNoticeMaterialize(input, snapshot, seal, output string, capture bool) (summary sourceNoticeSummary, result error) {
	value, raw, err := sourceNoticeContext(snapshot, seal)
	if err != nil {
		return summary, err
	}
	bodies, err := sourceNoticeObserve(input, raw, capture)
	if err != nil {
		return summary, err
	}
	inputPhysical, err := filepath.EvalSymlinks(input)
	if err != nil {
		return summary, err
	}
	snapshotPhysical, err := filepath.EvalSymlinks(snapshot)
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
	for _, source := range []string{inputPhysical, snapshotPhysical} {
		source, err = filepath.Abs(source)
		if err != nil {
			return summary, err
		}
		if output == source || strings.HasPrefix(output, source+string(filepath.Separator)) {
			return summary, fmt.Errorf("notice output must stay outside its inputs")
		}
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return summary, err
	}
	owned, err := os.Lstat(output)
	if err != nil {
		return summary, err
	}
	defer func() {
		if result != nil {
			current, err := os.Lstat(output)
			if err == nil && os.SameFile(owned, current) {
				result = errors.Join(result, os.RemoveAll(output))
			} else {
				result = errors.Join(result, fmt.Errorf("notice reservation replaced; replacement retained"))
			}
		}
	}()
	r, err := os.OpenRoot(output)
	if err != nil {
		return summary, err
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			summary = sourceNoticeSummary{}
			result = errors.Join(result, closeErr)
		}
	}()
	opened, err := r.Lstat(".")
	if err != nil || !os.SameFile(owned, opened) {
		return summary, fmt.Errorf("notice reservation changed")
	}
	for _, member := range value.Members {
		if err := r.MkdirAll(path.Dir(member.Path), 0700); err != nil {
			return summary, err
		}
		file, err := r.OpenFile(member.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0644)
		if err != nil {
			return summary, err
		}
		if err := sourceWriteComplete(file, bodies[member.Path], 0644); err != nil {
			return summary, err
		}
	}
	file, err := r.OpenFile("notice-originals.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0644)
	if err != nil {
		return summary, err
	}
	if err := sourceWriteComplete(file, raw, 0644); err != nil {
		return summary, err
	}
	if _, err := sourceNoticeObserve(output, raw, false); err != nil {
		return summary, err
	}
	if _, err := sourceNoticeObserve(input, raw, capture); err != nil {
		return summary, err
	}
	if _, finalRaw, err := sourceNoticeContext(snapshot, seal); err != nil || !bytes.Equal(raw, finalRaw) {
		return summary, fmt.Errorf("notice source context changed during readback")
	}
	current, err := os.Lstat(output)
	if err != nil || !os.SameFile(owned, current) || current.Mode() != os.ModeDir|0700 {
		return summary, fmt.Errorf("notice output root changed during readback")
	}
	return sourceNoticeSummaryFor(value, raw), nil
}

// sourceNoticeVerify authenticates the complete restored layout before any
// native collector may consume it; no checkout fallback is involved.
func sourceNoticeVerify(root, snapshot, seal string) (sourceNoticeSummary, error) {
	return sourceNoticeReadback(root, snapshot, seal, false)
}

func sourceNoticeReadback(root, snapshot, seal string, checkout bool) (sourceNoticeSummary, error) {
	value, raw, err := sourceNoticeContext(snapshot, seal)
	if err != nil {
		return sourceNoticeSummary{}, err
	}
	if _, err := sourceNoticeObserve(root, raw, checkout); err != nil {
		return sourceNoticeSummary{}, err
	}
	if _, finalRaw, err := sourceNoticeContext(snapshot, seal); err != nil || !bytes.Equal(raw, finalRaw) {
		return sourceNoticeSummary{}, fmt.Errorf("notice source context changed during verification")
	}
	return sourceNoticeSummaryFor(value, raw), nil
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
	if info.Mode() != 0644 && info.Mode() != 0755 && info.Mode() != 0600 && info.Mode() != 0444 {
		return nil, fmt.Errorf("unsupported build record mode")
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
	case len(args) == 5 && (args[0] == "--capture-notice-originals" || args[0] == "--restore-notice-originals"):
		value, err = sourceNoticeMaterialize(args[1], args[2], args[3], args[4], args[0] == "--capture-notice-originals")
	case len(args) == 4 && args[0] == "--verify-notice-originals":
		value, err = sourceNoticeVerify(args[1], args[2], args[3])
	case len(args) == 4 && args[0] == "--verify-notice-checkout":
		value, err = sourceNoticeReadback(args[1], args[2], args[3], true)
	case len(args) == 4 && (args[0] == "--restore-tools" || args[0] == "--verify-tools"):
		value, err = sourceToolSource(args[1], args[2], args[3], args[0] == "--restore-tools")
	case len(args) == 4 && args[0] == "--restore-build":
		value, err = sourceRestoreBuild(args[1], args[2], args[3])
	case len(args) == 3 && args[0] == "--seal-inputs":
		var seal string
		seal, err = sourceSealTreePhase(args[1], args[2], "retained-build-inputs")
		value = struct {
			ManifestSHA256 string `json:"manifestSha256"`
		}{seal}
	case (len(args) == 4 || len(args) == 5) && args[0] == "--capture-build":
		notices := ""
		if len(args) == 5 {
			notices = args[4]
			if notices == "" {
				err = fmt.Errorf("explicit notice root is empty")
				break
			}
		}
		value, err = sourceCaptureBuildWithNotices(args[1], args[2], args[3], notices, sourceCapturePolicy{
			sourcePreparedProfile,
			"bf332001875b5075845459450c72d98523c1db0923df40a04525740892c0e0e6",
			"6f1f58f338a3e3fd6c65bcc42c5b2bc56b71a7affc758e14e096c122f28a5455",
			"82f66c6a4a5a557c851f32611737c6e6ba8f2bdc316a012e6d8b62e80f6cf76b",
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
	case (len(args) == 4 || len(args) == 5) && args[0] == "--verify-build-records":
		notices := ""
		if len(args) == 5 {
			notices = args[4]
			if notices == "" {
				err = fmt.Errorf("explicit notice root is empty")
				break
			}
		}
		value, err = sourceVerifyBuildRecordsWithNotices(args[1], args[2], args[3], notices)
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
		"selected-verifier-bootstrap": false,
		"selected-notice-originals":   false,
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
