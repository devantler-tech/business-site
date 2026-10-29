// Output-bound provenance for the frozen Console's copied icons, flags and translations.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

type copiedAsset struct {
	Family       string   `json:"family"`
	Path         string   `json:"path"`
	Bytes        int      `json:"bytes"`
	SHA256       string   `json:"sha256"`
	GitBlob      string   `json:"gitBlob,omitempty"`
	OriginalBlob string   `json:"originalBlob,omitempty"`
	OriginalPath string   `json:"originalPath,omitempty"`
	Outputs      []string `json:"outputs"`
}
type copiedArchive struct {
	Family            string `json:"family"`
	Path              string `json:"path"`
	SHA256            string `json:"sha256"`
	Integrity         string `json:"integrity"`
	LicensePath       string `json:"licensePath"`
	Notice            string `json:"notice"`
	PackageJSONSHA256 string `json:"packageJsonSha256"`
}
type copiedCatalogue struct {
	SchemaVersion  int             `json:"schemaVersion"`
	UpstreamCommit string          `json:"upstreamCommit"`
	LockSHA256     string          `json:"lockSha256"`
	Assets         []copiedAsset   `json:"assets"`
	Archives       []copiedArchive `json:"archives"`
	Notices        []inputRecord   `json:"notices"`
}
type copiedPolicy struct {
	CatalogueHash     string
	Assets, Outputs   int
	TextNotices       string
	TextPolicy        textPolicy
	ProviderConsumers []inputRecord
	SMTPConsumers     []inputRecord
	MDI               []mdiProvenance
	MDINotices        []inputRecord
	MDIConsumers      []inputRecord
}

var copiedFrozen = copiedPolicy{CatalogueHash: "b457cbb2df1ecc95fd91a015d2d2def35298ef0cb445768104d128be9c062186", Assets: 1858, Outputs: 3716, ProviderConsumers: neutralProviderConsumers(), SMTPConsumers: neutralSMTPConsumers(), MDI: mdiFrozen, MDIConsumers: neutralMDIConsumers(), MDINotices: []inputRecord{{"Pictogrammers-LICENSE", "f3bc8715bad84b26396bb42d2abd11f919cf58163be158eab79ecbaabf84cdf2"}, {"Google-Mail-LICENSE", "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd"}}}

func init() { iconExtensions["--copied-assets"] = runCopiedAssets }

// Inventory creation is exclusive and pending-only. Completed verification
// checks both retained files before emitting anything and never repairs them.
func runCopiedAssets(args []string, out, diagnostic io.Writer) int {
	fail := func(e error) int { fmt.Fprintln(diagnostic, "copied-assets:", e); return 1 }
	values := map[string]string{}
	mode := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if _, ok := values[a]; ok {
			return fail(fmt.Errorf("duplicate argument"))
		}
		switch a {
		case "--snapshot", "--inventory", "--verify":
			if mode != "" {
				return fail(fmt.Errorf("ambiguous mode"))
			}
			mode = a
			values[a] = "true"
		case "--build", "--notices", "--recipe":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing argument value"))
			}
			i++
			values[a] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument"))
		}
	}
	if mode == "" || len(values) != 4 {
		return fail(fmt.Errorf("expected mode, build, notices and recipe"))
	}
	build, notices, recipe := values["--build"], values["--notices"], values["--recipe"]
	policy := copiedFrozen
	if policy.TextNotices == "" {
		policy.TextNotices = filepath.Join(filepath.Dir(notices), "zitadel-text-fonts")
		policy.TextPolicy = textFrozen
	}
	var value any
	if mode == "--snapshot" {
		s, e := copiedObserve(build, notices, recipe, readAt, policy)
		if e != nil {
			return fail(e)
		}
		value = s
	} else {
		if mode == "--verify" {
			if _, e := os.Lstat(filepath.Join(build, "build-receipt.pending")); !os.IsNotExist(e) {
				return fail(fmt.Errorf("verification requires completed build"))
			}
		}
		if mode == "--inventory" {
			if _, e := os.Lstat(filepath.Join(build, "build-receipt.pending")); e != nil {
				return fail(fmt.Errorf("capture requires pending build"))
			}
		}
		inv, raw, e := copiedCapture(build, notices, recipe, readAt, policy)
		if e != nil {
			return fail(e)
		}
		value = inv
		if mode == "--verify" {
			retained, e := readAt(build, "copied-asset-notices.txt", 2<<20)
			if e != nil || !bytes.Equal(retained, raw) {
				return fail(fmt.Errorf("retained copied-asset notices differ"))
			}
		} else {
			root, e := os.OpenRoot(build)
			if e != nil {
				return fail(e)
			}
			defer root.Close()
			file, e := root.OpenFile("copied-asset-notices.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return fail(e)
			}
			n, e := file.Write(raw)
			closeErr := file.Close()
			if e != nil || closeErr != nil || n != len(raw) {
				return fail(fmt.Errorf("incomplete copied-asset notice capture"))
			}
		}
	}
	encoded, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return fail(e)
	}
	encoded = append(encoded, '\n')
	if mode == "--verify" {
		retained, e := readAt(build, "copied-asset-notices-inventory.json", 8<<20)
		if e != nil || !bytes.Equal(retained, encoded) {
			return fail(fmt.Errorf("retained copied-asset inventory differs"))
		}
	}
	if n, e := out.Write(encoded); e != nil || n != len(encoded) {
		return fail(fmt.Errorf("incomplete copied-asset inventory output"))
	}
	return 0
}

