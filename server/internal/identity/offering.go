package identity

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"regexp"
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

// launchOffer is the published launch discount: percent off the one-time
// price of an order that includes hosting, for the first limit customers.
type launchOffer struct {
	Percent         int64             `json:"percent"`
	Limit           int               `json:"limit"`
	AppliesTo       string            `json:"appliesTo"`
	RequiresHosting bool              `json:"requiresHosting"`
	Text            map[string]string `json:"text"`
}

// addonText is an add-on's published title and description in one language.
type addonText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// addonOffer is a published standard add-on with a fixed VAT-inclusive price
// in whole kroner: kind "one-time" or "monthly" (monthly ones need hosting),
// for the listed offerings, up to Max units (default 1). Tier names a
// feature-request priority tier; an order has at most one.
type addonOffer struct {
	ID        string               `json:"id"`
	Kind      string               `json:"kind"`
	Price     int64                `json:"price"`
	Offerings []string             `json:"offerings"`
	Max       int                  `json:"max,omitempty"`
	Tier      string               `json:"tier,omitempty"`
	Text      map[string]addonText `json:"text"`
}

type offeringCatalogue struct {
	Offerings      []publishedOffering `json:"offerings"`
	Hosting        map[string]string   `json:"hosting"`
	VAT            map[string]string   `json:"vat"`
	LaunchDiscount *launchOffer        `json:"launchDiscount"`
	Consultation   map[string]string   `json:"consultation"`
	Addons         []addonOffer        `json:"addons"`
}

var offerings = mustOfferings(offeringsJSON)

func mustOfferings(raw []byte) offeringCatalogue {
	var c offeringCatalogue
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		panic("identity: invalid embedded offerings: " + err.Error())
	}
	// The portal implements only this rule; a catalogue that says otherwise
	// must not change what orders cost without a code change.
	if d := c.LaunchDiscount; d != nil && (d.Percent < 1 || d.Percent > 99 || d.Limit < 1 || d.AppliesTo != "setup" || !d.RequiresHosting || d.Text["en"] == "" || d.Text["da"] == "") {
		panic("identity: unsupported launch discount in embedded offerings")
	}
	if !validAddons(c) {
		panic("identity: invalid add-ons in embedded offerings")
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

// validAddons admits only add-ons the portal can sell: unique lowercase ids,
// a known kind, a positive whole-krone price, existing offerings, a unit limit
// of at most 10, a tier only on monthly add-ons, and both languages.
func validAddons(c offeringCatalogue) bool {
	offered := map[string]bool{}
	for _, o := range c.Offerings {
		offered[o.ID] = true
	}
	seen := map[string]bool{}
	for _, a := range c.Addons {
		if !addonID.MatchString(a.ID) || seen[a.ID] || (a.Kind != "one-time" && a.Kind != "monthly") || a.Price < 1 || a.Price > 100000 ||
			len(a.Offerings) == 0 || a.Max < 0 || a.Max > 10 || (a.Tier != "" && (a.Kind != "monthly" || (a.Tier != "priority" && a.Tier != "express"))) {
			return false
		}
		seen[a.ID] = true
		for _, o := range a.Offerings {
			if !offered[o] {
				return false
			}
		}
		for _, lang := range []string{"en", "da"} {
			if a.Text[lang].Title == "" || a.Text[lang].Description == "" {
				return false
			}
		}
	}
	return true
}

var addonID = regexp.MustCompile(`^[a-z][a-z-]{1,30}$`)
