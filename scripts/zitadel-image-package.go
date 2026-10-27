// Command zitadel-image-package builds a closed local identity image context.
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
	"runtime"
	"sort"
	"strings"
	"syscall"
)

const imagePin = "8afa5dd37a8e723efb9eace8d09fd5c1cccef694"
const imageBase = "gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3"

type imageBinding struct{ Path, SHA256 string }
type imageText struct {
	RawDiagnostic string `json:"rawDiagnostic"`
	Bytes         int64  `json:"bytes"`
	SHA256        string `json:"sha256"`
}
type imageEvidence struct {
	SchemaVersion         int            `json:"schemaVersion"`
	UpstreamCommit        string         `json:"upstreamCommit"`
	Profile               string         `json:"profile"`
	ImageAdmission        string         `json:"imageAdmission"`
	LicenseClosure        string         `json:"licenseClosure"`
	Reproducible          bool           `json:"reproducible"`
	Version               string         `json:"version"`
	Platform              string         `json:"platform"`
	BinarySHA256          string         `json:"binarySha256"`
	NoticeInventorySHA256 string         `json:"noticeInventorySha256"`
	Inputs                []imageBinding `json:"inputs"`
	imageText
	Toolchain, SourceLicense, Modules *imageText
}
type imageFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type imageManifest struct {
	SchemaVersion  int         `json:"schemaVersion"`
	Profile        string      `json:"profile"`
	UpstreamCommit string      `json:"upstreamCommit"`
	Base           string      `json:"base"`
	Files          []imageFile `json:"files"`
	ImageAdmission string      `json:"imageAdmission"`
	LicenseClosure string      `json:"licenseClosure"`
	Reproducible   bool        `json:"reproducible"`
}

func imageHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// Confine reads, reject every linked component and check the complete read.
func imageRead(root, name string, limit int64) (data []byte, result error) {
	if name == "." || path.Clean(name) != name || path.IsAbs(name) || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\n\r\t") {
		return nil, fmt.Errorf("unclean package input")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid package root")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			data = nil
			result = err
		}
	}()
	parts := strings.Split(name, "/")
	for i := range parts {
		st, e := r.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil || st.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !st.IsDir()) {
			return nil, fmt.Errorf("linked or missing package input: %s", name)
		}
	}
	f, err := r.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > limit {
		_ = f.Close()
		return nil, fmt.Errorf("invalid package input size/type: %s", name)
	}
	b, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || int64(len(b)) != st.Size() {
		return nil, fmt.Errorf("incomplete package input: %s", name)
	}
	return b, nil
}

// Reject duplicate keys and trailing documents before decoding selected fields.
func imageDecode(b []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return fmt.Errorf("duplicate/invalid JSON key")
					}
					seen[s] = true
					if e = value(); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if err := value(); err != nil {
						return err
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
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON document")
	}
	return json.Unmarshal(b, into)
}

func imageValidEvidence(e imageEvidence) bool {
	return e.SchemaVersion == 1 && e.UpstreamCommit == imagePin && e.Profile == "api-login-v1-console" && e.ImageAdmission == "unknown" && e.LicenseClosure == "unknown" && !e.Reproducible
}

func imageBound(inputs []imageBinding, name, hash string) bool {
	count := 0
	for _, x := range inputs {
		if x.Path == name {
			count++
			if x.SHA256 != hash {
				return false
			}
		}
	}
	return count == 1
}

func imageWrite(root, name string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, e := f.Write(b)
	syncErr := f.Sync()
	modeErr := f.Chmod(mode)
	closeErr := f.Close()
	if e != nil || syncErr != nil || modeErr != nil || closeErr != nil || n != len(b) {
		return fmt.Errorf("incomplete package write")
	}
	return nil
}

func imageOutputPath(build, repo, output string) (string, error) {
	abs, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	target := filepath.Join(parent, filepath.Base(abs))
	for _, root := range []string{build, repo} {
		resolved, e := filepath.EvalSymlinks(root)
		if e != nil {
			return "", e
		}
		resolved, e = filepath.Abs(resolved)
		if e != nil {
			return "", e
		}
		if target == resolved || strings.HasPrefix(target, resolved+string(filepath.Separator)) {
			return "", fmt.Errorf("package output must be outside inputs")
		}
	}
	return target, nil
}

