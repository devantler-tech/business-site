package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
)

var handoffRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var handoffRun = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)

var handoffWitnesses = []string{
	"zitadel-build/build-receipt.json",
	"zitadel-image/image-smoke.json",
	"zitadel-image/service-smoke.json",
	"zitadel-image/browser-smoke.json",
	"zitadel-image/browser-binding.json",
	"zitadel-offline-replay/replay-receipt.json",
	"zitadel-offline-replay/native/build-receipt.json",
	"zitadel-offline-replay/input-retention.json",
	"zitadel-offline-replay/input-archive.json",
	"zitadel-offline-replay/input-roundtrip.json",
	"zitadel-base-source/evidence/capture.json",
	"zitadel-base-source/evidence/verify.json",
	"zitadel-base-source/evidence/pack.json",
	"zitadel-base-source/evidence/base-source.json",
}

type handoffObject map[string]json.RawMessage

// Selected values are interpreted only for cross-witness joins. All other
// native fields remain opaque data bound by the complete original byte hash.
func (o handoffObject) text(key string) string {
	var value string
	_ = json.Unmarshal(o[key], &value)
	return value
}

func (o handoffObject) number(key string) int64 {
	var value int64
	_ = json.Unmarshal(o[key], &value)
	return value
}

func (o handoffObject) is(key string, want bool) bool {
	var value *bool
	return json.Unmarshal(o[key], &value) == nil && value != nil && *value == want
}

func (o handoffObject) object(key string) handoffObject {
	var value handoffObject
	_ = json.Unmarshal(o[key], &value)
	return value
}

func (o handoffObject) objects(key string) []handoffObject {
	var value []handoffObject
	_ = json.Unmarshal(o[key], &value)
	return value
}

type handoffDocument struct {
	SchemaVersion        int             `json:"schemaVersion"`
	Profile              string          `json:"profile"`
	SourceRepository     string          `json:"sourceRepository"`
	SourceSHA            string          `json:"sourceSha"`
	WorkflowPath         string          `json:"workflowPath"`
	OriginalCIRunID      string          `json:"originalCIRunId"`
	OriginalCIRunAttempt uint64          `json:"originalCIRunAttempt"`
	RecipeSHA256         string          `json:"recipeSha256"`
	SourceManifestSHA256 string          `json:"sourceManifestSha256"`
	InputManifestSHA256  string          `json:"inputManifestSha256"`
	OriginalBinarySHA256 string          `json:"originalBinarySha256"`
	ReplayBinarySHA256   string          `json:"replayBinarySha256"`
	LocalImageID         string          `json:"localImageId"`
	ContextSHA256        string          `json:"contextSha256"`
	BaseRootFS           json.RawMessage `json:"baseRootFS"`
	Files                []sourceFile    `json:"files"`
	ForgeAdmission       bool            `json:"forgeAdmission"`
	SourceOfferPublished bool            `json:"sourceOfferPublished"`
	ImagePublished       bool            `json:"imagePublished"`
	SignatureVerified    bool            `json:"signatureVerified"`
	PortalActivated      bool            `json:"portalActivated"`
}

func runHandoff(args []string, out io.Writer) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("handoff requires verified file metadata")
	}
	f := flag.NewFlagSet("prepare-handoff", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	input := f.String("input", "", "original native job root")
	output := f.String("output", "", "new manifest")
	revision := f.String("source-sha", "", "caller source revision")
	run := f.String("run-id", "", "caller CI run")
	attempt := f.Uint64("run-attempt", 0, "caller CI attempt")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || !filepath.IsAbs(*input) || filepath.Clean(*input) != *input ||
		!handoffRevision.MatchString(*revision) || !handoffRun.MatchString(*run) || *attempt == 0 || *attempt > 1000000 {
		return fmt.Errorf("explicit root, revision and original run/attempt required")
	}
	if _, err := strconv.ParseUint(*run, 10, 64); err != nil {
		return err
	}
	name, err := sourceOutputPath(*output)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("new handoff output required")
	}
	doc, err := observeHandoff(*input)
	if err != nil {
		return err
	}
	doc.SourceSHA, doc.OriginalCIRunID, doc.OriginalCIRunAttempt = *revision, *run, *attempt
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err = writeHandoff(name, body); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Operation      string `json:"operation"`
		ManifestSHA256 string `json:"manifestSha256"`
		ForgeAdmission bool   `json:"forgeAdmission"`
	}{Operation: "prepare-handoff", ManifestSHA256: sourceSHA(body)})
}

