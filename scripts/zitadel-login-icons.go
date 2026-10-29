// Command zitadel-login-icons validates the bounded, frozen Login V1 icon family.
package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"crypto/rand"
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
	"syscall"
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

// Companion modes are available only in explicit source-file compositions.
// Existing icon/text-font commands do not acquire an implicit new dependency.
var iconExtensions = map[string]func([]string, io.Writer, io.Writer) int{}

// run emits nothing in the inactive state, and emits JSON only after a complete check.
func run(args []string, out, diagnostic io.Writer) int {
	if len(args) == 0 {
		return 0
	}
	if args[0] == "--text-fonts" {
		return runTextFonts(args[1:], out, diagnostic)
	}
	if extension, ok := iconExtensions[args[0]]; ok {
		return extension(args[1:], out, diagnostic)
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

// digest identifies the exact retained bytes in the output inventory.
func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// blob reproduces Git's frozen object identity; SHA-256 separately binds retained bytes.
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
	f, err := confined.OpenFile(relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
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

// exactDirectory refuses absent, linked, nonregular or additional selected members.
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

// inspect uses confined disk reads for the selected build and retained notice roots.
func inspect(build, notices string) (inventory, error) {
	return inspectWithRead(build, notices, readAt)
}

// inspectWithRead joins the complete snapshots supplied by the confined reader.
func inspectWithRead(build, notices string, readInput func(string, string, int64) ([]byte, error)) (inventory, error) {
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
		b, e := readInput(root, p, cap)
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
		b, e := readInput(build, p, 1<<20)
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
	noticeBytes := map[string][]byte{}
	for p, want := range map[string]string{"LICENSE.md": "55a4cc3d450a9dfa4d12c7de238442562899868299b800dbac6c70ca6d99c490", "NOTICE.md": "bbbf1549408fef9c90199ac0859a49a110683cf3b89394e0402d030a7cfe5155", "clipboard.svg": "9a526c626cdb157f44c29831d5c99eba3d719a92758a257891164538090fd739"} {
		b, e := read(notices, p, 1<<20)
		if e != nil {
			return result, e
		}
		if digest(b) != want {
			return result, fmt.Errorf("notice/source identity differs: %s", p)
		}
		noticeBytes[p] = b
	}
	// Keep the derived source path below the selected build root. Promoting it
	// to a new root would allow a linked prepared/source to escape confinement.
	sourcePrefix := "prepared/source/"
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
	statik, err := read(build, sourcePrefix+statikPath, 32<<20)
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
	for name, body := range members {
		if err = neutralProviderOutput(name, body); err != nil {
			return result, err
		}
	}
	fontNames := []string{"lgn-icons.eot", "lgn-icons.svg", "lgn-icons.ttf", "lgn-icons.woff"}
	if err = exactDirectory(build, sourcePrefix+iconRoot+"fonts", fontNames); err != nil {
		return result, err
	}
	fonts := map[string][]byte{}
	for _, name := range fontNames {
		p := iconRoot + "fonts/" + name
		b, e := read(build, sourcePrefix+p, 1<<20)
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
	if err = exactDirectory(build, sourcePrefix+iconRoot+"svg", vectorNames); err != nil {
		return result, err
	}
	for _, name := range vectorNames {
		b, e := read(build, sourcePrefix+iconRoot+"svg/"+name, 1<<20)
		if e != nil {
			return result, e
		}
		vectors[name] = b
	}
	if blob(vectors["clipboard-solid.svg"]) != "7edc3eda1ebbc7cc7f60763f06beaa4b25579602" {
		return result, fmt.Errorf("original solid clipboard input differs")
	}
	vectors["clipboard.svg"] = noticeBytes["clipboard.svg"]
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
	f, err := parser.ParseFile(token.NewFileSet(), "statik.go", data, parser.ParseComments)
	if err != nil || f.Name.Name != "statik" || len(f.Decls) != 3 {
		return bad()
	}
	for _, group := range f.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:") || strings.HasPrefix(comment.Text, "// +build") {
				return bad()
			}
		}
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

// checkCmap requires all three frozen encodings to map the selected codepoints to their glyph slots.
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

// checkGlyphs joins each frozen SVG-font codepoint to its actual normalized source geometry.
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

// The neutral provider profile changes decoration only in one immutable upstream
// source family. Archive data cannot select paths, replacements or commands.
type neutralProviderInput struct {
	path, sha256 string
	mode         os.FileMode
	kind         string
	expected     string
}

var neutralProviderInputs = []neutralProviderInput{
	{path: "console/src/app/modules/idp-table/idp-table.component.html", sha256: "f4bd9cab64343c81bd37942e50caa7d0895804d1e650f0dbdb381b5e802805bf", mode: 0644, kind: "console", expected: "2217a14c4f92e2341adcaf43e75e3c6f5a5fec8f14b40a10e5f54b4731f1d34e"},
	{path: "console/src/app/modules/policies/idp-settings/idp-settings.component.html", sha256: "4f54262e2e976803dee6bb96839867a703da8b14796bdc3ef43c0d59004b416b", mode: 0644, kind: "console", expected: "41b64b2ed1477508569367a19d11ece120cd95c0ccee7af3f4a27805d2896ad8"},
	{path: "console/src/app/modules/providers/provider-apple/provider-apple.component.html", sha256: "f4a37534bc2a7f440087d94962ae7ee9e9e218cc88a7a237823ac89b057b8da7", mode: 0644, kind: "console", expected: "bcd033476803838515de5f25057ac4df9749f26049bf4a0c2926acb83ea01b94"},
	{path: "console/src/app/modules/providers/provider-azure-ad/provider-azure-ad.component.html", sha256: "06ae466d2c46902757041c68371887643f511b8887e4cd3c87d17115d711174d", mode: 0644, kind: "console", expected: "bb307b70f936d780f8584f01c4691efd042d43d95041df294f2af1f48ae94459"},
	{path: "console/src/app/modules/providers/provider-github-es/provider-github-es.component.html", sha256: "35b3ad53ae12758b0d7911c9e287cceedd1882b4e032f6622c0d20ec95e142e2", mode: 0644, kind: "console", expected: "380b3dca85ef437d4446987a5e22020c1a7e1bec1bc874ff7942dd0e204a1efd"},
	{path: "console/src/app/modules/providers/provider-github/provider-github.component.html", sha256: "278c76029f8d65ebdb251c32b7e93447bf8b10c61f4c33294ff60d1dd166876d", mode: 0644, kind: "console", expected: "2eca45dc2386dc08bdd6af010b3782f3e3f9531260db70f22117aa115e976205"},
	{path: "console/src/app/modules/providers/provider-gitlab-self-hosted/provider-gitlab-self-hosted.component.html", sha256: "8d77282cde7e54bb17ae6418af74395dda950b26c346c67e38a5a21385e547ec", mode: 0644, kind: "console", expected: "99871b68ae002cd48b82de6ef6b7beeaa3f3f5f2dc9b0df66a398149e0929e9f"},
	{path: "console/src/app/modules/providers/provider-gitlab/provider-gitlab.component.html", sha256: "084cbfa2681d78fa14c5123a073826b52ed31c62b4031669a956c5c97122e85f", mode: 0644, kind: "console", expected: "341c4a94d2817aed886fe8268c31f890188fb8468e25d09e6e987a511305449b"},
	{path: "console/src/app/modules/providers/provider-google/provider-google.component.html", sha256: "ca58e0f11408808cf8906c84df4b39e05c38c52d24102298d65a11c07524b7ac", mode: 0644, kind: "console", expected: "8fae34c70584f876ab9dad8608830b5f95d430310d1b33bca04e847ea492c7e8"},
	{path: "console/src/app/modules/providers/provider-oauth/provider-oauth.component.html", sha256: "7d23482c90f1e58196b78c26e9b2410de9b4c7834882354403150af477f70a93", mode: 0644, kind: "console", expected: "eae8329b095b5987d8340a386957fb67d5ec58488e97f221c03cbf8592d71266"},
	{path: "console/src/app/modules/providers/provider-saml-sp/provider-saml-sp.component.html", sha256: "63d880c2bd57c367580f43533ea79ef73eb5764530c84189308ff679479131ad", mode: 0644, kind: "console", expected: "0b5410b7aa9b905e0f4c074e85eda06905d51cef28cf3f3ca49c6b2146a3acff"},
	{path: "internal/api/ui/login/static/templates/login.html", sha256: "4db65e16a4a4dab0af1c19e9dfd4a9df6497327e4c158d461035b4d4cca24913", mode: 0644, kind: "login", expected: "d749528ee622c0d376afc918d4d205bf9d42b6c33243301e721a997be631d4bf"},
	{path: "internal/api/ui/login/static/templates/register_option.html", sha256: "fb9793de2be4fa47191a2907e5de9deec1d7093eae27334d9d10eaf3ffc156d7", mode: 0644, kind: "login", expected: "160874790c22abfd44bc977b2d978a8fbae73dc0e4adfa52917b4b3854ec123b"},
	{path: "console/src/app/modules/idp-table/idp-table.component.scss", sha256: "adf0930761ba3e329eefe3b685cd348935741cc99f43fc0d09ff1cbe6d328e2c", mode: 0644, kind: "table-style", expected: "3109087ad9a64409e4e30d6eab7ea7b8f48b3c7194042d0171d1ee9f9926efb8"},
	{path: "console/src/app/modules/policies/idp-settings/idp-settings.component.scss", sha256: "60f4249cdc02789825fa72bea4c26309e2b827ea64b618c82220113b8b376c12", mode: 0644, kind: "chooser-style", expected: "8e5bf3f536e63f9e3fab90a2be0c46e7239190e2cc7ea558a0fcd5f90e0b6217"},
	{path: "console/src/app/modules/providers/providers.scss", sha256: "a85293c7b43639478d6646e6ef908e29ce00a9daddb5e06855a3728386111b17", mode: 0644, kind: "editor-style", expected: "fc44668ff48fa9442f5b592abeac0402da17bd177b5809b8c5e1ea8675b58413"},
	{path: "internal/api/ui/login/static/resources/themes/scss/styles/identity_provider/identity_provider_base.scss", sha256: "e5ad481b25c8722d3a1b5afc2592a057c773dfcbab63fd35c5764aedebf0916d", mode: 0644, kind: "login-style", expected: "a25666a0c03c21542c84ab72d228c5ce91c6e7b00a712289d44a4f720f613732"},
	{path: "internal/api/ui/login/static/resources/themes/scss/styles/vars.scss", sha256: "61f59f9b4d8dda32cde72d269917f761c3eac54909265ee930b791d9a589a1dd", mode: 0644, kind: "variables", expected: "a15f2692a597e8a8c3848d62073db1d48dce82aea66d6cabfda1d2019825a25c"},
	{path: "console/src/assets/images/idp/apple-dark.svg", sha256: "d375e51b38ccc427162e5a06c3263df659095b3924f3a5007c4cf7eb0c05974c", mode: 0755, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/apple.svg", sha256: "8b7b000b5be857e72b18e61874b9e9380035408f5550dc5b0899d965f7fac6a2", mode: 0755, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/github-dark.svg", sha256: "2d766668e8fbcfb5f98d6f86acc0767ed95ceb1431e37187389dd62d3bb73c24", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/github.svg", sha256: "12794390cce7d0682ffc783c785e4282305684431b30b29ed75c224da24035b4", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/gitlab.svg", sha256: "ffb42267e29f711ed2bdf66e7827303272ecf83d1a81ffb3d4e68110716cb133", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/google.png", sha256: "473e0563b5c8fc7adc05c37e60a8d73ccb975a383a0cbe7ddb76935b2dda3f53", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/ms.svg", sha256: "929f48f88c8ca7f3f5d294be47ec4caf51acc28ac25340c19a903125d7ecd84a", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/oauth.svg", sha256: "2a5c60a1e0d2ce7957f92ce741f6e0c95f61a9bb9042e88f6dcdce0df3fc6400", mode: 0644, kind: "retire", expected: ""},
	{path: "console/src/assets/images/idp/saml-icon.svg", sha256: "8d6baf4e08d21cc1b53b01594dc979a38d33b9f8c44311cc7bdc4286bb4d4b75", mode: 0644, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/apple-dark.svg", sha256: "d375e51b38ccc427162e5a06c3263df659095b3924f3a5007c4cf7eb0c05974c", mode: 0755, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/apple.svg", sha256: "8b7b000b5be857e72b18e61874b9e9380035408f5550dc5b0899d965f7fac6a2", mode: 0755, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/github-white.png", sha256: "60d03f0f1a09aafcc2df2e10b4ffd24eab44787b451f8fdb367c3baf589cf95a", mode: 0644, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/github.png", sha256: "367d5cb2adc609451cf9127250edfe893837485e7c779d7dcb4e31234f59b001", mode: 0644, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/gitlab.png", sha256: "d56b28d4ad3c65fb4e1a2fe79eee4b821f6e9ab3f01963fa5f1b71ec7c5b10cf", mode: 0644, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/google.png", sha256: "473e0563b5c8fc7adc05c37e60a8d73ccb975a383a0cbe7ddb76935b2dda3f53", mode: 0644, kind: "retire", expected: ""},
	{path: "internal/api/ui/login/static/resources/images/idp/ms.svg", sha256: "929f48f88c8ca7f3f5d294be47ec4caf51acc28ac25340c19a903125d7ecd84a", mode: 0644, kind: "retire", expected: ""},
}

var neutralSMTPInputs = []neutralProviderInput{
	{path: "console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.html", sha256: "01a6530916d5417a53b17b705d021647e2c59af0b8eff9e4b4895bbe44c5be24", mode: 0644, kind: "smtp-chooser", expected: "7adb2e384cb0af6db510f3c1b07a9b91729ea922a004e4b6fed6fbd98352b1fc"},
	{path: "console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.scss", sha256: "5f44a3766ed3b14f69939811a6718270feb0a262a950f446b32b6ef179a51a18", mode: 0644, kind: "smtp-chooser-style", expected: "db4f7c1746b2f3f19a1de401ac5cf658439e59d3f045106ff4aa05dc4eb8a8eb"},
	{path: "console/src/app/modules/smtp-provider/known-smtp-providers-settings.ts", sha256: "3ba4f1c0d1060d5ea1f0dc2f936fe05928e5c4cdcf34264a68fd8cf858230167", mode: 0644, kind: "smtp-settings", expected: "f340db31d1e1d4db10e1646eb218b2e46fe7e6568e9801b26494f94602c168cd"},
	{path: "console/src/app/modules/smtp-provider/smtp-provider.component.html", sha256: "fbfd6946b3bf85f1a1f363e6477300d14b564b060c34fd19ae5d136d97324ec6", mode: 0644, kind: "smtp-editor", expected: "3c6be2a65c023c2aa1fa0ab86dc54530e0db7d3d79880099be3772f062db579f"},
	{path: "console/src/app/modules/smtp-provider/smtp-provider.scss", sha256: "c63a74b193724641f6d2c329759c3877fe41cdf3c1415766635ffe08f8b386ab", mode: 0644, kind: "smtp-editor-style", expected: "194fd266ef3301dcb35b6ead7d92d5ac9860685d52e1312f693dfa644300eb24"},
	{path: "console/src/assets/images/smtp/aws-ses.svg", sha256: "91c0f2067ee1c2d3ba7b6e5f171968599074908e214823d654d7a6f5e17d9923", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/brevo.svg", sha256: "c4cb54430034449ef93447bada3bd1c05c5935a9af1c1d239700c37c55436252", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/google.png", sha256: "473e0563b5c8fc7adc05c37e60a8d73ccb975a383a0cbe7ddb76935b2dda3f53", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/mailchimp.svg", sha256: "df456abcbb2543864aa36ea3db7555502d5f091f77f16f974ea41e6de12894fd", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/mailgun.svg", sha256: "35647003fcc8bd00fbddcfa415e37a56633e0778f89ba6850c64b01b7f9706c9", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/mailjet.svg", sha256: "09d2104401c81e490b33cee16cc961993e4c21c755b5dc4043061c8a368dd893", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/outlook.svg", sha256: "11951b969a6169db6c92f03e93039ce66c1576886c462c03e0549dc872db7283", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/postmark.png", sha256: "81ac09ed4332ff08fa1eb4ce3f225176e8083835529cd4a7fe899cbde71d31fc", mode: 0644, kind: "retire"},
	{path: "console/src/assets/images/smtp/sendgrid.png", sha256: "8e5559c9e74a4b5ea732f9dcbaa0cc24dae759317edd1aed02e9a572141cee7a", mode: 0644, kind: "retire"},
}

func init() {
	iconExtensions["--neutral-provider-icons"] = runNeutralProviderIcons
	iconExtensions["--neutral-smtp-icons"] = runNeutralSMTPIcons
	iconExtensions["--neutral-mdi-icons"] = runNeutralMDIIcons
}

// runNeutralProviderIcons validates the complete frozen decoration profile before
// replacing its eighteen consumers and retiring sixteen images in a confined export.
func runNeutralProviderIcons(args []string, out, diagnostic io.Writer) int {
	return prepareNeutralIconProfile(args, out, diagnostic, "provider")
}

// runNeutralSMTPIcons preserves the five SMTP consumers while retiring only its
// closed nine-image family. Existing provider and Login profiles stay separate.
func runNeutralSMTPIcons(args []string, out, diagnostic io.Writer) int {
	return prepareNeutralIconProfile(args, out, diagnostic, "smtp")
}

// runNeutralMDIIcons retains the standard icon family while replacing only the
// unresolved OpenID/JWT decorations in their five fixed native consumers.
func runNeutralMDIIcons(args []string, out, diagnostic io.Writer) int {
	return prepareNeutralIconProfile(args, out, diagnostic, "mdi")
}

// All three profiles use the same confined, prevalidated stage writer. Their original
// membership, transformations and receipt cardinalities are fixed independently.
func prepareNeutralIconProfile(args []string, out, diagnostic io.Writer, profile string) int {
	fail := func(err error) int { fmt.Fprintln(diagnostic, "provider-icons:", err); return 1 }
	if profile != "provider" && profile != "smtp" && profile != "mdi" {
		return fail(fmt.Errorf("unknown fixed icon profile"))
	}
	if len(args) != 3 || args[0] != "--prepare" || args[1] != "--source" || args[2] == "" {
		return fail(fmt.Errorf("explicit preparation and source required"))
	}
	source, err := filepath.Abs(args[2])
	if err != nil {
		return fail(err)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || resolved != source {
		return fail(fmt.Errorf("canonical source required"))
	}
	before, err := os.Lstat(source)
	if err != nil || !before.IsDir() {
		return fail(fmt.Errorf("source directory required"))
	}
	confined, err := os.OpenRoot(source)
	if err != nil {
		return fail(err)
	}
	defer confined.Close()
	if _, err = confined.Lstat(".git"); !os.IsNotExist(err) {
		return fail(fmt.Errorf("exported source required"))
	}
	opened, err := confined.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return fail(fmt.Errorf("source directory changed"))
	}
	inputs := neutralProviderInputs
	families := map[string][]string{
		"console/src/assets/images/idp":                     {"apple-dark.svg", "apple.svg", "github-dark.svg", "github.svg", "gitlab.svg", "google.png", "ms.svg", "oauth.svg", "saml-icon.svg"},
		"internal/api/ui/login/static/resources/images/idp": {"apple-dark.svg", "apple.svg", "github-white.png", "github.png", "gitlab.png", "google.png", "ms.svg"},
	}
	if profile == "smtp" {
		inputs = neutralSMTPInputs
		families = map[string][]string{"console/src/assets/images/smtp": {"aws-ses.svg", "brevo.svg", "google.png", "mailchimp.svg", "mailgun.svg", "mailjet.svg", "outlook.svg", "postmark.png", "sendgrid.png"}}
	}
	if profile == "mdi" {
		inputs = neutralMDIInputs()
		names := []string{"openid.svg", "jwt.svg"}
		for _, asset := range mdiFrozen {
			names = append(names, path.Base(asset.Path))
		}
		families = map[string][]string{"console/src/assets/mdi": names}
	}
	for dir, names := range families {
		if err = exactDirectory(source, dir, names); err != nil {
			return fail(err)
		}
	}
	originals, replacements := map[string][]byte{}, map[string][]byte{}
	consoleSlots, loginSlots, settingsValues := 0, 0, 0
	// Validate the complete original set before changing the private stage.
	for _, input := range inputs {
		body, e := neutralProviderRead(confined, input)
		if e != nil || digest(body) != input.sha256 {
			return fail(fmt.Errorf("original identity differs: %s", input.path))
		}
		originals[input.path] = body
		if input.kind == "retire" || input.kind == "retain" {
			continue
		}
		text := string(body)
		switch input.kind {
		case "mdi-registry", "mdi-table", "mdi-chooser", "mdi-editor-oidc", "mdi-editor-jwt":
			var slots int
			text, slots, e = neutralMDIChange(input, text)
			if e != nil {
				return fail(e)
			}
			consoleSlots += slots
		case "smtp-chooser", "smtp-editor":
			old := `<img class="smtp-logo" [src]="provider.image" [alt]="provider.description" />`
			if input.kind == "smtp-editor" {
				old = `<img class="smtp-logo" [src]="state.defaults.image" [alt]="state.defaults.description" />`
			}
			if strings.Count(text, old) != 1 {
				return fail(fmt.Errorf("SMTP decoration slot differs"))
			}
			text = strings.Replace(text, old, `<i class="smtp-logo neutral-smtp-icon las la-envelope" aria-hidden="true"></i>`, 1)
			consoleSlots++
		case "smtp-settings":
			var lines []string
			declarations := 0
			for _, line := range strings.Split(text, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "image: string;" {
					declarations++
					continue
				}
				if strings.HasPrefix(trimmed, "image: './assets/images/smtp/") {
					settingsValues++
					continue
				}
				lines = append(lines, line)
			}
			if declarations != 1 || settingsValues != 9 {
				return fail(fmt.Errorf("SMTP decoration settings differ"))
			}
			text = strings.Join(lines, "\n")
		case "smtp-chooser-style", "smtp-editor-style":
			selector, size := ".new-smtp-wrapper", "36px"
			if input.kind == "smtp-editor-style" {
				selector, size = ".wizard-header", "48px"
			}
			text += "\n" + selector + " .neutral-smtp-icon {\n  display: inline-flex;\n  align-items: center;\n  justify-content: center;\n  font-size: " + size + ";\n  flex-shrink: 0;\n}\n"
		case "console":
			var lines []string
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "src=\"./assets/images/idp/") {
					if strings.Contains(line, " light\"") {
						continue
					}
					indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
					line = indent + "<i class=\"idp-logo neutral-provider-icon las la-arrow-right\" aria-hidden=\"true\"></i>"
					consoleSlots++
				}
				lines = append(lines, line)
			}
			text = strings.Join(lines, "\n")
		case "login":
			old := "<span class=\"logo\"></span>"
			if strings.Count(text, old) != 1 {
				return fail(fmt.Errorf("login decoration differs"))
			}
			text = strings.Replace(text, old, "<span class=\"logo lgn-icon-arrow-right-solid\" aria-hidden=\"true\"></span>", 1)
			loginSlots++
		case "table-style", "chooser-style", "editor-style":
			size := "36px"
			if input.kind == "table-style" {
				size = "28px"
			}
			text += "\n.neutral-provider-icon {\n  display: inline-flex;\n  align-items: center;\n  justify-content: center;\n  width: " + size + ";\n  height: " + size + ";\n  font-size: " + size + ";\n  margin-right: 1rem;\n  flex-shrink: 0;\n}\n"
		case "login-style":
			start := strings.Index(text, "\n  &.google {")
			if start < 0 || !strings.HasSuffix(text, "\n}\n") {
				return fail(fmt.Errorf("login provider styles differ"))
			}
			text = text[:start] + "\n}\n"
			old := "  span.logo {\n"
			if strings.Count(text, old) != 1 {
				return fail(fmt.Errorf("login slot differs"))
			}
			text = strings.Replace(text, old, old+"    display: inline-flex;\n    align-items: center;\n    justify-content: center;\n    font-size: 25px;\n", 1)
		case "variables":
			var lines []string
			removed := 0
			for _, line := range strings.Split(text, "\n") {
				selected := false
				for _, name := range []string{"google", "github", "gitlab", "azure", "apple"} {
					if strings.Contains(line, "--"+name+"-image-src:") {
						selected = true
					}
				}
				if selected {
					removed++
					continue
				}
				lines = append(lines, line)
			}
			if removed != 10 {
				return fail(fmt.Errorf("provider variables differ"))
			}
			text = strings.Join(lines, "\n")
		default:
			return fail(fmt.Errorf("unsupported fixed input"))
		}
		if text == string(body) {
			return fail(fmt.Errorf("unchanged selected decoration"))
		}
		if digest([]byte(text)) != input.expected {
			return fail(fmt.Errorf("neutral consumer identity differs"))
		}
		replacements[input.path] = []byte(text)
	}
	if profile == "smtp" && (len(originals) != 14 || len(replacements) != 5 || consoleSlots != 2 || loginSlots != 0 || settingsValues != 9) {
		return fail(fmt.Errorf("incomplete neutral SMTP profile"))
	}
	if profile == "provider" && (len(originals) != 34 || len(replacements) != 18 || consoleSlots != 27 || loginSlots != 2 || settingsValues != 0) {
		return fail(fmt.Errorf("incomplete neutral provider profile"))
	}
	if profile == "mdi" && (len(originals) != 32 || len(replacements) != 5 || consoleSlots != 6 || loginSlots != 0 || settingsValues != 0) {
		return fail(fmt.Errorf("incomplete neutral MDI profile"))
	}
	// Refuse a changed original before replacing any selected body.
	for _, input := range inputs {
		body, e := neutralProviderRead(confined, input)
		if e != nil || !bytes.Equal(body, originals[input.path]) {
			return fail(fmt.Errorf("original changed before preparation"))
		}
	}
	for _, input := range inputs {
		if input.kind == "retain" {
			continue
		}
		if input.kind == "retire" {
			if err = confined.Remove(input.path); err != nil {
				return fail(err)
			}
			continue
		}
		// Replace a generated stage member atomically; never truncate a hard-linked
		// original or follow a swapped destination link outside the admitted root.
		temporary := path.Join(path.Dir(input.path), ".provider-icons-"+rand.Text())
		file, e := confined.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return fail(e)
		}
		_, e = file.Write(replacements[input.path])
		if e == nil {
			e = file.Chmod(0644)
		}
		closeErr := file.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			e = confined.Rename(temporary, input.path)
		}
		if e != nil {
			_ = confined.Remove(temporary)
			return fail(e)
		}
	}
	// Read the completed stage back before emitting either profile's success.
	for _, input := range inputs {
		if input.kind == "retire" {
			if _, e := confined.Lstat(input.path); !os.IsNotExist(e) {
				return fail(fmt.Errorf("retired decoration remains"))
			}
			continue
		}
		body, e := neutralProviderRead(confined, input)
		want := replacements[input.path]
		if input.kind == "retain" {
			want = originals[input.path]
		}
		if e != nil || !bytes.Equal(body, want) {
			return fail(fmt.Errorf("prepared decoration readback differs"))
		}
	}
	after, err := os.Lstat(source)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
		return fail(fmt.Errorf("prepared source root changed"))
	}
	receipt := map[string]any{"schemaVersion": 1, "profile": "native-neutral-provider-icons-v1", "changedFiles": 18, "removedFiles": 16, "consoleSlots": consoleSlots, "loginSlots": loginSlots, "imageAdmission": "unknown", "licenseClosure": "unknown"}
	if profile == "smtp" {
		receipt["profile"], receipt["changedFiles"], receipt["removedFiles"], receipt["settingsValues"] = "native-neutral-smtp-icons-v1", 5, 9, settingsValues
	}
	if profile == "mdi" {
		receipt["profile"], receipt["changedFiles"], receipt["removedFiles"], receipt["retainedFiles"] = "native-neutral-mdi-icons-v1", 5, 2, 25
	}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fail(err)
	}
	if _, err = out.Write(append(body, '\n')); err != nil {
		return fail(err)
	}
	return 0
}