type copiedBinding struct {
	copiedAsset
	Names []textFontName `json:"names,omitempty"`
}
type copiedAlias struct {
	Family   string `json:"family"`
	Path     string `json:"path"`
	Target   string `json:"target"`
	Resolved string `json:"resolved"`
}
type copiedState struct {
	SchemaVersion  int             `json:"schemaVersion"`
	Inputs         []inputRecord   `json:"inputs"`
	Assets         []copiedBinding `json:"assets"`
	Raw            []byte          `json:"checkedNoticeBytes"`
	ImageAdmission string          `json:"imageAdmission"`
	LicenseClosure string          `json:"licenseClosure"`
	Aliases        []copiedAlias   `json:"aliases"`
}

type copiedInventory struct {
	SchemaVersion  int             `json:"schemaVersion"`
	UpstreamCommit string          `json:"upstreamCommit"`
	Profile        string          `json:"profile"`
	Inputs         []inputRecord   `json:"inputs"`
	Assets         []copiedBinding `json:"assets"`
	RawDiagnostic  string          `json:"rawDiagnostic"`
	Bytes          int             `json:"bytes"`
	SHA256         string          `json:"sha256"`
	BinarySHA256   string          `json:"binarySha256"`
	ImageAdmission string          `json:"imageAdmission"`
	LicenseClosure string          `json:"licenseClosure"`
	Reproducible   bool            `json:"reproducible"`
	Aliases        []copiedAlias   `json:"aliases"`
}

// The existing font collector supplies the canonical preparation/toolchain,
// recipe and final binary/receipt join. Compare its complete retained outputs,
// rather than trusting a digest claimed by either saved inventory.
func copiedCapture(build, notices, recipe string, read textReader, policy copiedPolicy) (copiedInventory, []byte, error) {
	fail := func(e error) (copiedInventory, []byte, error) { return copiedInventory{}, nil, e }
	state, e := copiedObserve(build, notices, recipe, read, policy)
	if e != nil {
		return fail(e)
	}
	before, e := read(build, "copied-assets.before.json", 8<<20)
	if e != nil {
		return fail(e)
	}
	var saved copiedState
	if textDecode(before, &saved) != nil || !reflect.DeepEqual(saved, state) {
		return fail(fmt.Errorf("copied-asset inputs changed across compilation"))
	}
	fontInventory, fontRaw, e := textCapture(build, policy.TextNotices, recipe, read, policy.TextPolicy)
	if e != nil {
		return fail(e)
	}
	encoded, e := json.MarshalIndent(fontInventory, "", "  ")
	if e != nil {
		return fail(e)
	}
	encoded = append(encoded, '\n')
	retained, e := read(build, "text-font-notices-inventory.json", 4<<20)
	if e != nil || !bytes.Equal(retained, encoded) {
		return fail(fmt.Errorf("retained text-font inventory differs"))
	}
	retainedRaw, e := read(build, textRaw, 2<<20)
	if e != nil || !bytes.Equal(retainedRaw, fontRaw) {
		return fail(fmt.Errorf("retained text-font notices differ"))
	}
	generated, e := read(build, "generated.sha256", 8<<20)
	if e != nil {
		return fail(e)
	}
	manifest := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(generated), "\n"), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || !textHex(parts[0], 64) || !textPath(parts[1]) || manifest[parts[1]] != "" {
			return fail(fmt.Errorf("incomplete generated asset manifest"))
		}
		manifest[parts[1]] = parts[0]
	}
	for _, asset := range state.Assets {
		for _, name := range asset.Outputs {
			if manifest[name] != asset.SHA256 {
				return fail(fmt.Errorf("generated copied-asset manifest differs"))
			}
		}
	}
	state.Inputs = append(state.Inputs, inputRecord{"copied-assets.before.json", digest(before)}, inputRecord{"generated.sha256", digest(generated)}, inputRecord{"text-font-notices-inventory.json", digest(retained)}, inputRecord{textRaw, digest(retainedRaw)})
	for _, in := range fontInventory.Inputs {
		if in.Path == "build-receipt.json" {
			state.Inputs = append(state.Inputs, in)
		}
	}
	inv := copiedInventory{SchemaVersion: 1, UpstreamCommit: upstream, Profile: "api-login-v1-console", Inputs: state.Inputs, Assets: state.Assets, RawDiagnostic: "copied-asset-notices.txt", Bytes: len(state.Raw), SHA256: digest(state.Raw), BinarySHA256: fontInventory.BinarySHA256, ImageAdmission: "unknown", LicenseClosure: "unknown", Aliases: state.Aliases}
	return inv, state.Raw, nil
}

