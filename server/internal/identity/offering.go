package identity

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"strings"
)

// offerings.json mirrors the published offerings and VAT-inclusive prices
// (src/components/business, #154). scripts/proposal-offerings.test.mjs fails
// when the two differ, so a generated quote always matches the public site.
//
//go:embed offerings.json
var offeringsJSON []byte

type offeringText struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Includes    []string `json:"includes"`
	Boundary    string   `json:"boundary"`
}

type publishedOffering struct {
	ID         string                  `json:"id"`
	SetupDKK   int64                   `json:"setup"`
	MonthlyDKK int64                   `json:"monthly"`
	Text       map[string]offeringText `json:"text"`
}

type offeringCatalogue struct {
	Offerings []publishedOffering `json:"offerings"`
	Hosting   map[string]string   `json:"hosting"`
	VAT       map[string]string   `json:"vat"`
}

var offerings = mustOfferings(offeringsJSON)

func mustOfferings(raw []byte) offeringCatalogue {
	var c offeringCatalogue
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		panic("identity: invalid embedded offerings: " + err.Error())
	}
	return c
}

// generatedProposal drafts a proposal version from the request's published
// offering, in the operator's page language (ADR 0030, #552). Prices are the
// published VAT-inclusive totals in minor units. A request outside the published
// offerings has no generated version: its scope and price are set by hand.
// The draft is only a starting value; the operator still saves and shares it.
func generatedProposal(product, title, lang string) (projectProposal, bool) {
	if lang != "da" {
		lang = "en"
	}
	for _, o := range offerings.Offerings {
		if o.ID != product {
			continue
		}
		t, vat := o.Text[lang], offerings.VAT[lang]
		return projectProposal{
			Scope:        t.Title + ": " + strings.TrimSpace(title) + "\n\n" + t.Description,
			Deliverables: "- " + strings.Join(t.Includes, "\n- "),
			Currency:     "DKK",
			OneTimeMinor: o.SetupDKK * 100,
			MonthlyMinor: o.MonthlyDKK * 100,
			OneTimeTerms: t.Boundary + " " + vat,
			MonthlyTerms: offerings.Hosting[lang] + " " + vat,
		}, true
	}
	return projectProposal{}, false
}