func imagePackage(build, repo, output string) (string, error) {
	var err error
	output, err = imageOutputPath(build, repo, output)
	if err != nil {
		return "", err
	}
	for _, pending := range []string{"build-receipt.pending", "console-notices-inventory.pending", "console-notices-supplement-inventory.pending", "login-icons-inventory.pending"} {
		if _, err := os.Lstat(filepath.Join(build, pending)); !os.IsNotExist(err) {
			return "", fmt.Errorf("pending or unavailable build evidence")
		}
	}
	data := map[string][]byte{}
	evidence := map[string]imageEvidence{}
	for _, name := range []string{"build-receipt.json", "console-notices-inventory.json", "console-notices-supplement-inventory.json", "login-icons-inventory.json"} {
		b, err := imageRead(build, name, 4<<20)
		if err != nil {
			return "", err
		}
		var e imageEvidence
		if err = imageDecode(b, &e); err != nil || !imageValidEvidence(e) {
			return "", fmt.Errorf("invalid build evidence: %s", name)
		}
		evidence[name] = e
		if name == "build-receipt.json" {
			data[name] = b
		} else {
			data["notices/"+name] = b
		}
	}
	receipt := evidence["build-receipt.json"]
	if receipt.Version != "4.20.0" || receipt.Platform != "linux/amd64" {
		return "", fmt.Errorf("wrong build profile/platform/version")
	}
	b, err := imageRead(build, "service/zitadel", 512<<20)
	if err != nil || imageHash(b) != receipt.BinarySHA256 {
		return "", fmt.Errorf("binary differs from completed receipt")
	}
	data["zitadel"] = b
	rh := imageHash(data["build-receipt.json"])
	console := evidence["console-notices-inventory.json"]
	icons := evidence["login-icons-inventory.json"]
	supplement := evidence["console-notices-supplement-inventory.json"]
	if !imageBound(console.Inputs, "build-receipt.json", rh) || !imageBound(icons.Inputs, "build-receipt.json", rh) || supplement.NoticeInventorySHA256 != imageHash(data["notices/console-notices-inventory.json"]) {
		return "", fmt.Errorf("unbound notice inventory")
	}
	for name, meta := range map[string]*imageText{"console-third-party-notices.txt": &console.imageText, "console-notices-supplement.txt": &supplement.imageText, "go-toolchain-notices.txt": console.Toolchain, "zitadel-root-license.txt": console.SourceLicense, "go-module-notices.txt": console.Modules} {
		if meta == nil || meta.RawDiagnostic != name || meta.Bytes < 1 {
			return "", fmt.Errorf("missing required notice identity: %s", name)
		}
		text, e := imageRead(build, name, 2<<20)
		if e != nil || int64(len(text)) != meta.Bytes || imageHash(text) != meta.SHA256 {
			return "", fmt.Errorf("retained notice differs: %s", name)
		}
		data["notices/"+name] = text
	}
	for _, name := range []string{"LICENSE.md", "NOTICE.md", "clipboard.svg"} {
		text, e := imageRead(repo, "third_party/zitadel-login-icons/"+name, 1<<20)
		if e != nil || !imageBound(icons.Inputs, name, imageHash(text)) {
			return "", fmt.Errorf("retained icon material differs")
		}
		data["notices/login-icons/"+name] = text
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return "", err
	}
	m := imageManifest{SchemaVersion: 1, Profile: "cli-version-smoke", UpstreamCommit: imagePin, Base: imageBase, ImageAdmission: "unknown", LicenseClosure: "unknown"}
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := os.FileMode(0444)
		if name == "zitadel" {
			mode = 0555
		}
		if err := imageWrite(output, name, data[name], mode); err != nil {
			return "", err
		}
		m.Files = append(m.Files, imageFile{Path: name, Bytes: len(data[name]), SHA256: imageHash(data[name]), Mode: uint32(mode)})
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	manifest = append(manifest, '\n')
	if err = imageWrite(output, "package-manifest.json", manifest, 0444); err != nil {
		return "", err
	}
	seal := imageHash(manifest)
	if err = imageVerifyContext(output, seal); err != nil {
		return "", err
	}
	return seal, nil
}

