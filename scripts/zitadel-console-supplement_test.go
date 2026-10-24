package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type supplementFixture struct {
	noticeFixture
	notices, catalogueHash string
	aliases, roots         map[string]string
	text                   []byte
}

func newSupplementFixture(t *testing.T) supplementFixture {
	t.Helper()
	f := supplementFixture{noticeFixture: newNoticeFixture(t), notices: t.TempDir(), aliases: map[string]string{}, roots: map[string]string{}}
	names := []string{"qrcode", "@ng-icons/core", "grpc-web", "@connectrpc/connect", "@connectrpc/connect-web", "@bufbuild/protobuf", "google-protobuf", "angularx-qrcode"}
	versions := []string{"1.5.4", "33.4.0", "2.0.2", "2.1.2", "2.1.2", "2.13.0", "4.0.2", "21.0.5"}
	packages := []any{}
	source := filepath.Join(f.build, "prepared/source")
	for i, name := range names {
		root := "node_modules/.pnpm/" + strings.ReplaceAll(name, "/", "+") + "@" + versions[i] + "_fixture-peer/node_modules/" + name
		f.roots[name] = root
		metadata := map[string]any{"name": name, "version": versions[i]}
		if name == "angularx-qrcode" {
			metadata["dependencies"] = map[string]string{"qrcode": "1.5.4"}
		}
		data := noticeJSON(t, metadata)
		writeNotice(t, source, root+"/package.json", data)
		text := []byte("complete fixture terms for " + name + "\r\n")
		installed := []byte("fixture compiled source\n" + string(text))
		writeNotice(t, source, root+"/dist/selected.js", installed)
		file := strings.ReplaceAll(strings.TrimPrefix(name, "@"), "/", "-") + ".txt"
		writeNotice(t, f.notices, file, text)
		components := []any{}
		if name != "angularx-qrcode" {
			components = append(components, map[string]any{"role": "license", "kind": "installed", "original": "dist/selected.js", "originalBytes": len(installed), "originalSha256": noticeHash(installed), "offset": len("fixture compiled source\n"), "bytes": len(text), "sha256": noticeHash(text), "retained": file})
			f.text = append(f.text, text...)
		}
		packages = append(packages, map[string]any{"name": name, "version": versions[i], "sri": "sha512-fixture-identity", "tarball": "fixture.tgz", "packageJsonSha256": noticeHash(data), "components": components})
	}
	for _, name := range names {
		alias := "console/node_modules/" + name
		if name == "qrcode" {
			alias = filepath.ToSlash(filepath.Join(filepath.Dir(f.roots["angularx-qrcode"]), "qrcode"))
		}
		f.aliases[name] = alias
		absolute := filepath.Join(source, filepath.FromSlash(alias))
		if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
			t.Fatal(err)
		}
		target, err := filepath.Rel(filepath.Dir(absolute), filepath.Join(source, filepath.FromSlash(f.roots[name])))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(target, absolute); err != nil {
			t.Fatal(err)
		}
	}
	lock, _ := os.ReadFile(filepath.Join(source, "pnpm-lock.yaml"))
	catalogue := noticeJSON(t, map[string]any{"schemaVersion": 1, "upstreamCommit": noticePin, "lockSha256": noticeHash(lock), "ngIconsProvenance": "fixture only", "packages": packages})
	writeNotice(t, f.notices, "catalogue.json", catalogue)
	f.catalogueHash = noticeHash(catalogue)
	addSourceMapSubject(t, &f)
	return f
}

