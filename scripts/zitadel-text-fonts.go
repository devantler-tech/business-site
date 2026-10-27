// Explicit source-bound attribution for the frozen identity text fonts.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode/utf16"
)

const textOFL = "internal/api/ui/login/static/resources/fonts/lato/OFL.txt"
const textBefore = "text-fonts.before.json"
const textRaw = "text-font-notices.txt"

type textPolicy struct {
	CatalogueHash       string
	Fonts, Outputs      int
	ApacheHash, OFLHash string
}

var textFrozen = textPolicy{"04619cd3cd0de555538bc81dd8d70e3ff0ccf575bde586cd6a32a0a924fa73cb", 54, 84, "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30", "74ba064d03f1f1c4a952da936c3eb71866c34404916734de3cae73b34357e59e"}

type textFont struct {
	Path            string   `json:"path"`
	Bytes           int      `json:"bytes"`
	SHA256          string   `json:"sha256"`
	GitBlob         string   `json:"gitBlob"`
	DeclaredLicense string   `json:"declaredLicense"`
	Outputs         []string `json:"outputs"`
}
type textCatalogue struct {
	SchemaVersion       int           `json:"schemaVersion"`
	UpstreamCommit      string        `json:"upstreamCommit"`
	Fonts               []textFont    `json:"fonts"`
	DeferredIconOutputs []inputRecord `json:"deferredIconOutputs,omitempty"`
}
type textBinding struct {
	textFont
	Names []textFontName `json:"names"`
}
type textState struct {
	SchemaVersion int            `json:"schemaVersion"`
	Inputs        []inputRecord  `json:"inputs"`
	Fonts         []textBinding  `json:"fonts"`
	Raw           []byte         `json:"checkedNoticeBytes"`
	Preparation   map[string]any `json:"checkedPreparation"`
}
type textInventory struct {
	SchemaVersion  int           `json:"schemaVersion"`
	UpstreamCommit string        `json:"upstreamCommit"`
	Profile        string        `json:"profile"`
	Inputs         []inputRecord `json:"inputs"`
	Fonts          []textBinding `json:"fonts"`
	RawDiagnostic  string        `json:"rawDiagnostic"`
	Bytes          int           `json:"bytes"`
	SHA256         string        `json:"sha256"`
	BinarySHA256   string        `json:"binarySha256"`
	ImageAdmission string        `json:"imageAdmission"`
	LicenseClosure string        `json:"licenseClosure"`
	Reproducible   bool          `json:"reproducible"`
}
type textReader func(string, string, int64) ([]byte, error)

