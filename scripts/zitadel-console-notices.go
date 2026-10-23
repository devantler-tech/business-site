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

type noticeState struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Raw               []byte `json:"raw"`
	PreparationSHA256 string `json:"preparationSha256"`
	LockSHA256        string `json:"lockSha256"`
	InputsSHA256      string `json:"inputsSha256"`
	PackagesSHA256    string `json:"packagesSha256"`
	RecipeSHA256      string `json:"recipeSha256"`
}

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
}

func main() { os.Exit(noticeRun(os.Args[1:], os.Stdout, os.Stderr)) }

// noticeRun is inert unless one complete explicit mode is selected.
func noticeRun(args []string, out, diagnostic io.Writer) int {
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
		case "--build", "--recipe":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing argument"))
			}
			i++
			values[key] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument"))
		}
	}
	if len(values) != 3 || (values["--snapshot"] == "") == (values["--inventory"] == "") || values["--build"] == "" || values["--recipe"] == "" {
		return fail(fmt.Errorf("expected one mode, build and recipe"))
	}
	var result any
	if values["--snapshot"] != "" {
		state, err := noticeSnapshot(values["--build"], values["--recipe"], noticeRead)
		if err != nil {
			return fail(err)
		}
		result = state
	} else {
		inventory, raw, err := noticeCapture(values["--build"], values["--recipe"], noticeRead)
		if err != nil {
			return fail(err)
		}
		root, err := os.OpenRoot(values["--build"])
		if err != nil {
			return fail(fmt.Errorf("output root unavailable"))
		}
		defer root.Close()
		f, err := root.OpenFile(noticeRaw, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fail(fmt.Errorf("raw diagnostic already exists or is unavailable"))
		}
		n, writeErr := f.Write(raw)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil || n != len(raw) {
			_ = root.Remove(noticeRaw)
			return fail(fmt.Errorf("incomplete raw diagnostic write"))
		}
		result = inventory
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
	return 0
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
func noticeSnapshot(build, recipe string, read noticeReader) (noticeState, error) {
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
	return noticeState{SchemaVersion: 1, Raw: console, PreparationSHA256: noticeDigest(prep), LockSHA256: noticeDigest(lock), InputsSHA256: noticeDigest(inputs), PackagesSHA256: noticeDigest(packages), RecipeSHA256: noticeDigest(recipeBytes)}, nil
}

// noticeCapture joins the unchanged checked snapshot to the final manifest and binary receipt.
func noticeCapture(build, recipe string, read noticeReader) (noticeInventory, []byte, error) {
	var result noticeInventory
	current, err := noticeSnapshot(build, recipe, read)
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
	return result, append([]byte(nil), before.Raw...), nil
}