func TestSupplementRetainsCheckedTextAndTransitivePackageEdge(t *testing.T) {
	f := newSupplementFixture(t)
	state, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, noticeRead)
	if err != nil {
		t.Fatal(err)
	}
	if state.CatalogueSHA256 != f.catalogueHash || len(state.Inputs) != 30 || len(state.Aliases) != 9 || len(state.Sections) != 9 || len(state.Text) == 0 {
		t.Fatal("missing supplement bindings")
	}
	for _, name := range []string{"qrcode", "@bufbuild/protobuf", "google-protobuf"} {
		if !bytes.Contains(state.Text, []byte("complete fixture terms for "+name+"\r\n")) {
			t.Fatalf("lost verbatim terms for %s", name)
		}
	}
	// A hoisted unrelated package must not replace angularx-qrcode's selected edge.
	writeNotice(t, filepath.Join(f.build, "prepared/source"), "console/node_modules/qrcode/package.json", []byte("unrelated hoisted package"))
	again, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, noticeRead)
	if err != nil || !bytes.Equal(noticeJSON(t, state), noticeJSON(t, again)) {
		t.Fatalf("hoisted qrcode substituted for selected edge: %v", err)
	}
}

func TestSupplementRefusesIncompleteSubstitutedAndEscapingInputs(t *testing.T) {
	for _, kind := range []string{"missing-text", "truncated-text", "changed-text", "changed-package", "changed-version", "changed-source", "changed-lock", "linked-text", "linked-package-leaf", "escaping-alias", "dangling-alias", "cyclic-alias", "linked-parent", "changed-parent-edge", "changed-catalogue", "duplicate-catalogue-key", "partial-read-error"} {
		t.Run(kind, func(t *testing.T) {
			f := newSupplementFixture(t)
			source := filepath.Join(f.build, "prepared/source")
			reader := noticeReader(noticeRead)
			switch kind {
			case "missing-text":
				if err := os.Remove(filepath.Join(f.notices, "qrcode.txt")); err != nil {
					t.Fatal(err)
				}
			case "truncated-text":
				writeNotice(t, f.notices, "qrcode.txt", []byte("short"))
			case "changed-text":
				writeNotice(t, f.notices, "qrcode.txt", []byte("complete fixture terms for other!\r\n"))
			case "changed-package":
				writeNotice(t, source, f.roots["qrcode"]+"/package.json", []byte(`{"name":"qrcode","version":"1.5.4","substituted":true}`))
			case "changed-version":
				writeNotice(t, source, f.roots["qrcode"]+"/package.json", []byte(`{"name":"qrcode","version":"1.5.5"}`))
			case "changed-source":
				writeNotice(t, source, f.roots["@bufbuild/protobuf"]+"/dist/selected.js", []byte("different compiled source"))
			case "changed-lock":
				writeNotice(t, source, "pnpm-lock.yaml", []byte("changed lock"))
			case "linked-text":
				p := filepath.Join(f.notices, "qrcode.txt")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.notices, "grpc-web.txt"), p); err != nil {
					t.Fatal(err)
				}
			case "linked-package-leaf":
				p := filepath.Join(source, f.roots["qrcode"], "package.json")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("package.json.real", p); err != nil {
					t.Fatal(err)
				}
			case "escaping-alias", "dangling-alias", "cyclic-alias":
				p := filepath.Join(source, f.aliases["grpc-web"])
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				target := t.TempDir()
				if kind == "dangling-alias" {
					target = "missing"
				}
				if kind == "cyclic-alias" {
					target = "grpc-web"
				}
				if err := os.Symlink(target, p); err != nil {
					t.Fatal(err)
				}
			case "linked-parent":
				p := filepath.Join(source, "console/node_modules/@ng-icons")
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("@ng-icons.real", p); err != nil {
					t.Fatal(err)
				}
			case "changed-parent-edge":
				writeNotice(t, source, f.roots["angularx-qrcode"]+"/package.json", []byte(`{"name":"angularx-qrcode","version":"21.0.5","dependencies":{"qrcode":"other"}}`))
			case "changed-catalogue":
				writeNotice(t, f.notices, "catalogue.json", []byte(`{}`))
			case "duplicate-catalogue-key":
				b, _ := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
				writeNotice(t, f.notices, "catalogue.json", append([]byte(`{"schemaVersion":1,`), b[1:]...))
				f.catalogueHash = noticeHash(append([]byte(`{"schemaVersion":1,`), b[1:]...))
			case "partial-read-error":
				reader = func(root, relative string, limit int64) ([]byte, error) {
					b, e := noticeRead(root, relative, limit)
					if relative == "qrcode.txt" {
						return b, errors.New("failure after plausible text")
					}
					return b, e
				}
			}
			if _, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, reader); err == nil {
				t.Fatalf("%s admitted", kind)
			}
		})
	}
}