// Read every selected member through the original open root; late path swaps
// cannot redirect a subsequent read or write outside that source reservation.
func neutralProviderRead(root *os.Root, input neutralProviderInput) ([]byte, error) {
	parts := strings.Split(input.path, "/")
	for i := range parts {
		info, err := root.Lstat(path.Join(parts[:i+1]...))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unavailable or linked provider input")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("provider input parent differs")
		}
	}
	file, err := root.OpenFile(input.path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Mode() != input.mode || info.Size() > 1<<20 {
		_ = file.Close()
		return nil, fmt.Errorf("provider input mode or size differs")
	}
	body, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	after, statErr := file.Stat()
	closeErr := file.Close()
	if err != nil || statErr != nil || closeErr != nil || int64(len(body)) != info.Size() || after.Mode() != info.Mode() || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return nil, fmt.Errorf("provider input read incomplete")
	}
	return body, nil
}

// neutralProviderConsumers returns the transformed consumers and their fixed
// expected hashes, excluding retired images from the copied-asset policy.
func neutralProviderConsumers() []inputRecord {
	var consumers []inputRecord
	for _, input := range neutralProviderInputs {
		if input.kind != "retire" {
			expected := input.expected
			// These two consumers subsequently receive the separate MDI profile.
			for _, mdi := range neutralMDIConsumers() {
				if mdi.Path == input.path {
					expected = mdi.SHA256
				}
			}
			consumers = append(consumers, inputRecord{input.path, expected})
		}
	}
	return consumers
}

