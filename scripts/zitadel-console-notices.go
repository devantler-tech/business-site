// Command zitadel-console-notices captures selected same-build notice evidence.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"
)

const noticePin = "8afa5dd37a8e723efb9eace8d09fd5c1cccef694"
const noticeConsole = "console/dist/console/3rdpartylicenses.txt"
const noticeEmbedded = "internal/api/ui/console/static/3rdpartylicenses.txt"
const noticeBefore = "console-notices.before.json"
const noticeRaw = "console-third-party-notices.txt"
const noticeLimit = 2 << 20

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
	Role           string `json:"role"`
	Kind           string `json:"kind"`
	Original       string `json:"original"`
	OriginalBytes  int    `json:"originalBytes"`
	OriginalSHA256 string `json:"originalSha256"`
	Offset         int    `json:"offset"`
	Bytes          int    `json:"bytes"`
	SHA256         string `json:"sha256"`
	Retained       string `json:"retained"`
	GitBlob        string `json:"gitBlob,omitempty"`
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

const supplementCatalogueHash = "8d3788c0b223eba81f8d0b462ec4322b3757cef3f1150b3bc54388b5127e9cd7"
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

// noticeSupplementSnapshot checks a hash-pinned eight-package closure (seven
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
	if catalogue.SchemaVersion != 1 || catalogue.UpstreamCommit != noticePin || len(catalogue.Packages) != 8 {
		return nil, fmt.Errorf("unsupported supplement catalogue")
	}
	lock, err := read(build, "prepared/source/pnpm-lock.yaml", 16<<20)
	if err != nil || noticeDigest(lock) != catalogue.LockSHA256 {
		return nil, fmt.Errorf("supplement frozen lock differs")
	}
	result := &supplementState{CatalogueSHA256: expected, Catalogue: catalogue, Inputs: []noticeBinding{{"catalogue.json", expected}}}
	names := []string{"qrcode", "@ng-icons/core", "grpc-web", "@connectrpc/connect", "@connectrpc/connect-web", "@bufbuild/protobuf", "google-protobuf", "angularx-qrcode"}
	versions := []string{"1.5.4", "33.4.0", "2.0.2", "2.1.2", "2.1.2", "2.13.0", "4.0.2", "21.0.5"}
	source := filepath.Join(build, "prepared/source")
	resolved := map[string]supplementAlias{}
	// The parent resolves first. qrcode is its sibling edge in this pnpm
	// node_modules directory, never a guessed/hoisted console dependency.
	for _, i := range []int{7, 0, 1, 2, 3, 4, 5, 6} {
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
			text, e := read(notices, component.Retained, noticeLimit)
			if e != nil || len(text) != component.Bytes || noticeDigest(text) != component.SHA256 || !utf8.Valid(text) {
				return nil, fmt.Errorf("supplement retained text differs")
			}
			if component.Kind == "installed" {
				original, e := read(source, root+"/"+component.Original, 2<<20)
				if e != nil || len(original) != component.OriginalBytes || noticeDigest(original) != component.OriginalSHA256 || component.Offset < 0 || component.Offset > len(original) || component.Bytes > len(original)-component.Offset || !bytes.Equal(text, original[component.Offset:component.Offset+component.Bytes]) {
					return nil, fmt.Errorf("supplement installed text identity differs")
				}
				result.Inputs = append(result.Inputs, noticeBinding{"prepared/source/" + root + "/" + component.Original, noticeDigest(original)})
			} else if component.Kind != "immutable-source" || !strings.HasPrefix(component.Original, "https://raw.githubusercontent.com/") {
				return nil, fmt.Errorf("unsupported supplement source")
			}
			result.Inputs = append(result.Inputs, noticeBinding{component.Retained, noticeDigest(text)})
			header := fmt.Sprintf("Package: %s@%s\nComponent: %s\nSource: %s\n\n", pkg.Name, pkg.Version, component.Role, component.Original)
			result.Text = append(result.Text, []byte(header)...)
			result.Sections = append(result.Sections, supplementSection{pkg.Name, pkg.Version, component.Role, component.Original, len(result.Text), len(text), noticeDigest(text)})
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

type noticeState struct {
	SchemaVersion     int              `json:"schemaVersion"`
	Raw               []byte           `json:"raw"`
	PreparationSHA256 string           `json:"preparationSha256"`
	LockSHA256        string           `json:"lockSha256"`
	InputsSHA256      string           `json:"inputsSha256"`
	PackagesSHA256    string           `json:"packagesSha256"`
	RecipeSHA256      string           `json:"recipeSha256"`
	Supplement        *supplementState `json:"supplement,omitempty"`
}

type noticeSupplementConfig struct{ Directory, CatalogueHash string }

type noticeBinding struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type noticeInventory struct {
	SchemaVersion  int             `json:"schemaVersion"`
	UpstreamCommit string          `json:"upstreamCommit"`
	Profile        string          `json:"profile"`
	OriginalPaths  []string        `json:"originalPaths"`
	RawDiagnostic  string          `json:"rawDiagnostic"`
	Bytes          int             `json:"bytes"`
	SHA256         string          `json:"sha256"`
	Inputs         []noticeBinding `json:"inputs"`
	ImageAdmission string          `json:"imageAdmission"`
	LicenseClosure string          `json:"licenseClosure"`
	Reproducible   bool            `json:"reproducible"`
	supplement     *supplementState
}

func main() { os.Exit(noticeRun(os.Args[1:], os.Stdout, os.Stderr)) }

// noticeRun is inert unless one complete explicit mode is selected.
func noticeRun(args []string, out, diagnostic io.Writer) int {
	return noticeRunWithCatalogue(args, out, diagnostic, supplementCatalogueHash)
}

// noticeRunWithCatalogue keeps the curation identity injectable in hermetic
// tests. The public command always supplies its compiled, reviewed constant.
func noticeRunWithCatalogue(args []string, out, diagnostic io.Writer, catalogueHash string) int {
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
		case "--snapshot", "--inventory":
			values[key] = "true"
		case "--build", "--recipe", "--supplement":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing argument"))
			}
			i++
			values[key] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument"))
		}
	}
	validCount := len(values) == 3 || (len(values) == 4 && values["--supplement"] != "")
	if !validCount || (values["--snapshot"] == "") == (values["--inventory"] == "") || values["--build"] == "" || values["--recipe"] == "" {
		return fail(fmt.Errorf("expected one mode, build and recipe"))
	}
	var result any
	options := []noticeSupplementConfig{}
	if values["--supplement"] != "" {
		options = append(options, noticeSupplementConfig{values["--supplement"], catalogueHash})
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
		if err = noticeWriteDiagnostic(values["--build"], noticeRaw, raw); err != nil {
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
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return fail(fmt.Errorf("incomplete inventory output"))
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
		if e = noticeWriteDiagnostic(values["--build"], supplementRaw, s.Text); e != nil {
			return fail(e)
		}
		if e = noticeWriteDiagnostic(values["--build"], supplementInventoryPending, append(encoded, '\n')); e != nil {
			return fail(e)
		}
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
		result.Supplement, err = noticeSupplementSnapshot(build, options[0].Directory, options[0].CatalogueHash, read)
		if err != nil {
			return result, err
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
	beforeBytes, err := read(build, noticeBefore, 4<<20)
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
	return result, append([]byte(nil), before.Raw...), nil
}