func TestSupplementOutputUsesCheckedReads(t *testing.T) {
	f := newSupplementFixture(t)
	reads := 0
	reader := func(root, relative string, limit int64) ([]byte, error) {
		b, e := noticeRead(root, relative, limit)
		if root == f.notices && relative == "qrcode.txt" {
			reads++
			writeNotice(t, root, relative, []byte("unchecked replacement"))
		}
		return b, e
	}
	state, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, reader)
	if err != nil || reads != 1 || !bytes.Contains(state.Text, []byte("complete fixture terms for qrcode\r\n")) {
		t.Fatalf("unchecked reread: %v", err)
	}
}

func TestSupplementCaptureRejectsPostCompileReplacement(t *testing.T) {
	for _, kind := range []string{"alias", "terms-and-catalogue", "metadata-and-catalogue", "other-build-notice"} {
		t.Run(kind, func(t *testing.T) {
			f := newSupplementFixture(t)
			config := noticeSupplementConfig{Directory: f.notices, CatalogueHash: f.catalogueHash}
			state, err := noticeSnapshot(f.build, f.recipe, noticeRead, config)
			if err != nil {
				t.Fatal(err)
			}
			writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
			source := filepath.Join(f.build, "prepared/source")
			switch kind {
			case "alias":
				p := filepath.Join(source, f.aliases["grpc-web"])
				link, e := os.Readlink(p)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Remove(p); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink("./"+link, p); e != nil {
					t.Fatal(e)
				}
			case "terms-and-catalogue", "metadata-and-catalogue":
				b, _ := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
				var c map[string]any
				if err = json.Unmarshal(b, &c); err != nil {
					t.Fatal(err)
				}
				p := c["packages"].([]any)[0].(map[string]any)
				if kind == "metadata-and-catalogue" {
					meta := []byte(`{"name":"qrcode","version":"1.5.4","different":true}`)
					writeNotice(t, source, f.roots["qrcode"]+"/package.json", meta)
					p["packageJsonSha256"] = noticeHash(meta)
				} else {
					text := []byte("coherently changed after compilation\n")
					writeNotice(t, f.notices, "qrcode.txt", text)
					writeNotice(t, source, f.roots["qrcode"]+"/dist/selected.js", text)
					component := p["components"].([]any)[0].(map[string]any)
					component["bytes"] = len(text)
					component["sha256"] = noticeHash(text)
					component["originalBytes"] = len(text)
					component["originalSha256"] = noticeHash(text)
					component["offset"] = 0
				}
				b = noticeJSON(t, c)
				writeNotice(t, f.notices, "catalogue.json", b)
				config.CatalogueHash = noticeHash(b)
			case "other-build-notice":
				writeNotice(t, f.build, "prepared/source/"+noticeConsole, []byte("other build\n"))
				writeNotice(t, f.build, "prepared/source/"+noticeEmbedded, []byte("other build\n"))
			}
			if _, _, err = noticeCapture(f.build, f.recipe, noticeRead, config); err == nil || !strings.Contains(err.Error(), "compile snapshot") {
				t.Fatalf("%s substitution admitted: %v", kind, err)
			}
		})
	}
}