// observeHandoff joins observations, not authenticated provenance or release
// authority. The native engines own complete archive and runtime validation.
func observeHandoff(input string) (doc handoffDocument, resultErr error) {
	before, err := os.Lstat(input)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return doc, fmt.Errorf("physical native job root required")
	}
	root, err := os.OpenRoot(input)
	if err != nil {
		return doc, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return doc, fmt.Errorf("native job root changed before observation")
	}
	observed := make(map[string]handoffSnapshot)
	objects := make(map[string]handoffObject)
	doc = handoffDocument{SchemaVersion: 1, Profile: "identity-publication-handoff-v1",
		SourceRepository: "devantler-tech/business-site", WorkflowPath: ".github/workflows/ci.yaml"}
	for _, name := range handoffWitnesses {
		snapshot, err := readHandoffSnapshot(root, name, 8<<20, true, 0)
		if err != nil {
			return doc, fmt.Errorf("witness %s: %w", name, err)
		}
		var object handoffObject
		if err = sourceJSON(snapshot.data, &object); err != nil || len(object) == 0 {
			return doc, errors.Join(fmt.Errorf("complete witness object required: %s", name), err)
		}
		objects[name], observed[name] = object, snapshot
		doc.Files = append(doc.Files, snapshot.file)
	}
	for _, asset := range []struct {
		name string
		max  int64
		mode os.FileMode
	}{
		{"zitadel-offline-replay/inputs.tar", 16 << 30, 0600},
		{"zitadel-base-source/base-source.tar", 16 << 20, 0444},
		{"zitadel-image/candidate-image.tar", 1 << 30, 0444},
	} {
		snapshot, err := readHandoffSnapshot(root, asset.name, asset.max, false, asset.mode)
		if err != nil {
			return doc, fmt.Errorf("archive %s: %w", asset.name, err)
		}
		observed[asset.name] = snapshot
		doc.Files = append(doc.Files, snapshot.file)
	}
	if err = joinHandoff(&doc, objects, observed); err != nil {
		return doc, err
	}
	for name, snapshot := range observed {
		current, err := root.Lstat(name)
		if err != nil || !sameHandoffSnapshot(snapshot.info, current) {
			return doc, fmt.Errorf("handoff input changed after observation: %s", name)
		}
	}
	current, err := os.Lstat(input)
	if err != nil || !current.IsDir() || !os.SameFile(before, current) {
		return doc, fmt.Errorf("native job root changed during observation")
	}
	return doc, nil
}

func joinHandoff(doc *handoffDocument, o map[string]handoffObject, files map[string]handoffSnapshot) error {
	build := o["zitadel-build/build-receipt.json"]
	replay := o["zitadel-offline-replay/replay-receipt.json"]
	native := o["zitadel-offline-replay/native/build-receipt.json"]
	source := build.object("sourceSnapshot")
	if build.number("schemaVersion") != 1 || build.text("profile") != "api-login-v1-console" ||
		build.text("platform") != "linux/amd64" || build.text("version") != "4.20.0" ||
		build.text("upstreamCommit") != "8afa5dd37a8e723efb9eace8d09fd5c1cccef694" || !build.is("reproducible", false) ||
		source.text("preparedProfileSha256") != "06862d888dc1cc209f48f22c38cca746846bbd232eb6be6560797cfcc0d4f0f3" ||
		source.number("files") != 9528 || source.number("bytes") != 261325885 || source.number("records") != 58 {
		return fmt.Errorf("frozen connected source profile required")
	}
	doc.RecipeSHA256, doc.SourceManifestSHA256 = build.text("recipeSha256"), source.text("manifestSha256")
	doc.OriginalBinarySHA256, doc.ReplayBinarySHA256 = build.text("binarySha256"), replay.text("binarySha256")
	doc.InputManifestSHA256 = replay.text("inputManifestSha256")
	for _, seal := range []string{doc.RecipeSHA256, doc.SourceManifestSHA256, doc.OriginalBinarySHA256, doc.ReplayBinarySHA256, doc.InputManifestSHA256} {
		if !sourceDigest.MatchString(seal) {
			return fmt.Errorf("complete identity seals required")
		}
	}
	if err := joinHandoffReplay(doc, replay, native, o, files); err != nil {
		return err
	}
	if err := joinHandoffBase(o, files); err != nil {
		return err
	}
	return joinHandoffImage(doc, o, files)
}

