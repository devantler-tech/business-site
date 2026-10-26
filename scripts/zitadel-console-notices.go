// Command zitadel-console-notices captures selected same-build notice evidence.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const noticePin = "8afa5dd37a8e723efb9eace8d09fd5c1cccef694"
const noticeConsole = "console/dist/console/3rdpartylicenses.txt"
const noticeEmbedded = "internal/api/ui/console/static/3rdpartylicenses.txt"
const noticeBefore = "console-notices.before.json"
const noticeRaw = "console-third-party-notices.txt"
const noticeLimit = 2 << 20
const noticeSnapshotLimit = 4 << 20
const sourceLicensePath = "prepared/source/LICENSE"
const sourceLicenseRaw = "zitadel-root-license.txt"
const sourceLicenseHash = "4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b"
const moduleCatalogueHash = "dfdff48e6a821277bfc9fa1b2a2133e316dfc8850c6f4fc13cdbf97625c17047"
const moduleNoticeRaw = "go-module-notices.txt"

type noticeReader func(string, string, int64) ([]byte, error)

type supplementState struct {
	CatalogueSHA256 string              `json:"catalogueSha256"`
	Text            []byte              `json:"text"`
	Inputs          []noticeBinding     `json:"inputs"`
	Aliases         []supplementAlias   `json:"aliases"`
	Sections        []supplementSection `json:"sections"`
	Catalogue       supplementCatalogue `json:"catalogue"`
}

type supplementAlias struct {
	Package  string `json:"package"`
	Path     string `json:"path"`
	Target   string `json:"target"`
	Resolved string `json:"resolved"`
}

type supplementComponent struct {
	Role           string               `json:"role"`
	Kind           string               `json:"kind"`
	Original       string               `json:"original"`
	OriginalBytes  int                  `json:"originalBytes"`
	OriginalSHA256 string               `json:"originalSha256"`
	Offset         int                  `json:"offset"`
	Bytes          int                  `json:"bytes"`
	SHA256         string               `json:"sha256"`
	Retained       string               `json:"retained"`
	GitBlob        string               `json:"gitBlob,omitempty"`
	SourceMap      *supplementSourceMap `json:"sourceMap,omitempty"`
}

type supplementSourceMap struct {
	Index  *int   `json:"index"`
	Source string `json:"source"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type supplementIdentity struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type supplementPackage struct {
	Name               string                `json:"name"`
	Version            string                `json:"version"`
	SRI                string                `json:"sri"`
	Tarball            string                `json:"tarball"`
	PackageJSONSHA256  string                `json:"packageJsonSha256"`
	Components         []supplementComponent `json:"components"`
	IdentityOnlyInputs []supplementIdentity  `json:"identityOnlyInputs,omitempty"`
}

type supplementCatalogue struct {
	SchemaVersion     int                 `json:"schemaVersion"`
	UpstreamCommit    string              `json:"upstreamCommit"`
	LockSHA256        string              `json:"lockSha256"`
	NgIconsProvenance string              `json:"ngIconsProvenance"`
	Packages          []supplementPackage `json:"packages"`
}

type supplementSection struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Role    string `json:"role"`
	Source  string `json:"source"`
	Offset  int    `json:"offset"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

const supplementCatalogueHash = "4f964bd659a7656f7951c363535120af0fe881be37b1ac522e0964dd41647206"
const supplementRaw = "console-notices-supplement.txt"
const supplementInventoryPending = "console-notices-supplement-inventory.pending"

// supplementResolve admits one normal pnpm alias hop into this prepared store.
// Every target component is subsequently checked by noticeRead; no global link
// policy is relaxed and no outside target content is read.
func supplementResolve(source, alias, name string) (supplementAlias, error) {
	var result supplementAlias
	if path.Clean(alias) != alias || path.IsAbs(alias) || strings.HasPrefix(alias, "../") {
		return result, fmt.Errorf("unclean package alias")
	}
	parts := strings.Split(alias, "/")
	for i := 1; i < len(parts); i++ {
		info, err := os.Lstat(filepath.Join(source, filepath.FromSlash(strings.Join(parts[:i], "/"))))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("linked or unavailable alias parent")
		}
	}
	link, err := os.Readlink(filepath.Join(source, filepath.FromSlash(alias)))
	if err != nil || link == "" || len(link) > 4096 || filepath.IsAbs(link) {
		return result, fmt.Errorf("invalid package alias")
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(alias)), link))
	rel := filepath.ToSlash(target)
	prefix := "node_modules/.pnpm/"
	suffix := "/node_modules/" + name
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, suffix) {
		return result, fmt.Errorf("escaping or unsupported package alias")
	}
	store := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), suffix)
	if store == "" || strings.Contains(store, "/") || store == "." || store == ".." {
		return result, fmt.Errorf("ambiguous package store")
	}
	return supplementAlias{Package: name, Path: alias, Target: link, Resolved: rel}, nil
}

