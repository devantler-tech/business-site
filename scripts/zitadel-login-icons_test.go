package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type iconFixture struct {
	Fonts   map[string]string `json:"fonts"`
	Vectors map[string]string `json:"vectors"`
}

func fixture(t *testing.T) iconFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/zitadel-login-icons/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f iconFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fontBytes(t *testing.T) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for n, v := range fixture(t).Fonts {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			t.Fatal(err)
		}
		result[n] = b
	}
	return result
}

func zipData(t *testing.T, names []string, data map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0644)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func statikText(payload []byte) []byte {
	return []byte(fmt.Sprintf("package statik\nimport \"github.com/rakyll/statik/fs\"\nconst Login = \"login\"\nfunc init(){data := %q; fs.RegisterWithNamespace(\"login\",data)}\n", string(payload)))
}

func manifest(t *testing.T, build string) {
	t.Helper()
	base := filepath.Join(build, "prepared/source")
	paths := []string{"internal/api/ui/login/statik/statik.go"}
	for _, n := range []string{"eot", "svg", "ttf", "woff"} {
		paths = append(paths, "internal/api/ui/login/static/resources/fonts/lgn-icons/fonts/lgn-icons."+n)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(base, p))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(data), p)
	}
	put(t, filepath.Join(build, "generated.sha256"), []byte(b.String()))
}

func buildFixture(t *testing.T) (string, string) {
	t.Helper()
	build := t.TempDir()
	notices := filepath.Join(t.TempDir(), "notices")
	base := filepath.Join(build, "prepared/source/internal/api/ui/login/static/resources/fonts/lgn-icons")
	members := map[string][]byte{}
	names := []string{}
	for n, b := range fontBytes(t) {
		put(t, filepath.Join(base, "fonts", n), b)
		name := "resources/fonts/lgn-icons/fonts/" + n
		members[name] = b
		names = append(names, name)
	}
	for n, b := range fixture(t).Vectors {
		put(t, filepath.Join(base, "svg", n), []byte(b))
	}
	sort.Strings(names)
	put(t, filepath.Join(build, "prepared/source/internal/api/ui/login/statik/statik.go"), statikText(zipData(t, names, members)))
	put(t, filepath.Join(build, "build-receipt.pending"), []byte(`{"schemaVersion":1,"upstreamCommit":"8afa5dd37a8e723efb9eace8d09fd5c1cccef694","profile":"api-login-v1-console","platform":"linux/amd64","imageAdmission":"unknown","licenseClosure":"unknown","reproducible":false}`))
	put(t, filepath.Join(build, "prepared/source-preparation.json"), []byte(`{"schemaVersion":1,"upstreamCommit":"8afa5dd37a8e723efb9eace8d09fd5c1cccef694","imageAdmission":"unknown","licenseClosure":"unknown"}`))
	for _, n := range []string{"LICENSE.md", "NOTICE.md", "clipboard.svg"} {
		b, err := os.ReadFile(filepath.Join("../third_party/zitadel-login-icons", n))
		if err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(notices, n), b)
	}
	manifest(t, build)
	return build, notices
}

func invoke(build, notices string) (int, string, string) {
	var out, err bytes.Buffer
	code := run([]string{"--inventory", "--build", build, "--notices", notices}, &out, &err)
	return code, out.String(), err.String()
}

func changeJSONField(t *testing.T, path, field string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object[field] = value
	data, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, data)
}