func TestSupplementCommandBindsExactInventoryAndNeverOverwrites(t *testing.T) {
	f := newSupplementFixture(t)
	var out, diagnostic bytes.Buffer
	args := []string{"--snapshot", "--build", f.build, "--recipe", f.recipe, "--supplement", f.notices}
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatalf("snapshot: %s", diagnostic.String())
	}
	writeNotice(t, f.build, noticeBefore, out.Bytes())
	out.Reset()
	diagnostic.Reset()
	args[0] = "--inventory"
	if rc := noticeRunWithCatalogue(args, &out, &diagnostic, f.catalogueHash); rc != 0 {
		t.Fatalf("capture: %s", diagnostic.String())
	}
	data, err := os.ReadFile(filepath.Join(f.build, supplementInventoryPending))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Notice    string              `json:"noticeInventorySha256"`
		Hash      string              `json:"sha256"`
		Bytes     int                 `json:"bytes"`
		Sections  []supplementSection `json:"sections"`
		License   string              `json:"licenseClosure"`
		Admission string              `json:"imageAdmission"`
	}
	if err = json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(f.build, supplementRaw))
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Notice != noticeHash(out.Bytes()) || inventory.Hash != noticeHash(text) || inventory.Bytes != len(text) || inventory.License != "unknown" || inventory.Admission != "unknown" {
		t.Fatal("wrong same-build inventory binding")
	}
	for _, section := range inventory.Sections {
		if section.Offset < 0 || section.Bytes < 1 || section.Offset+section.Bytes > len(text) || noticeHash(text[section.Offset:section.Offset+section.Bytes]) != section.SHA256 {
			t.Fatal("section lost exact bytes")
		}
	}
	if err = noticeWriteDiagnostic(f.build, supplementRaw, []byte("overwrite")); err == nil {
		t.Fatal("overwrote supplement")
	}
	again, _ := os.ReadFile(filepath.Join(f.build, supplementRaw))
	if !bytes.Equal(text, again) {
		t.Fatal("existing supplement changed")
	}
	// A synthetic fixture hash cannot be selected by the real command.
	if rc := noticeRun(args, &out, &diagnostic); rc == 0 {
		t.Fatal("public command accepted an unreviewed catalogue")
	}
}

func TestSupplementPinnedTextsIncludeBothProtobufLicenses(t *testing.T) {
	directory := filepath.Join("..", "third_party", "zitadel-console-notices")
	data, err := noticeRead(directory, "catalogue.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if noticeHash(data) != supplementCatalogueHash {
		t.Fatal("reviewed catalogue changed")
	}
	var catalogue supplementCatalogue
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Packages) != 9 || catalogue.Packages[8].Name != "opentype.js" || catalogue.Packages[8].Version != "1.3.5" {
		t.Fatal("selected opentype subject missing")
	}
	for _, pkg := range catalogue.Packages {
		roles := map[string]bool{}
		for _, component := range pkg.Components {
			text, e := noticeRead(directory, component.Retained, noticeLimit)
			if e != nil || len(text) != component.Bytes || noticeHash(text) != component.SHA256 {
				t.Fatalf("changed retained component %s/%s: %v", pkg.Name, component.Role, e)
			}
			roles[component.Role] = true
		}
		if pkg.Name == "qrcode" && (len(pkg.Components) != 1 || pkg.Components[0].Original != "license") {
			t.Fatal("lowercase qrcode license lost")
		}
		if pkg.Name == "@bufbuild/protobuf" && (!roles["apache-license"] || !roles["google-bsd-license"] || !roles["buf-attribution"]) {
			t.Fatal("protobuf-es lost a license component")
		}
		if pkg.Name == "google-protobuf" && (!roles["bsd-license"] || !roles["apache-license"] || !roles["closure-attribution"]) {
			t.Fatal("google-protobuf lost a license component")
		}
		if pkg.Name == "opentype.js" {
			if len(pkg.Components) != 2 || !roles["root-license"] || !roles["tiny-inflate-license"] || len(pkg.IdentityOnlyInputs) != 2 {
				t.Fatal("opentype lost root/embedded terms or complete bundle identities")
			}
			component := pkg.Components[1]
			if component.Kind != "installed-source-map" || component.SourceMap == nil || component.SourceMap.Index == nil || *component.SourceMap.Index != 0 || component.SourceMap.Source != "../src/tiny-inflate@1.0.3.esm.mjs" || component.SourceMap.Bytes != 11040 || component.SourceMap.SHA256 != "961d49e9aa0b5823e235e5667e0384919291bfac9788f7aff2c72a515e3dc8f7" || component.Bytes != 1084 || component.Offset != 0 {
				t.Fatal("source-map curation identity changed")
			}
		}
	}
}

