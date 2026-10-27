package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
)

func textStatikFixture(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range entries {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return []byte("package statik\nimport \"github.com/rakyll/statik/fs\"\nconst Login = \"login\"\nfunc init(){data := " + strconv.Quote(b.String()) + ";fs.RegisterWithNamespace(\"login\", data)}\n")
}

func textFixture(t *testing.T) (string, string, string, textPolicy) {
	t.Helper()
	build, notices := t.TempDir(), t.TempDir()
	put := func(root, name string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	font := textNameFixture([]testTextName{{0, "Copyright original", nil}, {3, "Example:1", nil}, {5, "Version 1", nil}, {13, "Original complete license", nil}, {14, "https://example.invalid/terms", nil}})
	name := "internal/api/ui/login/static/resources/fonts/example/Regular.ttf"
	console := "internal/api/ui/console/static/assets/fonts/example.ttf"
	cat := textCatalogue{SchemaVersion: 1, UpstreamCommit: upstream, Fonts: []textFont{{Path: name, Bytes: len(font), SHA256: digest(font), GitBlob: blob(font), DeclaredLicense: "OFL-1.1", Outputs: []string{name, console}}}}
	b, _ := json.Marshal(cat)
	put(notices, "catalogue.json", b)
	policy := textPolicy{CatalogueHash: digest(b), Fonts: 1, Outputs: 2, ApacheHash: digest([]byte("Apache original terms")), OFLHash: digest([]byte("Original Lato copyright\nOFL terms"))}
	put(notices, "Apache-2.0.txt", []byte("Apache original terms"))
	put(build, "prepared/source/"+textOFL, []byte("Original Lato copyright\nOFL terms"))
	put(build, "prepared/source/"+name, font)
	put(build, "prepared/source/"+console, font)
	put(build, "prepared/source-preparation.json", []byte(`{"schemaVersion":1,"upstreamCommit":"`+upstream+`","imageAdmission":"unknown","licenseClosure":"unknown"}`))
	archive := textStatikFixture(t, map[string][]byte{"resources/fonts/example/Regular.ttf": font})
	put(build, "prepared/source/"+statikPath, archive)
	packages := fmt.Sprintf(`{"ImportPath":"github.com/zitadel/zitadel/internal/api/ui/login/statik","Dir":%q,"GoFiles":["generate.go","statik.go"]}`+"\n"+`{"ImportPath":"github.com/zitadel/zitadel/internal/api/ui/console","Dir":%q,"EmbedPatterns":["static"],"EmbedFiles":["static/assets/fonts/example.ttf"]}`+"\n", filepath.Join(build, "prepared/source/internal/api/ui/login/statik"), filepath.Join(build, "prepared/source/internal/api/ui/console"))
	put(build, "packages.jsonl", []byte(packages))
	put(build, "generated.sha256", []byte(digest(font)+"  "+name+"\n"+digest(font)+"  "+console+"\n"+digest(archive)+"  "+statikPath+"\n"))
	put(build, "service/zitadel", []byte("compiled binary"))
	recipe := filepath.Join(build, "recipe.sh")
	put(build, "recipe.sh", []byte("checked build recipe"))
	put(build, "zitadel-login-icons.go", []byte("selected strict statik decoder"))
	put(build, "zitadel-text-fonts.go", []byte("selected text-font collector"))
	receipt := fmt.Sprintf(`{"schemaVersion":1,"upstreamCommit":%q,"profile":"api-login-v1-console","version":"4.20.0","platform":"linux/amd64","recipeSha256":%q,"binarySha256":%q,"imageAdmission":"unknown","licenseClosure":"unknown","reproducible":false}`, upstream, digest([]byte("checked build recipe")), digest([]byte("compiled binary")))
	var receiptObject map[string]any
	json.Unmarshal([]byte(receipt), &receiptObject)
	var prepObject any
	json.Unmarshal(mustTextRead(t, build, "prepared/source-preparation.json"), &prepObject)
	receiptObject["preparation"] = prepObject
	receiptObject["sourceDate"] = "2026-10-22T07:05:44Z"
	receiptObject["tools"] = map[string]any{"go": "1.26.10", "node": "22.23.3", "pnpm": "10.30.3"}
	receiptBytes, _ := json.Marshal(receiptObject)
	put(build, "build-receipt.pending", receiptBytes)
	return build, notices, recipe, policy
}

func TestTextDistributionRetainsCheckedBytes(t *testing.T) {
	b, n, r, p := textFixture(t)
	state, err := textObserve(b, n, r, readAt, p)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(state)
	if err = os.WriteFile(filepath.Join(b, textBefore), before, 0600); err != nil {
		t.Fatal(err)
	}
	inv, raw, err := textCapture(b, n, r, readAt, p)
	if err != nil {
		t.Fatal(err)
	}
	if inv.BinarySHA256 != digest([]byte("compiled binary")) || len(inv.Fonts) != 1 || len(inv.Fonts[0].Names) != 5 || len(inv.Fonts[0].Outputs) != 2 || !bytes.Contains(raw, []byte("Copyright original")) || !bytes.Contains(raw, []byte("Original complete license")) || !bytes.Contains(raw, []byte("Apache original terms")) || !bytes.Contains(raw, []byte("Original Lato copyright")) {
		t.Fatal("distribution lost original notices or binary/output binding")
	}
	if inv.ImageAdmission != "unknown" || inv.LicenseClosure != "unknown" || inv.Reproducible {
		t.Fatal("attribution widened release authority")
	}
	if !reflect.DeepEqual(inv.Inputs[len(inv.Inputs)-1], inputRecord{"build-receipt.json", digest(mustTextRead(t, b, "build-receipt.pending"))}) {
		t.Fatal("pending receipt was not normalized to final identity")
	}
	// No second read of a changing input may substitute bytes after validation.
	reads := map[string]int{}
	read := func(root, name string, limit int64) ([]byte, error) {
		reads[root+"/"+name]++
		if reads[root+"/"+name] > 1 && name == "Apache-2.0.txt" {
			return []byte("substitution"), nil
		}
		return readAt(root, name, limit)
	}
	_, again, err := textCapture(b, n, r, read, p)
	if err != nil || !bytes.Equal(raw, again) || reads[n+"/Apache-2.0.txt"] != 1 {
		t.Fatal("did not retain the checked notice bytes", err)
	}
}

func mustTextRead(t *testing.T, root, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(root, name))
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestTextDeferredIconManifestCannotHideMissingBindings(t *testing.T) {
	for _, kind := range []string{"valid", "missing manifest", "wrong manifest", "changed bytes"} {
		t.Run(kind, func(t *testing.T) {
			b, n, r, p := textFixture(t)
			data := []byte("separately deferred icon bytes")
			hash := digest(data)
			var c textCatalogue
			if e := textDecode(mustTextRead(t, n, "catalogue.json"), &c); e != nil {
				t.Fatal(e)
			}
			names := []string{"console/dist/console/media/icons.woff2", "internal/api/ui/console/static/media/icons.woff2"}
			for _, name := range names {
				c.DeferredIconOutputs = append(c.DeferredIconOutputs, inputRecord{name, hash})
				if e := os.MkdirAll(filepath.Dir(filepath.Join(b, "prepared/source", name)), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(b, "prepared/source", name), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			catalogue, _ := json.Marshal(c)
			p.CatalogueHash = digest(catalogue)
			os.WriteFile(filepath.Join(n, "catalogue.json"), catalogue, 0600)
			packages := bytes.ReplaceAll(mustTextRead(t, b, "packages.jsonl"), []byte(`"static/assets/fonts/example.ttf"`), []byte(`"static/assets/fonts/example.ttf","static/media/icons.woff2"`))
			os.WriteFile(filepath.Join(b, "packages.jsonl"), packages, 0600)
			manifest := mustTextRead(t, b, "generated.sha256")
			for _, name := range names {
				manifest = append(manifest, []byte(hash+"  "+name+"\n")...)
			}
			os.WriteFile(filepath.Join(b, "generated.sha256"), manifest, 0600)
			s, e := textObserve(b, n, r, readAt, p)
			if e != nil {
				t.Fatal(e)
			}
			before, _ := json.Marshal(s)
			os.WriteFile(filepath.Join(b, textBefore), before, 0600)
			switch kind {
			case "missing manifest":
				manifest = bytes.ReplaceAll(manifest, []byte(hash+"  "+names[0]+"\n"), nil)
				os.WriteFile(filepath.Join(b, "generated.sha256"), manifest, 0600)
			case "wrong manifest":
				manifest = bytes.ReplaceAll(manifest, []byte(hash), []byte(strings.Repeat("0", 64)))
				os.WriteFile(filepath.Join(b, "generated.sha256"), manifest, 0600)
			case "changed bytes":
				os.WriteFile(filepath.Join(b, "prepared/source", names[0]), []byte("replacement"), 0600)
			}
			inv, _, e := textCapture(b, n, r, readAt, p)
			if kind == "valid" {
				if e != nil || len(inv.Fonts) != 1 {
					t.Fatal("byte-bound icons widened text attribution or were lost", e)
				}
			} else if e == nil {
				t.Fatal("unbound separately deferred icon output admitted")
			}
		})
	}
}

func TestTextCaptureRejectsCoherentlyEmptyBinary(t *testing.T) {
	b, n, r, p := textFixture(t)
	s, e := textObserve(b, n, r, readAt, p)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(s)
	os.WriteFile(filepath.Join(b, textBefore), before, 0600)
	if e = os.WriteFile(filepath.Join(b, "service/zitadel"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	var rec map[string]any
	json.Unmarshal(mustTextRead(t, b, "build-receipt.pending"), &rec)
	rec["binarySha256"] = digest(nil)
	raw, _ := json.Marshal(rec)
	os.WriteFile(filepath.Join(b, "build-receipt.pending"), raw, 0600)
	if _, _, e = textCapture(b, n, r, readAt, p); e == nil {
		t.Fatal("empty binary and matching receipt admitted")
	}
}

func TestTextDistributionRefusesChangedOrIncompleteEvidence(t *testing.T) {
	for _, name := range []string{"changed source", "missing source", "extra source", "linked source", "changed generated copy", "missing generated copy", "extra generated copy", "omitted compiler font", "compiler error", "duplicate compiler package", "changed statik member", "malformed package stream", "missing snapshot", "changed snapshot", "changed manifest", "duplicate manifest", "omitted statik manifest", "wrong statik manifest", "changed recipe", "changed binary", "changed receipt", "missing receipt preparation", "wrong receipt preparation", "missing receipt uncertainty", "ambiguous receipt", "partial read", "changed catalogue", "changed Apache text", "changed OFL text"} {
		t.Run(name, func(t *testing.T) {
			b, n, r, p := textFixture(t)
			s, e := textObserve(b, n, r, readAt, p)
			if e != nil {
				t.Fatal(e)
			}
			before, _ := json.Marshal(s)
			if e = os.WriteFile(filepath.Join(b, textBefore), before, 0600); e != nil {
				t.Fatal(e)
			}
			put := func(root, name string, data []byte) {
				t.Helper()
				if e := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(root, name), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			src := "prepared/source/internal/api/ui/login/static/resources/fonts/example/Regular.ttf"
			read := readAt
			switch name {
			case "changed source":
				put(b, src, []byte("replacement"))
			case "missing source":
				os.Remove(filepath.Join(b, src))
			case "extra source":
				put(b, strings.Replace(src, "Regular.ttf", "Extra.ttf", 1), []byte("extra"))
			case "linked source":
				os.Remove(filepath.Join(b, src))
				os.Symlink(r, filepath.Join(b, src))
			case "changed generated copy":
				put(b, "prepared/source/internal/api/ui/console/static/assets/fonts/example.ttf", []byte("replacement"))
			case "missing generated copy":
				os.Remove(filepath.Join(b, "prepared/source/internal/api/ui/console/static/assets/fonts/example.ttf"))
			case "extra generated copy":
				put(b, "prepared/source/internal/api/ui/console/static/assets/fonts/extra.woff2", []byte("extra"))
			case "omitted compiler font":
				put(b, "packages.jsonl", bytes.ReplaceAll(mustTextRead(t, b, "packages.jsonl"), []byte(`"static/assets/fonts/example.ttf"`), nil))
			case "compiler error":
				put(b, "packages.jsonl", append(mustTextRead(t, b, "packages.jsonl"), []byte(`{"ImportPath":"other","Incomplete":true}`)...))
			case "duplicate compiler package":
				v := mustTextRead(t, b, "packages.jsonl")
				put(b, "packages.jsonl", append(v, v...))
			case "changed statik member":
				put(b, "prepared/source/"+statikPath, textStatikFixture(t, map[string][]byte{"resources/fonts/example/Regular.ttf": []byte("replacement")}))
			case "malformed package stream":
				put(b, "packages.jsonl", append(mustTextRead(t, b, "packages.jsonl"), '{'))
			case "missing snapshot":
				os.Remove(filepath.Join(b, textBefore))
			case "changed snapshot":
				put(b, textBefore, []byte(`{"schemaVersion":1}`))
			case "changed manifest":
				put(b, "generated.sha256", bytes.ReplaceAll(mustTextRead(t, b, "generated.sha256"), []byte(s.Fonts[0].SHA256), []byte(strings.Repeat("0", 64))))
			case "duplicate manifest":
				v := mustTextRead(t, b, "generated.sha256")
				put(b, "generated.sha256", append(v, v...))
			case "omitted statik manifest":
				v := mustTextRead(t, b, "generated.sha256")
				put(b, "generated.sha256", []byte(strings.Join(strings.Split(string(v), "\n")[:2], "\n")+"\n"))
			case "wrong statik manifest":
				v := mustTextRead(t, b, "generated.sha256")
				put(b, "generated.sha256", bytes.ReplaceAll(v, []byte(digest(mustTextRead(t, b, "prepared/source/"+statikPath))), []byte(strings.Repeat("0", 64))))
			case "missing receipt preparation", "wrong receipt preparation", "missing receipt uncertainty":
				var v map[string]any
				json.Unmarshal(mustTextRead(t, b, "build-receipt.pending"), &v)
				if name == "missing receipt preparation" {
					delete(v, "preparation")
				} else if name == "wrong receipt preparation" {
					v["preparation"] = map[string]any{"substitution": true}
				} else {
					delete(v, "reproducible")
				}
				raw, _ := json.Marshal(v)
				put(b, "build-receipt.pending", raw)
			case "changed recipe":
				put(b, "recipe.sh", []byte("replacement recipe"))
			case "changed binary":
				put(b, "service/zitadel", []byte("replacement binary"))
			case "changed receipt":
				put(b, "build-receipt.pending", bytes.ReplaceAll(mustTextRead(t, b, "build-receipt.pending"), []byte(`"linux/amd64"`), []byte(`"linux/arm64"`)))
			case "ambiguous receipt":
				put(b, "build-receipt.json", mustTextRead(t, b, "build-receipt.pending"))
			case "partial read":
				read = func(root, name string, limit int64) ([]byte, error) {
					v, e := readAt(root, name, limit)
					if name == "packages.jsonl" {
						return v, fmt.Errorf("partial source read")
					}
					return v, e
				}
			case "changed catalogue":
				put(n, "catalogue.json", append(mustTextRead(t, n, "catalogue.json"), ' '))
			case "changed Apache text":
				put(n, "Apache-2.0.txt", []byte("replacement"))
			case "changed OFL text":
				put(b, "prepared/source/"+textOFL, []byte("replacement"))
			}
			if _, _, e := textCapture(b, n, r, read, p); e == nil {
				t.Fatal("accepted incomplete or changed distribution evidence")
			}
		})
	}
}

type testTextName struct {
	id   uint16
	text string
	raw  []uint16
}

// textNameFixture is a metadata-only SFNT fixture, not a usable font program.
func textNameFixture(records []testTextName) []byte {
	storage := 6 + 12*len(records)
	table := make([]byte, storage)
	binary.BigEndian.PutUint16(table[2:], uint16(len(records)))
	binary.BigEndian.PutUint16(table[4:], uint16(storage))
	for i, record := range records {
		units := record.raw
		if units == nil {
			units = utf16.Encode([]rune(record.text))
		}
		entry := table[6+12*i : 18+12*i]
		for j, value := range []uint16{3, 1, 0x0409, record.id, uint16(2 * len(units)), uint16(len(table) - storage)} {
			binary.BigEndian.PutUint16(entry[2*j:], value)
		}
		for _, unit := range units {
			table = binary.BigEndian.AppendUint16(table, unit)
		}
	}
	font := make([]byte, 28)
	binary.BigEndian.PutUint32(font, 0x00010000)
	binary.BigEndian.PutUint16(font[4:], 1)
	copy(font[12:], "name")
	binary.BigEndian.PutUint32(font[20:], 28)
	binary.BigEndian.PutUint32(font[24:], uint32(len(table)))
	return append(font, table...)
}

func TestTextFontNamesPreserveCompleteAttribution(t *testing.T) {
	// A cutoff or lossy UTF-16 decoder must fail this consumer-visible comparison.
	license := "Original terms © 𝒜\r\n" + strings.Repeat("complete license clause\n", 220) + "END OF ORIGINAL TERMS"
	font := textNameFixture([]testTextName{
		{0, "Copyright Example", nil}, {3, "Example:Regular:1", nil},
		{5, "Version 1.104", nil}, {13, license, nil}, {14, "https://example.invalid/terms", nil},
	})
	got, err := textFontNames(font)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("retained %d attribution records; want all five", len(got))
	}
	if got[0].ID != 0 || got[0].Text != "Copyright Example" || got[0].Offset != 94 || got[0].Bytes != 34 {
		t.Fatalf("original copyright/span changed: %+v", got[0])
	}
	if got[0].Platform != 3 || got[0].Encoding != 1 || got[0].Language != 0x0409 || got[0].SHA256 != "04bfdf819e5d2eaa3b5cf8582f409524412760e02e678a908d56e26d593ab38f" {
		t.Fatal("original platform or UTF-16 byte identity changed")
	}
	if got[3].ID != 13 || got[3].Text != license {
		t.Fatal("complete original terms changed or truncated")
	}
	if got[4].Text != "https://example.invalid/terms" {
		t.Fatal("license URL changed")
	}
}

func TestTextFontNamesRejectIncompleteAttribution(t *testing.T) {
	// Each otherwise readable observation must fail rather than become partial success.
	records := []testTextName{{0, "Copyright Example", nil}, {3, "Example:Regular:1", nil}, {5, "Version 1", nil}, {13, "Original terms", nil}, {14, "https://example.invalid/terms", nil}}
	for _, test := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"truncated header", func(b []byte) []byte { return b[:11] }},
		{"unsupported font", func(b []byte) []byte { copy(b, "OTTO"); return b }},
		{"missing name table", func(b []byte) []byte { copy(b[12:], "head"); return b }},
		{"overlapping table directory", func(b []byte) []byte { binary.BigEndian.PutUint32(b[20:], 12); return b }},
		{"truncated name table", func(b []byte) []byte { return b[:len(b)-1] }},
		{"unsupported name format", func(b []byte) []byte { binary.BigEndian.PutUint16(b[28:], 1); return b }},
		{"strings overlap records", func(b []byte) []byte { binary.BigEndian.PutUint16(b[32:], 6); return b }},
		{"odd UTF-16 length", func(b []byte) []byte { binary.BigEndian.PutUint16(b[42:], 3); return b }},
		{"out of bounds string", func(b []byte) []byte { binary.BigEndian.PutUint16(b[44:], 65535); return b }},
		{"empty copyright", func(b []byte) []byte { binary.BigEndian.PutUint16(b[42:], 0); return b }},
		{"missing English record", func(b []byte) []byte { binary.BigEndian.PutUint16(b[38:], 0x0407); return b }},
		{"missing selected encoding", func(b []byte) []byte { binary.BigEndian.PutUint16(b[36:], 10); return b }},
		{"duplicate name table", func(b []byte) []byte {
			out := make([]byte, 44)
			copy(out, b[:28])
			binary.BigEndian.PutUint16(out[4:], 2)
			binary.BigEndian.PutUint32(out[20:], 44)
			copy(out[28:], out[12:28])
			return append(out, b[28:]...)
		}},
		{"overlapping font tables", func(b []byte) []byte {
			out := make([]byte, 44)
			copy(out, b[:28])
			binary.BigEndian.PutUint16(out[4:], 2)
			binary.BigEndian.PutUint32(out[20:], 44)
			copy(out[28:], "head")
			binary.BigEndian.PutUint32(out[36:], 45)
			binary.BigEndian.PutUint32(out[40:], 2)
			return append(out, b[28:]...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := textFontNames(test.change(textNameFixture(records))); err == nil {
				t.Fatal("accepted incomplete or unsupported attribution")
			}
		})
	}
	for _, test := range []struct {
		name  string
		units []uint16
	}{{"unpaired high surrogate", []uint16{0xd800}}, {"unpaired low surrogate", []uint16{0xdc00}}, {"invalid surrogate pair", []uint16{0xd800, 'x'}}, {"NUL attribution", []uint16{0}}} {
		t.Run(test.name, func(t *testing.T) {
			copyRecords := append([]testTextName(nil), records...)
			copyRecords[0].raw = test.units
			if _, err := textFontNames(textNameFixture(copyRecords)); err == nil {
				t.Fatal("accepted invalid selected string")
			}
		})
	}
	t.Run("missing license", func(t *testing.T) {
		missing := append([]testTextName(nil), records[:3]...)
		missing = append(missing, records[4])
		if _, err := textFontNames(textNameFixture(missing)); err == nil {
			t.Fatal("accepted missing license declaration")
		}
	})
	t.Run("ambiguous duplicate", func(t *testing.T) {
		duplicate := append(append([]testTextName(nil), records...), records[3])
		if _, err := textFontNames(textNameFixture(duplicate)); err == nil {
			t.Fatal("accepted ambiguous duplicate declaration")
		}
	})
	t.Run("partial unselected record", func(t *testing.T) {
		withIgnored := append(append([]testTextName(nil), records...), testTextName{1, "Other family record", nil})
		b := textNameFixture(withIgnored)
		binary.BigEndian.PutUint16(b[28+6+5*12+10:], 65535)
		if _, err := textFontNames(b); err == nil {
			t.Fatal("a clean selected set hid an incomplete unselected record")
		}
	})
}

type textShortWriter struct{}

func (textShortWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }

func TestTextInputRefusesSpecialFileWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "font.ttf")
	if e := syscall.Mkfifo(name, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := readAt(root, "font.ttf", 1<<20); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("special file admitted")
		}
	case <-time.After(300 * time.Millisecond):
		// Release the old blocking implementation before reporting the failure.
		f, e := os.OpenFile(name, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if e == nil {
			defer f.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("special input blocked before file-type validation")
	}
}

func TestTextCLIIsExplicitAndVerificationReadOnly(t *testing.T) {
	b, n, r, p := textFixture(t)
	old := textFrozen
	textFrozen = p
	defer func() { textFrozen = old }()
	args := []string{"--text-fonts", "--snapshot", "--build", b, "--notices", n, "--recipe", r}
	var out, diag bytes.Buffer
	if run(nil, &out, &diag) != 0 || out.Len() != 0 || diag.Len() != 0 {
		t.Fatal("disabled path is not inert")
	}
	for _, args := range [][]string{{"--text-fonts"}, {"--text-fonts", "--snapshot", "--verify"}, {"--text-fonts", "--snapshot", "--build", b, "--build", b}, {"--text-fonts", "--inventory", "--unknown"}} {
		if run(args, &out, &diag) == 0 {
			t.Fatal("invalid mode admitted")
		}
	}
	out.Reset()
	diag.Reset()
	if run(args, &out, &diag) != 0 {
		t.Fatal(diag.String())
	}
	if e := os.WriteFile(filepath.Join(b, textBefore), out.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	if run(args, textShortWriter{}, &diag) == 0 {
		t.Fatal("short output write accepted")
	}
	args[1] = "--inventory"
	out.Reset()
	diag.Reset()
	if run(args, &out, &diag) != 0 {
		t.Fatal(diag.String())
	}
	inventory := append([]byte(nil), out.Bytes()...)
	if run(args, &out, &diag) == 0 {
		t.Fatal("existing retained notice overwritten")
	}
	args[1] = "--verify"
	if run(args, &out, &diag) == 0 {
		t.Fatal("pending build verified")
	}
	if e := os.Rename(filepath.Join(b, "build-receipt.pending"), filepath.Join(b, "build-receipt.json")); e != nil {
		t.Fatal(e)
	}
	before := mustTextRead(t, b, textRaw)
	out.Reset()
	diag.Reset()
	if run(args, &out, &diag) != 0 || !bytes.Equal(out.Bytes(), inventory) {
		t.Fatal("completed verification changed its result", diag.String())
	}
	if !bytes.Equal(before, mustTextRead(t, b, textRaw)) {
		t.Fatal("verification changed retained bytes")
	}
	if e := os.WriteFile(filepath.Join(b, textRaw), []byte("replacement"), 0600); e != nil {
		t.Fatal(e)
	}
	if run(args, &out, &diag) == 0 {
		t.Fatal("changed retained notice accepted")
	}
}

func TestTextFontNamesRejectSharedStringAmplification(t *testing.T) {
	// Five references to the same bounded source span cannot amplify retained text without a limit.
	b := textNameFixture([]testTextName{{0, "Copyright", nil}, {3, "Identity", nil}, {5, "Version", nil}, {13, strings.Repeat("x", 20000), nil}, {14, "URL", nil}})
	licenseOffset := binary.BigEndian.Uint16(b[28+6+3*12+10:])
	for i := 0; i < 5; i++ {
		binary.BigEndian.PutUint16(b[28+6+i*12+8:], 40000)
		binary.BigEndian.PutUint16(b[28+6+i*12+10:], licenseOffset)
	}
	if _, err := textFontNames(b); err == nil {
		t.Fatal("accepted amplified attribution beyond the output bound")
	}
}