// neutralSMTPConsumers keeps the SMTP source binding separate from the 18 IdP consumers.
func neutralSMTPConsumers() []inputRecord {
	var consumers []inputRecord
	for _, input := range neutralSMTPInputs {
		if input.kind != "retire" {
			consumers = append(consumers, inputRecord{input.path, input.expected})
		}
	}
	return consumers
}

// Recognize the retired native provider families in output paths and text.
// Repeated slash decoding covers the source compiler's escaped-string forms;
// this is a closed-family check, not a general JavaScript or URL parser.
func neutralProviderOutput(name string, body []byte) error {
	lower := strings.ToLower(filepath.ToSlash(name))
	for _, mark := range []string{"openid.svg", "jwt.svg"} {
		if strings.Contains(lower, "mdi/"+mark) {
			return fmt.Errorf("retired MDI decoration remains")
		}
	}
	for _, family := range []string{"idp", "smtp"} {
		if strings.Contains(lower, "/images/"+family+"/") || strings.HasPrefix(lower, "images/"+family+"/") {
			return fmt.Errorf("retired %s provider image output remains", family)
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".js", ".css", ".json", ".map", ".scss", ".svg":
		text := strings.ToLower(string(body))
		slash := strings.NewReplacer("\\/", "/", "\\u002f", "/", "\\x2f", "/")
		for i := 0; i < 3; i++ {
			text = slash.Replace(text)
		}
		for _, mark := range []string{"openid.svg", "jwt.svg"} {
			if strings.Contains(text, "mdi/"+mark) {
				return fmt.Errorf("retired MDI decoration reference remains")
			}
		}
		for _, family := range []string{"idp", "smtp"} {
			if strings.Contains(text, "images/"+family+"/") {
				return fmt.Errorf("retired %s provider image URL remains", family)
			}
		}
	}
	return nil
}

// All three source image directories are retired. Missing parents are valid
// in a restored tree; a linked, unreadable or nonempty family is never absence.
func neutralProviderSourceFamilies(source string) error {
	for _, family := range []string{"console/src/assets/images/idp", "internal/api/ui/login/static/resources/images/idp", "console/src/assets/images/smtp"} {
		current := source
		absent := false
		for _, part := range strings.Split(family, "/") {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				absent = true
				break
			}
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("provider source family observation incomplete")
			}
		}
		if absent {
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) != 0 {
			return fmt.Errorf("retired provider source image remains")
		}
	}
	for _, mark := range []string{"openid.svg", "jwt.svg"} {
		if _, err := os.Lstat(filepath.Join(source, "console/src/assets/mdi", mark)); !os.IsNotExist(err) {
			return fmt.Errorf("retired MDI source remains or cannot be observed")
		}
	}
	return nil
}