func TestSupplementImmutableSourceVariantsAreExplicit(t *testing.T) {
	for _, kind := range []string{"complete", "header", "wrong-full-digest", "wrong-full-size", "wrong-header-span", "wrong-source", "unknown-kind"} {
		t.Run(kind, func(t *testing.T) {
			f := newSupplementFixture(t)
			data, _ := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
			var catalogue map[string]any
			if err := json.Unmarshal(data, &catalogue); err != nil {
				t.Fatal(err)
			}
			component := catalogue["packages"].([]any)[2].(map[string]any)["components"].([]any)[0].(map[string]any)
			text, _ := os.ReadFile(filepath.Join(f.notices, "grpc-web.txt"))
			component["kind"] = "immutable-source"
			component["offset"] = 0
			component["original"] = "https://raw.githubusercontent.com/grpc/grpc-web/ea2d8dae34e8cc19df2c1836a5e590a6e30bcb87/LICENSE"
			component["originalBytes"] = len(text)
			component["originalSha256"] = noticeHash(text)
			switch kind {
			case "header", "wrong-header-span":
				component["kind"] = "immutable-source-header"
				component["originalBytes"] = len(text) + 100
				component["originalSha256"] = noticeHash([]byte("curation-only full upstream source identity"))
				if kind == "wrong-header-span" {
					component["offset"] = len(text) + 101
				}
			case "wrong-full-digest":
				component["originalSha256"] = strings.Repeat("0", 64)
			case "wrong-full-size":
				component["originalBytes"] = len(text) + 1
			case "wrong-source":
				component["original"] = "http://unapproved.invalid/LICENSE"
			case "unknown-kind":
				component["kind"] = "silently-generalized"
			}
			data = noticeJSON(t, catalogue)
			writeNotice(t, f.notices, "catalogue.json", data)
			_, err := noticeSupplementSnapshot(f.build, f.notices, noticeHash(data), noticeRead)
			valid := kind == "complete" || kind == "header"
			if valid && err != nil {
				t.Fatalf("valid %s refused: %v", kind, err)
			}
			if !valid && err == nil {
				t.Fatalf("invalid %s admitted", kind)
			}
		})
	}
}

