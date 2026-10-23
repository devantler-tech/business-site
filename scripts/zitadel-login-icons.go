// Command zitadel-login-icons validates the bounded, frozen Login V1 icon family.
package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const upstream = "8afa5dd37a8e723efb9eace8d09fd5c1cccef694"
const iconRoot = "internal/api/ui/login/static/resources/fonts/lgn-icons/"
const statikPath = "internal/api/ui/login/statik/statik.go"
const iconRevision = "37d58b353175bd5466820c4c1873b4764dd92719"

var fontHashes = map[string]string{
	"lgn-icons.eot":  "5c86410c95697facdbb47ff14207fbc93e401ba968d3092cfea428cc9385967b",
	"lgn-icons.ttf":  "bc1f192936879a2100e1f78f13b29f3b96cb934fb0a4a7c05041c9396f360a0a",
	"lgn-icons.woff": "5fc5d9d78d03820e1c03f84bbcd85d4741a5e5fa4598bd415743a63a17d0d787",
	"lgn-icons.svg":  "93a7b0b83d3d80b64e50ba100ec12cf35eb0b0b3abd3a41ed6d323c58da88383",
}

type glyphInput struct{ name, blob string }

// Index is the fixed private-use codepoint minus U+E900, not source-directory order.
var glyphInputs = []glyphInput{
	{"times-solid", "3605fbf3e9bb7e8018c6a12e10e08ba5d8aad333"},
	{"user-plus-solid", "840278b3e1eaf66a639ce7a66ebb73100010474b"},
	{"angle-left-solid", "67bad049e7c0c2d221c75c8b47c329c11c6ab28c"},
	{"angle-right-solid", "2e6375f681dd06596aa1b79309d83f14046173b8"},
	{"arrow-left-solid", "c5c59870bce8833781121b6cb1a99bf09b82ce04"},
	{"arrow-right-solid", "442d9eff8a9b2d7cab051dd3e366c0a9ab6a1104"},
	{"clipboard-check-solid", "c7cf91dfcd6b9b8f42027950e0625395b556c2c4"},
	{"clipboard", "3425dcbc324a7633a58ce9bdd6f5f2ec24f6832a"},
	{"exclamation-circle-solid", "9d4a667fa3ef8ca2e011f873ab2ac3d00e8af63d"},
	{"check-solid", "fa3b17b3064550666bef1283505f1a7cfe942af6"},
}