// copiedObserve authenticates the frozen source, notice and package inputs before
// joining their selected assets to the generated output. Incomplete input fails.
func copiedObserve(build, notices, recipe string, read textReader, policy copiedPolicy) (copiedState, error) {
	state := copiedState{SchemaVersion: 1, ImageAdmission: "unknown", LicenseClosure: "unknown"}
	fail := func(e error) (copiedState, error) { return copiedState{}, e }
	checked := map[string][]byte{}
	get := func(root, name, key string, limit int64) ([]byte, error) {
		if b, ok := checked[key]; ok {
			return b, nil
		}
		b, e := read(root, name, limit)
		if e != nil {
			return nil, fmt.Errorf("incomplete copied-asset input %s: %w", key, e)
		}
		checked[key] = b
		state.Inputs = append(state.Inputs, inputRecord{key, digest(b)})
		return b, nil
	}
	body, e := get(notices, "catalogue.json", "catalogue.json", 2<<20)
	if e != nil {
		return fail(e)
	}
	var cat copiedCatalogue
	if digest(body) != policy.CatalogueHash || textDecode(body, &cat) != nil || cat.SchemaVersion != 1 || cat.UpstreamCommit != upstream || len(cat.Assets) != policy.Assets || len(cat.Archives) != 2 || len(cat.Notices) == 0 || len(cat.Notices) > 8 {
		return fail(fmt.Errorf("frozen copied-asset catalogue differs"))
	}
	lock, e := get(build, "prepared/source/pnpm-lock.yaml", "prepared/source/pnpm-lock.yaml", 4<<20)
	if e != nil || !textHex(cat.LockSHA256, 64) || digest(lock) != cat.LockSHA256 {
		return fail(fmt.Errorf("selected original package lock differs"))
	}
	if len(policy.ProviderConsumers) == 0 || len(policy.ProviderConsumers) > 18 {
		return fail(fmt.Errorf("neutral consumer policy incomplete"))
	}
	if len(policy.SMTPConsumers) == 0 || len(policy.SMTPConsumers) > 5 {
		return fail(fmt.Errorf("neutral SMTP consumer policy incomplete"))
	}
	seenConsumers := map[string]bool{}
	consumers := append(append([]inputRecord{}, policy.ProviderConsumers...), policy.SMTPConsumers...)
	if len(policy.MDI) > 25 || len(policy.MDI) > 0 && (len(policy.MDINotices) == 0 || len(policy.MDINotices) > 2 || len(policy.MDIConsumers) == 0 || len(policy.MDIConsumers) > 5) || len(policy.MDI) == 0 && (len(policy.MDINotices) != 0 || len(policy.MDIConsumers) != 0) {
		return fail(fmt.Errorf("MDI provenance policy incomplete"))
	}
	seenMDI := map[string]bool{}
	for _, mdi := range policy.MDIConsumers {
		if seenMDI[mdi.Path] {
			return fail(fmt.Errorf("duplicate MDI consumer"))
		}
		seenMDI[mdi.Path] = true
		overlap := false
		for _, existing := range consumers {
			if existing.Path != mdi.Path {
				continue
			}
			if existing != mdi || mdi.Path != "console/src/app/modules/idp-table/idp-table.component.html" && mdi.Path != "console/src/app/modules/policies/idp-settings/idp-settings.component.html" {
				return fail(fmt.Errorf("ambiguous shared decoration consumer"))
			}
			overlap = true
		}
		if !overlap {
			consumers = append(consumers, mdi)
		}
	}
	for _, consumer := range consumers {
		if !textPath(consumer.Path) || !textHex(consumer.SHA256, 64) || seenConsumers[consumer.Path] {
			return fail(fmt.Errorf("neutral consumer policy differs"))
		}
		seenConsumers[consumer.Path] = true
		body, err := get(build, "prepared/source/"+consumer.Path, "neutral-source/"+consumer.Path, 1<<20)
		if err != nil || digest(body) != consumer.SHA256 {
			return fail(fmt.Errorf("neutral provider consumer changed"))
		}
	}
	var raw bytes.Buffer
	fmt.Fprintln(&raw, "Frozen Console copied-asset attribution. This inventory is not full distribution or image admission.")
	terms := map[string][]byte{}
	for _, n := range cat.Notices {
		if !textPath(n.Path) || !textHex(n.SHA256, 64) || terms[n.Path] != nil {
			return fail(fmt.Errorf("ambiguous original notice"))
		}
		b, e := get(notices, n.Path, "notices/"+n.Path, 1<<20)
		if e != nil || len(b) == 0 || digest(b) != n.SHA256 {
			return fail(fmt.Errorf("original notice differs"))
		}
		terms[n.Path] = b
		fmt.Fprintf(&raw, "\nFULL ORIGINAL RECORD %s\n", n.Path)
		raw.Write(b)
		raw.WriteByte('\n')
	}
	packages := map[string]string{}
	source := filepath.Join(build, "prepared/source")
	for _, a := range cat.Archives {
		decoded, sriErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(a.Integrity, "sha512-"))
		if (a.Family != "material-icons" && a.Family != "flag-icons") || packages[a.Family] != "" || !textHex(a.SHA256, 64) || a.Path != "archives/"+a.SHA256+".tgz" || a.LicensePath != "package/LICENSE" || terms[a.Notice] == nil || !textHex(a.PackageJSONSHA256, 64) || sriErr != nil || len(decoded) != 64 || a.Integrity != "sha512-"+base64.StdEncoding.EncodeToString(decoded) {
			return fail(fmt.Errorf("ambiguous original package archive"))
		}
		alias, e := copiedPackageAlias(source, a.Family)
		if e != nil {
			return fail(e)
		}
		metadata, e := get(source, alias.Resolved+"/package.json", "package/"+a.Family+"/package.json", 1<<20)
		if e != nil || digest(metadata) != a.PackageJSONSHA256 {
			return fail(fmt.Errorf("connected package metadata differs"))
		}
		if _, e = jsonObject(metadata); e != nil {
			return fail(e)
		}
		var identity struct{ Name, Version string }
		if e = json.Unmarshal(metadata, &identity); e != nil {
			return fail(e)
		}
		wantName, wantVersion := copiedPackageIdentity(a.Family)
		if identity.Name != wantName || identity.Version != wantVersion {
			return fail(fmt.Errorf("connected package identity differs"))
		}
		license, e := get(source, alias.Resolved+"/LICENSE", "package/"+a.Family+"/LICENSE", 1<<20)
		if e != nil || !bytes.Equal(license, terms[a.Notice]) {
			return fail(fmt.Errorf("original package license differs"))
		}
		packages[a.Family] = alias.Resolved
		state.Aliases = append(state.Aliases, alias)
		fmt.Fprintf(&raw, "\nCATALOGUE ORIGINAL PACKAGE ARCHIVE %s: SHA256 %s; %s. Connected originals below are checked byte-for-byte; complete archive capture/replay is verified by the selected-pnpm source boundary.\n", a.Path, a.SHA256, a.Integrity)
	}
	expected, sourceNames, compiled := map[string]bool{}, map[string]bool{}, map[string]bool{}
	seen := map[string]bool{}
	for _, a := range cat.Assets {
		key := a.Family + ":" + a.Path
		if !textPath(a.Path) || a.Bytes < 1 || a.Bytes > 4<<20 || !textHex(a.SHA256, 64) || seen[key] || len(a.Outputs) != 2 {
			return fail(fmt.Errorf("invalid selected copied asset"))
		}
		seen[key] = true
		var original []byte
		if a.Family == "translations" {
			if !strings.HasPrefix(a.Path, "console/src/assets/i18n/") || !strings.HasSuffix(a.Path, ".json") || !textHex(a.GitBlob, 40) || a.OriginalBlob != "" || a.OriginalPath != "" {
				return fail(fmt.Errorf("incomplete unmodified translation source identity"))
			}
			original, e = get(build, "prepared/source/"+a.Path, "source/"+a.Path, 4<<20)
			if e != nil || blob(original) != a.GitBlob || !utf8.Valid(original) {
				return fail(fmt.Errorf("original translation bytes differ"))
			}
			if _, e = jsonObject(original); e != nil {
				return fail(fmt.Errorf("invalid translation object: %w", e))
			}
			for _, context := range []struct{ source, notice string }{
				{"LICENSE", "ZITADEL-LICENSE"},
				{"LICENSING.md", "ZITADEL-LICENSING.md"},
				{"CONTRIBUTING.md", "ZITADEL-CONTRIBUTING.md"},
			} {
				body, e := get(build, "prepared/source/"+context.source, "source/"+context.source, 1<<20)
				if e != nil || terms[context.notice] == nil || !bytes.Equal(body, terms[context.notice]) {
					return fail(fmt.Errorf("original translation licensing context differs"))
				}
			}
			sourceNames[a.Path] = true
		} else if a.Family == "line-awesome" {
			if !strings.HasPrefix(a.Path, "console/src/assets/icons/line-awesome/") || !textHex(a.GitBlob, 40) || !textHex(a.OriginalBlob, 40) {
				return fail(fmt.Errorf("incomplete Line Awesome source identity"))
			}
			original, e = get(build, "prepared/source/"+a.Path, "source/"+a.Path, 4<<20)
			if e != nil || blob(original) != a.GitBlob {
				return fail(fmt.Errorf("original Line Awesome bytes differ"))
			}
			sourceNames[a.Path] = true
			if a.OriginalBlob != a.GitBlob {
				if a.OriginalPath == "" || terms[a.OriginalPath] == nil || blob(terms[a.OriginalPath]) != a.OriginalBlob {
					return fail(fmt.Errorf("modified CSS lacks its original source record"))
				}
				fmt.Fprintf(&raw, "\nMODIFIED SOURCE %s: frozen ZITADEL blob %s differs from retained original %s (%s). Both complete sources are retained.\n", a.Path, a.GitBlob, a.OriginalPath, a.OriginalBlob)
			} else if a.OriginalPath != "" {
				return fail(fmt.Errorf("ambiguous unmodified asset source"))
			}
		} else {
			pkg := packages[a.Family]
			if pkg == "" || !strings.HasPrefix(a.Path, "package/") || a.GitBlob != "" || a.OriginalBlob != "" || a.OriginalPath != "" {
				return fail(fmt.Errorf("unsupported copied-asset origin"))
			}
			original, e = get(source, pkg+"/"+strings.TrimPrefix(a.Path, "package/"), "package/"+a.Family+"/"+a.Path, 4<<20)
			if e != nil {
				return fail(e)
			}
		}
		if len(original) != a.Bytes || digest(original) != a.SHA256 {
			return fail(fmt.Errorf("selected original asset differs: %s", a.Path))
		}
		for i, out := range a.Outputs {
			prefix := []string{"console/dist/console/", "internal/api/ui/console/static/"}[i]
			if !textPath(out) || !strings.HasPrefix(out, prefix) || !copiedSelected(strings.TrimPrefix(out, prefix)) || expected[out] {
				return fail(fmt.Errorf("ambiguous selected asset output"))
			}
			if i == 1 && strings.TrimPrefix(out, prefix) != strings.TrimPrefix(a.Outputs[0], "console/dist/console/") {
				return fail(fmt.Errorf("unpaired selected output"))
			}
			expected[out] = true
			b, e := get(build, "prepared/source/"+out, "output/"+out, 4<<20)
			if e != nil || !bytes.Equal(original, b) {
				return fail(fmt.Errorf("selected output differs: %s", out))
			}
			if i == 1 {
				compiled[out] = true
			}
		}
		fmt.Fprintf(&raw, "\nSOURCE %s (%s)\nSHA256 %s; %d bytes\nOUTPUTS %s\n", a.Path, a.Family, a.SHA256, a.Bytes, strings.Join(a.Outputs, ", "))
		binding := copiedBinding{copiedAsset: a}
		if strings.HasSuffix(a.Path, ".ttf") {
			binding.Names, e = fontAttributionNames(original, false)
			if e != nil {
				return fail(e)
			}
			for _, n := range binding.Names {
				fmt.Fprintf(&raw, "\nORIGINAL FONT NAME %d (platform %d, encoding %d, language 0x%04x; bytes %d:%d; SHA256 %s)\n%s\n", n.ID, n.Platform, n.Encoding, n.Language, n.Offset, n.Bytes, n.SHA256, n.Text)
			}
		}
		state.Assets = append(state.Assets, binding)
	}
	if len(expected) != policy.Outputs {
		return fail(fmt.Errorf("selected copied-output count differs"))
	}
	mdi, err := copiedMDIObserve(build, notices, policy, get, sourceNames, expected, compiled, &raw)
	if err != nil {
		return fail(err)
	}
	state.Assets = append(state.Assets, mdi...)
	if len(expected) != policy.Outputs+2*len(policy.MDI) {
		return fail(fmt.Errorf("MDI copied-output count differs"))
	}
	if e = copiedFilesystemSet(filepath.Join(build, "prepared/source"), expected, sourceNames, func(name string) ([]byte, error) { return get(build, "prepared/source/"+name, "output/"+name, 64<<20) }); e != nil {
		return fail(e)
	}
	compiler, e := get(build, "packages.jsonl", "packages.jsonl", 64<<20)
	if e != nil {
		return fail(e)
	}
	if e = copiedCompilerSet(compiler, build, compiled); e != nil {
		return fail(e)
	}
	for _, before := range state.Aliases {
		after, e := copiedPackageAlias(source, before.Family)
		if e != nil || after != before {
			return fail(fmt.Errorf("connected package alias changed during observation"))
		}
	}
	if _, e = get(filepath.Dir(recipe), filepath.Base(recipe), "build-recipe", 1<<20); e != nil {
		return fail(e)
	}
	if _, e = get(filepath.Dir(recipe), "zitadel-copied-assets.go", "verifier/zitadel-copied-assets.go", 1<<20); e != nil {
		return fail(e)
	}
	if raw.Len() > 2<<20 {
		return fail(fmt.Errorf("excessive copied-asset notice text"))
	}
	state.Raw = raw.Bytes()
	sort.Slice(state.Inputs, func(i, j int) bool { return state.Inputs[i].Path < state.Inputs[j].Path })
	return state, nil
}

