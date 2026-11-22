package webapp

import (
	"strings"
	"testing"
	"testing/fstest"
)

func orderTermsFixture() fstest.MapFS {
	source := companyFixture()
	source["terms/index.html"] = &fstest.MapFile{Data: []byte(`<html><body><nav>Navigation must stay out</nav><main data-legal-page="terms"><h1>Project terms</h1><section><h2>Withdrawal form</h2><p>Keep this complete agreement.</p><ul><li>To: Devantler Tech, [postal address on the company information page], ned@devantler.tech</li><li>I/We withdraw from the following service:</li></ul><p><a href="https://example.invalid/help">Complaints help</a></p></section></main></body></html>`)}
	source["da/terms/index.html"] = &fstest.MapFile{Data: []byte(`<main data-legal-page="terms"><h1>Handelsbetingelser</h1><h2>Standardfortrydelsesformular</h2><p>Gem hele denne aftale.</p><ul><li>Til: Devantler Tech, [postadressen under virksomhedsoplysninger], ned@devantler.tech</li><li>Jeg/vi ønsker at fortryde følgende tjenesteydelse:</li></ul></main>`)}
	return source
}

// A mutable URL, missing paragraphs or unfilled disclosure must never become
// the customer's supposedly complete durable agreement.
func TestOrderTermsSnapshotIncludesFullTextAndPrivateDisclosure(t *testing.T) {
	source := orderTermsFixture()
	artifact, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	served, err := artifact.WithCompanyDisclosure("Example & Sample Road 1\n1234 Exampletown")
	if err != nil {
		t.Fatal(err)
	}
	terms, err := served.OrderTerms()
	if err != nil {
		t.Fatal("complete synthetic agreement refused")
	}
	for i, wants := range [][]string{
		{"Project terms", "Withdrawal form", "Keep this complete agreement.", "To: Devantler Tech, Example & Sample Road 1, 1234 Exampletown, ned@devantler.tech", "I/We withdraw from the following service:", "https://example.invalid/help"},
		{"Handelsbetingelser", "Standardfortrydelsesformular", "Gem hele denne aftale.", "Til: Devantler Tech, Example & Sample Road 1, 1234 Exampletown, ned@devantler.tech", "Jeg/vi ønsker at fortryde følgende tjenesteydelse:"},
	} {
		for _, want := range wants {
			if !strings.Contains(terms[i], want) {
				t.Error("agreement omitted required synthetic content")
			}
		}
		for _, forbidden := range []string{"Navigation must stay out", "__BUSINESS_ADDRESS", "[postal address", "[postadressen", "<main", "&amp;"} {
			if strings.Contains(terms[i], forbidden) {
				t.Error("agreement leaked navigation, markup or unfinished disclosure")
			}
		}
	}
	// Changes to mutable source files cannot replace an admitted agreement.
	source["terms/index.html"].Data = []byte("replacement")
	again, err := served.OrderTerms()
	if err != nil || again != terms {
		t.Fatal("agreement did not retain the immutable public snapshot")
	}
}

func TestOrderTermsRefuseIncompleteOrAmbiguousSnapshot(t *testing.T) {
	for _, mode := range []string{"missing-terms", "duplicate-terms", "wrong-kind", "empty", "missing-address", "duplicate-address", "unfilled-address", "active-content", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			source := orderTermsFixture()
			switch mode {
			case "missing-terms":
				delete(source, "da/terms/index.html")
			case "duplicate-terms":
				source["terms/index.html"].Data = append(source["terms/index.html"].Data, []byte(`<main data-legal-page="terms">Other terms</main>`)...)
			case "wrong-kind":
				source["terms/index.html"].Data = []byte(`<main data-legal-page="privacy">Wrong document</main>`)
			case "empty":
				source["terms/index.html"].Data = []byte(`<main data-legal-page="terms"></main>`)
			case "missing-address":
				delete(source, "da/company/index.html")
			case "duplicate-address":
				source["company/index.html"].Data = []byte(`<address data-company-address>First</address><address data-company-address>Second</address>`)
			case "active-content":
				source["terms/index.html"].Data = []byte(`<main data-legal-page="terms"><script>Do not send this</script></main>`)
			case "oversized":
				source["terms/index.html"].Data = []byte(`<main data-legal-page="terms">` + strings.Repeat("long ", 20000) + `</main>`)
			}
			if mode != "unfilled-address" {
				for _, path := range []string{"company/index.html", "da/company/index.html"} {
					if page := source[path]; page != nil {
						page.Data = []byte(strings.NewReplacer("__BUSINESS_ADDRESS_LINE_1__", "Example Road", "__BUSINESS_ADDRESS_LINE_2__", "1234 Exampletown").Replace(string(page.Data)))
					}
				}
			}
			artifact, err := Load(source)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := artifact.OrderTerms(); err != ErrArtifact || got != [2]string{} {
				t.Fatal("incomplete agreement was accepted or private partial content returned")
			}
		})
	}
}
