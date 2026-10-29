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
}

var copiedFrozen = copiedPolicy{CatalogueHash: "b457cbb2df1ecc95fd91a015d2d2def35298ef0cb445768104d128be9c062186", Assets: 1858, Outputs: 3716, ProviderConsumers: neutralProviderConsumers(), SMTPConsumers: neutralSMTPConsumers()}

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
	return strings.HasPrefix(name, "assets/icons/line-awesome/") || strings.HasPrefix(name, "assets/i18n/") || strings.HasPrefix(name, "media/MaterialIcons-Regular-") || strings.HasPrefix(name, "media/") && strings.HasSuffix(name, ".svg")
}

// copiedFilesystemSet rejects linked, special and unexpected copied-asset members
// and any retired provider artwork in the actual source and generated outputs.
func copiedFilesystemSet(source string, expected, sourceNames map[string]bool, observe func(string) ([]byte, error)) error {
	if err := neutralProviderSourceFamilies(source); err != nil {
		return err
	}
	for _, prefix := range []string{"console/src/assets/icons/line-awesome", "console/src/assets/i18n", "console/dist/console", "internal/api/ui/console/static"} {
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