func TestInventoryDefaultOffAndCompleteSelectedOutput(t *testing.T) {
	var out, err bytes.Buffer
	if code := run(nil, &out, &err); code != 0 || out.Len() != 0 || err.Len() != 0 {
		t.Fatalf("default operation had effects: %d %s %s", code, &out, &err)
	}
	build, notices := buildFixture(t)
	code, a, msg := invoke(build, notices)
	if code != 0 {
		t.Fatalf("complete selected inventory rejected: %s", msg)
	}
	_, b, _ := invoke(build, notices)
	if a != b {
		t.Fatal("inventory is not deterministic")
	}
	var got struct {
		SchemaVersion     int
		Profile           string
		Fonts             []json.RawMessage
		Glyphs            []json.RawMessage
		GeneratorSettings string
		ImageAdmission    string
		LicenseClosure    string
		Reproducible      bool
	}
	if err := json.Unmarshal([]byte(a), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Profile != "api-login-v1-console" || len(got.Fonts) != 4 || len(got.Glyphs) != 10 || got.GeneratorSettings != "unknown" || got.ImageAdmission != "unknown" || got.LicenseClosure != "unknown" || got.Reproducible {
		t.Fatalf("wrong admitted boundary: %s", a)
	}
}

func TestInventoryUsesTheCheckedClipboardSnapshot(t *testing.T) {
	build, notices := buildFixture(t)
	noticeRoot, err := filepath.EvalSymlinks(notices)
	if err != nil {
		t.Fatal(err)
	}
	clipboardReads := 0
	read := func(root, relative string, limit int64) ([]byte, error) {
		data, err := readAt(root, relative, limit)
		if err == nil && root == noticeRoot && relative == "clipboard.svg" {
			clipboardReads++
			if clipboardReads == 1 {
				// Change the real file after its first complete read. Both forms
				// share the upstream blob but only the first has the pinned digest.
				put(t, filepath.Join(notices, relative), bytes.TrimSuffix(data, []byte("\n")))
			}
		}
		return data, err
	}
	got, err := inspectWithRead(build, notices, read)
	if err != nil {
		t.Fatal(err)
	}
	const want = "9a526c626cdb157f44c29831d5c99eba3d719a92758a257891164538090fd739"
	if clipboardReads != 1 || got.Glyphs[7].RetainedSHA256 != want {
		t.Fatalf("inventory used a second, unchecked clipboard snapshot: reads=%d retained=%s", clipboardReads, got.Glyphs[7].RetainedSHA256)
	}
	found := false
	for _, input := range got.Inputs {
		if input.Path == "clipboard.svg" {
			found = true
			if input.SHA256 != want {
				t.Fatalf("clipboard input and glyph snapshots differ: %s", input.SHA256)
			}
		}
	}
	if !found {
		t.Fatal("checked clipboard input absent from inventory")
	}
}

func TestInventoryRejectsMissingSubstitutedAndPartialInputs(t *testing.T) {
	cases := map[string]func(*testing.T, string, string){
		"wrong profile": func(t *testing.T, b, n string) {
			changeJSONField(t, filepath.Join(b, "build-receipt.pending"), "profile", "other")
		},
		"wrong platform": func(t *testing.T, b, n string) {
			changeJSONField(t, filepath.Join(b, "build-receipt.pending"), "platform", "linux/arm64")
		},
		"wrong reproducibility": func(t *testing.T, b, n string) {
			changeJSONField(t, filepath.Join(b, "build-receipt.pending"), "reproducible", true)
		},
		"wrong preparation": func(t *testing.T, b, n string) {
			changeJSONField(t, filepath.Join(b, "prepared/source-preparation.json"), "upstreamCommit", "other")
		},
		"missing manifest": func(t *testing.T, b, n string) {
			if err := os.Remove(filepath.Join(b, "generated.sha256")); err != nil {
				t.Fatal(err)
			}
		},
		"partial manifest": func(t *testing.T, b, n string) {
			put(t, filepath.Join(b, "generated.sha256"), []byte(strings.Repeat("a", 64)+"  internal/api/ui/login/statik/statik.go\n"))
		},
		"duplicate manifest": func(t *testing.T, b, n string) {
			p := filepath.Join(b, "generated.sha256")
			v, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			put(t, p, append(v, v...))
		},
		"plausible prefix then invalid manifest": func(t *testing.T, b, n string) {
			p := filepath.Join(b, "generated.sha256")
			v, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			put(t, p, append(v, []byte("bad trailing input\n")...))
		},
		"missing notice": func(t *testing.T, b, n string) {
			if e := os.Remove(filepath.Join(n, "LICENSE.md")); e != nil {
				t.Fatal(e)
			}
		},
		"truncated license": func(t *testing.T, b, n string) {
			put(t, filepath.Join(n, "LICENSE.md"), []byte("Copyright (c) Icons8\nMIT\n"))
		},
		"wrong clipboard": func(t *testing.T, b, n string) {
			put(t, filepath.Join(n, "clipboard.svg"), []byte(fixture(t).Vectors["clipboard-solid.svg"]))
		},
		"extra font": func(t *testing.T, b, n string) {
			put(t, filepath.Join(b, "prepared/source/internal/api/ui/login/static/resources/fonts/lgn-icons/fonts/extra.ttf"), []byte("extra"))
		},
		"different embedded payload": func(t *testing.T, b, n string) {
			p := filepath.Join(b, "prepared/source/internal/api/ui/login/statik/statik.go")
			put(t, p, statikText(zipData(t, []string{"other"}, map[string][]byte{"other": []byte("valid ZIP but incomplete")})))
			manifest(t, b)
		},
		"linked parent": func(t *testing.T, b, n string) {
			p := filepath.Join(n, "LICENSE.md")
			v, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(t.TempDir(), "license")
			put(t, target, v)
			if e = os.Remove(p); e != nil {
				t.Fatal(e)
			}
			if e = os.Symlink(target, p); e != nil {
				t.Fatal(e)
			}
		},
		"linked prepared source root": func(t *testing.T, b, n string) {
			p := filepath.Join(b, "prepared/source")
			outside := filepath.Join(t.TempDir(), "source")
			if err := os.Rename(p, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, p); err != nil {
				t.Fatal(err)
			}
		},
	}
	wanted := map[string]string{
		"wrong profile":                          "unsupported build profile",
		"wrong platform":                         "unsupported build profile",
		"wrong reproducibility":                  "unsupported build profile",
		"wrong preparation":                      "source-preparation.json upstreamCommit",
		"missing manifest":                       "generated.sha256: no such file",
		"partial manifest":                       "statik digest does not match selected output",
		"duplicate manifest":                     "duplicate or unclean generated member",
		"plausible prefix then invalid manifest": "malformed generated inventory",
		"missing notice":                         "LICENSE.md: no such file",
		"truncated license":                      "notice/source identity differs: LICENSE.md",
		"wrong clipboard":                        "notice/source identity differs: clipboard.svg",
		"extra font":                             "selected directory membership differs",
		"different embedded payload":             "embedded font differs",
		"linked parent":                          "linked input: LICENSE.md",
		"linked prepared source root":            "linked input: prepared/source/",
	}
	for _, ext := range []string{"eot", "ttf", "woff", "svg"} {
		for _, kind := range []string{"missing", "substituted"} {
			ext, kind := ext, kind
			wanted[kind+" "+ext] = "font source/output identity differs:"
			if kind == "missing" {
				wanted[kind+" "+ext] = "selected directory membership differs"
				if ext == "eot" {
					wanted[kind+" "+ext] = "lgn-icons.eot: no such file"
				}
			}
			cases[kind+" "+ext] = func(t *testing.T, b, n string) {
				p := filepath.Join(b, "prepared/source/internal/api/ui/login/static/resources/fonts/lgn-icons/fonts/lgn-icons."+ext)
				if kind == "missing" {
					if e := os.Remove(p); e != nil {
						t.Fatal(e)
					}
				} else {
					put(t, p, []byte("substitution"))
					manifest(t, b)
				}
			}
		}
	}
	for name := range fixture(t).Vectors {
		name := name
		wanted["substituted vector "+name] = "immutable vector differs:"
		if name == "clipboard-solid.svg" {
			wanted["substituted vector "+name] = "original solid clipboard input differs"
		}
		cases["substituted vector "+name] = func(t *testing.T, b, n string) {
			put(t, filepath.Join(b, "prepared/source/internal/api/ui/login/static/resources/fonts/lgn-icons/svg", name), []byte(`<svg viewBox="0 0 32 32"><path d="M0 0Z"/></svg>`))
		}
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b, n := buildFixture(t)
			change(t, b, n)
			code, out, msg := invoke(b, n)
			if code == 0 || out != "" || msg == "" {
				t.Fatalf("failure published success inventory: %d %q %q", code, out, msg)
			}
			if wanted[name] == "" || !strings.Contains(msg, wanted[name]) {
				t.Fatalf("named rejection did not reach its check (want %q): %s", wanted[name], msg)
			}
		})
	}
}

