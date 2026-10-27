package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This miniature distribution exercises original archives, actual filesystem
// copies and compiler observations. Native CI separately exercises the frozen
// 1,836-member distribution; these are deliberately not native-font fixtures.
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
	packages := mustTextRead(t, b, "packages.jsonl")
	var compiler []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(packages)), "\n") {
		var p map[string]any
		if e := json.Unmarshal([]byte(line), &p); e != nil {
			t.Fatal(e)
		}
		compiler = append(compiler, p)
	}
	for _, family := range []string{"line-awesome", "material-icons", "flag-icons"} {
		body := []byte("original " + family)
		name := map[string]string{"line-awesome": "console/src/assets/icons/line-awesome/css/example.css", "material-icons": "package/dist/fonts/MaterialIcons-Regular.woff2", "flag-icons": "package/flags/4x3/dk.svg"}[family]
		out := map[string]string{"line-awesome": "assets/icons/line-awesome/css/example.css", "material-icons": "media/MaterialIcons-Regular-EXAMPLE.woff2", "flag-icons": "media/dk-EXAMPLE.svg"}[family]
		a := copiedAsset{Family: family, Path: name, Bytes: len(body), SHA256: digest(body), Outputs: []string{"console/dist/console/" + out, "internal/api/ui/console/static/" + out}}
		if family == "line-awesome" {
			a.GitBlob = blob(body)
			a.OriginalBlob = a.GitBlob
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
	return b, n, recipe, copiedPolicy{CatalogueHash: digest(encoded), Assets: 3, Outputs: 6, TextNotices: fonts, TextPolicy: fontPolicy}
}

func TestCopiedAssetsRetainOriginalTermsAndExactCompilerCopies(t *testing.T) {
	b, n, r, p := copiedFixture(t)
	state, e := copiedObserve(b, n, r, readAt, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(state.Assets) != 3 || !bytes.Contains(state.Raw, []byte("Copyright fixture author")) || !bytes.Contains(state.Raw, []byte("Original complete MIT grant")) {
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
	if inv.BinarySHA256 != digest([]byte("compiled binary")) || len(inv.Assets) != 3 || !bytes.Contains(raw, []byte("Original complete MIT grant")) {
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