// neutralMDIInputs closes all 27 native graphics and the five consumers after
// the provider profile. Retained artwork remains byte-identical to upstream.
func neutralMDIInputs() []neutralProviderInput {
	inputs := []neutralProviderInput{
		{path: "console/src/app/app.component.ts", sha256: "f266e00e6d404e94e58fad66972b3da6c74a4818992bbec91d59a8b151113c4e", mode: 0644, kind: "mdi-registry", expected: "235dc03feb62db25a22a0e3c2741e916ed40b659677febd691b362fed21bc2ab"},
		{path: "console/src/app/modules/idp-table/idp-table.component.html", sha256: "2217a14c4f92e2341adcaf43e75e3c6f5a5fec8f14b40a10e5f54b4731f1d34e", mode: 0644, kind: "mdi-table", expected: "5c489734a039df3535d00e7761200621bfca99e25d63955b70a1d1e712f76544"},
		{path: "console/src/app/modules/policies/idp-settings/idp-settings.component.html", sha256: "41b64b2ed1477508569367a19d11ece120cd95c0ccee7af3f4a27805d2896ad8", mode: 0644, kind: "mdi-chooser", expected: "015e783ee150d21280d7748ab897c1b709fb5e208777370059716f7b10766188"},
		{path: "console/src/app/modules/providers/provider-oidc/provider-oidc.component.html", sha256: "fac7817b5db6d79d22d4a278ba4bd0e4151d30f137c448264c53899ac85b7a81", mode: 0644, kind: "mdi-editor-oidc", expected: "3c97248b11a743f010ed71a85e18d019c0b71fb528674e664f4090c00e79ca80"},
		{path: "console/src/app/modules/providers/provider-jwt/provider-jwt.component.html", sha256: "19471a418b11da8bf48740dc16162ce93a7d12c69705a3af1012a4f4fe659bf8", mode: 0644, kind: "mdi-editor-jwt", expected: "5d95c62b1cbc5a3195d01593233e485e71d1a6f15a856bc9f8515593ccab85c9"},
		{path: "console/src/assets/mdi/openid.svg", sha256: "05780f911a252dff978d08568675cbe24a53093fcbb04d5be9a9e1fa3e1c4f63", mode: 0644, kind: "retire"},
		{path: "console/src/assets/mdi/jwt.svg", sha256: "24c9cd3a49a7459d14da7146832b4e986bfbd484bfc3cde7f485da8f6ccc5c03", mode: 0644, kind: "retire"},
	}
	for _, asset := range mdiFrozen {
		inputs = append(inputs, neutralProviderInput{path: asset.Path, sha256: asset.SHA256, mode: 0644, kind: "retain", expected: asset.SHA256})
	}
	return inputs
}

