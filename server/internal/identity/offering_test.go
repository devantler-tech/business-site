package identity

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestProposalGenerationUsesPublishedPrices proves each published offering
// yields a version the ordinary proposal form accepts unchanged, at the exact
// VAT-inclusive guide prices, and that other requests are never priced.
func TestProposalGenerationUsesPublishedPrices(t *testing.T) {
	want := map[string][2]int64{"website": {600000, 30000}, "app": {1500000, 80000}, "service": {950000, 50000}}
	for product, prices := range want {
		for _, lang := range []string{"en", "da"} {
			d, ok := generatedProposal(product, "  Fictional bakery  ", lang)
			if !ok || d.OneTimeMinor != prices[0] || d.MonthlyMinor != prices[1] || d.Currency != "DKK" {
				t.Fatalf("%s/%s generated %+v", product, lang, d)
			}
			if !strings.Contains(d.Scope, ": Fictional bakery\n\n") || !strings.HasPrefix(d.Deliverables, "- ") {
				t.Fatalf("%s/%s scope or deliverables lost the request: %q", product, lang, d.Scope)
			}
			form := url.Values{"submission_key": {"AAAAAAAAAAAAAAAAAAAAAAAAAA"}, "expected_version": {"0"}, "currency": {d.Currency},
				"scope": {d.Scope}, "deliverables": {d.Deliverables}, "one_time_terms": {d.OneTimeTerms}, "monthly_terms": {d.MonthlyTerms},
				"one_time_dkk": {strconv.FormatInt(d.OneTimeDKK(), 10)}, "monthly_dkk": {strconv.FormatInt(d.MonthlyDKK(), 10)}}
			r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			parsed, valid := parseProposal(r)
			if !valid || parsed.Scope != d.Scope || parsed.OneTimeMinor != d.OneTimeMinor || parsed.MonthlyMinor != d.MonthlyMinor || parsed.MonthlyTerms != d.MonthlyTerms {
				t.Fatalf("%s/%s generated version is not a valid proposal form", product, lang)
			}
		}
	}
	if da, _ := generatedProposal("website", "x", "da"); !strings.Contains(da.OneTimeTerms, "inkl. 25 % moms") {
		t.Fatal("Danish version lacks the Danish VAT note")
	}
	if en, _ := generatedProposal("website", "x", "fr"); !strings.Contains(en.OneTimeTerms, "25% Danish VAT") {
		t.Fatal("unknown language did not fall back to English")
	}
	if da, _ := generatedProposal("website", "x", "da"); !strings.Contains(da.OneTimeTerms, "Den første samtale er gratis.") {
		t.Fatal("Danish version lacks the free first consultation")
	}
	if en, _ := generatedProposal("website", "x", "en"); !strings.Contains(en.OneTimeTerms, "The first consultation is free.") {
		t.Fatal("English version lacks the free first consultation")
	}
	if offerings.LaunchDiscount.Percent != 25 || offerings.LaunchDiscount.Limit != 50 || offerings.LaunchDiscount.AppliesTo != "setup" || !offerings.LaunchDiscount.RequiresHosting {
		t.Fatalf("launch discount drifted: %+v", offerings.LaunchDiscount)
	}
	for _, product := range []string{"other", "", "Website"} {
		if _, ok := generatedProposal(product, "x", "en"); ok {
			t.Fatalf("%q was priced without a published offering", product)
		}
	}
}

// TestProposalGeneratedVersionSavesWithoutTyping submits the operator page's
// own generated-draft form: no scope or price is typed, and the saved version
// carries the published price for the request's offering.
func TestProposalGeneratedVersionSavesWithoutTyping(t *testing.T) {
	b := proposalFixture(t)
	section := regexp.MustCompile(`(?s)<section class="generated-proposal" data-proposal-source="generated".*?</section>`)
	hidden := regexp.MustCompile(`<input type="hidden" name="([a-z_]+)" value="([^"]*)">`)
	for _, lang := range []string{"en", "da"} {
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang="+lang, nil, "")
		block := section.FindString(body)
		if status != 200 || block == "" || !strings.Contains(block, "15000 DKK") || !strings.Contains(block, "800 DKK") {
			t.Fatalf("%s detail did not show the generated draft: %d", lang, status)
		}
		f := url.Values{}
		for _, m := range hidden.FindAllStringSubmatch(block, -1) {
			f.Add(m[1], html.UnescapeString(m[2]))
		}
		if len(f) != 11 || f.Get("scope") == "" || f.Get("one_time_dkk") != "15000" || f.Get("monthly_dkk") != "800" {
			t.Fatalf("%s generated form is incomplete: %v", lang, len(f))
		}
		if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", f, b.site.URL); status != http.StatusSeeOther {
			t.Fatalf("%s generated version was refused: %d", lang, status)
		}
	}
	var scope, deliverables, terms string
	var setup, monthly int64
	if err := b.pool.QueryRow(b.f.ctx, `SELECT scope,deliverables,one_time_terms,one_time_minor,monthly_minor FROM portal_proposals WHERE request_id=$1 AND version=2`, proposalRequestID).Scan(&scope, &deliverables, &terms, &setup, &monthly); err != nil {
		t.Fatal(err)
	}
	if scope != "En webapp: Fictional proposal request\n\nErstat en tilbagevendende manuel opgave med et værktøj til din måde at arbejde på." || !strings.Contains(deliverables, "\n- ") || setup != 1500000 || monthly != 80000 || !strings.HasSuffix(terms, "Priserne er inkl. 25 % moms.") {
		t.Fatalf("saved generated version differs: %q %d %d", scope, setup, monthly)
	}
	if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES('BBBBBBBBBBBBBBBBBBBBBBBBBB','private-client-key','BBBBBBBBBBBBBBBBBBBBBBBBBB','other','Fictional other request','Something else')`); err != nil {
		t.Fatal(err)
	}
	status, body, _ := b.request("GET", "/portal/operator/requests/BBBBBBBBBBBBBBBBBBBBBBBBBB", nil, "")
	if status != 200 || strings.Contains(body, "generated-proposal") || !strings.Contains(body, `name="one_time_dkk" type="number"`) {
		t.Fatal("a request outside the published offerings was given a generated price")
	}
}