// addSourceMapSubject keeps the original eight fixture identities intact and
// adds one real escaped JSON source map, not a stub of the map decoder.
func addSourceMapSubject(t *testing.T, f *supplementFixture) {
	t.Helper()
	root := "node_modules/.pnpm/opentype.js@1.3.5_fixture-peer/node_modules/opentype.js"
	f.roots["opentype.js"] = root
	source := filepath.Join(f.build, "prepared/source")
	metadata := noticeJSON(t, map[string]any{"name": "opentype.js", "version": "1.3.5"})
	writeNotice(t, source, root+"/package.json", metadata)
	license := []byte("complete fixture root terms for opentype.js\r\n")
	text := []byte("/* fixture MIT notice: quotes \"kept\", slash \\, multibyte å\r\nall terms retained */\r\n")
	decoded := append(append([]byte(nil), text...), []byte("unselected source tail\n")...)
	mapBytes := noticeJSON(t, map[string]any{"version": 3, "sources": []string{"../src/tiny-inflate@1.0.3.esm.mjs", "other.js"}, "sourcesContent": []any{string(decoded), "unrelated"}, "names": []string{}, "mappings": ""})
	writeNotice(t, source, root+"/LICENSE", license)
	writeNotice(t, source, root+"/dist/opentype.min.mjs.map", mapBytes)
	writeNotice(t, f.notices, "opentype-license.txt", license)
	writeNotice(t, f.notices, "tiny-inflate-license.txt", text)
	components := []any{
		map[string]any{"role": "root-license", "kind": "installed", "original": "LICENSE", "originalBytes": len(license), "originalSha256": noticeHash(license), "offset": 0, "bytes": len(license), "sha256": noticeHash(license), "retained": "opentype-license.txt"},
		map[string]any{"role": "tiny-inflate-license", "kind": "installed-source-map", "original": "dist/opentype.min.mjs.map", "originalBytes": len(mapBytes), "originalSha256": noticeHash(mapBytes), "offset": 0, "bytes": len(text), "sha256": noticeHash(text), "retained": "tiny-inflate-license.txt", "sourceMap": map[string]any{"index": 0, "source": "../src/tiny-inflate@1.0.3.esm.mjs", "bytes": len(decoded), "sha256": noticeHash(decoded)}},
	}
	identities := []any{}
	for _, path := range []string{"dist/opentype.mjs", "dist/opentype.js"} {
		bundle := []byte("fixture complete selected bundle: " + path + "\n")
		writeNotice(t, source, root+"/"+path, bundle)
		identities = append(identities, map[string]any{"path": path, "bytes": len(bundle), "sha256": noticeHash(bundle)})
	}
	pkg := map[string]any{"name": "opentype.js", "version": "1.3.5", "sri": "sha512-fixture-identity", "tarball": "fixture.tgz", "packageJsonSha256": noticeHash(metadata), "components": components, "identityOnlyInputs": identities}
	data, err := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue map[string]any
	if err = json.Unmarshal(data, &catalogue); err != nil {
		t.Fatal(err)
	}
	catalogue["packages"] = append(catalogue["packages"].([]any), pkg)
	data = noticeJSON(t, catalogue)
	writeNotice(t, f.notices, "catalogue.json", data)
	f.catalogueHash = noticeHash(data)
	alias := "console/node_modules/opentype.js"
	f.aliases["opentype.js"] = alias
	absolute := filepath.Join(source, alias)
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		t.Fatal(err)
	}
	target, err := filepath.Rel(filepath.Dir(absolute), filepath.Join(source, root))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, absolute); err != nil {
		t.Fatal(err)
	}
}

func TestSupplementRetainsTheDecodedSourceMapNotice(t *testing.T) {
	f := newSupplementFixture(t)
	state, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, noticeRead)
	if err != nil {
		t.Fatalf("complete selected source-map component refused: %v", err)
	}
	text, err := os.ReadFile(filepath.Join(f.notices, "tiny-inflate-license.txt"))
	if err != nil || !bytes.Contains(state.Text, text) || len(state.Aliases) != 9 || len(state.Sections) != 9 || len(state.Inputs) != 30 {
		t.Fatal("lost decoded bytes, original subjects or exact-input bindings")
	}
	section := state.Sections[len(state.Sections)-1]
	if section.Package != "opentype.js" || section.Role != "tiny-inflate-license" || !bytes.Equal(state.Text[section.Offset:section.Offset+section.Bytes], text) {
		t.Fatal("source-map section does not retain exact decoded text")
	}
}