func joinHandoffReplay(doc *handoffDocument, replay, native handoffObject, o map[string]handoffObject, files map[string]handoffSnapshot) error {
	if replay.number("schemaVersion") != 1 || replay.text("profile") != "native-retained-input-replay-v1" || replay.text("platform") != "linux/amd64" ||
		replay.text("sourceManifestSha256") != doc.SourceManifestSHA256 || replay.text("recipeSha256") != doc.RecipeSHA256 ||
		!replay.is("reproducible", false) || replay.text("imageAdmission") != "unknown" || replay.text("licenseClosure") != "unknown" {
		return fmt.Errorf("complete separate offline observation required")
	}
	for _, field := range []string{"dependencyInputsCaptured", "nativeOfflineReplayProved", "networkDenied", "capabilitiesDropped", "nonRoot", "noNewPrivileges"} {
		if !replay.is(field, true) {
			return fmt.Errorf("offline observation missing %s", field)
		}
	}
	if native.number("schemaVersion") != 1 || native.text("profile") != "api-login-v1-console" || native.text("platform") != "linux/amd64" ||
		native.text("binarySha256") != doc.ReplayBinarySHA256 || native.text("recipeSha256") != doc.RecipeSHA256 || native.object("sourceInput").text("manifestSha256") != doc.SourceManifestSHA256 {
		return fmt.Errorf("offline binary/source observation differs")
	}
	for _, name := range []string{"input-retention", "input-archive", "input-roundtrip"} {
		if o["zitadel-offline-replay/"+name+".json"].text("manifestSha256") != doc.InputManifestSHA256 {
			return fmt.Errorf("input archive roundtrip differs")
		}
	}
	seal := files["zitadel-offline-replay/inputs.tar"].file.SHA256
	if seal != replay.text("inputArchiveSha256") || seal != o["zitadel-offline-replay/input-archive.json"].text("archiveSha256") || seal != o["zitadel-offline-replay/input-roundtrip.json"].text("archiveSha256") {
		return fmt.Errorf("input archive bytes differ")
	}
	return nil
}

func joinHandoffBase(o map[string]handoffObject, files map[string]handoffSnapshot) error {
	p, err := loadBaseProfile()
	if err != nil {
		return err
	}
	expected, err := baseManifestBytes(p)
	if err != nil {
		return err
	}
	seal := files["zitadel-base-source/evidence/base-source.json"].file.SHA256
	if seal != sourceSHA(expected) {
		return fmt.Errorf("frozen base-source manifest differs")
	}
	for _, op := range []string{"capture", "verify", "pack"} {
		receipt := o["zitadel-base-source/evidence/"+op+".json"]
		if receipt.number("schemaVersion") != 1 || receipt.text("operation") != op+"-base-source" || receipt.text("baseImage") != frozenBaseImage || receipt.text("manifestSha256") != seal || receipt.number("payloadFiles") != 41 {
			return fmt.Errorf("base-source witnesses differ")
		}
		for _, field := range []string{"baseImageSignatureVerified", "openpgpVerified", "sourceReleaseVerified", "imageAdmission"} {
			if !receipt.is(field, false) {
				return fmt.Errorf("base collector authority differs")
			}
		}
	}
	if files["zitadel-base-source/base-source.tar"].file.SHA256 != o["zitadel-base-source/evidence/pack.json"].text("archiveSha256") {
		return fmt.Errorf("base-source archive differs")
	}
	return nil
}