func imageVerifyContext(root, seal string) error {
	b, err := imageRead(root, "package-manifest.json", 1<<20)
	if err != nil || len(seal) != 64 || imageHash(b) != seal {
		return fmt.Errorf("package manifest changed")
	}
	var m imageManifest
	if err = imageDecode(b, &m); err != nil || m.SchemaVersion != 1 || m.UpstreamCommit != imagePin || m.Profile != "cli-version-smoke" || m.Base != imageBase || m.ImageAdmission != "unknown" || m.LicenseClosure != "unknown" || m.Reproducible || len(m.Files) != 13 {
		return fmt.Errorf("invalid package manifest")
	}
	want := map[string]bool{"package-manifest.json": true}
	for _, f := range m.Files {
		if want[f.Path] {
			return fmt.Errorf("duplicate package member")
		}
		want[f.Path] = true
		b, e := imageRead(root, f.Path, 512<<20)
		st, se := os.Lstat(filepath.Join(root, f.Path))
		if e != nil || se != nil || len(b) != f.Bytes || imageHash(b) != f.SHA256 || uint32(st.Mode().Perm()) != f.Mode || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("package member differs: %s", f.Path)
		}
	}
	return filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == root {
			return nil
		}
		name, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		name = filepath.ToSlash(name)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked package member")
		}
		if d.IsDir() {
			if name != "notices" && name != "notices/login-icons" {
				return fmt.Errorf("extra package directory")
			}
			return nil
		}
		if !want[name] {
			return fmt.Errorf("extra package member: %s", name)
		}
		return nil
	})
}

func imageRun(args []string, _ io.Writer, diagnostic io.Writer) int {
	if len(args) == 0 {
		return 0
	}
	fail := func(err error) int { _, _ = fmt.Fprintln(diagnostic, "identity-image:", err); return 1 }
	if args[0] == "--browser-worker" {
		if len(args) != 4 || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || runtime.Version() != "go1.26.10" {
			return fail(fmt.Errorf("explicit native browser worker required"))
		}
		if err := browserWorker(args[1], args[2], args[3]); err != nil {
			return fail(err)
		}
		return 0
	}
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		k := args[i]
		if values[k] != "" {
			return fail(fmt.Errorf("duplicate argument"))
		}
		switch k {
		case "--smoke", "--service-smoke", "--browser-smoke":
			values[k] = "true"
		case "--build", "--repository", "--output":
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "--") {
				return fail(fmt.Errorf("missing argument"))
			}
			i++
			values[k] = args[i]
		default:
			return fail(fmt.Errorf("unexpected argument"))
		}
	}
	if (values["--smoke"] != "" || values["--service-smoke"] != "" || values["--browser-smoke"] != "") && len(values) == 4 && values["--build"] != "" && values["--repository"] != "" && values["--output"] != "" {
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || runtime.Version() != "go1.26.10" {
			return fail(fmt.Errorf("native Linux amd64 Go 1.26.10 required"))
		}
		var probes []func(string, string) error
		if values["--service-smoke"] != "" || values["--browser-smoke"] != "" {
			probes = append(probes, func(id, owner string) error {
				return imageService(id, owner, values["--repository"], values["--output"], imageExecute, values["--browser-smoke"] != "")
			})
		}
		if err := imageSmoke(values["--build"], values["--repository"], values["--output"], imageExecute, probes...); err != nil {
			return fail(err)
		}
		return 0
	}
	return fail(fmt.Errorf("expected explicit smoke, build, repository and output arguments"))
}

func main() { os.Exit(imageRun(os.Args[1:], os.Stdout, os.Stderr)) }