func textDecode(data []byte, into any) error {
	if _, err := jsonObject(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(into)
}
func textPath(s string) bool {
	return s != "" && s != "." && path.Clean(s) == s && !path.IsAbs(s) && !strings.HasPrefix(s, "../") && !strings.ContainsAny(s, "\\\t\r\n")
}
func textHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func textExtension(s string) bool {
	switch strings.ToLower(path.Ext(s)) {
	case ".ttf", ".woff", ".woff2", ".otf", ".eot":
		return true
	}
	return false
}

// textSet inspects complete source/output directories; an extra selected font
// cannot disappear merely because the digest manifest omitted it.
func textSet(source string, expected map[string]bool) error {
	roots := []string{"console/src/assets/fonts", "internal/api/ui/login/static/resources/fonts", "console/dist/console", "internal/api/ui/console/static"}
	found := map[string]bool{}
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(source, root), func(p string, d fs.DirEntry, e error) error {
			if os.IsNotExist(e) && p == filepath.Join(source, root) {
				return nil
			}
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(source, p)
			if e != nil {
				return e
			}
			rel = filepath.ToSlash(rel)
			if rel == strings.TrimSuffix(iconRoot, "/") && d.IsDir() {
				return filepath.SkipDir
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("linked font observation")
			}
			if d.IsDir() {
				return nil
			}
			st, e := d.Info()
			if e != nil || !st.Mode().IsRegular() {
				return fmt.Errorf("nonregular font observation")
			}
			if textExtension(rel) {
				if !expected[rel] {
					return fmt.Errorf("unexpected selected font: %s", rel)
				}
				found[rel] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for name := range expected {
		if !found[name] {
			return fmt.Errorf("missing selected font: %s", name)
		}
	}
	return nil
}

// textPackages validates every compiler observation, and the two packages that
// enact inclusion. It does not execute generated Go or infer inclusion from CSS.
func textPackages(data []byte, build string, console map[string]bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	seen := map[string]bool{}
	for {
		var raw json.RawMessage
		e := d.Decode(&raw)
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("incomplete compiler stream")
		}
		obj, e := jsonObject(raw)
		if e != nil {
			return e
		}
		var p struct {
			ImportPath, Dir                    string
			GoFiles, EmbedPatterns, EmbedFiles []string
			Incomplete                         bool
			Error                              json.RawMessage
			DepsErrors                         []json.RawMessage
		}
		if e = json.Unmarshal(raw, &p); e != nil || p.Incomplete || len(p.Error) > 0 && string(p.Error) != "null" || len(p.DepsErrors) > 0 {
			return fmt.Errorf("failed compiler package")
		}
		_ = obj
		var relative string
		switch p.ImportPath {
		case "github.com/zitadel/zitadel/internal/api/ui/login/statik":
			relative = "internal/api/ui/login/statik"
		case "github.com/zitadel/zitadel/internal/api/ui/console":
			relative = "internal/api/ui/console"
		default:
			continue
		}
		if seen[relative] || p.Dir != filepath.Join(build, "prepared/source", relative) {
			return fmt.Errorf("ambiguous compiler selection")
		}
		seen[relative] = true
		if strings.HasSuffix(relative, "/statik") {
			sort.Strings(p.GoFiles)
			if !reflect.DeepEqual(p.GoFiles, []string{"generate.go", "statik.go"}) {
				return fmt.Errorf("compiled Login archive not selected")
			}
			continue
		}
		if !reflect.DeepEqual(p.EmbedPatterns, []string{"static"}) {
			return fmt.Errorf("unsupported Console embed selection")
		}
		files := map[string]bool{}
		fonts := map[string]bool{}
		for _, name := range p.EmbedFiles {
			if !textPath(name) || files[name] {
				return fmt.Errorf("duplicate or invalid compiler member")
			}
			files[name] = true
			if textExtension(name) {
				fonts[path.Join(relative, name)] = true
			}
		}
		if !reflect.DeepEqual(fonts, console) {
			return fmt.Errorf("Console compiler font set differs")
		}
	}
	if len(seen) != 2 {
		return fmt.Errorf("missing font compiler selection")
	}
	return nil
}

// textObserve returns the same bytes it validated. Capture never opens a notice
// again after checking it; pre/post comparison covers fonts, archive and compiler.
func textObserve(build, notices, recipe string, read textReader, policy textPolicy) (textState, error) {
	result := textState{SchemaVersion: 1}
	fail := func(e error) (textState, error) { return textState{}, e }
	get := func(root, name, label string, limit int64) ([]byte, error) {
		b, e := read(root, name, limit)
		if e != nil || len(b) == 0 || int64(len(b)) > limit {
			return nil, fmt.Errorf("incomplete text-font input: %s", label)
		}
		result.Inputs = append(result.Inputs, inputRecord{label, digest(b)})
		return b, nil
	}
	catBytes, e := get(notices, "catalogue.json", "text-fonts/catalogue.json", 1<<20)
	if e != nil {
		return fail(e)
	}
	var cat textCatalogue
	if digest(catBytes) != policy.CatalogueHash || textDecode(catBytes, &cat) != nil || cat.SchemaVersion != 1 || cat.UpstreamCommit != upstream || len(cat.Fonts) != policy.Fonts {
		return fail(fmt.Errorf("frozen text-font catalogue differs"))
	}
	apache, e := get(notices, "Apache-2.0.txt", "text-fonts/Apache-2.0.txt", 1<<20)
	if e != nil || digest(apache) != policy.ApacheHash {
		return fail(fmt.Errorf("retained Apache terms differ"))
	}
	ofl, e := get(build, "prepared/source/"+textOFL, "prepared/source/"+textOFL, 1<<20)
	if e != nil || digest(ofl) != policy.OFLHash {
		return fail(fmt.Errorf("original Lato OFL source differs"))
	}
	prep, e := get(build, "prepared/source-preparation.json", "prepared/source-preparation.json", 1<<20)
	if e != nil {
		return fail(e)
	}
	var preparation struct {
		SchemaVersion                                  int
		UpstreamCommit, ImageAdmission, LicenseClosure string
	}
	if _, e = jsonObject(prep); e != nil {
		return fail(e)
	}
	if e = json.Unmarshal(prep, &preparation); e != nil || preparation.SchemaVersion != 1 || preparation.UpstreamCommit != upstream || preparation.ImageAdmission != "unknown" || preparation.LicenseClosure != "unknown" {
		return fail(fmt.Errorf("wrong prepared source identity"))
	}
	if e = json.Unmarshal(prep, &result.Preparation); e != nil {
		return fail(e)
	}
	if _, e = get(filepath.Dir(recipe), filepath.Base(recipe), "build-recipe", 1<<20); e != nil {
		return fail(e)
	}
	for _, name := range []string{"zitadel-login-icons.go", "zitadel-text-fonts.go"} {
		if _, e = get(filepath.Dir(recipe), name, "verifier/"+name, 1<<20); e != nil {
			return fail(e)
		}
	}
	statik, e := get(build, "prepared/source/"+statikPath, "prepared/source/"+statikPath, 32<<20)
	if e != nil {
		return fail(e)
	}
	members, e := decodeStatik(statik)
	if e != nil {
		return fail(e)
	}
	expected, outputs, console, login := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	var raw bytes.Buffer
	fmt.Fprint(&raw, "Frozen ZITADEL text-font attribution\nSelected Windows Unicode BMP / en-US name records 0, 3, 5, 13, 14.\nOriginal strings follow without truncation. Metadata is not an image admission.\n")
	for _, f := range cat.Fonts {
		if !textPath(f.Path) || !strings.HasSuffix(f.Path, ".ttf") || f.Bytes < 1 || f.Bytes > 1<<20 || !textHex(f.SHA256, 64) || !textHex(f.GitBlob, 40) || expected[f.Path] || !(strings.HasPrefix(f.Path, "console/src/assets/fonts/") || strings.HasPrefix(f.Path, "internal/api/ui/login/static/resources/fonts/")) || !(f.DeclaredLicense == "OFL-1.1" || f.DeclaredLicense == "Apache-2.0") {
			return fail(fmt.Errorf("invalid selected source font"))
		}
		expected[f.Path] = true
		b, e := get(build, "prepared/source/"+f.Path, "prepared/source/"+f.Path, 1<<20)
		if e != nil || len(b) != f.Bytes || digest(b) != f.SHA256 || blob(b) != f.GitBlob {
			return fail(fmt.Errorf("frozen text-font source differs: %s", f.Path))
		}
		names, e := textFontNames(b)
		if e != nil {
			return fail(e)
		}
		binding := textBinding{f, names}
		result.Fonts = append(result.Fonts, binding)
		fmt.Fprintf(&raw, "\nSOURCE %s\nSHA256 %s\nGit blob %s\nDeclared license %s\n", f.Path, f.SHA256, f.GitBlob, f.DeclaredLicense)
		for _, n := range names {
			fmt.Fprintf(&raw, "\nNAME %d (platform %d, encoding %d, language 0x%04x; original bytes %d:%d, SHA256 %s)\n%s\n", n.ID, n.Platform, n.Encoding, n.Language, n.Offset, n.Bytes, n.SHA256, n.Text)
		}
		if len(f.Outputs) == 0 {
			return fail(fmt.Errorf("font lacks output binding"))
		}
		for _, out := range f.Outputs {
			if !textPath(out) || outputs[out] {
				return fail(fmt.Errorf("duplicate/invalid selected output"))
			}
			outputs[out] = true
			expected[out] = true
			copy, e := get(build, "prepared/source/"+out, "output/"+out, 1<<20)
			if e != nil || !bytes.Equal(b, copy) {
				return fail(fmt.Errorf("generated font differs: %s", out))
			}
			if strings.HasPrefix(out, "internal/api/ui/login/static/") {
				member := strings.TrimPrefix(out, "internal/api/ui/login/static/")
				login[member] = true
				if !bytes.Equal(b, members[member]) {
					return fail(fmt.Errorf("embedded Login font differs: %s", member))
				}
			} else if strings.HasPrefix(out, "internal/api/ui/console/static/") {
				console[out] = true
			} else if !strings.HasPrefix(out, "console/dist/console/") {
				return fail(fmt.Errorf("unsupported generated font binding"))
			}
		}
	}
	if len(outputs) != policy.Outputs {
		return fail(fmt.Errorf("selected output count differs"))
	}
	// These are closed byte identities of existing icon-font outputs, not text
	// font attributions or grants. Their distribution-wide review remains #308.
	for _, other := range cat.DeferredIconOutputs {
		if !textPath(other.Path) || !textHex(other.SHA256, 64) || expected[other.Path] || !(strings.HasPrefix(other.Path, "console/dist/console/") || strings.HasPrefix(other.Path, "internal/api/ui/console/static/")) {
			return fail(fmt.Errorf("invalid separately deferred icon output"))
		}
		expected[other.Path] = true
		b, e := get(build, "prepared/source/"+other.Path, "deferred-icon-output/"+other.Path, 1<<20)
		if e != nil || digest(b) != other.SHA256 {
			return fail(fmt.Errorf("separately deferred icon output changed"))
		}
		if strings.HasPrefix(other.Path, "internal/api/ui/console/static/") {
			console[other.Path] = true
		}
	}
	for name := range members {
		if textExtension(name) && !strings.HasPrefix(name, "resources/fonts/lgn-icons/") && !login[name] {
			return fail(fmt.Errorf("unexpected embedded Login font"))
		}
	}
	if e = textSet(filepath.Join(build, "prepared/source"), expected); e != nil {
		return fail(e)
	}
	packages, e := get(build, "packages.jsonl", "packages.jsonl", 64<<20)
	if e != nil {
		return fail(e)
	}
	if e = textPackages(packages, build, console); e != nil {
		return fail(e)
	}
	fmt.Fprintln(&raw, "\nFULL ORIGINAL LATO OFL SOURCE (its own copyright header, not substituted for the other fonts):")
	raw.Write(ofl)
	fmt.Fprintln(&raw, "\nFULL APACHE LICENSE 2.0 (selected original Roboto name records declare this license):")
	raw.Write(apache)
	if raw.Len() > 2<<20 {
		return fail(fmt.Errorf("excessive retained font attribution"))
	}
	result.Raw = raw.Bytes()
	sort.Slice(result.Inputs, func(i, j int) bool { return result.Inputs[i].Path < result.Inputs[j].Path })
	return result, nil
}

// textCapture binds unchanged pre-compilation inputs to the generated manifest,
// exact final binary and completed-receipt bytes. Pending is normalized only in
// the inventory label; pending and completed receipts may never coexist.
func textCapture(build, notices, recipe string, read textReader, policy textPolicy) (textInventory, []byte, error) {
	var result textInventory
	state, e := textObserve(build, notices, recipe, read, policy)
	if e != nil {
		return result, nil, e
	}
	before, e := read(build, textBefore, 4<<20)
	if e != nil {
		return result, nil, e
	}
	var saved textState
	if e = textDecode(before, &saved); e != nil || !reflect.DeepEqual(saved, state) {
		return result, nil, fmt.Errorf("font inputs changed across compilation")
	}
	generated, e := read(build, "generated.sha256", 8<<20)
	if e != nil {
		return result, nil, e
	}
	manifest := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(generated), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || !textHex(f[0], 64) || !textPath(f[1]) || manifest[f[1]] != "" {
			return result, nil, fmt.Errorf("incomplete or duplicate generated manifest")
		}
		manifest[f[1]] = f[0]
	}
	for _, f := range state.Fonts {
		for _, name := range f.Outputs {
			if manifest[name] != f.SHA256 {
				return result, nil, fmt.Errorf("generated font manifest differs")
			}
		}
	}
	for _, in := range state.Inputs {
		name := ""
		if in.Path == "prepared/source/"+statikPath {
			name = statikPath
		}
		if strings.HasPrefix(in.Path, "deferred-icon-output/") {
			name = strings.TrimPrefix(in.Path, "deferred-icon-output/")
		}
		if name != "" && manifest[name] != in.SHA256 {
			return result, nil, fmt.Errorf("archive or separately deferred icon manifest differs")
		}
	}
	state.Inputs = append(state.Inputs, inputRecord{textBefore, digest(before)}, inputRecord{"generated.sha256", digest(generated)})
	receiptName := ""
	for _, name := range []string{"build-receipt.pending", "build-receipt.json"} {
		if _, e := os.Lstat(filepath.Join(build, name)); e == nil {
			if receiptName != "" {
				return result, nil, fmt.Errorf("ambiguous build receipt")
			}
			receiptName = name
		} else if !os.IsNotExist(e) {
			return result, nil, e
		}
	}
	if receiptName == "" {
		return result, nil, fmt.Errorf("build receipt missing")
	}
	receipt, e := read(build, receiptName, 1<<20)
	if e != nil {
		return result, nil, e
	}
	obj, e := jsonObject(receipt)
	if e != nil {
		return result, nil, e
	}
	var decoded map[string]any
	if e = json.Unmarshal(receipt, &decoded); e != nil {
		return result, nil, e
	}
	for key, want := range map[string]any{"schemaVersion": float64(1), "upstreamCommit": upstream, "profile": "api-login-v1-console", "version": "4.20.0", "sourceDate": "2026-10-22T07:05:44Z", "platform": "linux/amd64", "imageAdmission": "unknown", "licenseClosure": "unknown", "reproducible": false} {
		if !reflect.DeepEqual(decoded[key], want) {
			return result, nil, fmt.Errorf("wrong build receipt identity: %s", key)
		}
	}
	if !reflect.DeepEqual(decoded["preparation"], state.Preparation) || !reflect.DeepEqual(decoded["tools"], map[string]any{"go": "1.26.10", "node": "22.23.3", "pnpm": "10.30.3"}) {
		return result, nil, fmt.Errorf("receipt preparation or compiler toolchain differs")
	}
	var recipeHash, binaryHash string
	if json.Unmarshal(obj["recipeSha256"], &recipeHash) != nil || json.Unmarshal(obj["binarySha256"], &binaryHash) != nil {
		return result, nil, fmt.Errorf("missing receipt digests")
	}
	for _, in := range state.Inputs {
		if in.Path == "build-recipe" && in.SHA256 != recipeHash {
			return result, nil, fmt.Errorf("build recipe differs from receipt")
		}
	}
	binary, e := read(build, "service/zitadel", 512<<20)
	if e != nil || len(binary) == 0 || !textHex(binaryHash, 64) || digest(binary) != binaryHash {
		return result, nil, fmt.Errorf("binary differs from font receipt")
	}
	state.Inputs = append(state.Inputs, inputRecord{"build-receipt.json", digest(receipt)})
	result = textInventory{SchemaVersion: 1, UpstreamCommit: upstream, Profile: "api-login-v1-console", Inputs: state.Inputs, Fonts: state.Fonts, RawDiagnostic: textRaw, Bytes: len(state.Raw), SHA256: digest(state.Raw), BinarySHA256: binaryHash, ImageAdmission: "unknown", LicenseClosure: "unknown"}
	return result, state.Raw, nil
}

// Explicit opt-in only; ordinary website builds do not invoke this mode.
func runTextFonts(args []string, out, diagnostic io.Writer) int {
	fail := func(e error) int { fmt.Fprintln(diagnostic, "text-fonts:", e); return 1 }
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
	var value any
	if mode == "--snapshot" {
		s, e := textObserve(values["--build"], values["--notices"], values["--recipe"], readAt, textFrozen)
		if e != nil {
			return fail(e)
		}
		value = s
	} else {
		inv, raw, e := textCapture(values["--build"], values["--notices"], values["--recipe"], readAt, textFrozen)
		if e != nil {
			return fail(e)
		}
		value = inv
		if mode == "--verify" {
			if _, e = os.Lstat(filepath.Join(values["--build"], "build-receipt.pending")); !os.IsNotExist(e) {
				return fail(fmt.Errorf("verification requires completed build"))
			}
			b, e := readAt(values["--build"], textRaw, 2<<20)
			if e != nil || !bytes.Equal(b, raw) {
				return fail(fmt.Errorf("retained font notices differ"))
			}
		} else {
			f, e := os.OpenFile(filepath.Join(values["--build"], textRaw), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return fail(e)
			}
			n, e := f.Write(raw)
			ce := f.Close()
			if e != nil || ce != nil || n != len(raw) {
				return fail(fmt.Errorf("incomplete font notice capture"))
			}
		}
	}
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return fail(e)
	}
	encoded := append(b, '\n')
	if mode == "--verify" {
		retained, e := readAt(values["--build"], "text-font-notices-inventory.json", 2<<20)
		if e != nil || !bytes.Equal(retained, encoded) {
			return fail(fmt.Errorf("retained font inventory differs"))
		}
	}
	if n, e := out.Write(encoded); e != nil || n != len(encoded) {
		return fail(fmt.Errorf("incomplete inventory output"))
	}
	return 0
}