// noticeSupplementSnapshot checks a hash-pinned nine-package closure (eight
// notice subjects plus qrcode's parent). It emits only checked selected bytes.
func noticeSupplementSnapshot(build, notices, expected string, read noticeReader) (*supplementState, error) {
	data, err := read(notices, "catalogue.json", 1<<20)
	if err != nil || noticeDigest(data) != expected {
		return nil, fmt.Errorf("supplement catalogue identity differs")
	}
	if _, err = noticeObject(data); err != nil {
		return nil, err
	}
	var catalogue supplementCatalogue
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&catalogue); err != nil {
		return nil, fmt.Errorf("invalid supplement catalogue: %w", err)
	}
	if catalogue.SchemaVersion != 1 || catalogue.UpstreamCommit != noticePin || len(catalogue.Packages) != 9 {
		return nil, fmt.Errorf("unsupported supplement catalogue")
	}
	lock, err := read(build, "prepared/source/pnpm-lock.yaml", 16<<20)
	if err != nil || noticeDigest(lock) != catalogue.LockSHA256 {
		return nil, fmt.Errorf("supplement frozen lock differs")
	}
	result := &supplementState{CatalogueSHA256: expected, Catalogue: catalogue, Inputs: []noticeBinding{{"catalogue.json", expected}}}
	names := []string{"qrcode", "@ng-icons/core", "grpc-web", "@connectrpc/connect", "@connectrpc/connect-web", "@bufbuild/protobuf", "google-protobuf", "angularx-qrcode", "opentype.js"}
	versions := []string{"1.5.4", "33.4.0", "2.0.2", "2.1.2", "2.1.2", "2.13.0", "4.0.2", "21.0.5", "1.3.5"}
	source := filepath.Join(build, "prepared/source")
	resolved := map[string]supplementAlias{}
	// The parent resolves first. qrcode is its sibling edge in this pnpm
	// node_modules directory, never a guessed/hoisted console dependency.
	for _, i := range []int{7, 0, 1, 2, 3, 4, 5, 6, 8} {
		pkg := catalogue.Packages[i]
		if pkg.Name != names[i] || pkg.Version != versions[i] || !strings.HasPrefix(pkg.SRI, "sha512-") || !noticeDigestPattern.MatchString(pkg.PackageJSONSHA256) {
			return nil, fmt.Errorf("unsupported supplement package")
		}
		alias := "console/node_modules/" + pkg.Name
		if i == 0 {
			alias = path.Join(path.Dir(resolved["angularx-qrcode"].Resolved), "qrcode")
		}
		observation, e := supplementResolve(source, alias, pkg.Name)
		if e != nil {
			return nil, e
		}
		metadata, e := read(source, observation.Resolved+"/package.json", 1<<20)
		if e != nil || noticeDigest(metadata) != pkg.PackageJSONSHA256 {
			return nil, fmt.Errorf("supplement package metadata differs")
		}
		obj, e := noticeObject(metadata)
		if e != nil || obj["name"] != pkg.Name || obj["version"] != pkg.Version {
			return nil, fmt.Errorf("supplement package identity differs")
		}
		if i == 7 {
			deps, ok := obj["dependencies"].(map[string]any)
			if !ok || deps["qrcode"] != "1.5.4" {
				return nil, fmt.Errorf("qrcode parent edge differs")
			}
		}
		resolved[pkg.Name] = observation
		result.Aliases = append(result.Aliases, observation)
		result.Inputs = append(result.Inputs, noticeBinding{"prepared/source/" + observation.Resolved + "/package.json", noticeDigest(metadata)})
	}
	for _, pkg := range catalogue.Packages {
		root := resolved[pkg.Name].Resolved
		if pkg.Name != "angularx-qrcode" && len(pkg.Components) == 0 {
			return nil, fmt.Errorf("supplement terms absent")
		}
		roles := map[string]bool{}
		for _, component := range pkg.Components {
			if component.Role == "" || roles[component.Role] || component.Retained == "" || path.Base(component.Retained) != component.Retained || component.Bytes < 1 || component.Bytes > noticeLimit || !noticeDigestPattern.MatchString(component.SHA256) {
				return nil, fmt.Errorf("invalid or duplicate supplement component")
			}
			roles[component.Role] = true
			if component.Kind != "installed-source-map" && component.SourceMap != nil {
				return nil, fmt.Errorf("unexpected source-map descriptor")
			}
			text, e := read(notices, component.Retained, noticeLimit)
			if e != nil || len(text) != component.Bytes || noticeDigest(text) != component.SHA256 || !utf8.Valid(text) {
				return nil, fmt.Errorf("supplement retained text differs")
			}
			sectionSource := component.Original
			if component.Kind == "installed" || component.Kind == "installed-source-map" {
				original, e := read(source, root+"/"+component.Original, 2<<20)
				if e != nil || len(original) != component.OriginalBytes || noticeDigest(original) != component.OriginalSHA256 {
					return nil, fmt.Errorf("supplement installed text identity differs")
				}
				result.Inputs = append(result.Inputs, noticeBinding{"prepared/source/" + root + "/" + component.Original, noticeDigest(original)})
				selected := original
				if component.Kind == "installed-source-map" {
					selected, e = supplementMappedSource(original, component.SourceMap)
					if e != nil {
						return nil, e
					}
					sectionSource += fmt.Sprintf("#sourcesContent[%d]:%s", *component.SourceMap.Index, component.SourceMap.Source)
				}
				if component.Offset < 0 || component.Offset > len(selected) || component.Bytes > len(selected)-component.Offset || !bytes.Equal(text, selected[component.Offset:component.Offset+component.Bytes]) {
					return nil, fmt.Errorf("supplement selected text differs")
				}
			} else {
				if !strings.HasPrefix(component.Original, "https://raw.githubusercontent.com/") || !noticeDigestPattern.MatchString(component.OriginalSHA256) || component.OriginalBytes < 1 || component.OriginalBytes > 2<<20 || component.Offset < 0 || component.Offset > component.OriginalBytes || component.Bytes > component.OriginalBytes-component.Offset {
					return nil, fmt.Errorf("invalid immutable supplement source")
				}
				switch component.Kind {
				case "immutable-source":
					if component.Offset != 0 || component.Bytes != component.OriginalBytes || component.SHA256 != component.OriginalSHA256 {
						return nil, fmt.Errorf("complete immutable source identity differs")
					}
				case "immutable-source-header":
					// The full source digest is a curation observation, not a
					// build-time read. Only this separately pinned span is emitted.
				default:
					return nil, fmt.Errorf("unsupported supplement source")
				}
			}
			result.Inputs = append(result.Inputs, noticeBinding{component.Retained, noticeDigest(text)})
			header := fmt.Sprintf("Package: %s@%s\nComponent: %s\nSource: %s\n\n", pkg.Name, pkg.Version, component.Role, sectionSource)
			result.Text = append(result.Text, []byte(header)...)
			result.Sections = append(result.Sections, supplementSection{pkg.Name, pkg.Version, component.Role, sectionSource, len(result.Text), len(text), noticeDigest(text)})
			result.Text = append(result.Text, text...)
			result.Text = append(result.Text, '\n', '\n')
		}
		for _, identity := range pkg.IdentityOnlyInputs {
			original, e := read(source, root+"/"+identity.Path, 2<<20)
			if e != nil || len(original) != identity.Bytes || noticeDigest(original) != identity.SHA256 {
				return nil, fmt.Errorf("supplement package code identity differs")
			}
			result.Inputs = append(result.Inputs, noticeBinding{"prepared/source/" + root + "/" + identity.Path, noticeDigest(original)})
		}
	}
	if len(result.Text) > noticeLimit {
		return nil, fmt.Errorf("supplement exceeds limit")
	}
	return result, nil
}

