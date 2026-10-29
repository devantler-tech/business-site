package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// An undeclared translation must not disappear from the closed copied-asset
// selection merely because the original collector recognized only icon paths.
func TestCopiedTranslationsRejectUndeclaredSourceOutputAndCompilerMembers(t *testing.T) {
	for _, member := range []string{"source", "console-output", "embedded-output", "compiler"} {
		t.Run(member, func(t *testing.T) {
			b, n, r, p := copiedFixture(t)
			if _, err := copiedObserve(b, n, r, readAt, p); err != nil {
				t.Fatalf("valid baseline: %v", err)
			}
			if member == "compiler" {
				appendTranslationCompilerMember(t, b)
			} else {
				name := map[string]string{
					"source":          "console/src/assets/i18n/zz.json",
					"console-output":  "console/dist/console/assets/i18n/zz.json",
					"embedded-output": "internal/api/ui/console/static/assets/i18n/zz.json",
				}[member]
				filename := filepath.Join(b, "prepared/source", name)
				if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(`{"HELLO":"Undeclared translation"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := copiedObserve(b, n, r, readAt, p); err == nil {
				t.Fatal("undeclared translation was omitted from asset admission")
			}
		})
	}
}

func appendTranslationCompilerMember(t *testing.T, build string) {
	appendCopiedCompilerMember(t, build, "static/assets/i18n/zz.json")
}

func appendCopiedCompilerMember(t *testing.T, build, member string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(mustTextRead(t, build, "packages.jsonl")))
	var stream bytes.Buffer
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["ImportPath"] == "github.com/zitadel/zitadel/internal/api/ui/console" {
			record["EmbedFiles"] = append(record["EmbedFiles"].([]any), member)
		}
		if err := json.NewEncoder(&stream).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(build, "packages.jsonl"), stream.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCopiedSMTPGraphicsCannotReturnInCompleteCompilerObservation(t *testing.T) {
	for _, name := range []string{"aws-ses.svg", "brevo.svg", "google.png", "mailchimp.svg", "mailgun.svg", "mailjet.svg", "outlook.svg", "postmark.png", "sendgrid.png"} {
		t.Run(name, func(t *testing.T) {
			build, notices, recipe, policy := copiedFixture(t)
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err != nil {
				t.Fatalf("valid baseline: %v", err)
			}
			// These paths are outside the copied catalogue. The complete compiler
			// observation must reject them before selecting its catalogue subset.
			appendCopiedCompilerMember(t, build, "static/assets/images/smtp/"+name)
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err == nil {
				t.Fatal("retired SMTP compiler member was hidden by catalogue filtering")
			}
		})
	}
}

func TestCopiedTranslationsRejectChangedMissingLinkedMalformedAndUncompiledInputs(t *testing.T) {
	for _, mode := range []string{"changed-source", "missing-source", "linked-source", "changed-output", "missing-output", "uncompiled", "changed-scope", "missing-contribution", "malformed", "duplicate", "non-object", "invalid-utf8"} {
		t.Run(mode, func(t *testing.T) {
			b, n, r, p := copiedFixture(t)
			if _, err := copiedObserve(b, n, r, readAt, p); err != nil {
				t.Fatalf("valid baseline: %v", err)
			}
			source := filepath.Join(b, "prepared/source/console/src/assets/i18n/en.json")
			output := filepath.Join(b, "prepared/source/internal/api/ui/console/static/assets/i18n/en.json")
			var err error
			switch mode {
			case "changed-source":
				err = os.WriteFile(source, []byte(`{"HELLO":"Substituted"}`), 0600)
			case "missing-source":
				err = os.Remove(source)
			case "linked-source":
				if err = os.Remove(source); err == nil {
					err = os.Symlink(output, source)
				}
			case "changed-output":
				err = os.WriteFile(output, []byte(`{"HELLO":"Substituted"}`), 0600)
			case "missing-output":
				err = os.Remove(output)
			case "uncompiled":
				raw := mustTextRead(t, b, "packages.jsonl")
				err = os.WriteFile(filepath.Join(b, "packages.jsonl"), bytes.ReplaceAll(raw, []byte("static/assets/i18n/en.json"), []byte("static/unselected.json")), 0600)
			case "changed-scope":
				err = os.WriteFile(filepath.Join(b, "prepared/source/LICENSING.md"), []byte("Different scope"), 0600)
			case "missing-contribution":
				err = os.Remove(filepath.Join(b, "prepared/source/CONTRIBUTING.md"))
			default:
				payload := map[string][]byte{
					"malformed":    []byte(`{"HELLO":`),
					"duplicate":    []byte(`{"GREETING":{"HELLO":"one","HELLO":"two"}}`),
					"non-object":   []byte(`["not a translation object"]`),
					"invalid-utf8": {'{', '"', 'X', '"', ':', '"', 0xff, '"', '}'},
				}[mode]
				// Deliberately re-seal this synthetic catalogue and both outputs so
				// only structural validation can reject the malformed translation.
				var cat copiedCatalogue
				if err = json.Unmarshal(mustTextRead(t, n, "catalogue.json"), &cat); err != nil {
					t.Fatal(err)
				}
				for i := range cat.Assets {
					a := &cat.Assets[i]
					if a.Family != "translations" {
						continue
					}
					a.Bytes, a.SHA256, a.GitBlob = len(payload), digest(payload), blob(payload)
					for _, name := range append([]string{a.Path}, a.Outputs...) {
						if err = os.WriteFile(filepath.Join(b, "prepared/source", name), payload, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				encoded, encodeErr := json.Marshal(cat)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				err = os.WriteFile(filepath.Join(n, "catalogue.json"), encoded, 0600)
				p.CatalogueHash = digest(encoded)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := copiedObserve(b, n, r, readAt, p); err == nil {
				t.Fatal("incomplete translation admission succeeded")
			}
		})
	}
}

// This miniature distribution exercises original archives, actual filesystem
// copies and compiler observations. Native CI separately exercises the frozen
// 1,858-member distribution; these are deliberately not native-font fixtures.
func copiedFixture(t *testing.T) (string, string, string, copiedPolicy) {
	t.Helper()
	b, fonts, recipe, fontPolicy := textFixture(t)
	n := t.TempDir()
	put := func(root, name string, body []byte) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(root, name), body, 0600); e != nil {
			t.Fatal(e)
		}
	}
	cat := copiedCatalogue{SchemaVersion: 1, UpstreamCommit: upstream}
	lock := []byte("frozen original package selection")
	put(b, "prepared/source/pnpm-lock.yaml", lock)
	cat.LockSHA256 = digest(lock)
	terms := []byte("Original complete MIT grant\nCopyright fixture author\n")
	put(n, "MIT.txt", terms)
	cat.Notices = []inputRecord{{"MIT.txt", digest(terms)}}
	for _, context := range []struct{ source, notice, body string }{
		{"LICENSE", "ZITADEL-LICENSE", "Complete fixture source license\n"},
		{"LICENSING.md", "ZITADEL-LICENSING.md", "Complete fixture licensing scope\n"},
		{"CONTRIBUTING.md", "ZITADEL-CONTRIBUTING.md", "Complete fixture translation contribution context\n"},
	} {
		put(b, "prepared/source/"+context.source, []byte(context.body))
		put(n, context.notice, []byte(context.body))
		cat.Notices = append(cat.Notices, inputRecord{context.notice, digest([]byte(context.body))})
	}
	packages := mustTextRead(t, b, "packages.jsonl")
	var compiler []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(packages)), "\n") {
		var p map[string]any
		if e := json.Unmarshal([]byte(line), &p); e != nil {
			t.Fatal(e)
		}
		compiler = append(compiler, p)
	}
	for _, family := range []string{"line-awesome", "material-icons", "flag-icons", "translations"} {
		body := []byte("original " + family)
		if family == "translations" {
			body = []byte(`{"GREETING":{"HELLO":"Hello, fixture"}}`)
		}
		name := map[string]string{"line-awesome": "console/src/assets/icons/line-awesome/css/example.css", "material-icons": "package/dist/fonts/MaterialIcons-Regular.woff2", "flag-icons": "package/flags/4x3/dk.svg", "translations": "console/src/assets/i18n/en.json"}[family]
		out := map[string]string{"line-awesome": "assets/icons/line-awesome/css/example.css", "material-icons": "media/MaterialIcons-Regular-EXAMPLE.woff2", "flag-icons": "media/dk-EXAMPLE.svg", "translations": "assets/i18n/en.json"}[family]
		a := copiedAsset{Family: family, Path: name, Bytes: len(body), SHA256: digest(body), Outputs: []string{"console/dist/console/" + out, "internal/api/ui/console/static/" + out}}
		if family == "line-awesome" || family == "translations" {
			a.GitBlob = blob(body)
			if family == "line-awesome" {
				a.OriginalBlob = a.GitBlob
			}
			put(b, "prepared/source/"+name, body)
		} else {
			packageName, version := "flag-icons", "7.5.0"
			if family == "material-icons" {
				packageName, version = "material-design-icons-iconfont", "6.7.0"
			}
			metadata := []byte(fmt.Sprintf(`{"name":%q,"version":%q}`, packageName, version))
			resolved := "node_modules/.pnpm/" + packageName + "@" + version + "/node_modules/" + packageName
			put(b, "prepared/source/"+resolved+"/package.json", metadata)
			put(b, "prepared/source/"+resolved+"/LICENSE", terms)
			put(b, "prepared/source/"+resolved+"/"+strings.TrimPrefix(name, "package/"), body)
			if e := os.MkdirAll(filepath.Join(b, "prepared/source/console/node_modules"), 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink("../../"+resolved, filepath.Join(b, "prepared/source/console/node_modules/"+packageName)); e != nil {
				t.Fatal(e)
			}
			var compressed bytes.Buffer
			g := gzip.NewWriter(&compressed)
			tw := tar.NewWriter(g)
			for _, member := range []struct {
				name string
				body []byte
			}{{name, body}, {"package/LICENSE", terms}} {
				if e := tw.WriteHeader(&tar.Header{Name: member.name, Mode: 0644, Size: int64(len(member.body)), Typeflag: tar.TypeReg}); e != nil {
					t.Fatal(e)
				}
				if _, e := tw.Write(member.body); e != nil {
					t.Fatal(e)
				}
			}
			if e := tw.Close(); e != nil {
				t.Fatal(e)
			}
			if e := g.Close(); e != nil {
				t.Fatal(e)
			}
			archive := compressed.Bytes()
			sum := sha512.Sum512(archive)
			archivePath := "archives/" + digest(archive) + ".tgz"
			put(b, archivePath, archive)
			cat.Archives = append(cat.Archives, copiedArchive{Family: family, Path: archivePath, SHA256: digest(archive), Integrity: "sha512-" + base64.StdEncoding.EncodeToString(sum[:]), LicensePath: "package/LICENSE", Notice: "MIT.txt", PackageJSONSHA256: digest(metadata)})
		}
		cat.Assets = append(cat.Assets, a)
		for _, output := range a.Outputs {
			put(b, "prepared/source/"+output, body)
		}
		compiler[1]["EmbedFiles"] = append(compiler[1]["EmbedFiles"].([]any), "static/"+out)
		f, e := os.OpenFile(filepath.Join(b, "generated.sha256"), os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		for _, output := range a.Outputs {
			fmt.Fprintf(f, "%s  %s\n", a.SHA256, output)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
	}
	var stream bytes.Buffer
	for _, p := range compiler {
		raw, _ := json.Marshal(p)
		stream.Write(raw)
		stream.WriteByte('\n')
	}
	put(b, "packages.jsonl", stream.Bytes())
	// The pre-existing text collector records the icon-font bytes separately;
	// this batch adds the original grants rather than silently relabeling them.
	var fc textCatalogue
	json.Unmarshal(mustTextRead(t, fonts, "catalogue.json"), &fc)
	for _, output := range cat.Assets[1].Outputs {
		fc.DeferredIconOutputs = append(fc.DeferredIconOutputs, inputRecord{output, cat.Assets[1].SHA256})
	}
	fb, _ := json.Marshal(fc)
	put(fonts, "catalogue.json", fb)
	fontPolicy.CatalogueHash = digest(fb)
	put(b, "zitadel-copied-assets.go", []byte("selected copied-asset verifier"))
	encoded, _ := json.Marshal(cat)
	put(n, "catalogue.json", encoded)
	neutralPath := "console/src/fixture-provider.html"
	neutralBody := []byte(`<i class="neutral-provider-icon" aria-hidden="true"></i>`)
	put(b, "prepared/source/"+neutralPath, neutralBody)
	put(b, "prepared/source/console/src/fixture-smtp.html", []byte("fixed SMTP consumer"))
	return b, n, recipe, copiedPolicy{CatalogueHash: digest(encoded), Assets: 4, Outputs: 8, TextNotices: fonts, TextPolicy: fontPolicy, ProviderConsumers: []inputRecord{{neutralPath, digest(neutralBody)}}, SMTPConsumers: []inputRecord{{"console/src/fixture-smtp.html", digest([]byte("fixed SMTP consumer"))}}}
}

func TestCopiedAssetsRetainOriginalTermsAndExactCompilerCopies(t *testing.T) {
	b, n, r, p := copiedFixture(t)
	state, e := copiedObserve(b, n, r, readAt, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(state.Assets) != 4 || !bytes.Contains(state.Raw, []byte("Copyright fixture author")) || !bytes.Contains(state.Raw, []byte("Original complete MIT grant")) || !bytes.Contains(state.Raw, []byte("Complete fixture translation contribution context")) || bytes.Contains(state.Raw, []byte("Hello, fixture")) {
		t.Fatal("selected original attribution was lost")
	}
	if state.ImageAdmission != "unknown" || state.LicenseClosure != "unknown" {
		t.Fatal("asset attribution widened release authority")
	}
}

func TestCopiedAssetsRejectMissingChangedExtraAndUncompiledCopies(t *testing.T) {
	for _, mode := range []string{"missing", "changed", "extra", "uncompiled", "changed-original", "changed-grant"} {
		t.Run(mode, func(t *testing.T) {
			b, n, r, p := copiedFixture(t)
			name := filepath.Join(b, "prepared/source/internal/api/ui/console/static/assets/icons/line-awesome/css/example.css")
			switch mode {
			case "missing":
				os.Remove(name)
			case "changed":
				os.WriteFile(name, []byte("substituted output"), 0600)
			case "extra":
				os.WriteFile(filepath.Join(filepath.Dir(name), "unexpected.css"), []byte("extra"), 0600)
			case "uncompiled":
				raw := mustTextRead(t, b, "packages.jsonl")
				os.WriteFile(filepath.Join(b, "packages.jsonl"), bytes.ReplaceAll(raw, []byte("static/assets/icons/line-awesome/css/example.css"), []byte("static/unselected.css")), 0600)
			case "changed-original":
				os.WriteFile(filepath.Join(b, "prepared/source/console/src/assets/icons/line-awesome/css/example.css"), []byte("substituted original"), 0600)
			case "changed-grant":
				os.WriteFile(filepath.Join(n, "MIT.txt"), []byte("MIT label only"), 0600)
			}
			if _, e := copiedObserve(b, n, r, readAt, p); e == nil {
				t.Fatal("incomplete distribution accepted")
			}
		})
	}
}

func TestCopiedConnectedPackageBoundaryRejectsSubstitutions(t *testing.T) {
	for _, mode := range []string{"escape", "unclean-target", "linked-parent", "package-metadata", "connected-source", "alias-race"} {
		t.Run(mode, func(t *testing.T) {
			b, n, r, p := copiedFixture(t)
			alias := filepath.Join(b, "prepared/source/console/node_modules/flag-icons")
			resolved := filepath.Join(b, "prepared/source/node_modules/.pnpm/flag-icons@7.5.0/node_modules/flag-icons")
			reader := textReader(readAt)
			switch mode {
			case "escape":
				os.Remove(alias)
				os.Symlink("/tmp/outside", alias)
			case "unclean-target":
				os.Remove(alias)
				os.Symlink("../../missing/../node_modules/.pnpm/flag-icons@7.5.0/node_modules/flag-icons", alias)
			case "linked-parent":
				parent := filepath.Dir(alias)
				os.Rename(parent, parent+"-real")
				os.Symlink(parent+"-real", parent)
			case "package-metadata":
				os.WriteFile(filepath.Join(resolved, "package.json"), []byte(`{"name":"other","version":"7.5.0"}`), 0600)
			case "connected-source":
				os.WriteFile(filepath.Join(resolved, "flags/4x3/dk.svg"), []byte("substitution"), 0600)
			case "alias-race":
				reader = func(root, name string, limit int64) ([]byte, error) {
					body, e := readAt(root, name, limit)
					if strings.Contains(name, "flag-icons@7.5.0/") && strings.HasSuffix(name, "package.json") {
						os.Remove(alias)
						os.Symlink("/tmp/outside", alias)
					}
					return body, e
				}
			}
			if _, e := copiedObserve(b, n, r, reader, p); e == nil {
				t.Fatal("substituted connected package admitted")
			}
		})
	}
}

func copiedCapturedFixture(t *testing.T) (string, string, string, copiedPolicy) {
	t.Helper()
	b, n, r, p := copiedFixture(t)
	fs, e := textObserve(b, p.TextNotices, r, readAt, p.TextPolicy)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(fs)
	if e = os.WriteFile(filepath.Join(b, textBefore), before, 0600); e != nil {
		t.Fatal(e)
	}
	fi, raw, e := textCapture(b, p.TextNotices, r, readAt, p.TextPolicy)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.MarshalIndent(fi, "", "  ")
	encoded = append(encoded, '\n')
	if e = os.WriteFile(filepath.Join(b, "text-font-notices-inventory.json"), encoded, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(b, textRaw), raw, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := copiedObserve(b, n, r, readAt, p)
	if e != nil {
		t.Fatal(e)
	}
	before, _ = json.Marshal(s)
	if e = os.WriteFile(filepath.Join(b, "copied-assets.before.json"), before, 0600); e != nil {
		t.Fatal(e)
	}
	return b, n, r, p
}

func TestCopiedCaptureBindsCompletedSourceAndBinary(t *testing.T) {
	b, n, r, p := copiedCapturedFixture(t)
	inv, raw, e := copiedCapture(b, n, r, readAt, p)
	if e != nil {
		t.Fatal(e)
	}
	if inv.BinarySHA256 != digest([]byte("compiled binary")) || len(inv.Assets) != 4 || !bytes.Contains(raw, []byte("Original complete MIT grant")) {
		t.Fatal("capture lost original terms or final binary identity")
	}
	if inv.ImageAdmission != "unknown" || inv.LicenseClosure != "unknown" || inv.Reproducible {
		t.Fatal("capture widened release authority")
	}
}

func TestCopiedCaptureRejectsIncompleteFinalBindings(t *testing.T) {
	for _, mode := range []string{"snapshot", "generated", "font-inventory", "font-notice", "binary", "ambiguous-receipt"} {
		t.Run(mode, func(t *testing.T) {
			b, n, r, p := copiedCapturedFixture(t)
			switch mode {
			case "snapshot":
				os.WriteFile(filepath.Join(b, "copied-assets.before.json"), []byte(`{}`), 0600)
			case "generated":
				v := mustTextRead(t, b, "generated.sha256")
				v = bytes.ReplaceAll(v, []byte(digest([]byte("original line-awesome"))), []byte(strings.Repeat("0", 64)))
				os.WriteFile(filepath.Join(b, "generated.sha256"), v, 0600)
			case "font-inventory":
				os.WriteFile(filepath.Join(b, "text-font-notices-inventory.json"), []byte(`{}`), 0600)
			case "font-notice":
				os.WriteFile(filepath.Join(b, textRaw), []byte("replaced original notice"), 0600)
			case "binary":
				os.WriteFile(filepath.Join(b, "service/zitadel"), []byte("changed binary"), 0600)
			case "ambiguous-receipt":
				os.WriteFile(filepath.Join(b, "build-receipt.json"), mustTextRead(t, b, "build-receipt.pending"), 0600)
			}
			if _, _, e := copiedCapture(b, n, r, readAt, p); e == nil {
				t.Fatal("incomplete final asset binding accepted")
			}
		})
	}
}

func TestIconAttributionPreservesAvailableRecordsWithoutInventingOthers(t *testing.T) {
	font := textNameFixture([]testTextName{{0, "Copyright original icon author", nil}, {5, "Version 1", nil}})
	names, e := fontAttributionNames(font, false)
	if e != nil || len(names) != 2 || names[0].Text != "Copyright original icon author" || names[1].Text != "Version 1" {
		t.Fatal("original available icon attribution was lost", e)
	}
	if _, e = textFontNames(font); e == nil {
		t.Fatal("text-font completeness was weakened")
	}
}

func TestCopiedCompletedCLIRequiresExactSavedInventoryAndStaysReadOnly(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "malformed", "changed-binding", "extra-bytes", "symlink", "fifo"} {
		t.Run(mode, func(t *testing.T) {
			b, n, r, p := copiedCapturedFixture(t)
			previous := copiedFrozen
			copiedFrozen = p
			defer func() { copiedFrozen = previous }()
			args := []string{"--copied-assets", "--inventory", "--build", b, "--notices", n, "--recipe", r}
			var out, diagnostic bytes.Buffer
			if code := run(args, &out, &diagnostic); code != 0 {
				t.Fatalf("capture exit%d: %s", code, diagnostic.String())
			}
			name := filepath.Join(b, "copied-asset-notices-inventory.json")
			if e := os.WriteFile(name, out.Bytes(), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Rename(filepath.Join(b, "build-receipt.pending"), filepath.Join(b, "build-receipt.json")); e != nil {
				t.Fatal(e)
			}
			want := append([]byte{}, out.Bytes()...)
			switch mode {
			case "missing":
				os.Remove(name)
			case "malformed":
				os.WriteFile(name, []byte(`{"broken":`), 0600)
			case "changed-binding":
				os.WriteFile(name, bytes.ReplaceAll(want, []byte(digest([]byte("compiled binary"))), []byte(strings.Repeat("f", 64))), 0600)
			case "extra-bytes":
				os.WriteFile(name, append(want, '\n'), 0600)
			case "symlink":
				os.Remove(name)
				os.Symlink(filepath.Join(b, "copied-assets.before.json"), name)
			case "fifo":
				os.Remove(name)
				if e := syscall.Mkfifo(name, 0600); e != nil {
					t.Fatal(e)
				}
			}
			before := map[string][]byte{}
			for _, f := range []string{"copied-asset-notices.txt", "copied-assets.before.json", "build-receipt.json", "service/zitadel"} {
				before[f] = mustTextRead(t, b, f)
			}
			out.Reset()
			diagnostic.Reset()
			args[1] = "--verify"
			code := run(args, &out, &diagnostic)
			if mode == "valid" {
				if code != 0 || !bytes.Equal(out.Bytes(), want) {
					t.Fatalf("read-only verification differs: exit%d %s", code, diagnostic.String())
				}
			} else if code == 0 || out.Len() != 0 {
				t.Fatal("incomplete saved inventory emitted success")
			}
			for f, want := range before {
				if !bytes.Equal(mustTextRead(t, b, f), want) {
					t.Fatal("verification changed retained evidence", f)
				}
			}
		})
	}
}

func TestCopiedProviderGraphicsCannotReturnInSourceOrEitherCompilerCopy(t *testing.T) {
	cases := map[string]string{
		"console/src/assets/images/idp/google.png":                    "retired source",
		"console/src/assets/images/idp/extra.webp":                    "additional source",
		"internal/api/ui/login/static/resources/images/idp/extra.svg": "additional login source",
		"console/dist/console/assets/images/idp/google.png":           "retired output",
		"console/dist/console/assets/images/idp/extra.webp":           "additional output",
		"internal/api/ui/console/static/assets/images/idp/google.png": "retired embedded copy",
		"console/dist/console/provider.js":                            `const icon="./assets/images/idp/google.png";`,
		"internal/api/ui/console/static/provider.css":                 `.icon{background:url(./assets/images/idp/apple.svg)}`,
		"console/dist/console/escaped.js":                             `const icon="assets\/images\/idp\/google.png";`,
		"console/src/assets/images/smtp/google.png":                   "retired SMTP source",
		"console/src/assets/images/smtp/extra.webp":                   "additional SMTP source",
		"console/dist/console/assets/images/smtp/google.png":          "retired SMTP output",
		"internal/api/ui/console/static/assets/images/smtp/extra.svg": "additional SMTP embedded output",
		"console/dist/console/smtp.js":                                `const icon="./assets/images/smtp/google.png";`,
		"internal/api/ui/console/static/smtp.css":                     `.icon{background:url(./assets/images/smtp/outlook.svg)}`,
		"console/dist/console/smtp-escaped.js":                        `const icon="assets\/images\/smtp\/google.png";`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			build, notices, recipe, policy := copiedFixture(t)
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err != nil {
				t.Fatalf("baseline: %v", err)
			}
			put(t, filepath.Join(build, "prepared/source", name), []byte(body))
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err == nil {
				t.Fatal("retired provider image or URL omitted from output observation")
			}
		})
	}
}

func TestCopiedSMTPConsumersMustRemainBound(t *testing.T) {
	for _, attack := range []string{"missing", "changed"} {
		t.Run(attack, func(t *testing.T) {
			build, notices, recipe, policy := copiedFixture(t)
			name := filepath.Join(build, "prepared/source/console/src/fixture-smtp.html")
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err != nil {
				t.Fatal(err)
			}
			if attack == "missing" {
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(name, []byte("unbound SMTP defaults"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := copiedObserve(build, notices, recipe, readAt, policy); err == nil {
				t.Fatal("unexamined SMTP consumer accepted")
			}
		})
	}
}