func TestCompletedReceiptAndAmbiguousReceipt(t *testing.T) {
	b, n := buildFixture(t)
	code, before, msg := invoke(b, n)
	if code != 0 {
		t.Fatal(msg)
	}
	if err := os.Rename(filepath.Join(b, "build-receipt.pending"), filepath.Join(b, "build-receipt.json")); err != nil {
		t.Fatal(err)
	}
	code, after, msg := invoke(b, n)
	if code != 0 || before != after {
		t.Fatalf("completed receipt cannot reproduce inventory: %d %s", code, msg)
	}
	data, err := os.ReadFile(filepath.Join(b, "build-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(b, "build-receipt.pending"), data)
	code, out, _ := invoke(b, n)
	if code == 0 || out != "" {
		t.Fatal("ambiguous receipt accepted")
	}
}

func TestStatikIsInspectedWithoutExecution(t *testing.T) {
	payload := zipData(t, []string{"a"}, map[string][]byte{"a": []byte("complete")})
	good := statikText(payload)
	members, err := decodeStatik(good)
	if err != nil || string(members["a"]) != "complete" {
		t.Fatalf("valid archive rejected: %v", err)
	}
	for name, data := range map[string][]byte{
		"compiler excluded registration":   append([]byte("//go:build ignore\n\n"), good...),
		"legacy compiler exclusion":        append([]byte("// +build ignore\n\n"), good...),
		"wrong namespace":                  bytes.Replace(good, []byte(`RegisterWithNamespace("login"`), []byte(`RegisterWithNamespace("other"`), 1),
		"second init":                      append(append([]byte{}, good...), []byte("func init(){}\n")...),
		"reassigned literal":               bytes.Replace(good, []byte("; fs.Register"), []byte("; data=\"changed\"; fs.Register"), 1),
		"concatenation":                    bytes.Replace(good, []byte("; fs.Register"), []byte("+\"\"; fs.Register"), 1),
		"duplicate archive entry":          statikText(zipData(t, []string{"a", "a"}, map[string][]byte{"a": []byte("complete")})),
		"unclean archive member":           statikText(zipData(t, []string{"../a"}, map[string][]byte{"../a": []byte("complete")})),
		"truncated ZIP":                    statikText(payload[:len(payload)-10]),
		"CRC failure after plausible data": statikText(bytes.Replace(payload, []byte("complete"), []byte("incomple"), 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStatik(data); err == nil {
				t.Fatal("incomplete/unsafe evidence accepted")
			}
		})
	}
}

func TestAllFontWrappersAndGlyphs(t *testing.T) {
	fonts := fontBytes(t)
	if err := checkWrappers(fonts); err != nil {
		t.Fatal(err)
	}
	vectors := map[string][]byte{}
	for n, s := range fixture(t).Vectors {
		vectors[n] = []byte(s)
	}
	clipboard, err := os.ReadFile("../third_party/zitadel-login-icons/clipboard.svg")
	if err != nil {
		t.Fatal(err)
	}
	vectors["clipboard.svg"] = clipboard
	if _, err := checkGlyphs(fonts["lgn-icons.svg"], vectors); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lgn-icons.eot", "lgn-icons.ttf", "lgn-icons.woff"} {
		t.Run(name, func(t *testing.T) {
			bad := fontBytes(t)
			bad[name] = bad[name][:len(bad[name])-1]
			if checkWrappers(bad) == nil {
				t.Fatal("truncated wrapper accepted")
			}
		})
	}
	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("codepoint-%d", i), func(t *testing.T) {
			old := fmt.Sprintf("&#xe90%x;", i)
			bad := bytes.Replace(fonts["lgn-icons.svg"], []byte(old), []byte("&#xf000;"), 1)
			if _, err := checkGlyphs(bad, vectors); err == nil {
				t.Fatal("wrong codepoint accepted")
			}
		})
	}
	vectors["clipboard.svg"] = vectors["clipboard-solid.svg"]
	if _, err := checkGlyphs(fonts["lgn-icons.svg"], vectors); err == nil {
		t.Fatal("solid clipboard substituted for actual glyph")
	}
}