// neutralMDIConsumers binds the completed decoration changes, excluding artwork.
func neutralMDIConsumers() []inputRecord {
	var result []inputRecord
	for _, input := range neutralMDIInputs() {
		if input.kind != "retain" && input.kind != "retire" {
			result = append(result, inputRecord{input.path, input.expected})
		}
	}
	return result
}

// neutralMDIChange replaces only fixed decorative slots and obsolete SVG
// registrations. Native provider identifiers, forms, links and LDAP stay intact.
func neutralMDIChange(input neutralProviderInput, text string) (string, int, error) {
	if input.kind == "mdi-registry" {
		var lines []string
		removed := 0
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "this.matIconRegistry.addSvgIcon('mdi_openid',") || strings.Contains(line, "this.matIconRegistry.addSvgIcon('mdi_jwt',") {
				removed++
				continue
			}
			lines = append(lines, line)
		}
		if removed != 2 {
			return "", 0, fmt.Errorf("MDI registrations differ")
		}
		return strings.Join(lines, "\n"), 0, nil
	}
	size, class := "48", "idp-logo"
	if input.kind == "mdi-table" {
		size, class = "28", "idp-icon"
	}
	if input.kind == "mdi-chooser" {
		size, class = "36", "icon"
	}
	slots := 0
	for _, mark := range []string{"openid", "jwt"} {
		alt := mark
		if input.kind == "mdi-table" && mark == "openid" {
			alt = "oidc"
		}
		old := `<mat-icon class="` + class + `" svgIcon="mdi_` + mark + `" alt="` + alt + `" />`
		count := strings.Count(text, old)
		if count == 0 {
			continue
		}
		if count != 1 {
			return "", 0, fmt.Errorf("MDI decoration slot differs")
		}
		glyph := "la-arrow-right"
		if mark == "jwt" {
			glyph = "la-key"
		}
		replacement := `<i class="` + class + ` neutral-mdi-icon las ` + glyph + `" aria-hidden="true" style="display: inline-flex; align-items: center; justify-content: center; width: ` + size + `px; height: ` + size + `px; font-size: ` + size + `px;"></i>`
		text = strings.Replace(text, old, replacement, 1)
		slots++
	}
	if input.kind == "mdi-chooser" {
		if strings.Count(text, ` svgIcon="mdi_jwt"`) != 1 {
			return "", 0, fmt.Errorf("native LDAP decoration differs")
		}
		text = strings.Replace(text, ` svgIcon="mdi_jwt"`, "", 1)
	}
	if strings.Contains(text, "mdi_openid") || strings.Contains(text, "mdi_jwt") {
		return "", 0, fmt.Errorf("retired MDI registration remains")
	}
	return text, slots, nil
}