func TestSupplementSourceMapRejectsCoherentlyChangedInputs(t *testing.T) {
	for _, kind := range []string{"decoded-tail", "duplicate-source", "misaligned", "selected-null", "selected-object", "wrong-version", "malformed", "trailing", "duplicate-key", "invalid-utf8", "missing-descriptor", "missing-index", "wrong-index", "wrong-span", "descriptor-on-installed"} {
		t.Run(kind, func(t *testing.T) {
			f := newSupplementFixture(t)
			catalogueBytes, err := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
			if err != nil {
				t.Fatal(err)
			}
			var catalogue map[string]any
			if err = json.Unmarshal(catalogueBytes, &catalogue); err != nil {
				t.Fatal(err)
			}
			pkg := catalogue["packages"].([]any)[8].(map[string]any)
			component := pkg["components"].([]any)[1].(map[string]any)
			selection := component["sourceMap"].(map[string]any)
			mapPath := f.roots["opentype.js"] + "/dist/opentype.min.mjs.map"
			source := filepath.Join(f.build, "prepared/source")
			data, err := os.ReadFile(filepath.Join(source, mapPath))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err = json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "decoded-tail":
				document["sourcesContent"].([]any)[0] = document["sourcesContent"].([]any)[0].(string) + "changed outside retained span"
			case "duplicate-source":
				document["sources"] = append(document["sources"].([]any), selection["source"])
				document["sourcesContent"] = append(document["sourcesContent"].([]any), "duplicate selected content")
			case "misaligned":
				document["sourcesContent"] = []any{}
			case "selected-null":
				document["sourcesContent"].([]any)[0] = nil
			case "selected-object":
				document["sourcesContent"].([]any)[0] = map[string]any{"not": "source text"}
			case "wrong-version":
				document["version"] = 2
			case "missing-descriptor":
				delete(component, "sourceMap")
			case "missing-index":
				delete(selection, "index")
			case "wrong-index":
				selection["index"] = 1
			case "wrong-span":
				component["offset"] = 1
			case "descriptor-on-installed":
				pkg["components"].([]any)[0].(map[string]any)["sourceMap"] = selection
			}
			data = noticeJSON(t, document)
			switch kind {
			case "malformed":
				data = []byte("{")
			case "trailing":
				data = append(data, []byte("{}")...)
			case "duplicate-key":
				data = append([]byte(`{"version":3,`), data[1:]...)
			case "invalid-utf8":
				data = append(data, 0xff)
			}
			writeNotice(t, source, mapPath, data)
			// Rebind the complete map and catalogue. These failures must reach
			// selection/decoded-source validation, not only the outer hash guard.
			component["originalBytes"] = len(data)
			component["originalSha256"] = noticeHash(data)
			catalogueBytes = noticeJSON(t, catalogue)
			writeNotice(t, f.notices, "catalogue.json", catalogueBytes)
			if _, err = noticeSupplementSnapshot(f.build, f.notices, noticeHash(catalogueBytes), noticeRead); err == nil {
				t.Fatalf("admitted coherently rebound %s", kind)
			}
		})
	}
}

func TestSupplementSourceMapRejectsUnavailableAndChangedFiles(t *testing.T) {
	for _, kind := range []string{"missing-map", "truncated-map", "changed-map", "changed-module", "changed-browser", "changed-package", "changed-text", "linked-map", "read-error"} {
		t.Run(kind, func(t *testing.T) {
			f := newSupplementFixture(t)
			source := filepath.Join(f.build, "prepared/source")
			root := f.roots["opentype.js"]
			mapPath := root + "/dist/opentype.min.mjs.map"
			reader := noticeReader(noticeRead)
			switch kind {
			case "missing-map":
				if err := os.Remove(filepath.Join(source, mapPath)); err != nil {
					t.Fatal(err)
				}
			case "truncated-map", "changed-map":
				writeNotice(t, source, mapPath, []byte(`{"version":3}`))
			case "changed-module":
				writeNotice(t, source, root+"/dist/opentype.mjs", []byte("substitute bundle"))
			case "changed-browser":
				writeNotice(t, source, root+"/dist/opentype.js", []byte("substitute browser bundle"))
			case "changed-package":
				writeNotice(t, source, root+"/package.json", []byte(`{"name":"opentype.js","version":"1.3.6"}`))
			case "changed-text":
				writeNotice(t, f.notices, "tiny-inflate-license.txt", []byte("substitute terms"))
			case "linked-map":
				p := filepath.Join(source, mapPath)
				if err := os.Rename(p, p+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("opentype.min.mjs.map.real", p); err != nil {
					t.Fatal(err)
				}
			case "read-error":
				reader = func(directory, relative string, limit int64) ([]byte, error) {
					data, err := noticeRead(directory, relative, limit)
					if relative == mapPath {
						return data, errors.New("failure after plausible complete map")
					}
					return data, err
				}
			}
			if _, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, reader); err == nil {
				t.Fatalf("admitted %s", kind)
			}
		})
	}
}