func joinHandoffImage(doc *handoffDocument, o map[string]handoffObject, files map[string]handoffSnapshot) error {
	image := o["zitadel-image/image-smoke.json"]
	doc.LocalImageID, doc.ContextSHA256, doc.BaseRootFS = image.text("localImageId"), image.text("contextSha256"), image["baseRootFS"]
	if len(doc.LocalImageID) != 71 || doc.LocalImageID[:7] != "sha256:" || !sourceDigest.MatchString(doc.LocalImageID[7:]) || !sourceDigest.MatchString(doc.ContextSHA256) ||
		image.number("schemaVersion") != 1 || image.text("profile") != "cli-version-smoke" || image.text("base") != frozenBaseImage || !image.is("candidateReloaded", true) || !image.is("reproducible", false) {
		return fmt.Errorf("original saved/reloaded image observation required")
	}
	archive, actual := image.object("candidateArchive"), files["zitadel-image/candidate-image.tar"].file
	if archive.text("path") != "candidate-image.tar" || archive.text("sha256") != actual.SHA256 || archive.number("bytes") != actual.Bytes || archive.number("mode") != 0444 {
		return fmt.Errorf("original image archive differs")
	}
	var layers []string
	rootFS := image.object("baseRootFS")
	if err := json.Unmarshal(rootFS["layers"], &layers); err != nil || len(rootFS) != 2 || rootFS.text("type") != "layers" || len(layers) != 16 {
		return fmt.Errorf("complete observed RootFS required")
	}
	for _, layer := range layers {
		if len(layer) != 71 || layer[:7] != "sha256:" || !sourceDigest.MatchString(layer[7:]) {
			return fmt.Errorf("invalid observed layer digest")
		}
	}
	matched := map[string]bool{}
	for _, file := range image.objects("files") {
		path := file.text("path")
		if path == "zitadel" || path == "build-receipt.json" {
			if matched[path] {
				return fmt.Errorf("duplicate original image payload")
			}
			want := doc.OriginalBinarySHA256
			if path == "build-receipt.json" {
				want = files["zitadel-build/build-receipt.json"].file.SHA256
			}
			if file.text("sha256") != want {
				return fmt.Errorf("original image binary/receipt differs")
			}
			matched[path] = true
		}
	}
	if len(matched) != 2 {
		return fmt.Errorf("original image binary/receipt missing")
	}
	for _, name := range []string{"image-smoke", "service-smoke", "browser-binding"} {
		witness := o["zitadel-image/"+name+".json"]
		if witness.number("schemaVersion") != 1 || witness.text("localImageId") != doc.LocalImageID || witness.text("cleanup") != "complete" || witness.text("imageAdmission") != "unknown" || witness.text("licenseClosure") != "unknown" {
			return fmt.Errorf("native image witness differs")
		}
	}
	service, browser, binding := o["zitadel-image/service-smoke.json"], o["zitadel-image/browser-smoke.json"], o["zitadel-image/browser-binding.json"]
	if service.text("profile") != "isolated-service-smoke" || !service.is("readOnlyRoot", true) || !service.is("authenticationProof", false) || !service.is("browserProof", false) {
		return fmt.Errorf("native service authority differs")
	}
	if browser.number("schemaVersion") != 1 || browser.text("cleanup") != "complete" || browser.text("imageAdmission") != "unknown" || browser.text("licenseClosure") != "unknown" || binding.text("browserReceiptSha256") != files["zitadel-image/browser-smoke.json"].file.SHA256 || !sourceDigest.MatchString(browser.text("browserSourceSha256")) || browser.text("browserSourceSha256") != binding.text("browserSourceSha256") {
		return fmt.Errorf("native browser witness differs")
	}
	for _, field := range []string{"portalFederation", "productionTLS", "invitationProof", "mfaProof", "browserSandboxDisabled"} {
		if !browser.is(field, false) {
			return fmt.Errorf("native browser authority differs")
		}
	}
	return nil
}

// writeHandoff publishes only one local data file. Failure removes only the
// descriptor identity this invocation created, including a late close failure.
func writeHandoff(name string, body []byte) (resultErr error) {
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return err
	}
	var created os.FileInfo
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, removeSourceOutput(name, created))
		}
	}()
	f, err := root.OpenFile(filepath.Base(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	created, err = f.Stat()
	if err != nil {
		return err
	}
	_, err = io.Copy(f, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Chmod(0444); err != nil {
		return err
	}
	_, err = readSourceFile(root, sourceFile{filepath.Base(name), int64(len(body)), sourceSHA(body), 0444})
	return err
}