// These are the complete observed output families, not catalogue-driven globs:
// an additional asset in one of these classes must receive a new frozen review.
func copiedSelected(name string) bool {
	return strings.HasPrefix(name, "assets/icons/line-awesome/") || strings.HasPrefix(name, "assets/i18n/") || strings.HasPrefix(name, "assets/mdi/") || strings.HasPrefix(name, "media/MaterialIcons-Regular-") || strings.HasPrefix(name, "media/") && strings.HasSuffix(name, ".svg")
}

// copiedMDIObserve preserves complete terms and joins each retained native
// wrapper to both generated copies and the complete Console compiler selection.
// Primary hashes and attribution describe provenance, never trademark endorsement.
func copiedMDIObserve(build, notices string, policy copiedPolicy, get func(string, string, string, int64) ([]byte, error), sourceNames, expected, compiled map[string]bool, raw *bytes.Buffer) ([]copiedBinding, error) {
	terms := map[string]bool{}
	for _, notice := range policy.MDINotices {
		if !textPath(notice.Path) || !textHex(notice.SHA256, 64) || terms[notice.Path] {
			return nil, fmt.Errorf("ambiguous MDI original terms")
		}
		body, err := get(notices, notice.Path, "mdi-notices/"+notice.Path, 1<<20)
		if err != nil || len(body) == 0 || digest(body) != notice.SHA256 {
			return nil, fmt.Errorf("MDI original terms differ")
		}
		terms[notice.Path] = true
		fmt.Fprintf(raw, "\nFULL ORIGINAL MDI TERMS %s\n", notice.Path)
		raw.Write(body)
		raw.WriteByte('\n')
	}
	var result []copiedBinding
	usedTerms, seen := map[string]bool{}, map[string]bool{}
	for _, asset := range policy.MDI {
		if !textPath(asset.Path) || !strings.HasPrefix(asset.Path, "console/src/assets/mdi/") || strings.TrimPrefix(asset.Path, "console/src/assets/mdi/") != filepath.Base(asset.Path) || !strings.HasSuffix(asset.Path, ".svg") || asset.Bytes < 1 || asset.Bytes > 4<<20 || !textHex(asset.SHA256, 64) || !textHex(asset.GitBlob, 40) || !textHex(asset.PrimarySHA256, 64) || asset.Author == "" || len(asset.Author) > 128 || strings.ContainsAny(asset.Author, "\r\n") || !terms[asset.Notice] || seen[asset.Path] {
			return nil, fmt.Errorf("incomplete MDI provenance identity")
		}
		if !strings.HasPrefix(asset.PrimaryPath, "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/") && asset.PrimaryPath != "https://github.com/google/material-design-icons/blob/6d7ca43bd6e6668531a00fcaca06d921b63dd716/symbols/web/mail/materialsymbolsoutlined/mail_24px.svg" {
			return nil, fmt.Errorf("unbound MDI primary origin")
		}
		seen[asset.Path], usedTerms[asset.Notice] = true, true
		body, err := get(build, "prepared/source/"+asset.Path, "mdi-source/"+asset.Path, 4<<20)
		if err != nil || len(body) != asset.Bytes || digest(body) != asset.SHA256 || blob(body) != asset.GitBlob {
			return nil, fmt.Errorf("retained MDI original differs")
		}
		sourceNames[asset.Path] = true
		name := strings.TrimPrefix(asset.Path, "console/src/")
		outputs := []string{"console/dist/console/" + name, "internal/api/ui/console/static/" + name}
		for i, output := range outputs {
			if expected[output] {
				return nil, fmt.Errorf("duplicate retained MDI output")
			}
			copy, err := get(build, "prepared/source/"+output, "mdi-output/"+output, 4<<20)
			if err != nil || !bytes.Equal(copy, body) {
				return nil, fmt.Errorf("retained MDI output differs")
			}
			expected[output] = true
			if i == 1 {
				compiled[output] = true
			}
		}
		fmt.Fprintf(raw, "\nRETAINED MDI SOURCE %s\nSHA256 %s; Git blob %s; %d bytes. Native wrapper retained unchanged.\nPRIMARY %s\nPRIMARY SHA256 %s; author %s; complete terms %s. Native wrapper differs from primary: %t. Both native source and its modification/provenance record are retained; no trademark endorsement is implied.\nOUTPUTS %s\n", asset.Path, asset.SHA256, asset.GitBlob, asset.Bytes, asset.PrimaryPath, asset.PrimarySHA256, asset.Author, asset.Notice, asset.SHA256 != asset.PrimarySHA256, strings.Join(outputs, ", "))
		result = append(result, copiedBinding{copiedAsset: copiedAsset{Family: "mdi", Path: asset.Path, Bytes: asset.Bytes, SHA256: asset.SHA256, GitBlob: asset.GitBlob, Outputs: outputs}})
	}
	if len(usedTerms) != len(terms) {
		return nil, fmt.Errorf("unused MDI terms")
	}
	return result, nil
}