// textFontName retains an original selected string and its source byte span.
type textFontName struct {
	Platform uint16 `json:"platform"`
	Encoding uint16 `json:"encoding"`
	Language uint16 `json:"language"`
	ID       uint16 `json:"nameId"`
	Offset   int    `json:"sourceOffset"`
	Bytes    int    `json:"sourceBytes"`
	SHA256   string `json:"sourceSha256"`
	Text     string `json:"text"`
}

// textFontNames reads selected attribution records without interpreting glyphs.
func textFontNames(font []byte) ([]textFontName, error) {
	if len(font) < 12 || len(font) > 1<<20 || binary.BigEndian.Uint32(font) != 0x00010000 {
		return nil, fmt.Errorf("unsupported or incomplete text-font source")
	}
	count := int(binary.BigEndian.Uint16(font[4:]))
	if count < 1 || count > 128 || count > (len(font)-12)/16 {
		return nil, fmt.Errorf("incomplete font table directory")
	}
	var names []byte
	nameOffset := 0
	tags := map[string]bool{}
	var spans [][2]int
	for i := 0; i < count; i++ {
		entry := font[12+16*i : 28+16*i]
		tag := string(entry[:4])
		if tags[tag] {
			return nil, fmt.Errorf("duplicate font table")
		}
		tags[tag] = true
		off, size := int(binary.BigEndian.Uint32(entry[8:])), int(binary.BigEndian.Uint32(entry[12:]))
		if off > len(font) || size > len(font)-off || (size > 0 && off < 12+16*count) {
			return nil, fmt.Errorf("incomplete or overlapping font table")
		}
		for _, span := range spans {
			if size > 0 && off < span[1] && span[0] < off+size {
				return nil, fmt.Errorf("overlapping font table spans")
			}
		}
		if size > 0 {
			spans = append(spans, [2]int{off, off + size})
		}
		if tag == "name" {
			names, nameOffset = font[off:off+size], off
		}
	}
	if len(names) < 6 || binary.BigEndian.Uint16(names) != 0 {
		return nil, fmt.Errorf("missing or unsupported naming table")
	}
	n, storage := int(binary.BigEndian.Uint16(names[2:])), int(binary.BigEndian.Uint16(names[4:]))
	if n > 4096 || n > (len(names)-6)/12 || storage < 6+12*n || storage > len(names) {
		return nil, fmt.Errorf("incomplete or overlapping naming records")
	}
	selected := map[uint16]bool{0: true, 3: true, 5: true, 13: true, 14: true}
	seen := map[[4]uint16]bool{}
	result := []textFontName{}
	retainedBytes := 0
	for i := 0; i < n; i++ {
		entry := names[6+12*i : 18+12*i]
		key := [4]uint16{binary.BigEndian.Uint16(entry), binary.BigEndian.Uint16(entry[2:]), binary.BigEndian.Uint16(entry[4:]), binary.BigEndian.Uint16(entry[6:])}
		if seen[key] {
			return nil, fmt.Errorf("ambiguous naming record")
		}
		seen[key] = true
		size, offset := int(binary.BigEndian.Uint16(entry[8:])), storage+int(binary.BigEndian.Uint16(entry[10:]))
		if offset > len(names) || size > len(names)-offset {
			return nil, fmt.Errorf("incomplete attribution observation")
		}
		if key[0] != 3 || key[1] != 1 || key[2] != 0x0409 || !selected[key[3]] {
			continue
		}
		if size == 0 || size%2 != 0 || retainedBytes > (128<<10)-size {
			return nil, fmt.Errorf("invalid or excessive attribution string")
		}
		retainedBytes += size
		raw := names[offset : offset+size]
		text, err := textFontUnicode(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, textFontName{key[0], key[1], key[2], key[3], nameOffset + offset, size, fmt.Sprintf("%x", sha256.Sum256(raw)), text})
	}
	if len(result) != 5 {
		return nil, fmt.Errorf("complete selected copyright, version and license records required")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// textFontUnicode refuses invalid UTF-16 rather than replacing damaged notice text.
func textFontUnicode(raw []byte) (string, error) {
	var out strings.Builder
	for i := 0; i < len(raw); i += 2 {
		u := binary.BigEndian.Uint16(raw[i:])
		switch {
		case u == 0:
			return "", fmt.Errorf("NUL in selected attribution")
		case u >= 0xd800 && u <= 0xdbff:
			if i+4 > len(raw) {
				return "", fmt.Errorf("unpaired attribution surrogate")
			}
			low := binary.BigEndian.Uint16(raw[i+2:])
			if low < 0xdc00 || low > 0xdfff {
				return "", fmt.Errorf("invalid attribution surrogate pair")
			}
			out.WriteRune(utf16.DecodeRune(rune(u), rune(low)))
			i += 2
		case u >= 0xdc00 && u <= 0xdfff:
			return "", fmt.Errorf("unpaired attribution surrogate")
		default:
			out.WriteRune(rune(u))
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", fmt.Errorf("empty selected attribution")
	}
	return out.String(), nil
}