func TestSupplementSourceMapSnapshotRejectsCoherentMapReplacement(t *testing.T) {
	f := newSupplementFixture(t)
	config := noticeSupplementConfig{Directory: f.notices, CatalogueHash: f.catalogueHash}
	state, err := noticeSnapshot(f.build, f.recipe, noticeRead, config)
	if err != nil {
		t.Fatal(err)
	}
	writeNotice(t, f.build, noticeBefore, noticeJSON(t, state))
	source := filepath.Join(f.build, "prepared/source")
	mapPath := f.roots["opentype.js"] + "/dist/opentype.min.mjs.map"
	data, err := os.ReadFile(filepath.Join(source, mapPath))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["names"] = []string{"coherently changed map metadata"}
	data = noticeJSON(t, document)
	writeNotice(t, source, mapPath, data)
	catalogueBytes, err := os.ReadFile(filepath.Join(f.notices, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalogue map[string]any
	if err = json.Unmarshal(catalogueBytes, &catalogue); err != nil {
		t.Fatal(err)
	}
	component := catalogue["packages"].([]any)[8].(map[string]any)["components"].([]any)[1].(map[string]any)
	component["originalBytes"] = len(data)
	component["originalSha256"] = noticeHash(data)
	catalogueBytes = noticeJSON(t, catalogue)
	writeNotice(t, f.notices, "catalogue.json", catalogueBytes)
	config.CatalogueHash = noticeHash(catalogueBytes)
	if _, _, err = noticeCapture(f.build, f.recipe, noticeRead, config); err == nil || !strings.Contains(err.Error(), "compile snapshot") {
		t.Fatalf("coherent source-map substitution admitted: %v", err)
	}
}

func TestSupplementSourceMapUsesTheCheckedMapBytes(t *testing.T) {
	f := newSupplementFixture(t)
	mapPath := f.roots["opentype.js"] + "/dist/opentype.min.mjs.map"
	reads := 0
	reader := func(directory, relative string, limit int64) ([]byte, error) {
		data, err := noticeRead(directory, relative, limit)
		if relative == mapPath {
			reads++
			writeNotice(t, directory, relative, []byte("unchecked replacement"))
		}
		return data, err
	}
	state, err := noticeSupplementSnapshot(f.build, f.notices, f.catalogueHash, reader)
	if err != nil || reads != 1 || !bytes.Contains(state.Text, []byte("fixture MIT notice")) || bytes.Contains(state.Text, []byte("unchecked replacement")) {
		t.Fatalf("unchecked map reread: %v", err)
	}
}

func FuzzSupplementMappedSource(f *testing.F) {
	index := 0
	text := "complete fixture text\r\nquotes \" and multibyte å"
	selection := &supplementSourceMap{Index: &index, Source: "logical-source", Bytes: len(text), SHA256: noticeHash([]byte(text))}
	valid, err := json.Marshal(map[string]any{"version": 3, "sources": []string{"logical-source"}, "sourcesContent": []string{text}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"version":3,"sources":[],"sourcesContent":[]}`))
	f.Add([]byte(`{"version":3,"version":3}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16384 {
			return
		}
		got, err := supplementMappedSource(data, selection)
		if err == nil && !bytes.Equal(got, []byte(text)) {
			t.Fatal("accepted a different decoded source")
		}
	})
}