// copiedFilesystemSet rejects linked, special and unexpected copied-asset members
// and any retired provider artwork in the actual source and generated outputs.
func copiedFilesystemSet(source string, expected, sourceNames map[string]bool, observe func(string) ([]byte, error)) error {
	if err := neutralProviderSourceFamilies(source); err != nil {
		return err
	}
	prefixes := []string{"console/src/assets/icons/line-awesome", "console/src/assets/i18n", "console/dist/console", "internal/api/ui/console/static"}
	for name := range sourceNames {
		if strings.HasPrefix(name, "console/src/assets/mdi/") {
			prefixes = append(prefixes, "console/src/assets/mdi")
			break
		}
	}
	for _, prefix := range prefixes {
		e := filepath.WalkDir(filepath.Join(source, prefix), func(name string, entry fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			relative, e := filepath.Rel(source, name)
			if e != nil {
				return e
			}
			relative = filepath.ToSlash(relative)
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				return fmt.Errorf("special copied-asset path")
			}
			if strings.HasPrefix(prefix, "console/src/") {
				if !sourceNames[relative] {
					return fmt.Errorf("unexpected copied source: %s", relative)
				}
			} else {
				if err := neutralProviderOutput(relative, nil); err != nil {
					return err
				}
				switch strings.ToLower(filepath.Ext(relative)) {
				case ".html", ".js", ".css", ".json", ".map", ".scss", ".svg":
					body, err := observe(relative)
					if err != nil {
						return err
					}
					if err = neutralProviderOutput(relative, body); err != nil {
						return err
					}
				}
				member := strings.TrimPrefix(relative, prefix+"/")
				if copiedSelected(member) && !expected[relative] {
					return fmt.Errorf("unexpected copied output: %s", relative)
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// copiedCompilerSet rejects failed observations and retired images throughout
// the compiler stream, while comparing catalogue membership only for Console.
func copiedCompilerSet(data []byte, build string, expected map[string]bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	seen := false
	for {
		var raw json.RawMessage
		e := d.Decode(&raw)
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if _, e = jsonObject(raw); e != nil {
			return e
		}
		var p struct {
			ImportPath, Dir           string
			EmbedPatterns, EmbedFiles []string
			Incomplete                bool
			Error                     json.RawMessage
			DepsErrors                []json.RawMessage
		}
		if e = json.Unmarshal(raw, &p); e != nil || p.Incomplete || len(p.Error) > 0 && string(p.Error) != "null" || len(p.DepsErrors) > 0 {
			return fmt.Errorf("failed compiler observation")
		}
		// Retired graphics must be absent from every compiled package, before
		// selecting the Console package or its retained catalogue subset.
		for _, name := range p.EmbedFiles {
			if err := neutralProviderOutput(name, nil); err != nil {
				return err
			}
		}
		if p.ImportPath != "github.com/zitadel/zitadel/internal/api/ui/console" {
			continue
		}
		if seen || p.Dir != filepath.Join(build, "prepared/source/internal/api/ui/console") || !reflect.DeepEqual(p.EmbedPatterns, []string{"static"}) {
			return fmt.Errorf("ambiguous Console compiler selection")
		}
		seen = true
		all, selected := map[string]bool{}, map[string]bool{}
		for _, name := range p.EmbedFiles {
			if !textPath(name) || all[name] {
				return fmt.Errorf("invalid compiler member")
			}
			all[name] = true
			if strings.HasPrefix(name, "static/") && copiedSelected(strings.TrimPrefix(name, "static/")) {
				selected["internal/api/ui/console/"+name] = true
			}
		}
		if !reflect.DeepEqual(selected, expected) {
			return fmt.Errorf("copied-asset compiler set differs")
		}
	}
	if !seen {
		return fmt.Errorf("missing copied-asset compiler selection")
	}
	return nil
}

func copiedPackageIdentity(family string) (string, string) {
	if family == "flag-icons" {
		return "flag-icons", "7.5.0"
	}
	if family == "material-icons" {
		return "material-design-icons-iconfont", "6.7.0"
	}
	return "", ""
}

// Allow the same confined single-hop pnpm boundary used by the existing notice
// collector. Originals are inspected after the actual frontend build, before
// compilation; full original-archive verification remains the source adapter's
// separate capture/replay boundary, rather than a second tar interpreter here.
func copiedPackageAlias(source, family string) (copiedAlias, error) {
	name, version := copiedPackageIdentity(family)
	if name == "" {
		return copiedAlias{}, fmt.Errorf("unsupported connected package")
	}
	alias := "console/node_modules/" + name
	for _, parent := range []string{"console", "console/node_modules"} {
		st, e := os.Lstat(filepath.Join(source, parent))
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return copiedAlias{}, fmt.Errorf("linked or unavailable package parent")
		}
	}
	target, e := os.Readlink(filepath.Join(source, alias))
	if e != nil || target == "" || len(target) > 4096 || filepath.IsAbs(target) {
		return copiedAlias{}, fmt.Errorf("invalid connected package alias")
	}
	resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(alias), target)))
	want := "node_modules/.pnpm/" + name + "@" + version + "/node_modules/" + name
	if resolved != want || target != "../../"+want {
		return copiedAlias{}, fmt.Errorf("escaping or unselected connected package")
	}
	return copiedAlias{Family: family, Path: alias, Target: target, Resolved: resolved}, nil
}

// mdiProvenance binds retained native wrappers to their immutable primary origins.
type mdiProvenance struct {
	Path                                                        string
	Bytes                                                       int
	SHA256, GitBlob, PrimaryPath, PrimarySHA256, Author, Notice string
}

var mdiFrozen = []mdiProvenance{
	{Path: "console/src/assets/mdi/account-cancel-outline.svg", Bytes: 922, SHA256: "8f6a46b284db9315478f57ec4c728ba363b1b298d45320ae5ea5121751be2972", GitBlob: "01ca0712bb2b1004767a9168bfa25649075663b4", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/account-cancel-outline.svg", PrimarySHA256: "14d164d4a2ad635bf760f2de6b6c30ab2e8c4329ac69092b1ea4641dc3e0242c", Author: "Simran", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/account-check-outline.svg", Bytes: 627, SHA256: "3c7c793f99efe88f26f5e81f2d64f863d7128f41ab56c77da5f787b93b3fef83", GitBlob: "dd3d59285e24c4d78ce86a8ed8a6d765d7f87a3a", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/account-check-outline.svg", PrimarySHA256: "a6f32eb17b2657c4e4b3975a60206d334b4b975586135357f4a676fc20389f78", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/api.svg", Bytes: 460, SHA256: "cc63969631086dcd9b6bd0f6468357ae101df4bafa725b1be082c2a0679a7273", GitBlob: "38dfa2a980511d9f45713b3eed026b7f9ebcb23a", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/api.svg", PrimarySHA256: "4381cb06997f09c055ff42a4356579e6c558b2dd427b32e3a331722233c28b3c", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/arrow-decision-outline.svg", Bytes: 758, SHA256: "905827dc06b70f528f3554dbe3375b8649975dd41d77935d3400f3aefdcab684", GitBlob: "1ab13649c1956a42b98325b6c2980a18d5eb12e4", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/arrow-decision-outline.svg", PrimarySHA256: "97c0ccb9ca07c47c0d93d16e2fea8174cb18e293f8248510703eefcff24d267b", Author: "Michael Richins", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/arrow-expand.svg", Bytes: 220, SHA256: "dbcf265c15ecaad1e1838e4b32c0fe435622f11d4e1f4114cf0d33a7b48407e8", GitBlob: "6e1f0076c90f2d3444bd8fe05fe8d2bc95e9df72", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/arrow-expand.svg", PrimarySHA256: "cce26ca5435273e9e075dd29db1143775287c427a2af2355573db74e6f8f33b0", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/arrow-right-bottom.svg", Bytes: 422, SHA256: "2a6ab46fac9c301964183127184a92c46c3c1bd35fa64cf6ddcd60724da03100", GitBlob: "33848b9af717542183f4c04a8fb97c5c3e404c43", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/arrow-right-bottom.svg", PrimarySHA256: "d3e7e17a0fd4c3021ef7ea609c491d3464fddb566fafd76810d8b195de19d3fa", Author: "Colton Wiscombe", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/broom.svg", Bytes: 531, SHA256: "ef5cbd4c4378386bc8326fcfbc36e750d78f1412aafcae4f446f59e5153dd9d6", GitBlob: "1e61d76750802eac462b64faa8b5ad13aa389966", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/broom.svg", PrimarySHA256: "7a4a63bfcd9c216cd7199ad74912f735a3315c692996bf71a2f3fdedbe4dc9aa", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/content-copy.svg", Bytes: 417, SHA256: "f24c47ca28e49c25e74d695ca635ab3747bca373dfeb4eeaaa88c98c461c5a5a", GitBlob: "0968db1a753b41d152c93f42459c8dd579d51ef3", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/content-copy.svg", PrimarySHA256: "5584bbc724b175dc9a197294266ec1c579b798149333ea65ebdd811fbd02e7c1", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/counter.svg", Bytes: 957, SHA256: "5d761bb9b8cddb5338aa41f2098319a69a130a59eade692843f420f2e8f5c4b5", GitBlob: "613b110ae6901043ae3538bb36cce5cf740b1d6f", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/counter.svg", PrimarySHA256: "17602cbe113282a867e9d390091ee46587a848b2675bb3384be57b0b024ebedf", Author: "Christopher Schreiner", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/format-letter-case-lower.svg", Bytes: 1199, SHA256: "a5016e35d22e5acea3bd00958730e98385ba09c934dd3ea7cf15ba36988cd7fc", GitBlob: "ea48e78a219aadd8142aefa9bd704027ffeacda5", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/format-letter-case-lower.svg", PrimarySHA256: "9317f9fe414593739b6556e08c08b52bb3ceb70eb016d3e118b0125a14e78dc4", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/format-letter-case-upper.svg", Bytes: 1195, SHA256: "ee0684e2c0f6365aace6b792afff8af4989a2dd0cf02587925cc8f086017f0a1", GitBlob: "36cb7e19d9ac010e6cb81512aa6043b51c492b0a", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/format-letter-case-upper.svg", PrimarySHA256: "16a9661547440e57d9e7add82c11c51dd0b7a9150e914f5899a2cc6028942965", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/lightbulb-off-outline.svg", Bytes: 682, SHA256: "1aa5a7e9e0defa5e8e10b3898150b390018342e1f17ae387aefebde7feac5134", GitBlob: "cc1a4f89e0c4ffba34a02273de8f431d19fe8b66", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/lightbulb-off-outline.svg", PrimarySHA256: "26343d8b4c52c905072d32b9a38f7e3ac56f382ea458424c3c5877be4defcbc4", Author: "Hans Böhm", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/lightbulb-on-outline.svg", Bytes: 728, SHA256: "2f4dfb3dd2b08c565991ae567dd2e574b34f5efdb59cb53634af06ec8314a238", GitBlob: "cc8b9f780d89ca8d6ab795aa6191567d94738078", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/lightbulb-on-outline.svg", PrimarySHA256: "c1d233a37d67a340f9f42b578d70f447b4d5c22e7e96bed88dd74112f3348921", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/lock-question.svg", Bytes: 1043, SHA256: "eaa5977b8c3aa7d0913f5e527b01c16d2e0c9d17471213b1c6ad3f41c6fca578", GitBlob: "99495d9fd525ab077a811ca9e5ef5a410232530b", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/lock-question.svg", PrimarySHA256: "e2482adffc1aad0291d64280066d322d0f2f1ffcd5ce2f3d2176387d7287adf7", Author: "Simran", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/lock-reset.svg", Bytes: 876, SHA256: "f2ab09fdcdf9c5761b71202ff4674a8bc31abe80b5b6e26f36b65f3ccdffa0aa", GitBlob: "817a328bda95283f58c0988203d4a6365d5320c4", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/lock-reset.svg", PrimarySHA256: "e17c400d3b8308b1a0df55e60fdf211bed600a794eaeeb18137fc2962544b87e", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/mail.svg", Bytes: 309, SHA256: "05f47e61b392b3d9a8f0d9c6d11503f276841901efc5869a2b17f9f57b912870", GitBlob: "15e1d12d4e1a21ead0cf4a03f0dd10fb48d884a4", PrimaryPath: "https://github.com/google/material-design-icons/blob/6d7ca43bd6e6668531a00fcaca06d921b63dd716/symbols/web/mail/materialsymbolsoutlined/mail_24px.svg", PrimarySHA256: "05f47e61b392b3d9a8f0d9c6d11503f276841901efc5869a2b17f9f57b912870", Author: "Google", Notice: "Google-Mail-LICENSE"},
	{Path: "console/src/assets/mdi/numeric.svg", Bytes: 538, SHA256: "613206599b8162ee3359e6478a914b1c768b14035cc82fec229462c876e32a09", GitBlob: "8bd26d9fcdbf2c34048d0aa8555323fa812b0c46", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/numeric.svg", PrimarySHA256: "b2516da37500d52f0a43ff84eb36900c0e75801cf5118a93af1daca4c2711b07", Author: "Austin Andrews", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/pin-outline.svg", Bytes: 391, SHA256: "0314936a951083cda9223396c9639a285547aab344413b493443c023c86f9aeb", GitBlob: "5d2799f9825abcb265690e7881449d16ebcec316", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/pin-outline.svg", PrimarySHA256: "6048039bfe70cb103571416b5621db086f15ac9165ed9a3e0ce4a0c398e87c3a", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/pin.svg", Bytes: 354, SHA256: "6da5256e2bf228878047406b37faf135103f36e46f189bd5151582ac52b5244b", GitBlob: "22840369872cd00f25b8b582929e63fc0e38d76b", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/pin.svg", PrimarySHA256: "2f17cf4653d23302a5dd5695e814057322df1af2747b77b608055b0e4d3be539", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/radar.svg", Bytes: 761, SHA256: "ea31f4277b0166f24c0e51e9ad33740aae72c905e2b3ea5557d8813a59c7c99e", GitBlob: "aadfaa5b51def0ae61acafcdfb498c53c4fbb8b1", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/radar.svg", PrimarySHA256: "dff0497a00d7d72ab5f8c76f0bd3f6f97f0c8e2e72f9eab023784a767837060c", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/shield-alert.svg", Bytes: 390, SHA256: "fed8d1d1d364858530cb82a87638a6a35f04bd8c54450d6c2c0a63d8af2391fe", GitBlob: "a8fcb7abfd2b9b9e9e0a44323b5cdbad0a107602", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/shield-alert.svg", PrimarySHA256: "1cb13b4fe587c416952eb3855eb083e4f6f8c6c3a1a0ed0ec5bf3c39fc590c4e", Author: "Peter Noble", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/shield-check.svg", Bytes: 198, SHA256: "9c17250755c41530f8e5d51ef030ebb2d8b71682be4aa3209363ed930d65998b", GitBlob: "1d6fa58b22d622419c9256580b96f3b43dfdb796", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/shield-check.svg", PrimarySHA256: "e64250b11f695be074c753f54c4e06146153b0f3ffce57d6285e1db38565f972", Author: "Google", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/symbol.svg", Bytes: 594, SHA256: "3dac682300a0ce63e54aa76349f800f4b82de7cb545ea824194a3e9c618e4421", GitBlob: "0ebc772f39ca73729ff46dd1a90c8c15f6a3bb3a", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/symbol.svg", PrimarySHA256: "93314ad702d3bf1b181f46bff8aa0f0e9a0d5a423ffbfbdec46269d3264008f5", Author: "Haley Halcyon", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/textbox-password.svg", Bytes: 756, SHA256: "9c635718c3c5d3774179afa2944b4fb170eaf0ecd6280b6d7efcbd40d1fe5eca", GitBlob: "6f0743b6fa2f90bf4d1583314bd7bd062c4db926", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/form-textbox-password.svg", PrimarySHA256: "38dc71188bac84dd81c70f6bea5c100e5172572c37cc9342a603cf75665d847c", Author: "Michael Richins", Notice: "Pictogrammers-LICENSE"},
	{Path: "console/src/assets/mdi/usb-flash-drive-outline.svg", Bytes: 1037, SHA256: "9762ef5b6e151cf527c162f8de66f975298459ca4f13a176ee497af75dbcde40", GitBlob: "978edf050ae35760a214a82fddbcac955151cc4c", PrimaryPath: "https://github.com/Templarian/MaterialDesign-SVG/blob/9e04201d4557e729822fb57f62a316c3dea1d4a8/svg/usb-flash-drive-outline.svg", PrimarySHA256: "f809b8a5d9ef5681dcbe76459d6beef44275459c3f091bb413c58817315e193f", Author: "Yaroslav Bandura", Notice: "Pictogrammers-LICENSE"},
}
