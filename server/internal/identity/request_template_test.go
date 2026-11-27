package identity

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestRequestTemplateSelection exercises the actual response renderer. The
// raw-template lint exception must never leak fields into rendered attributes
// or turn false boolean attributes into selected options.
func TestRequestTemplateSelection(t *testing.T) {
	options := regexp.MustCompile(`<option value="([^"]+)"([^>]*)>`)
	for _, lang := range []string{"en", "da"} {
		for _, product := range []string{"website", "app", "service", "other"} {
			t.Run(lang+"/"+product, func(t *testing.T) {
				response := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/portal/requests/new?lang="+lang, nil)
				new(browser).renderRequests(response, request, 400, "new", projectRequest{Product: product}, nil, "invalid")
				body := response.Body.String()
				if response.Code != 400 || !strings.Contains(body, `<html lang="`+lang+`">`) || strings.Contains(body, ".Request.Product") || strings.Contains(body, "{{") {
					t.Fatal("actual request renderer leaked template fields or lost presentation state")
				}
				start := strings.Index(body, `<select id="request-product"`)
				if start < 0 {
					t.Fatal("rendered offering selector missing")
				}
				end := strings.Index(body[start:], "</select>")
				if end < 0 {
					t.Fatal("rendered offering selector is incomplete")
				}
				matches := options.FindAllStringSubmatch(body[start:start+end], -1)
				if len(matches) != 4 {
					t.Fatal("rendered offering options changed")
				}
				selected := 0
				for _, option := range matches {
					attributes := strings.TrimSpace(option[2])
					if attributes == "selected" {
						selected++
						if option[1] != product {
							t.Fatal("renderer selected a different offering")
						}
					} else if attributes != "" {
						t.Fatal("renderer emitted an unexpected offering attribute")
					}
				}
				if selected != 1 {
					t.Fatal("renderer did not select exactly one offering")
				}
			})
		}
	}
}

func TestRequestTemplateShowsTheCurrentReceiptStage(t *testing.T) {
	proposal := projectProposal{Version: 2}
	current := proposalAcceptance{Current: true, Proposal: proposal}
	historical := proposalAcceptance{Proposal: projectProposal{Version: 1}}
	withdrawn := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, en, da string
		shared       *projectProposal
		acceptances  []proposalAcceptance
		order        *projectOrder
	}{
		{name: "received", en: "Request received", da: "Forespørgsel modtaget"},
		{name: "history-only", en: "Request received", da: "Forespørgsel modtaget", acceptances: []proposalAcceptance{historical}},
		{name: "ready-for-review", en: "Proposal ready for review", da: "Tilbud klar til gennemgang", shared: &proposal, acceptances: []proposalAcceptance{historical}},
		{name: "accepted", en: "Proposal accepted", da: "Tilbud accepteret", shared: &proposal, acceptances: []proposalAcceptance{historical, current}},
		{name: "ordered", en: "Order recorded", da: "Bestilling registreret", shared: &proposal, acceptances: []proposalAcceptance{current}, order: &projectOrder{}},
		{name: "withdrawn", en: "Order withdrawn", da: "Fortrydelse registreret", shared: &proposal, acceptances: []proposalAcceptance{current}, order: &projectOrder{Withdrawn: &withdrawn}},
	} {
		for _, lang := range []string{"en", "da"} {
			t.Run(tc.name+"/"+lang, func(t *testing.T) {
				b := new(browser)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/portal/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ?lang="+lang, nil)
				request := projectRequest{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Product: "website", Title: "Fictional project"}
				page := b.requestPage(r, "receipt", request, nil, "")
				page.SharedProposal, page.Acceptances, page.Order = tc.shared, tc.acceptances, tc.order
				page.BillingEnabled = page.Order != nil
				if err := b.templates().ExecuteTemplate(w, "requests.html", page); err != nil {
					t.Fatal(err)
				}
				want, proposalHeading := tc.en, "Proposal details"
				if lang == "da" {
					want = tc.da
					proposalHeading = "Tilbud"
				}
				body := w.Body.String()
				if strings.Count(body, "<h2>"+want+"</h2>") != 1 {
					t.Errorf("request page does not have a single heading %q for the current stage", want)
				}
				if tc.shared != nil && !strings.Contains(body, `<h2 id="shared-proposal">`+proposalHeading+"</h2>") {
					t.Error("the proposal section should identify the details independently of the stage")
				}
			})
		}
	}
}
