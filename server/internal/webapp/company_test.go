package webapp

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const companyTemplate = `<html><body><address data-company-address data-pagefind-ignore><span>__BUSINESS_ADDRESS_LINE_1__</span><span>__BUSINESS_ADDRESS_LINE_2__</span></address></body></html>`

func companyFixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte("home")}, "da/index.html": {Data: []byte("dansk")},
		"company/index.html": {Data: []byte(companyTemplate)}, "da/company/index.html": {Data: []byte(companyTemplate)},
	}
}

func TestDisclosureIsEscapedConfinedImmutableAndDigestBound(t *testing.T) {
	source := companyFixture()
	artifact, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	served, err := artifact.WithCompanyDisclosure("  Example & <script> Road\r\n1234 Exampletown  \r\n")
	if err != nil {
		t.Fatal(err)
	}
	if served.Digest() == artifact.Digest() || !artifact.HasCompanySlots() || served.HasCompanySlots() {
		t.Fatal("template and served identities were conflated")
	}
	for _, route := range []string{"/company/", "/da/company/"} {
		response := httptest.NewRecorder()
		served.Handler(nil).ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), "Example &amp; &lt;script&gt; Road") || strings.Contains(response.Body.String(), "<script>") {
			t.Fatal("the actual served company page is missing escaped disclosure")
		}
	}
	for _, route := range []string{"/", "/da/"} {
		response := httptest.NewRecorder()
		served.Handler(nil).ServeHTTP(response, httptest.NewRequest("GET", route, nil))
		if response.Code != 200 || strings.Contains(response.Body.String(), "Example") {
			t.Fatal("address escaped its required pages")
		}
	}
	// A second projection cannot change the first immutable serving snapshot.
	other, err := artifact.WithCompanyDisclosure("Other Road\n4321 Elsewhere")
	if err != nil || other.Digest() == served.Digest() {
		t.Fatal("projection change was not rebound")
	}
	if !strings.Contains(string(artifact.files["company/index.html"]), "__BUSINESS_ADDRESS_LINE_1__") {
		t.Fatal("signed source bytes were mutated")
	}
	copyFS := make(fstest.MapFS, len(served.files))
	for name, data := range served.files {
		copyFS[name] = &fstest.MapFile{Data: data, Mode: fs.FileMode(0600)}
	}
	checked, err := Load(copyFS)
	if err != nil || checked.Digest() != served.Digest() {
		t.Fatal("effective digest differs from the real public artifact algorithm")
	}
}

func TestDisclosureRejectsMalformedInputAndSlots(t *testing.T) {
	artifact, err := Load(companyFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "one line", "one\ntwo\nthree", "one\r\ntw\ro", "one\ntwo\u200b", "one\ntwo\t", "one\n" + strings.Repeat("😀", 81), "one\n__BUSINESS_ADDRESS_LINE_2__", "one\n\xff"} {
		if _, err := artifact.WithCompanyDisclosure(input); err == nil || err != ErrArtifact {
			t.Fatal("invalid disclosure accepted or input exposed")
		}
	}
	for _, mode := range []string{"missing-page", "missing-slot", "duplicate", "attribute", "script", "outside-address", "outside-page"} {
		source := companyFixture()
		switch mode {
		case "missing-page":
			delete(source, "da/company/index.html")
		case "missing-slot":
			source["company/index.html"].Data = []byte(strings.ReplaceAll(companyTemplate, "__BUSINESS_ADDRESS_LINE_2__", ""))
		case "duplicate":
			source["company/index.html"].Data = []byte(companyTemplate + "__BUSINESS_ADDRESS_LINE_1__")
		case "attribute":
			source["company/index.html"].Data = []byte(strings.ReplaceAll(companyTemplate, "<span>__BUSINESS_ADDRESS_LINE_1__</span>", `<span title="__BUSINESS_ADDRESS_LINE_1__">other</span>`))
		case "script":
			source["company/index.html"].Data = []byte(strings.ReplaceAll(companyTemplate, "<span>__BUSINESS_ADDRESS_LINE_1__</span>", "<script>__BUSINESS_ADDRESS_LINE_1__</script>"))
		case "outside-address":
			source["company/index.html"].Data = []byte(strings.ReplaceAll(companyTemplate, "address", "div"))
		case "outside-page":
			source["index.html"].Data = []byte("__BUSINESS_ADDRESS_LINE_1__")
		}
		bad, err := Load(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bad.WithCompanyDisclosure("Example Road\n1234 Exampletown"); err != ErrArtifact {
			t.Fatalf("malformed slot accepted: %s", mode)
		}
	}
}