type fontRecord struct {
	Path           string `json:"path"`
	Size           int    `json:"size"`
	SHA256         string `json:"sha256"`
	EmbeddedMember string `json:"embeddedMember"`
}
type glyphRecord struct {
	Codepoint      string `json:"codepoint"`
	Name           string `json:"name"`
	SourceGitBlob  string `json:"sourceGitBlob"`
	RetainedSHA256 string `json:"retainedSha256"`
	GeometrySHA256 string `json:"geometrySha256"`
}
type inputRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type inventory struct {
	SchemaVersion     int           `json:"schemaVersion"`
	UpstreamCommit    string        `json:"upstreamCommit"`
	Profile           string        `json:"profile"`
	VectorRevision    string        `json:"vectorRevision"`
	SelectedLicense   string        `json:"selectedLicense"`
	Inputs            []inputRecord `json:"inputs"`
	Fonts             []fontRecord  `json:"fonts"`
	Glyphs            []glyphRecord `json:"glyphs"`
	DecodedMembers    int           `json:"decodedMembers"`
	GeneratorSettings string        `json:"generatorSettings"`
	ImageAdmission    string        `json:"imageAdmission"`
	LicenseClosure    string        `json:"licenseClosure"`
	Reproducible      bool          `json:"reproducible"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run emits nothing in the inactive state, and emits JSON only after a complete check.
func run(args []string, out, diagnostic io.Writer) int {
	if len(args) == 0 {
		return 0
	}
	fail := func(err error) int {
		if _, e := fmt.Fprintln(diagnostic, "login-icons:", err); e != nil {
			return 1
		}
		return 1
	}
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if _, ok := values[a]; ok {
			return fail(fmt.Errorf("duplicate argument %s", a))
		}
		switch a {
		case "--inventory":
			values[a] = "true"
		case "--build", "--notices":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing value for %s", a))
			}
			i++
			values[a] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument %s", a))
		}
	}
	if len(values) != 3 || values["--inventory"] != "true" {
		return fail(fmt.Errorf("expected --inventory --build <build-directory> --notices <notice-directory>"))
	}
	result, err := inspect(values["--build"], values["--notices"])
	if err != nil {
		return fail(err)
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fail(err)
	}
	if _, err = out.Write(append(b, '\n')); err != nil {
		return fail(err)
	}
	return 0
}

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func blob(b []byte) string {
	return fmt.Sprintf("%x", sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(b))), b...)))
}

// readAt admits regular files below a canonical root, never links or partial reads.
func readAt(root, relative string, limit int64) ([]byte, error) {
	if relative == "" || path.Clean(relative) != relative || strings.HasPrefix(relative, "../") || path.IsAbs(relative) {
		return nil, fmt.Errorf("unclean input path")
	}
	p := root
	for i, part := range strings.Split(relative, "/") {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked input: %s", relative)
		}
		if i < len(strings.Split(relative, "/"))-1 && !info.IsDir() {
			return nil, fmt.Errorf("non-directory input parent")
		}
	}
	confined, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	f, err := confined.Open(relative)
	rootErr := confined.Close()
	if err != nil {
		return nil, err
	}
	if rootErr != nil {
		_ = f.Close()
		return nil, rootErr
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		_ = f.Close()
		return nil, fmt.Errorf("nonregular or oversized input: %s", relative)
	}
	b, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(b)) != info.Size() || int64(len(b)) > limit {
		return nil, fmt.Errorf("incomplete or growing input: %s", relative)
	}
	return b, nil
}

func exactDirectory(root, relative string, want []string) error {
	// Reading a child first verifies the parents, including the selected directory.
	if len(want) == 0 {
		return fmt.Errorf("empty directory expectation")
	}
	child := path.Join(relative, want[0])
	if _, err := readAt(root, child, 32<<20); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(root, relative))
	if err != nil {
		return err
	}
	got := []string{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return fmt.Errorf("nonregular selected member")
		}
		got = append(got, e.Name())
	}
	expected := append([]string{}, want...)
	sort.Strings(expected)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(expected, "\n") {
		return fmt.Errorf("selected directory membership differs: %s", relative)
	}
	return nil
}

// jsonObject checks duplicate keys at every depth before decoding selected fields.
func jsonObject(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	var consume func() error
	consume = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := t.(json.Delim); ok {
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
					if e = consume(); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e := consume(); e != nil {
						return e
					}
				}
			default:
				return fmt.Errorf("unexpected JSON delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := consume(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing or incomplete JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return object, nil
}

func inspect(build, notices string) (inventory, error) {
	var result inventory
	build, err := filepath.EvalSymlinks(build)
	if err != nil {
		return result, err
	}
	notices, err = filepath.EvalSymlinks(notices)
	if err != nil {
		return result, err
	}
	read := func(root, p string, cap int64) ([]byte, error) {
		b, e := readAt(root, p, cap)
		if e == nil {
			result.Inputs = append(result.Inputs, inputRecord{p, digest(b)})
		}
		return b, e
	}
	receipt := ""
	for _, candidate := range []string{"build-receipt.pending", "build-receipt.json"} {
		_, e := os.Lstat(filepath.Join(build, candidate))
		if e == nil {
			if receipt != "" {
				return result, fmt.Errorf("ambiguous build receipts")
			}
			receipt = candidate
		} else if !os.IsNotExist(e) {
			return result, e
		}
	}
	if receipt == "" {
		return result, fmt.Errorf("missing build receipt")
	}
	for _, p := range []string{receipt, "prepared/source-preparation.json"} {
		b, e := readAt(build, p, 1<<20)
		if e != nil {
			return result, e
		}
		label := p
		if p == receipt {
			label = "build-receipt.json"
		}
		result.Inputs = append(result.Inputs, inputRecord{label, digest(b)})
		obj, e := jsonObject(b)
		if e != nil {
			return result, e
		}
		for key, want := range map[string]string{"upstreamCommit": `"` + upstream + `"`, "schemaVersion": "1", "imageAdmission": `"unknown"`, "licenseClosure": `"unknown"`} {
			if string(obj[key]) != want {
				return result, fmt.Errorf("receipt mismatch: %s %s", p, key)
			}
		}
		if p == receipt && (string(obj["profile"]) != `"api-login-v1-console"` || string(obj["platform"]) != `"linux/amd64"` || string(obj["reproducible"]) != "false") {
			return result, fmt.Errorf("unsupported build profile")
		}
	}
	if err = exactDirectory(notices, ".", []string{"LICENSE.md", "NOTICE.md", "clipboard.svg"}); err != nil {
		return result, err
	}
	for p, want := range map[string]string{"LICENSE.md": "55a4cc3d450a9dfa4d12c7de238442562899868299b800dbac6c70ca6d99c490", "NOTICE.md": "bbbf1549408fef9c90199ac0859a49a110683cf3b89394e0402d030a7cfe5155", "clipboard.svg": "9a526c626cdb157f44c29831d5c99eba3d719a92758a257891164538090fd739"} {
		b, e := read(notices, p, 1<<20)
		if e != nil {
			return result, e
		}
		if digest(b) != want {
			return result, fmt.Errorf("notice/source identity differs: %s", p)
		}
	}
	source := filepath.Join(build, "prepared/source")
	manifestBytes, err := read(build, "generated.sha256", 8<<20)
	if err != nil {
		return result, err
	}
	entries := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(manifestBytes), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(line[:64]) {
			return result, fmt.Errorf("malformed generated inventory")
		}
		p := line[66:]
		if path.Clean(p) != p || path.IsAbs(p) || strings.HasPrefix(p, "../") || entries[p] != "" {
			return result, fmt.Errorf("duplicate or unclean generated member")
		}
		entries[p] = line[:64]
	}
	statik, err := read(source, statikPath, 32<<20)
	if err != nil {
		return result, err
	}
	if entries[statikPath] != digest(statik) {
		return result, fmt.Errorf("statik digest does not match selected output")
	}
	members, err := decodeStatik(statik)
	if err != nil {
		return result, err
	}
	fontNames := []string{"lgn-icons.eot", "lgn-icons.svg", "lgn-icons.ttf", "lgn-icons.woff"}
	if err = exactDirectory(source, iconRoot+"fonts", fontNames); err != nil {
		return result, err
	}
	fonts := map[string][]byte{}
	for _, name := range fontNames {
		p := iconRoot + "fonts/" + name
		b, e := read(source, p, 1<<20)
		if e != nil {
			return result, e
		}
		hash := digest(b)
		if hash != fontHashes[name] || entries[p] != hash {
			return result, fmt.Errorf("font source/output identity differs: %s", name)
		}
		member := "resources/fonts/lgn-icons/fonts/" + name
		if !bytes.Equal(members[member], b) {
			return result, fmt.Errorf("embedded font differs: %s", name)
		}
		fonts[name] = b
		result.Fonts = append(result.Fonts, fontRecord{p, len(b), hash, member})
	}
	for name := range members {
		if strings.HasPrefix(name, "resources/fonts/lgn-icons/fonts/") {
			if _, ok := fonts[path.Base(name)]; !ok || name != "resources/fonts/lgn-icons/fonts/"+path.Base(name) {
				return result, fmt.Errorf("extra embedded icon font")
			}
		}
	}
	vectors := map[string][]byte{}
	vectorNames := []string{"clipboard-solid.svg"}
	for _, g := range glyphInputs {
		if g.name != "clipboard" {
			vectorNames = append(vectorNames, g.name+".svg")
		}
	}
	if err = exactDirectory(source, iconRoot+"svg", vectorNames); err != nil {
		return result, err
	}
	for _, name := range vectorNames {
		b, e := read(source, iconRoot+"svg/"+name, 1<<20)
		if e != nil {
			return result, e
		}
		vectors[name] = b
	}
	if blob(vectors["clipboard-solid.svg"]) != "7edc3eda1ebbc7cc7f60763f06beaa4b25579602" {
		return result, fmt.Errorf("original solid clipboard input differs")
	}
	clipboard, err := readAt(notices, "clipboard.svg", 1<<20)
	if err != nil {
		return result, err
	}
	vectors["clipboard.svg"] = clipboard
	for _, g := range glyphInputs {
		b := vectors[g.name+".svg"]
		if g.name == "clipboard" {
			b = bytes.TrimSuffix(b, []byte("\n"))
		}
		if blob(b) != g.blob {
			return result, fmt.Errorf("immutable vector differs: %s", g.name)
		}
	}
	if err = checkWrappers(fonts); err != nil {
		return result, err
	}
	geometry, err := checkGlyphs(fonts["lgn-icons.svg"], vectors)
	if err != nil {
		return result, err
	}
	for i, g := range glyphInputs {
		result.Glyphs = append(result.Glyphs, glyphRecord{fmt.Sprintf("U+%04X", 0xe900+i), g.name, g.blob, digest(vectors[g.name+".svg"]), geometry[i]})
	}
	sort.Slice(result.Inputs, func(i, j int) bool { return result.Inputs[i].Path < result.Inputs[j].Path })
	result.SchemaVersion = 1
	result.UpstreamCommit = upstream
	result.Profile = "api-login-v1-console"
	result.VectorRevision = iconRevision
	result.SelectedLicense = "MIT"
	result.DecodedMembers = len(members)
	result.GeneratorSettings = "unknown"
	result.ImageAdmission = "unknown"
	result.LicenseClosure = "unknown"
	return result, nil
}

// decodeStatik parses the closed generated registration shape, then CRC-reads every ZIP entry.
func decodeStatik(data []byte) (map[string][]byte, error) {
	bad := func() (map[string][]byte, error) { return nil, fmt.Errorf("unsupported or incomplete login statik") }
	if len(data) > 32<<20 {
		return bad()
	}
	f, err := parser.ParseFile(token.NewFileSet(), "statik.go", data, 0)
	if err != nil || f.Name.Name != "statik" || len(f.Decls) != 3 {
		return bad()
	}
	imp, ok := f.Decls[0].(*ast.GenDecl)
	if !ok || imp.Tok != token.IMPORT || len(imp.Specs) != 1 {
		return bad()
	}
	is, ok := imp.Specs[0].(*ast.ImportSpec)
	if !ok || is.Name != nil || is.Path.Value != `"github.com/rakyll/statik/fs"` {
		return bad()
	}
	c, ok := f.Decls[1].(*ast.GenDecl)
	if !ok || c.Tok != token.CONST || len(c.Specs) != 1 {
		return bad()
	}
	vs, ok := c.Specs[0].(*ast.ValueSpec)
	if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "Login" || len(vs.Values) != 1 {
		return bad()
	}
	literal, ok := vs.Values[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING || literal.Value != `"login"` {
		return bad()
	}
	fn, ok := f.Decls[2].(*ast.FuncDecl)
	if !ok || fn.Name.Name != "init" || fn.Recv != nil || fn.Type.Params.NumFields() != 0 || fn.Type.Results != nil || fn.Body == nil || len(fn.Body.List) != 2 {
		return bad()
	}
	assign, ok := fn.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return bad()
	}
	id, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || id.Name != "data" {
		return bad()
	}
	literal, ok = assign.Rhs[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return bad()
	}
	expr, ok := fn.Body.List[1].(*ast.ExprStmt)
	if !ok {
		return bad()
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return bad()
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "RegisterWithNamespace" {
		return bad()
	}
	id, ok = sel.X.(*ast.Ident)
	if !ok || id.Name != "fs" {
		return bad()
	}
	ns, ok := call.Args[0].(*ast.BasicLit)
	if !ok || ns.Value != `"login"` {
		return bad()
	}
	id, ok = call.Args[1].(*ast.Ident)
	if !ok || id.Name != "data" {
		return bad()
	}
	payload, err := strconv.Unquote(literal.Value)
	if err != nil || len(payload) > 16<<20 {
		return bad()
	}
	if len(payload) < 22 || !strings.HasPrefix(payload, "PK\x03\x04") || payload[len(payload)-22:len(payload)-18] != "PK\x05\x06" {
		return bad()
	}
	zr, err := zip.NewReader(strings.NewReader(payload), int64(len(payload)))
	if err != nil || len(zr.File) == 0 || len(zr.File) > 512 || zr.Comment != "" {
		return bad()
	}
	result := map[string][]byte{}
	seen := map[string]bool{}
	var total uint64
	for _, entry := range zr.File {
		name := entry.Name
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || path.Clean(clean) != clean || path.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.ContainsAny(clean, "\\\x00") || seen[name] {
			return bad()
		}
		seen[name] = true
		if !entry.Mode().IsRegular() || (entry.Method != zip.Store && entry.Method != zip.Deflate) || entry.UncompressedSize64 > 4<<20 {
			return bad()
		}
		total += entry.UncompressedSize64
		if total > 32<<20 {
			return bad()
		}
		r, err := entry.Open()
		if err != nil {
			return bad()
		}
		b, readErr := io.ReadAll(io.LimitReader(r, 4<<20+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || uint64(len(b)) != entry.UncompressedSize64 {
			return bad()
		}
		result[name] = b
	}
	return result, nil
}

// sfntTables validates the bounded TTF directory and returns complete table snapshots.
func sfntTables(b []byte) (map[string][]byte, error) {
	if len(b) < 188 || binary.BigEndian.Uint32(b) != 0x10000 || binary.BigEndian.Uint16(b[4:]) != 11 {
		return nil, fmt.Errorf("unsupported TTF directory")
	}
	result := map[string][]byte{}
	type span struct{ start, end uint64 }
	spans := []span{}
	for i := 0; i < 11; i++ {
		p := 12 + 16*i
		name := string(b[p : p+4])
		offset := uint64(binary.BigEndian.Uint32(b[p+8:]))
		size := uint64(binary.BigEndian.Uint32(b[p+12:]))
		if result[name] != nil || offset < 188 || offset%4 != 0 || size == 0 || offset+size > uint64(len(b)) {
			return nil, fmt.Errorf("invalid TTF table")
		}
		spans = append(spans, span{offset, offset + size})
		result[name] = b[offset : offset+size]
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return nil, fmt.Errorf("overlapping TTF table")
		}
	}
	return result, nil
}

// checkWrappers compares every WOFF table and the exact EOT TTF, not one sample.
func checkWrappers(fonts map[string][]byte) error {
	ttf := fonts["lgn-icons.ttf"]
	tables, err := sfntTables(ttf)
	if err != nil {
		return err
	}
	if err = checkCmap(tables); err != nil {
		return err
	}
	eot := fonts["lgn-icons.eot"]
	if len(eot) != 2280 || binary.LittleEndian.Uint32(eot) != 2280 || binary.LittleEndian.Uint32(eot[4:]) != uint32(len(ttf)) || binary.LittleEndian.Uint32(eot[8:]) != 0x20001 || binary.LittleEndian.Uint32(eot[12:]) != 0 || binary.LittleEndian.Uint16(eot[34:]) != 0x504c || !bytes.Equal(eot[164:], ttf) {
		return fmt.Errorf("EOT does not wrap exact TTF")
	}
	w := fonts["lgn-icons.woff"]
	if len(w) != 2192 || string(w[:4]) != "wOFF" || binary.BigEndian.Uint32(w[4:]) != 0x10000 || binary.BigEndian.Uint32(w[8:]) != uint32(len(w)) || binary.BigEndian.Uint16(w[12:]) != 11 || binary.BigEndian.Uint16(w[14:]) != 0 || binary.BigEndian.Uint32(w[16:]) != uint32(len(ttf)) {
		return fmt.Errorf("invalid WOFF header")
	}
	for _, offset := range []int{24, 28, 32, 36, 40} {
		if binary.BigEndian.Uint32(w[offset:]) != 0 {
			return fmt.Errorf("unexpected WOFF metadata/private data")
		}
	}
	seen := map[string]bool{}
	lastEnd := uint64(264)
	for i := 0; i < 11; i++ {
		p := 44 + 20*i
		name := string(w[p : p+4])
		offset := uint64(binary.BigEndian.Uint32(w[p+4:]))
		compressed := uint64(binary.BigEndian.Uint32(w[p+8:]))
		size := uint64(binary.BigEndian.Uint32(w[p+12:]))
		want, ok := tables[name]
		if !ok || seen[name] || offset < lastEnd || offset%4 != 0 || compressed == 0 || compressed > size || offset+compressed > uint64(len(w)) || size != uint64(len(want)) {
			return fmt.Errorf("invalid WOFF table")
		}
		seen[name] = true
		lastEnd = offset + compressed
		b := w[offset : offset+compressed]
		if compressed < size {
			r, e := zlib.NewReader(bytes.NewReader(b))
			if e != nil {
				return e
			}
			b, e = io.ReadAll(io.LimitReader(r, int64(size)+1))
			closeErr := r.Close()
			if e != nil {
				return e
			}
			if closeErr != nil {
				return closeErr
			}
		}
		if !bytes.Equal(b, want) {
			return fmt.Errorf("WOFF table differs: %s", name)
		}
	}
	return nil
}

func checkCmap(tables map[string][]byte) error {
	b := tables["cmap"]
	maxp := tables["maxp"]
	if len(b) < 28 || len(maxp) < 6 || binary.BigEndian.Uint16(maxp[4:]) != 14 || binary.BigEndian.Uint16(b) != 0 || binary.BigEndian.Uint16(b[2:]) != 3 {
		return fmt.Errorf("unsupported cmap/glyph slots")
	}
	want := map[int]uint16{0: 1, 1: 2, 32: 3}
	for i := 0; i < 10; i++ {
		want[0xe900+i] = uint16(4 + i)
	}
	pairs := map[uint32]bool{3: true, 0x10003: true, 0x30001: true}
	for i := 0; i < 3; i++ {
		p := 4 + 8*i
		pair := binary.BigEndian.Uint32(b[p:])
		if !pairs[pair] {
			return fmt.Errorf("unexpected cmap encoding")
		}
		delete(pairs, pair)
		offset := uint64(binary.BigEndian.Uint32(b[p+4:]))
		if offset+14 > uint64(len(b)) {
			return fmt.Errorf("incomplete cmap")
		}
		s := b[offset:]
		size := int(binary.BigEndian.Uint16(s[2:]))
		n := int(binary.BigEndian.Uint16(s[6:])) / 2
		if binary.BigEndian.Uint16(s) != 4 || n < 1 || n > 256 || size > len(s) || 16+8*n > size {
			return fmt.Errorf("invalid format-4 cmap")
		}
		s = s[:size]
		previous := -1
		for k := 0; k < n; k++ {
			end := int(binary.BigEndian.Uint16(s[14+2*k:]))
			start := int(binary.BigEndian.Uint16(s[16+2*n+2*k:]))
			delta := binary.BigEndian.Uint16(s[16+4*n+2*k:])
			rp := 16 + 6*n + 2*k
			ro := int(binary.BigEndian.Uint16(s[rp:]))
			if start > end || start <= previous {
				return fmt.Errorf("overlapping cmap ranges")
			}
			previous = end
			for code := start; code <= end; code++ {
				var id uint16
				if ro == 0 {
					id = uint16(code) + delta
				} else {
					index := rp + ro + 2*(code-start)
					if index+2 > len(s) {
						return fmt.Errorf("incomplete cmap glyph")
					}
					id = binary.BigEndian.Uint16(s[index:])
					if id != 0 {
						id += delta
					}
				}
				if id != want[code] {
					return fmt.Errorf("unexpected TTF codepoint U+%04X", code)
				}
			}
		}
		for code := range want {
			found := false
			for k := 0; k < n; k++ {
				if code >= int(binary.BigEndian.Uint16(s[16+2*n+2*k:])) && code <= int(binary.BigEndian.Uint16(s[14+2*k:])) {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("missing TTF codepoint")
			}
		}
	}
	return nil
}

func checkGlyphs(font []byte, vectors map[string][]byte) ([]string, error) {
	var doc struct {
		XMLName xml.Name `xml:"svg"`
		Font    struct {
			ID   string `xml:"id,attr"`
			Face struct {
				Units   string `xml:"units-per-em,attr"`
				Ascent  string `xml:"ascent,attr"`
				Descent string `xml:"descent,attr"`
			} `xml:"font-face"`
			Glyphs []struct {
				Unicode string `xml:"unicode,attr"`
				Name    string `xml:"glyph-name,attr"`
				D       string `xml:"d,attr"`
			} `xml:"glyph"`
		} `xml:"defs>font"`
	}
	if err := xml.Unmarshal(font, &doc); err != nil {
		return nil, err
	}
	if doc.Font.ID != "icomoon" || doc.Font.Face.Units != "1024" || doc.Font.Face.Ascent != "960" || doc.Font.Face.Descent != "-64" || len(doc.Font.Glyphs) != 11 {
		return nil, fmt.Errorf("unsupported SVG font")
	}
	glyphs := map[string]string{}
	for _, g := range doc.Font.Glyphs {
		if _, ok := glyphs[g.Unicode]; ok {
			return nil, fmt.Errorf("duplicate glyph")
		}
		if g.Unicode == " " {
			if g.D != "" || g.Name != "" {
				return nil, fmt.Errorf("unexpected space glyph")
			}
		} else {
			r := []rune(g.Unicode)
			if len(r) != 1 || r[0] < 0xe900 || r[0] > 0xe909 || g.Name != glyphInputs[int(r[0]-0xe900)].name {
				return nil, fmt.Errorf("unexpected glyph mapping")
			}
		}
		glyphs[g.Unicode] = g.D
	}
	result := []string{}
	for i, g := range glyphInputs {
		var vector struct {
			XMLName xml.Name `xml:"svg"`
			ViewBox string   `xml:"viewBox,attr"`
			Paths   []struct {
				D string `xml:"d,attr"`
			} `xml:"path"`
		}
		if err := xml.Unmarshal(vectors[g.name+".svg"], &vector); err != nil {
			return nil, err
		}
		if vector.ViewBox != "0 0 32 32" || len(vector.Paths) != 1 {
			return nil, fmt.Errorf("unsupported source vector")
		}
		a, err := normalizePath(vector.Paths[0].D, true)
		if err != nil {
			return nil, err
		}
		b, err := normalizePath(glyphs[string(rune(0xe900+i))], false)
		if err != nil {
			return nil, err
		}
		if a != b {
			return nil, fmt.Errorf("SVG geometry differs: %s", g.name)
		}
		result = append(result, digest([]byte(a)))
	}
	return result, nil
}

var pathTokens = regexp.MustCompile(`[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?|[A-Za-z]`)

// normalizePath uses only the selected closed command set and fixed eighth-unit grid.
func normalizePath(data string, transform bool) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("unsupported SVG path") }
	if len(data) > 1<<16 {
		return bad()
	}
	indices := pathTokens.FindAllStringIndex(data, -1)
	tokens := []string{}
	last := 0
	for _, index := range indices {
		if strings.Trim(data[last:index[0]], " ,\t\r\n") != "" {
			return bad()
		}
		tokens = append(tokens, data[index[0]:index[1]])
		last = index[1]
	}
	if strings.Trim(data[last:], " ,\t\r\n") != "" || len(tokens) == 0 {
		return bad()
	}
	var out strings.Builder
	var x, y, sx, sy, cx, cy float64
	curve := false
	cmd := byte(0)
	started := false
	closed := false
	emit := func(c byte, values ...float64) {
		out.WriteByte(c)
		for i, v := range values {
			if transform {
				if i%2 == 0 {
					v *= 32
				} else {
					v = 960 - 32*v
				}
			}
			fmt.Fprintf(&out, "%d,", int64(math.Round(v*8)))
		}
		out.WriteByte(';')
	}
	for i := 0; i < len(tokens); {
		if len(tokens[i]) == 1 && ((tokens[i][0] >= 'A' && tokens[i][0] <= 'Z') || (tokens[i][0] >= 'a' && tokens[i][0] <= 'z')) {
			cmd = tokens[i][0]
			i++
		}
		upper := cmd
		if upper >= 'a' && upper <= 'z' {
			upper -= 32
		}
		relative := cmd >= 'a' && cmd <= 'z'
		if !started && upper != 'M' {
			return bad()
		}
		if upper == 'Z' {
			if !started || closed {
				return bad()
			}
			emit('Z')
			x = sx
			y = sy
			curve = false
			closed = true
			cmd = 0
			continue
		}
		count := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4}[upper]
		if count == 0 || i+count > len(tokens) {
			return bad()
		}
		v := make([]float64, count)
		for k := range v {
			n, err := strconv.ParseFloat(tokens[i+k], 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e6 {
				return bad()
			}
			v[k] = n
		}
		i += count
		if relative {
			for k := range v {
				if upper == 'H' {
					v[k] += x
				} else if upper == 'V' {
					v[k] += y
				} else if k%2 == 0 {
					v[k] += x
				} else {
					v[k] += y
				}
			}
		}
		switch upper {
		case 'M':
			x, y = v[0], v[1]
			sx, sy = x, y
			emit('M', x, y)
			started = true
			curve = false
			cmd = 'L'
			if relative {
				cmd = 'l'
			}
		case 'L':
			x, y = v[0], v[1]
			emit('L', x, y)
			curve = false
		case 'H':
			x = v[0]
			emit('L', x, y)
			curve = false
		case 'V':
			y = v[0]
			emit('L', x, y)
			curve = false
		case 'C':
			emit('C', v...)
			cx, cy = v[2], v[3]
			x, y = v[4], v[5]
			curve = true
		case 'S':
			a, b := x, y
			if curve {
				a, b = 2*x-cx, 2*y-cy
			}
			emit('C', a, b, v[0], v[1], v[2], v[3])
			cx, cy = v[0], v[1]
			x, y = v[2], v[3]
			curve = true
		}
		closed = false
	}
	if !closed {
		return bad()
	}
	return out.String(), nil
}