// supplementMappedSource checks one exact embedded source. Logical identifiers
// are inert strings: they are never resolved as file paths or fetched as URLs.
func supplementMappedSource(data []byte, selected *supplementSourceMap) ([]byte, error) {
	if selected == nil || selected.Index == nil || *selected.Index < 0 || selected.Source == "" || len(selected.Source) > 4096 || selected.Bytes < 1 || selected.Bytes > noticeLimit || !noticeDigestPattern.MatchString(selected.SHA256) || !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid source-map descriptor")
	}
	if _, err := noticeObject(data); err != nil {
		return nil, fmt.Errorf("invalid source map: %w", err)
	}
	var document struct {
		Version  int               `json:"version"`
		Sources  []string          `json:"sources"`
		Contents []json.RawMessage `json:"sourcesContent"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Version != 3 || len(document.Sources) != len(document.Contents) || *selected.Index >= len(document.Sources) || document.Sources[*selected.Index] != selected.Source {
		return nil, fmt.Errorf("invalid source-map selection")
	}
	count := 0
	for _, identifier := range document.Sources {
		if identifier == selected.Source {
			count++
		}
	}
	var decoded string
	if count != 1 || json.Unmarshal(document.Contents[*selected.Index], &decoded) != nil || len(decoded) != selected.Bytes || noticeDigest([]byte(decoded)) != selected.SHA256 {
		return nil, fmt.Errorf("decoded source-map identity differs")
	}
	return []byte(decoded), nil
}

type noticeState struct {
	SchemaVersion     int                  `json:"schemaVersion"`
	Raw               []byte               `json:"raw"`
	PreparationSHA256 string               `json:"preparationSha256"`
	LockSHA256        string               `json:"lockSha256"`
	InputsSHA256      string               `json:"inputsSha256"`
	PackagesSHA256    string               `json:"packagesSha256"`
	RecipeSHA256      string               `json:"recipeSha256"`
	Supplement        *supplementState     `json:"supplement,omitempty"`
	Toolchain         *noticeToolchain     `json:"toolchain,omitempty"`
	SourceLicense     *noticeSourceLicense `json:"sourceLicense,omitempty"`
	Modules           *noticeModules       `json:"modules,omitempty"`
}

type noticeSupplementConfig struct {
	Directory, CatalogueHash, ToolchainRoot string
	SourceLicense                           bool
	ModuleDirectory, ModuleCatalogueHash    string
	RetainedGoCache                         bool
}

type noticeModuleIdentity struct {
	Path     string
	Version  string
	Sum      string
	GoModSum string
	Main     bool
	Replace  *noticeModuleIdentity
}

type noticeModuleComponent struct {
	File    string `json:"file"`
	Package string `json:"package,omitempty"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type noticeModuleEntry struct {
	Original   noticeModuleIdentity    `json:"original"`
	Effective  noticeModuleIdentity    `json:"effective"`
	Components []noticeModuleComponent `json:"components"`
	Origins    []noticeModuleOrigin    `json:"origins,omitempty"`
}

type noticeOriginSpan struct {
	Offset int    `json:"offset"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type noticeOriginTerm struct {
	Module string `json:"module"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type noticeOriginExternal struct {
	File     string `json:"file"`
	Upstream string `json:"upstream"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
}

type noticeOriginInput struct {
	File   string `json:"file"`
	Kind   string `json:"kind"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type noticeModuleOrigin struct {
	File         string                 `json:"file"`
	Package      string                 `json:"package"`
	Bytes        int                    `json:"bytes"`
	SHA256       string                 `json:"sha256"`
	Spans        []noticeOriginSpan     `json:"spans"`
	Terms        []noticeOriginTerm     `json:"terms,omitempty"`
	Notices      []noticeOriginExternal `json:"notices,omitempty"`
	SourceInputs []noticeOriginExternal `json:"sourceInputs,omitempty"`
	Inputs       []noticeOriginInput    `json:"inputs,omitempty"`
}

type noticeOriginSection struct {
	SourceOffset int    `json:"sourceOffset"`
	Offset       int    `json:"offset"`
	Bytes        int    `json:"bytes"`
	SHA256       string `json:"sha256"`
}

type noticeOriginExternalSection struct {
	noticeOriginExternal
	Offset int `json:"offset"`
}

type noticeOriginRecord struct {
	Source       string                        `json:"source"`
	Package      string                        `json:"package"`
	Bytes        int                           `json:"bytes"`
	SHA256       string                        `json:"sha256"`
	Spans        []noticeOriginSection         `json:"spans"`
	Terms        []noticeOriginTerm            `json:"terms,omitempty"`
	Notices      []noticeOriginExternalSection `json:"notices,omitempty"`
	SourceInputs []noticeOriginExternal        `json:"sourceInputs,omitempty"`
	Inputs       []noticeOriginInput           `json:"inputs,omitempty"`
}

type noticeSelectedSources map[string]map[string]map[string]bool

type noticeModuleCatalogue struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	UpstreamCommit  string              `json:"upstreamCommit"`
	MetadataProfile string              `json:"metadataProfile"`
	GoModSHA256     string              `json:"goModSha256"`
	GoSumSHA256     string              `json:"goSumSha256"`
	Modules         []noticeModuleEntry `json:"modules"`
}

type noticeModuleSection struct {
	Source  string `json:"source"`
	Package string `json:"package,omitempty"`
	Offset  int    `json:"offset"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type noticeModuleRecord struct {
	Original   noticeModuleIdentity  `json:"original"`
	Effective  noticeModuleIdentity  `json:"effective"`
	Components []noticeModuleSection `json:"components"`
	Origins    []noticeOriginRecord  `json:"origins,omitempty"`
}

type noticeModuleEvidence struct {
	CatalogueSHA256 string               `json:"catalogueSha256"`
	GoModSHA256     string               `json:"goModSha256"`
	GoSumSHA256     string               `json:"goSumSha256"`
	RawDiagnostic   string               `json:"rawDiagnostic"`
	Bytes           int                  `json:"bytes"`
	SHA256          string               `json:"sha256"`
	Modules         []noticeModuleRecord `json:"modules"`
}

type noticeModules struct {
	Evidence noticeModuleEvidence `json:"evidence"`
	Text     []byte               `json:"text"`
}

// noticeModuleEscape uses Go's ASCII uppercase escape for reviewed cache paths.
func noticeModuleEscape(value string) string {
	var result strings.Builder
	for _, c := range value {
		if c >= 'A' && c <= 'Z' {
			result.WriteByte('!')
			result.WriteRune(c + 'a' - 'A')
		} else {
			result.WriteRune(c)
		}
	}
	return result.String()
}

// noticeModuleRelativePath admits only canonical, module-relative file paths.
func noticeModuleRelativePath(value string) bool {
	return value != "" && value != "." && value != ".." &&
		path.Clean(value) == value && !path.IsAbs(value) &&
		!strings.HasPrefix(value, "../") && !strings.Contains(value, "\\") &&
		!strings.ContainsFunc(value, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) })
}

// noticeModuleSnapshot joins the whole compiler graph to one checked catalogue,
// this build's frozen lock and resolved cache, then retains its complete bytes.
func noticeModuleSnapshot(build, directory, catalogueHash, cacheDirectory string, packages []byte, read noticeReader) (*noticeModules, error) {
	data, err := read(directory, "catalogue.json", 1<<20)
	if err != nil || noticeDigest(data) != catalogueHash || !noticeDigestPattern.MatchString(catalogueHash) {
		return nil, fmt.Errorf("module catalogue unavailable or changed")
	}
	if _, err = noticeObject(data); err != nil {
		return nil, err
	}
	var catalogue noticeModuleCatalogue
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&catalogue); err != nil {
		return nil, fmt.Errorf("module catalogue JSON: %w", err)
	}
	if catalogue.SchemaVersion != 1 || catalogue.UpstreamCommit != noticePin || catalogue.MetadataProfile != "machine-id-no-jsonpath-v1" || len(catalogue.Modules) < 1 || len(catalogue.Modules) > 512 {
		return nil, fmt.Errorf("unsupported module catalogue")
	}
	locks := map[string][]byte{}
	for name, expected := range map[string]string{"go.mod": catalogue.GoModSHA256, "go.sum": catalogue.GoSumSHA256} {
		body, e := read(build, "prepared/source/"+name, 1<<20)
		if e != nil || !noticeDigestPattern.MatchString(expected) || noticeDigest(body) != expected {
			return nil, fmt.Errorf("module root lock differs")
		}
		locks[name] = body
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(locks["go.sum"]), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || sums[fields[0]+" "+fields[1]] != "" {
			return nil, fmt.Errorf("invalid or duplicate module sum")
		}
		sums[fields[0]+" "+fields[1]] = fields[2]
	}
	wanted := map[string]noticeModuleEntry{}
	previous := ""
	for _, entry := range catalogue.Modules {
		id, effective := entry.Original, entry.Effective
		if id.Path <= previous || id.Path == "" || id.Main || effective.Main || effective.Replace != nil || path.Clean(effective.Path) != effective.Path || strings.ContainsAny(effective.Path+effective.Version, "\\\r\n\t ") || effective.Version == "" || len(entry.Components) == 0 {
			return nil, fmt.Errorf("invalid, duplicate or unordered module identity")
		}
		previous = id.Path
		selected := id
		if id.Replace != nil {
			selected = *id.Replace
		}
		if !reflect.DeepEqual(selected, effective) || !strings.HasPrefix(effective.Sum, "h1:") || !strings.HasPrefix(effective.GoModSum, "h1:") {
			return nil, fmt.Errorf("module replacement identity differs")
		}
		if sums[effective.Path+" "+effective.Version] != effective.Sum || sums[effective.Path+" "+effective.Version+"/go.mod"] != effective.GoModSum {
			return nil, fmt.Errorf("selected module absent from frozen sums")
		}
		wanted[id.Path] = entry
	}
	// Reuse the observation already checked and bound by noticeSnapshot. A
	// second path read could validate a graph different from the retained hash.
	stream := json.NewDecoder(bytes.NewReader(packages))
	seenPackages := map[string]bool{}
	seenModules := map[string]bool{}
	selectedDirectories := map[string]map[string]bool{}
	selectedSources := map[string]noticeSelectedSources{}
	for {
		var raw json.RawMessage
		if e := stream.Decode(&raw); e == io.EOF {
			break
		} else if e != nil {
			return nil, fmt.Errorf("incomplete module compiler observation")
		}
		obj, e := noticeObject(raw)
		if e != nil {
			return nil, e
		}
		importPath, ok := obj["ImportPath"].(string)
		if !ok || importPath == "" || seenPackages[importPath] || (obj["Incomplete"] != nil && obj["Incomplete"] != false) || obj["Error"] != nil || (obj["DepsErrors"] != nil && !reflect.DeepEqual(obj["DepsErrors"], []any{})) {
			return nil, fmt.Errorf("invalid or duplicate module package observation")
		}
		seenPackages[importPath] = true
		if obj["Module"] == nil {
			if obj["Standard"] != true {
				return nil, fmt.Errorf("nonstandard package lacks its module")
			}
			continue
		}
		module, ok := obj["Module"].(map[string]any)
		if !ok || obj["Standard"] == true {
			return nil, fmt.Errorf("invalid module observation")
		}
		moduleBytes, e := json.Marshal(module)
		if e != nil {
			return nil, e
		}
		var observed noticeModuleIdentity
		if e = json.Unmarshal(moduleBytes, &observed); e != nil {
			return nil, e
		}
		if observed.Main {
			if observed.Path != "github.com/zitadel/zitadel" || observed.Version != "" || observed.Sum != "" || observed.GoModSum != "" || observed.Replace != nil || module["Dir"] != filepath.Join(build, "prepared/source") || module["GoMod"] != filepath.Join(build, "prepared/source/go.mod") {
				return nil, fmt.Errorf("wrong root module observation")
			}
			continue
		}
		entry, ok := wanted[observed.Path]
		if !ok || !reflect.DeepEqual(observed, entry.Original) {
			return nil, fmt.Errorf("selected module identity differs from catalogue")
		}
		effective := entry.Effective
		cacheRoot := cacheDirectory + "/" + noticeModuleEscape(effective.Path) + "@" + noticeModuleEscape(effective.Version)
		cacheMod := cacheDirectory + "/cache/download/" + noticeModuleEscape(effective.Path) + "/@v/" + noticeModuleEscape(effective.Version) + ".mod"
		if module["Dir"] != filepath.Join(build, cacheRoot) || module["GoMod"] != filepath.Join(build, cacheMod) {
			return nil, fmt.Errorf("selected module is outside this build cache")
		}
		packageDir, ok := obj["Dir"].(string)
		if !ok || (importPath != observed.Path && !strings.HasPrefix(importPath, observed.Path+"/")) {
			return nil, fmt.Errorf("package is outside its selected module")
		}
		relative := ""
		if importPath != observed.Path {
			relative = strings.TrimPrefix(importPath, observed.Path+"/")
			if !noticeModuleRelativePath(relative) {
				return nil, fmt.Errorf("package import suffix is not canonical")
			}
		}
		if packageDir != filepath.Join(build, cacheRoot, relative) {
			return nil, fmt.Errorf("package directory differs from its import identity")
		}
		if observed.Replace != nil {
			replacement, ok := module["Replace"].(map[string]any)
			if !ok || replacement["Dir"] != module["Dir"] || replacement["GoMod"] != module["GoMod"] {
				return nil, fmt.Errorf("replacement cache observation differs")
			}
		}
		seenModules[observed.Path] = true
		if selectedDirectories[observed.Path] == nil {
			selectedDirectories[observed.Path] = map[string]bool{}
		}
		selectedDirectories[observed.Path][relative] = true
		if selectedSources[observed.Path] == nil {
			selectedSources[observed.Path] = noticeSelectedSources{}
		}
		fields := map[string]map[string]bool{}
		for _, field := range []string{"GoFiles", "EmbedFiles", "IgnoredGoFiles"} {
			fields[field] = map[string]bool{}
			if obj[field] == nil {
				continue
			}
			files, ok := obj[field].([]any)
			if !ok {
				return nil, fmt.Errorf("invalid module package source observation")
			}
			for _, raw := range files {
				file, ok := raw.(string)
				if !ok || !noticeModuleRelativePath(file) || fields[field][file] || (field != "EmbedFiles" && path.Base(file) != file) {
					return nil, fmt.Errorf("invalid module package source filename")
				}
				fields[field][file] = true
			}
		}
		selectedSources[observed.Path][importPath] = fields
	}
	if len(seenModules) != len(wanted) {
		return nil, fmt.Errorf("selected module observation is incomplete")
	}
	result := &noticeModules{Evidence: noticeModuleEvidence{CatalogueSHA256: catalogueHash, GoModSHA256: catalogue.GoModSHA256, GoSumSHA256: catalogue.GoSumSHA256, RawDiagnostic: moduleNoticeRaw}}
	externalPurposes := map[string]bool{}
	for _, entry := range catalogue.Modules {
		effective := entry.Effective
		cache := cacheDirectory + "/cache/download/" + noticeModuleEscape(effective.Path) + "/@v/" + noticeModuleEscape(effective.Version)
		mod, e := read(build, cache+".mod", 1<<20)
		if e != nil {
			return nil, fmt.Errorf("selected module go.mod unavailable")
		}
		modHash := sha256.Sum256([]byte(noticeDigest(mod) + "  go.mod\n"))
		if "h1:"+base64.StdEncoding.EncodeToString(modHash[:]) != effective.GoModSum {
			return nil, fmt.Errorf("selected module go.mod checksum differs")
		}
		zipHash, e := read(build, cache+".ziphash", 128)
		if e != nil || strings.TrimSuffix(string(zipHash), "\n") != effective.Sum {
			return nil, fmt.Errorf("selected module archive checksum differs")
		}
		record := noticeModuleRecord{Original: entry.Original, Effective: effective}
		previousFile := ""
		for _, component := range entry.Components {
			if component.File <= previousFile || !noticeModuleRelativePath(component.File) || component.Bytes < 1 || component.Bytes > 64<<10 || !noticeDigestPattern.MatchString(component.SHA256) {
				return nil, fmt.Errorf("invalid or duplicate module component")
			}
			if component.Package == "" {
				if path.Base(component.File) != component.File {
					return nil, fmt.Errorf("nested component lacks a selected package")
				}
			} else {
				directory := path.Dir(component.File)
				if !noticeModuleRelativePath(component.Package) || directory == "." || (directory != component.Package && !strings.HasPrefix(component.Package, directory+"/")) || !selectedDirectories[entry.Original.Path][component.Package] {
					return nil, fmt.Errorf("nested component is not bound to its selected package")
				}
			}
			previousFile = component.File
			source := cacheDirectory + "/" + noticeModuleEscape(effective.Path) + "@" + noticeModuleEscape(effective.Version) + "/" + component.File
			text, e := read(build, source, 64<<10)
			if e != nil || len(text) != component.Bytes || noticeDigest(text) != component.SHA256 || !utf8.Valid(text) || bytes.ContainsRune(text, 0) {
				return nil, fmt.Errorf("selected module terms differ or are unavailable")
			}
			result.Text = append(result.Text, []byte("Source: "+entry.Original.Path+"@"+entry.Original.Version+" => "+effective.Path+"@"+effective.Version+"/"+component.File+"\n\n")...)
			record.Components = append(record.Components, noticeModuleSection{Source: source, Package: component.Package, Offset: len(result.Text), Bytes: len(text), SHA256: noticeDigest(text)})
			result.Text = append(result.Text, text...)
			result.Text = append(result.Text, '\n', '\n')
			if len(result.Text) > noticeLimit {
				return nil, fmt.Errorf("module notice output exceeds limit")
			}
		}
		if err := noticeRetainOrigins(build, directory, cacheDirectory, entry, wanted, selectedSources[entry.Original.Path], externalPurposes, read, result, &record); err != nil {
			return nil, err
		}
		result.Evidence.Modules = append(result.Evidence.Modules, record)
	}
	result.Evidence.Bytes = len(result.Text)
	result.Evidence.SHA256 = noticeDigest(result.Text)
	return result, nil
}

// Origins are a closed, reviewed set of selected source spans, not a filename
// discovery rule. Source-input and ignored-generator bytes are checked, never
// emitted or executed. Complete corresponding-source delivery is separate.
func noticeRetainOrigins(build, directory, cacheDirectory string, entry noticeModuleEntry, catalogue map[string]noticeModuleEntry, selected noticeSelectedSources, externalPurposes map[string]bool, read noticeReader, result *noticeModules, record *noticeModuleRecord) error {
	cache := cacheDirectory + "/" + noticeModuleEscape(entry.Effective.Path) + "@" + noticeModuleEscape(entry.Effective.Version) + "/"
	previous := ""
	for _, origin := range entry.Origins {
		if origin.File <= previous || !noticeModuleRelativePath(origin.File) || origin.Bytes < 1 || origin.Bytes > 1<<20 || !noticeDigestPattern.MatchString(origin.SHA256) || len(origin.Spans) == 0 || len(origin.Spans) > 32 || len(origin.Terms)+len(origin.Notices) == 0 {
			return fmt.Errorf("invalid or unordered module origin")
		}
		previous = origin.File
		packageRelative := ""
		if origin.Package != entry.Original.Path {
			if !strings.HasPrefix(origin.Package, entry.Original.Path+"/") {
				return fmt.Errorf("module origin has a foreign package")
			}
			packageRelative = strings.TrimPrefix(origin.Package, entry.Original.Path+"/")
		}
		fields := selected[origin.Package]
		if fields == nil || path.Join(packageRelative, path.Base(origin.File)) != origin.File || !fields["GoFiles"][path.Base(origin.File)] {
			return fmt.Errorf("module origin source is not compiler-selected")
		}
		source := cache + origin.File
		body, err := read(build, source, 1<<20)
		if err != nil || len(body) != origin.Bytes || noticeDigest(body) != origin.SHA256 || !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
			return fmt.Errorf("module origin source differs or is unavailable")
		}
		// Optional lists are omitted by the saved JSON snapshot when empty.
		// Canonicalize only empty representations here so the unchanged checked
		// state compares exactly after reading it back; keep every nonempty item.
		retained := noticeOriginRecord{
			Source: source, Package: origin.Package, Bytes: origin.Bytes, SHA256: origin.SHA256,
			Terms:        append([]noticeOriginTerm(nil), origin.Terms...),
			SourceInputs: append([]noticeOriginExternal(nil), origin.SourceInputs...),
			Inputs:       append([]noticeOriginInput(nil), origin.Inputs...),
		}
		previousTerm := ""
		for _, term := range origin.Terms {
			key := term.Module + "/" + term.File
			if key <= previousTerm || !noticeModuleRelativePath(term.File) || !noticeDigestPattern.MatchString(term.SHA256) {
				return fmt.Errorf("invalid module origin terms reference")
			}
			previousTerm = key
			found := false
			for _, component := range catalogue[term.Module].Components {
				if component.File == term.File && component.SHA256 == term.SHA256 {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("module origin terms are not retained components")
			}
		}
		previousInput := ""
		for _, input := range origin.Inputs {
			field := map[string]string{"embed": "EmbedFiles", "generator": "IgnoredGoFiles"}[input.Kind]
			name := strings.TrimPrefix(input.File, packageRelative+"/")
			if packageRelative == "" {
				name = input.File
			}
			if input.File <= previousInput || !noticeModuleRelativePath(input.File) || field == "" || path.Join(packageRelative, name) != input.File || !fields[field][name] || (input.Kind == "generator" && fields["GoFiles"][name]) || input.Bytes < 1 || input.Bytes > 1<<20 || !noticeDigestPattern.MatchString(input.SHA256) {
				return fmt.Errorf("module origin input is not its selected embed or ignored generator")
			}
			previousInput = input.File
			data, e := read(build, cache+input.File, 1<<20)
			if e != nil || len(data) != input.Bytes || noticeDigest(data) != input.SHA256 {
				return fmt.Errorf("module origin input differs or is unavailable")
			}
		}
		end := 0
		for _, span := range origin.Spans {
			if span.Offset < end || span.Offset > len(body) || span.Bytes < 1 || span.Bytes > len(body)-span.Offset || !noticeDigestPattern.MatchString(span.SHA256) {
				return fmt.Errorf("invalid or overlapping module origin span")
			}
			text := body[span.Offset : span.Offset+span.Bytes]
			if !utf8.Valid(text) || noticeDigest(text) != span.SHA256 {
				return fmt.Errorf("module origin span differs")
			}
			end = span.Offset + span.Bytes
			result.Text = append(result.Text, []byte(fmt.Sprintf("Origin: %s [%d:%d]\n\n", source, span.Offset, end))...)
			retained.Spans = append(retained.Spans, noticeOriginSection{span.Offset, len(result.Text), len(text), span.SHA256})
			result.Text = append(result.Text, text...)
			result.Text = append(result.Text, '\n', '\n')
		}
		externalFiles := map[string]bool{}
		for _, set := range []struct {
			values []noticeOriginExternal
			emit   bool
		}{{origin.Notices, true}, {origin.SourceInputs, false}} {
			previousFile := ""
			for _, input := range set.values {
				u, err := url.Parse(input.Upstream)
				if input.File <= previousFile || externalFiles[input.File] || !noticeModuleRelativePath(input.File) || !strings.HasPrefix(input.File, "incorporated/") || input.Bytes < 1 || input.Bytes > 512<<10 || !noticeDigestPattern.MatchString(input.SHA256) || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || len(input.Upstream) > 4096 || strings.ContainsFunc(input.Upstream, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
					return fmt.Errorf("invalid module origin external input")
				}
				previousFile = input.File
				externalFiles[input.File] = true
				if purpose, exists := externalPurposes[input.File]; exists && purpose != set.emit {
					return fmt.Errorf("module origin external input has contradictory purposes")
				}
				externalPurposes[input.File] = set.emit
				text, e := read(directory, input.File, 512<<10)
				if e != nil || len(text) != input.Bytes || noticeDigest(text) != input.SHA256 || !utf8.Valid(text) || bytes.ContainsRune(text, 0) {
					return fmt.Errorf("module origin external input differs or is unavailable")
				}
				if set.emit {
					result.Text = append(result.Text, []byte("Source: "+input.Upstream+"\n\n")...)
					retained.Notices = append(retained.Notices, noticeOriginExternalSection{input, len(result.Text)})
					result.Text = append(result.Text, text...)
					result.Text = append(result.Text, '\n', '\n')
				} else {
					result.Text = append(result.Text, []byte("Corresponding source input: "+input.Upstream+" (SHA-256 "+input.SHA256+")\n\n")...)
				}
			}
		}
		if len(result.Text) > noticeLimit {
			return fmt.Errorf("module origin notice output exceeds limit")
		}
		record.Origins = append(record.Origins, retained)
	}
	return nil
}

type noticeSourceLicenseEvidence struct {
	Source        string `json:"source"`
	RawDiagnostic string `json:"rawDiagnostic"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
}

type noticeSourceLicense struct {
	Evidence noticeSourceLicenseEvidence `json:"evidence"`
	Text     []byte                      `json:"text"`
}

// noticeSourceLicenseSnapshot retains only the complete frozen root licence
// from this build's prepared source. Component and module closure stay unknown.
func noticeSourceLicenseSnapshot(build string, read noticeReader) (*noticeSourceLicense, error) {
	data, err := read(build, sourceLicensePath, 64<<10)
	if err != nil || len(data) != 34522 || noticeDigest(data) != sourceLicenseHash || !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return nil, fmt.Errorf("source licence input differs or is unavailable")
	}
	return &noticeSourceLicense{Evidence: noticeSourceLicenseEvidence{sourceLicensePath, sourceLicenseRaw, len(data), noticeDigest(data)}, Text: data}, nil
}

type noticeToolchainSection struct {
	Source string `json:"source"`
	Offset int    `json:"offset"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type noticeToolchainEvidence struct {
	Version        string                   `json:"version"`
	RawDiagnostic  string                   `json:"rawDiagnostic"`
	Bytes          int                      `json:"bytes"`
	SHA256         string                   `json:"sha256"`
	Sections       []noticeToolchainSection `json:"sections"`
	VendorPackages []string                 `json:"vendorPackages,omitempty"`
}

type noticeToolchain struct {
	Evidence noticeToolchainEvidence `json:"evidence"`
	Text     []byte                  `json:"text"`
}

// noticeToolchainSnapshot retains checked distribution terms, not compiler code
// or a claim of complete selected-module/incorporated-source coverage.
func noticeToolchainSnapshot(root string, read noticeReader) (*noticeToolchain, error) {
	result := &noticeToolchain{Evidence: noticeToolchainEvidence{Version: "1.26.10", RawDiagnostic: "go-toolchain-notices.txt"}}
	for _, input := range []struct{ name, hash string }{
		{"LICENSE", "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"},
		{"PATENTS", "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc"},
		{"VERSION", ""},
	} {
		data, err := read(root, input.name, 16<<10)
		if err != nil || len(data) == 0 || !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
			return nil, fmt.Errorf("toolchain distribution input unavailable")
		}
		if input.hash != "" && noticeDigest(data) != input.hash {
			return nil, fmt.Errorf("toolchain distribution terms differ")
		}
		if input.name == "VERSION" {
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) > 2 || lines[0] != "go1.26.10" {
				return nil, fmt.Errorf("toolchain distribution version differs")
			}
			if len(lines) == 2 {
				if !strings.HasPrefix(lines[1], "time ") {
					return nil, fmt.Errorf("invalid distribution time")
				}
				if _, err := time.Parse(time.RFC3339, strings.TrimPrefix(lines[1], "time ")); err != nil {
					return nil, fmt.Errorf("invalid distribution time")
				}
			}
		}
		result.Text = append(result.Text, []byte("Source: GOROOT/"+input.name+"\n\n")...)
		result.Evidence.Sections = append(result.Evidence.Sections, noticeToolchainSection{"GOROOT/" + input.name, len(result.Text), len(data), noticeDigest(data)})
		result.Text = append(result.Text, data...)
		result.Text = append(result.Text, '\n', '\n')
	}
	// Equal checked texts share a retained span; each original vendor path is
	// still read independently and keeps its own provenance in the inventory.
	for _, module := range []string{"net", "crypto", "text", "sys"} {
		for index, name := range []string{"LICENSE", "PATENTS"} {
			source := "src/vendor/golang.org/x/" + module + "/" + name
			data, err := read(root, source, 16<<10)
			section := result.Evidence.Sections[index]
			if err != nil || !bytes.Equal(data, result.Text[section.Offset:section.Offset+section.Bytes]) {
				return nil, fmt.Errorf("selected toolchain vendor terms unavailable or different: %s", source)
			}
			section.Source = "GOROOT/" + source
			result.Evidence.Sections = append(result.Evidence.Sections, section)
		}
	}
	result.Evidence.Bytes = len(result.Text)
	result.Evidence.SHA256 = noticeDigest(result.Text)
	return result, nil
}

// noticeToolchainPackages binds the frozen profile's complete vendor selection
// to the actual selected compiler, not a coincidentally present source tree.
func noticeToolchainPackages(data []byte, root string) ([]string, error) {
	wanted := strings.Fields(`
vendor/golang.org/x/crypto/chacha20
vendor/golang.org/x/crypto/chacha20poly1305
vendor/golang.org/x/crypto/cryptobyte
vendor/golang.org/x/crypto/cryptobyte/asn1
vendor/golang.org/x/crypto/internal/alias
vendor/golang.org/x/crypto/internal/poly1305
vendor/golang.org/x/net/dns/dnsmessage
vendor/golang.org/x/net/http/httpguts
vendor/golang.org/x/net/http/httpproxy
vendor/golang.org/x/net/http2/hpack
vendor/golang.org/x/net/idna
vendor/golang.org/x/sys/cpu
vendor/golang.org/x/text/secure/bidirule
vendor/golang.org/x/text/transform
vendor/golang.org/x/text/unicode/bidi
vendor/golang.org/x/text/unicode/norm`)
	expected := make(map[string]bool, len(wanted))
	for _, name := range wanted {
		expected[name] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	seen, vendors := map[string]bool{}, map[string]bool{}
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("incomplete compiler vendor observation")
		}
		object, err := noticeObject(raw)
		if err != nil {
			return nil, err
		}
		name, ok := object["ImportPath"].(string)
		if !ok || name == "" || seen[name] || (object["Incomplete"] != nil && object["Incomplete"] != false) || object["Error"] != nil || (object["DepsErrors"] != nil && !reflect.DeepEqual(object["DepsErrors"], []any{})) {
			return nil, fmt.Errorf("invalid or duplicate compiler package observation")
		}
		seen[name] = true
		if !strings.HasPrefix(name, "vendor/") {
			continue
		}
		if !expected[name] || object["Standard"] != true || object["Module"] != nil || object["Dir"] != filepath.Join(root, "src", name) {
			return nil, fmt.Errorf("selected compiler vendor package differs")
		}
		vendors[name] = true
	}
	if len(vendors) != len(wanted) {
		return nil, fmt.Errorf("selected compiler vendor observation is incomplete")
	}
	return wanted, nil
}

type noticeBinding struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type noticeInventory struct {
	SchemaVersion  int                          `json:"schemaVersion"`
	UpstreamCommit string                       `json:"upstreamCommit"`
	Profile        string                       `json:"profile"`
	OriginalPaths  []string                     `json:"originalPaths"`
	RawDiagnostic  string                       `json:"rawDiagnostic"`
	Bytes          int                          `json:"bytes"`
	SHA256         string                       `json:"sha256"`
	Inputs         []noticeBinding              `json:"inputs"`
	ImageAdmission string                       `json:"imageAdmission"`
	LicenseClosure string                       `json:"licenseClosure"`
	Reproducible   bool                         `json:"reproducible"`
	Toolchain      *noticeToolchainEvidence     `json:"toolchain,omitempty"`
	SourceLicense  *noticeSourceLicenseEvidence `json:"sourceLicense,omitempty"`
	Modules        *noticeModuleEvidence        `json:"modules,omitempty"`
	supplement     *supplementState
	toolchain      *noticeToolchain
	sourceLicense  *noticeSourceLicense
	moduleNotices  *noticeModules
}

func main() { os.Exit(noticeRun(os.Args[1:], os.Stdout, os.Stderr)) }

// noticeRun is inert unless one complete explicit mode is selected.
func noticeRun(args []string, out, diagnostic io.Writer) int {
	return noticeRunWithCatalogues(args, out, diagnostic, supplementCatalogueHash, moduleCatalogueHash)
}

// noticeRunWithCatalogues exposes only test injection, never a CLI trust override.
func noticeRunWithCatalogue(args []string, out, diagnostic io.Writer, catalogueHash string) int {
	return noticeRunWithCatalogues(args, out, diagnostic, catalogueHash, moduleCatalogueHash)
}

// noticeJoinBuildInfo is the selected-module binary boundary, not a legal verdict.
func noticeJoinBuildInfo(info *debug.BuildInfo, expected []*debug.Module) error {
	if info == nil || info.GoVersion != "go1.26.10" || info.Path != "github.com/zitadel/zitadel" || info.Main.Path != "github.com/zitadel/zitadel" || info.Main.Version != "(devel)" || info.Main.Sum != "" || info.Main.Replace != nil || len(expected) == 0 || len(info.Deps) != len(expected) {
		return fmt.Errorf("module binary identity differs")
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		if _, ok := settings[setting.Key]; ok {
			return fmt.Errorf("duplicate binary setting")
		}
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != "linux" || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" {
		return fmt.Errorf("module binary target differs")
	}
	wanted := map[string]*debug.Module{}
	for _, module := range expected {
		if module == nil || module.Path == "" || wanted[module.Path] != nil {
			return fmt.Errorf("invalid expected binary module")
		}
		wanted[module.Path] = module
	}
	seen := map[string]bool{}
	for _, module := range info.Deps {
		if module == nil || seen[module.Path] || !reflect.DeepEqual(module, wanted[module.Path]) {
			return fmt.Errorf("binary dependency set differs")
		}
		seen[module.Path] = true
	}
	return nil
}

// noticeRunWithCatalogue keeps the curation identity injectable in hermetic
// tests. The public command always supplies its compiled, reviewed constant.
func noticeRunWithCatalogues(args []string, out, diagnostic io.Writer, catalogueHash, moduleHash string) int {
	if len(args) == 0 {
		return 0
	}
	fail := func(err error) int { _, _ = fmt.Fprintln(diagnostic, "console-notices:", err); return 1 }
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key := args[i]
		if _, ok := values[key]; ok {
			return fail(fmt.Errorf("duplicate argument"))
		}
		switch key {
		case "--snapshot", "--inventory", "--verify", "--source-license", "--retained-go-cache":
			values[key] = "true"
		case "--build", "--recipe", "--supplement", "--toolchain", "--modules":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing argument"))
			}
			i++
			values[key] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument"))
		}
	}
	optional := 0
	if values["--supplement"] != "" {
		optional++
	}
	if values["--toolchain"] != "" {
		optional++
	}
	if values["--source-license"] != "" {
		optional++
	}
	if values["--modules"] != "" {
		optional++
	}
	if values["--retained-go-cache"] != "" {
		if values["--modules"] == "" {
			return fail(fmt.Errorf("retained cache requires module retention"))
		}
		optional++
	}
	validCount := len(values) == 3+optional
	modes := 0
	for _, mode := range []string{"--snapshot", "--inventory", "--verify"} {
		if values[mode] != "" {
			modes++
		}
	}
	if !validCount || modes != 1 || values["--build"] == "" || values["--recipe"] == "" {
		return fail(fmt.Errorf("expected one mode, build and recipe"))
	}
	verify := values["--verify"] != ""
	checkOrWrite := func(name string, data []byte) error {
		if !verify {
			return noticeWriteDiagnostic(values["--build"], name, data)
		}
		retained, err := noticeRead(values["--build"], name, noticeSnapshotLimit)
		if err != nil || !bytes.Equal(retained, data) {
			return fmt.Errorf("completed diagnostic differs: %s", name)
		}
		return nil
	}
	if verify {
		if _, err := noticeRead(values["--build"], "build-receipt.json", noticeSnapshotLimit); err != nil {
			return fail(fmt.Errorf("completed build receipt unavailable"))
		}
	}
	var result any
	options := []noticeSupplementConfig{}
	if optional > 0 {
		options = append(options, noticeSupplementConfig{Directory: values["--supplement"], CatalogueHash: catalogueHash, ToolchainRoot: values["--toolchain"], SourceLicense: values["--source-license"] != "", ModuleDirectory: values["--modules"], ModuleCatalogueHash: moduleHash, RetainedGoCache: values["--retained-go-cache"] != ""})
	}
	var captured *noticeInventory
	if values["--snapshot"] != "" {
		state, err := noticeSnapshot(values["--build"], values["--recipe"], noticeRead, options...)
		if err != nil {
			return fail(err)
		}
		result = state
	} else {
		inventory, raw, err := noticeCapture(values["--build"], values["--recipe"], noticeRead, options...)
		if err != nil {
			return fail(err)
		}
		if err = checkOrWrite(noticeRaw, raw); err != nil {
			return fail(err)
		}
		result = inventory
		captured = &inventory
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fail(err)
	}
	b = append(b, '\n')
	// The public snapshot writer and capture reader share one aggregate budget.
	// Base64 and metadata overhead count, so refuse before emitting a snapshot
	// that a later capture would reject only after the service compilation.
	if values["--snapshot"] != "" && len(b) > noticeSnapshotLimit {
		return fail(fmt.Errorf("encoded compile snapshot exceeds %d-byte limit", noticeSnapshotLimit))
	}
	if captured != nil && captured.supplement != nil {
		s := captured.supplement
		inventory := struct {
			SchemaVersion         int                 `json:"schemaVersion"`
			UpstreamCommit        string              `json:"upstreamCommit"`
			Profile               string              `json:"profile"`
			RawDiagnostic         string              `json:"rawDiagnostic"`
			Bytes                 int                 `json:"bytes"`
			SHA256                string              `json:"sha256"`
			NoticeInventorySHA256 string              `json:"noticeInventorySha256"`
			CatalogueSHA256       string              `json:"catalogueSha256"`
			Catalogue             supplementCatalogue `json:"catalogue"`
			Aliases               []supplementAlias   `json:"aliases"`
			Sections              []supplementSection `json:"sections"`
			SelectedInputs        []noticeBinding     `json:"selectedInputs"`
			BuildInputs           []noticeBinding     `json:"buildInputs"`
			LicenseClosure        string              `json:"licenseClosure"`
			ImageAdmission        string              `json:"imageAdmission"`
			Reproducible          bool                `json:"reproducible"`
		}{1, noticePin, "api-login-v1-console", supplementRaw, len(s.Text), noticeDigest(s.Text), noticeDigest(b), s.CatalogueSHA256, s.Catalogue, s.Aliases, s.Sections, s.Inputs, captured.Inputs, "unknown", "unknown", false}
		encoded, e := json.MarshalIndent(inventory, "", "  ")
		if e != nil {
			return fail(e)
		}
		if e = checkOrWrite(supplementRaw, s.Text); e != nil {
			return fail(e)
		}
		name := supplementInventoryPending
		if verify {
			name = "console-notices-supplement-inventory.json"
		}
		if e = checkOrWrite(name, append(encoded, '\n')); e != nil {
			return fail(e)
		}
	}
	if captured != nil && captured.toolchain != nil {
		if err := checkOrWrite(captured.toolchain.Evidence.RawDiagnostic, captured.toolchain.Text); err != nil {
			return fail(err)
		}
	}
	if captured != nil && captured.sourceLicense != nil {
		if err := checkOrWrite(captured.sourceLicense.Evidence.RawDiagnostic, captured.sourceLicense.Text); err != nil {
			return fail(err)
		}
	}
	if captured != nil && captured.moduleNotices != nil {
		if err := checkOrWrite(moduleNoticeRaw, captured.moduleNotices.Text); err != nil {
			return fail(err)
		}
	}
	if verify {
		if err := checkOrWrite("console-notices-inventory.json", b); err != nil {
			return fail(err)
		}
	}
	// A complete inventory is emitted only after all retained diagnostics succeed.
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return fail(fmt.Errorf("incomplete inventory output"))
	}
	return 0
}

// noticeWriteDiagnostic never replaces an output, and propagates short writes,
// sync and close failures; a failed build cannot finalize its success receipt.
func noticeWriteDiagnostic(build, name string, data []byte) error {
	root, err := os.OpenRoot(build)
	if err != nil {
		return fmt.Errorf("diagnostic root unavailable")
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	rootErr := root.Close()
	if err != nil {
		return fmt.Errorf("diagnostic exists or is unavailable")
	}
	if rootErr != nil {
		_ = f.Close()
		return fmt.Errorf("diagnostic root close failed")
	}
	n, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(data) {
		return fmt.Errorf("incomplete diagnostic write")
	}
	return nil
}

// noticeRead confines every selected path and refuses nonregular or linked inputs.
func noticeRead(root, relative string, limit int64) ([]byte, error) {
	if relative == "" || path.Clean(relative) != relative || path.IsAbs(relative) || strings.HasPrefix(relative, "../") {
		return nil, fmt.Errorf("unclean input")
	}
	p := root
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid root")
	}
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		p = filepath.Join(p, part)
		info, e := os.Lstat(p)
		if e != nil {
			return nil, fmt.Errorf("missing input")
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, fmt.Errorf("linked or nonregular input")
		}
	}
	confined, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("root unavailable")
	}
	f, err := confined.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	closeErr := confined.Close()
	if err != nil {
		return nil, fmt.Errorf("input unavailable")
	}
	if closeErr != nil {
		_ = f.Close()
		return nil, fmt.Errorf("root close failed")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("nonregular opened input")
	}
	return noticeReadAll(f, info.Size(), limit)
}

// noticeReadAll propagates read/close errors and checks the opened file's exact size.
func noticeReadAll(f io.ReadCloser, size, limit int64) ([]byte, error) {
	if size < 0 || limit < 1 || size > limit {
		_ = f.Close()
		return nil, fmt.Errorf("oversized input")
	}
	b, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || int64(len(b)) != size || int64(len(b)) > limit {
		return nil, fmt.Errorf("incomplete input read")
	}
	return b, nil
}

// noticeDigest identifies bytes without interpreting or normalizing their text.
func noticeDigest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

var noticeDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// noticeObject rejects duplicate keys, excessive nesting and trailing JSON.
func noticeObject(data []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	var consume func(int) error
	consume = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting exceeds limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := key.(string)
					if !ok || seen[s] {
						return fmt.Errorf("duplicate JSON key")
					}
					seen[s] = true
					if e = consume(depth + 1); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e := consume(depth + 1); e != nil {
						return e
					}
				}
			default:
				return fmt.Errorf("invalid JSON delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := consume(0); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil || result == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return result, nil
}

// noticeManifest requires exactly one complete matching selected observation.
func noticeManifest(data []byte, wanted map[string]string) error {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " || !noticeDigestPattern.MatchString(line[:64]) {
			return fmt.Errorf("malformed manifest")
		}
		name := line[66:]
		if seen[name] {
			return fmt.Errorf("duplicate manifest member")
		}
		seen[name] = true
		if hash, ok := wanted[name]; ok && hash != line[:64] {
			return fmt.Errorf("substituted manifest member")
		}
	}
	for name := range wanted {
		if !seen[name] {
			return fmt.Errorf("missing manifest member")
		}
	}
	return nil
}

// noticePackages examines the whole Go observation stream, not a valid prefix.
func noticePackages(data []byte, build string) error {
	d := json.NewDecoder(bytes.NewReader(data))
	selected := 0
	for {
		var raw json.RawMessage
		err := d.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("incomplete compiler observation")
		}
		obj, err := noticeObject(raw)
		if err != nil {
			return fmt.Errorf("compiler observation JSON: %w", err)
		}
		if (obj["Incomplete"] != nil && obj["Incomplete"] != false) || obj["Error"] != nil || (obj["DepsErrors"] != nil && !reflect.DeepEqual(obj["DepsErrors"], []any{})) {
			return fmt.Errorf("compiler observation contains an incomplete or failed package")
		}
		if obj["ImportPath"] != "github.com/zitadel/zitadel/internal/api/ui/console" {
			continue
		}
		selected++
		if obj["Dir"] != filepath.Join(build, "prepared/source/internal/api/ui/console") || !reflect.DeepEqual(obj["EmbedPatterns"], []any{"static"}) {
			return fmt.Errorf("wrong compiler observation directory or pattern")
		}
		files, ok := obj["EmbedFiles"].([]any)
		if !ok {
			return fmt.Errorf("compiler observation lacks members")
		}
		seen := map[string]bool{}
		for _, item := range files {
			file, ok := item.(string)
			if !ok || file == "" || path.Clean(file) != file || path.IsAbs(file) || strings.HasPrefix(file, "../") || seen[file] {
				return fmt.Errorf("invalid or duplicate compiler observation member")
			}
			seen[file] = true
		}
		if !seen["static/3rdpartylicenses.txt"] {
			return fmt.Errorf("compiler observation omits notice")
		}
	}
	if selected != 1 {
		return fmt.Errorf("compiler observation selection is ambiguous or absent")
	}
	return nil
}

// noticeSnapshot records checked raw bytes and their pre-compilation input identities.
func noticeSnapshot(build, recipe string, read noticeReader, options ...noticeSupplementConfig) (noticeState, error) {
	var result noticeState
	console, err := read(build, "prepared/source/"+noticeConsole, noticeLimit)
	if err != nil || len(console) == 0 || !utf8.Valid(console) {
		return result, fmt.Errorf("invalid notice input")
	}
	embedded, err := read(build, "prepared/source/"+noticeEmbedded, noticeLimit)
	if err != nil || len(embedded) == 0 {
		return result, fmt.Errorf("invalid notice input")
	}
	if !bytes.Equal(console, embedded) {
		return result, fmt.Errorf("notice copies differ")
	}
	prep, err := read(build, "prepared/source-preparation.json", 1<<20)
	if err != nil {
		return result, fmt.Errorf("preparation input unavailable")
	}
	obj, err := noticeObject(prep)
	if err != nil {
		return result, err
	}
	for key, want := range map[string]any{"schemaVersion": float64(1), "upstreamCommit": noticePin, "imageAdmission": "unknown", "licenseClosure": "unknown"} {
		if obj[key] != want {
			return result, fmt.Errorf("wrong preparation identity")
		}
	}
	for _, key := range []string{"recipeSha256", "inputManifestSha256"} {
		value, ok := obj[key].(string)
		if !ok || !noticeDigestPattern.MatchString(value) {
			return result, fmt.Errorf("incomplete preparation identity")
		}
	}
	lock, err := read(build, "prepared/source/pnpm-lock.yaml", 16<<20)
	if err != nil || len(lock) == 0 {
		return result, fmt.Errorf("frozen lock unavailable")
	}
	inputs, err := read(build, "inputs.sha256", 1<<20)
	if err != nil {
		return result, fmt.Errorf("frozen lock manifest unavailable")
	}
	if err = noticeManifest(inputs, map[string]string{"pnpm-lock.yaml": noticeDigest(lock)}); err != nil {
		return result, fmt.Errorf("frozen lock: %w", err)
	}
	packages, err := read(build, "packages.jsonl", 64<<20)
	if err != nil {
		return result, fmt.Errorf("compiler observation unavailable")
	}
	if err = noticePackages(packages, build); err != nil {
		return result, err
	}
	recipeBytes, err := read(filepath.Dir(recipe), filepath.Base(recipe), 1<<20)
	if err != nil || len(recipeBytes) == 0 {
		return result, fmt.Errorf("recipe binding unavailable")
	}
	result = noticeState{SchemaVersion: 1, Raw: console, PreparationSHA256: noticeDigest(prep), LockSHA256: noticeDigest(lock), InputsSHA256: noticeDigest(inputs), PackagesSHA256: noticeDigest(packages), RecipeSHA256: noticeDigest(recipeBytes)}
	if len(options) > 1 {
		return result, fmt.Errorf("ambiguous supplement options")
	}
	if len(options) == 1 {
		if options[0].Directory != "" {
			result.Supplement, err = noticeSupplementSnapshot(build, options[0].Directory, options[0].CatalogueHash, read)
			if err != nil {
				return result, err
			}
		}
		if options[0].ToolchainRoot != "" {
			result.Toolchain, err = noticeToolchainSnapshot(options[0].ToolchainRoot, read)
			if err != nil {
				return result, err
			}
			result.Toolchain.Evidence.VendorPackages, err = noticeToolchainPackages(packages, options[0].ToolchainRoot)
			if err != nil {
				return result, err
			}
		}
		if options[0].SourceLicense {
			result.SourceLicense, err = noticeSourceLicenseSnapshot(build, read)
			if err != nil {
				return result, err
			}
		}
		if options[0].ModuleDirectory != "" {
			if obj["metadataProfile"] != "machine-id-no-jsonpath-v1" {
				return result, fmt.Errorf("unsupported module preparation profile")
			}
			cacheDirectory := "go-modules"
			if options[0].RetainedGoCache {
				cacheDirectory = "go-input-native/go-modules"
			}
			result.Modules, err = noticeModuleSnapshot(build, options[0].ModuleDirectory, options[0].ModuleCatalogueHash, cacheDirectory, packages, read)
			if err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// noticeCapture joins the unchanged checked snapshot to the final manifest and binary receipt.
func noticeCapture(build, recipe string, read noticeReader, options ...noticeSupplementConfig) (noticeInventory, []byte, error) {
	var result noticeInventory
	current, err := noticeSnapshot(build, recipe, read, options...)
	if err != nil {
		return result, nil, err
	}
	beforeBytes, err := read(build, noticeBefore, noticeSnapshotLimit)
	if err != nil {
		return result, nil, fmt.Errorf("compile snapshot unavailable")
	}
	if _, err = noticeObject(beforeBytes); err != nil {
		return result, nil, err
	}
	var before noticeState
	d := json.NewDecoder(bytes.NewReader(beforeBytes))
	d.DisallowUnknownFields()
	if err = d.Decode(&before); err != nil {
		return result, nil, fmt.Errorf("compile snapshot JSON: %w", err)
	}
	if !reflect.DeepEqual(current, before) {
		return result, nil, fmt.Errorf("compile snapshot changed")
	}
	generated, err := read(build, "generated.sha256", 8<<20)
	if err != nil {
		return result, nil, fmt.Errorf("generated manifest unavailable")
	}
	if err = noticeManifest(generated, map[string]string{noticeConsole: noticeDigest(before.Raw), noticeEmbedded: noticeDigest(before.Raw)}); err != nil {
		return result, nil, fmt.Errorf("generated manifest: %w", err)
	}
	receiptName := ""
	var receipt []byte
	for _, name := range []string{"build-receipt.pending", "build-receipt.json"} {
		_, statErr := os.Lstat(filepath.Join(build, name))
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return result, nil, fmt.Errorf("build receipt unavailable")
		}
		if receiptName != "" {
			return result, nil, fmt.Errorf("ambiguous build receipt")
		}
		receiptName = name
	}
	if receiptName == "" {
		return result, nil, fmt.Errorf("missing build receipt")
	}
	receipt, err = read(build, receiptName, 1<<20)
	if err != nil {
		return result, nil, fmt.Errorf("build receipt unavailable")
	}
	obj, err := noticeObject(receipt)
	if err != nil {
		return result, nil, err
	}
	for key, want := range map[string]any{"schemaVersion": float64(1), "upstreamCommit": noticePin, "version": "4.20.0", "sourceDate": "2026-10-22T07:05:44Z", "platform": "linux/amd64", "profile": "api-login-v1-console", "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false} {
		if obj[key] != want {
			return result, nil, fmt.Errorf("unsupported build receipt")
		}
	}
	prep, err := read(build, "prepared/source-preparation.json", 1<<20)
	if err != nil {
		return result, nil, fmt.Errorf("preparation unavailable")
	}
	prepObject, err := noticeObject(prep)
	if err != nil {
		return result, nil, err
	}
	if noticeDigest(prep) != before.PreparationSHA256 || !reflect.DeepEqual(obj["preparation"], prepObject) || !reflect.DeepEqual(obj["tools"], map[string]any{"go": "1.26.10", "node": "22.23.3", "pnpm": "10.30.3"}) {
		return result, nil, fmt.Errorf("build receipt preparation or toolchain differs")
	}
	if obj["recipeSha256"] != before.RecipeSHA256 {
		return result, nil, fmt.Errorf("recipe binding differs")
	}
	binary, err := read(build, "service/zitadel", 512<<20)
	if err != nil || len(binary) == 0 || obj["binarySha256"] != noticeDigest(binary) {
		return result, nil, fmt.Errorf("binary binding differs")
	}
	if before.Modules != nil {
		info, e := buildinfo.Read(bytes.NewReader(binary))
		if e != nil {
			return result, nil, fmt.Errorf("module binary metadata unreadable: %w", e)
		}
		var expected []*debug.Module
		for _, record := range before.Modules.Evidence.Modules {
			module := &debug.Module{Path: record.Original.Path, Version: record.Original.Version, Sum: record.Original.Sum}
			if replacement := record.Original.Replace; replacement != nil {
				module.Replace = &debug.Module{Path: replacement.Path, Version: replacement.Version, Sum: replacement.Sum}
			}
			expected = append(expected, module)
		}
		if e = noticeJoinBuildInfo(info, expected); e != nil {
			return result, nil, e
		}
	}
	result = noticeInventory{SchemaVersion: 1, UpstreamCommit: noticePin, Profile: "api-login-v1-console", OriginalPaths: []string{noticeConsole, noticeEmbedded}, RawDiagnostic: noticeRaw, Bytes: len(before.Raw), SHA256: noticeDigest(before.Raw), ImageAdmission: "unknown", LicenseClosure: "unknown", Reproducible: false, Inputs: []noticeBinding{
		{"build-receipt.json", noticeDigest(receipt)},
		{"prepared/source-preparation.json", before.PreparationSHA256},
		{"prepared/source/pnpm-lock.yaml", before.LockSHA256},
		{"inputs.sha256", before.InputsSHA256},
		{"packages.jsonl", before.PackagesSHA256},
		{"generated.sha256", noticeDigest(generated)},
		{noticeBefore, noticeDigest(beforeBytes)},
		{"scripts/build-zitadel-linux.sh", before.RecipeSHA256},
		{"service/zitadel", noticeDigest(binary)},
	}}
	result.supplement = before.Supplement
	result.toolchain = before.Toolchain
	if before.Toolchain != nil {
		result.Toolchain = &before.Toolchain.Evidence
	}
	result.sourceLicense = before.SourceLicense
	if before.SourceLicense != nil {
		result.SourceLicense = &before.SourceLicense.Evidence
	}
	result.moduleNotices = before.Modules
	if before.Modules != nil {
		result.Modules = &before.Modules.Evidence
	}
	return result, append([]byte(nil), before.Raw...), nil
}