func TestClosedPathNormalization(t *testing.T) {
	a, err := normalizePath("M1 2h3v4s1 2 3 4z", true)
	if err != nil {
		t.Fatal(err)
	}
	// Independently expanded path: smooth control reflects only after a curve.
	b, err := normalizePath("M32 896L128 896L128 768C128 768 160 704 224 640Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("normalization differs: %s / %s", a, b)
	}
	for _, bad := range []string{"M0 0A1 1 0 0 0 1 1", "M0", "M0 0 junk", "MNaN 0", "M1e100 0", "L1 2"} {
		if _, err := normalizePath(bad, true); err == nil {
			t.Errorf("unsupported path accepted: %s", bad)
		}
	}
}

func TestLoginProviderGraphicsCannotReturnInsideCompiledStatik(t *testing.T) {
	cases := map[string]string{
		"resources/images/idp/google.png":      "retired original",
		"resources/images/idp/extra.webp":      "additional image",
		"resources/themes/styles/provider.css": `.logo{background:url(/ui/login/resources/images/idp/apple.svg)}`,
		"templates/login.html":                 `<img src="resources/images/idp/google.png">`,
		"templates/register_option.html":       `<img src="resources/images/idp/github.png">`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			build, notices := buildFixture(t)
			if code, _, diagnostic := invoke(build, notices); code != 0 {
				t.Fatalf("baseline: %s", diagnostic)
			}
			members := map[string][]byte{}
			names := []string{}
			for key, value := range fontBytes(t) {
				p := "resources/fonts/lgn-icons/fonts/" + key
				members[p] = value
				names = append(names, p)
			}
			members[name] = []byte(body)
			names = append(names, name)
			sort.Strings(names)
			put(t, filepath.Join(build, "prepared/source", statikPath), statikText(zipData(t, names, members)))
			manifest(t, build)
			if code, _, _ := invoke(build, notices); code == 0 {
				t.Fatal("retired provider image or URL accepted inside actual decoded statik")
			}
		})
	}
}
