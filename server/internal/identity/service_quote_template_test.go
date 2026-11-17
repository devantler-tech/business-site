package identity

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// These presentation tests execute the production templates with explicit page
// data. They establish neither database lineage nor authority to share or accept.
func TestServiceQuoteTemplateOperatorFormsRequireExplicitService(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			page := serviceQuoteOperatorPage(lang)
			page.Generated, page.GeneratedOK = generatedProposal("website", "Fictional project", lang)
			doc, _ := serviceQuoteRender(t, "triage.html", page)
			var forms []*html.Node
			for _, form := range serviceQuoteElements(doc, "form") {
				if serviceQuoteAttr(form, "action") == "/portal/operator/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA/proposals" {
					forms = append(forms, form)
				}
			}
			if len(forms) != 2 {
				t.Fatalf("want generated and manual proposal forms, got %d", len(forms))
			}
			for i, form := range forms {
				controls := serviceQuoteControls(form)
				kinds := controls["service_kind"]
				if len(kinds) != 1 || kinds[0].Data != "select" || !serviceQuoteHasAttr(kinds[0], "required") {
					t.Fatalf("form %d cannot require an explicit service choice", i)
				}
				options := serviceQuoteElements(kinds[0], "option")
				if len(options) != 3 || serviceQuoteAttr(options[0], "value") != "" || serviceQuoteAttr(options[1], "value") != "software" || serviceQuoteAttr(options[2], "value") != "appointment" {
					t.Fatalf("form %d has no blank choice followed by the two supported services", i)
				}
				if serviceQuoteValue(kinds[0]) != "" {
					t.Fatalf("form %d classified the service before the operator chose it", i)
				}
				for _, name := range []string{"service_kind", "appointment_minutes", "appointment_terms_en", "appointment_terms_da"} {
					if len(controls[name]) != 1 {
						t.Fatalf("form %d must submit exactly one %s", i, name)
					}
					control := controls[name][0]
					id := serviceQuoteAttr(control, "id")
					labeled := false
					for _, label := range serviceQuoteElements(form, "label") {
						labeled = labeled || (id != "" && serviceQuoteAttr(label, "for") == id && strings.TrimSpace(serviceQuoteText(label)) != "")
					}
					if !labeled {
						t.Fatalf("form %d has an unlabeled %s", i, name)
					}
					if serviceQuoteValue(control) != "" {
						t.Fatalf("form %d invented a default for %s", i, name)
					}
				}
				minutes := controls["appointment_minutes"][0]
				if serviceQuoteAttr(minutes, "type") != "number" || serviceQuoteAttr(minutes, "min") != "1" || serviceQuoteAttr(minutes, "max") != "1440" || serviceQuoteAttr(minutes, "step") != "1" {
					t.Fatalf("form %d lost whole-minute representation bounds", i)
				}
				for _, name := range []string{"appointment_terms_en", "appointment_terms_da"} {
					if controls[name][0].Data != "textarea" || serviceQuoteAttr(controls[name][0], "data-character-limit") != "2000" {
						t.Fatalf("form %d cannot enter the agreed bilingual appointment terms", i)
					}
				}
				values := serviceQuoteFormValues(form)
				if i == 0 && (values.Get("scope") != page.Generated.Scope || values.Get("deliverables") != page.Generated.Deliverables || values.Get("one_time_dkk") != "6000" || values.Get("monthly_dkk") != "300") {
					t.Fatal("adding service controls changed the generated commercial suggestion")
				}
				// Supply only what an operator types, preserving the rendered field
				// names and generated commercial controls as the actual POST payload.
				if i == 1 {
					values.Set("scope", "Fictional agreed scope")
					values.Set("deliverables", "Fictional deliverable")
					values.Set("one_time_dkk", "6000")
					values.Set("monthly_dkk", "0")
					values.Set("one_time_terms", "Existing general terms")
					values.Set("monthly_terms", "No monthly service")
				}
				values.Set("service_kind", "appointment")
				values.Set("appointment_minutes", "45")
				values.Set("appointment_terms_en", "Agreed English appointment terms")
				values.Set("appointment_terms_da", "Aftalte danske vilkår for ydelsen")
				request := httptest.NewRequest("POST", serviceQuoteAttr(form, "action"), strings.NewReader(values.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				proposal, ok := parseProposal(request)
				if !ok || proposal.ContentFormat != 2 || proposal.Service.Kind != proposalServiceAppointment || proposal.Service.DurationMinutes != 45 || proposal.Service.AppointmentTermsEN != "Agreed English appointment terms" || proposal.Service.AppointmentTermsDA != "Aftalte danske vilkår for ydelsen" {
					t.Fatalf("form %d does not carry the explicit appointment to the native parser", i)
				}
			}
			ids := map[string]bool{}
			for _, node := range serviceQuoteElements(doc, "") {
				id := serviceQuoteAttr(node, "id")
				if id != "" && ids[id] {
					t.Fatalf("rendered page repeats control or section ID %q", id)
				}
				ids[id] = true
			}
		})
	}
}

func TestServiceQuoteTemplateAppointmentFactsRemainVisibleAndEscaped(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		for _, view := range []string{"operator-saved", "operator-receipt", "client-review", "client-receipt", "client-order-review"} {
			t.Run(lang+"/"+view, func(t *testing.T) {
				proposal := serviceQuotePresentation()
				var templateName string
				var data any
				switch view {
				case "operator-saved", "operator-receipt":
					page := serviceQuoteOperatorPage(lang)
					if view == "operator-saved" {
						page.Proposals = []projectProposal{proposal}
					} else {
						page.AcceptanceEnabled = true
						page.Acceptances = []proposalAcceptance{{Proposal: proposal, Current: true}}
					}
					templateName, data = "triage.html", page
				default:
					page := serviceQuoteClientPage(lang)
					if view == "client-review" {
						page.SharedProposal = &proposal
					} else if view == "client-receipt" {
						page.Acceptances = []proposalAcceptance{{Proposal: proposal, Current: true}}
					} else {
						page.BillingEnabled = true
						page.OrderAcceptance = "fictional-accepted-proposal"
						page.OrderProposal = &proposal
					}
					templateName, data = "requests.html", page
				}
				doc, body := serviceQuoteRender(t, templateName, data)
				text := serviceQuoteText(doc)
				wantKind, wantDuration, wantTerms, otherTerms := "Appointment", "45 minutes", proposal.Service.AppointmentTermsEN, proposal.Service.AppointmentTermsDA
				if lang == "da" {
					wantKind, wantDuration, wantTerms, otherTerms = "Ydelse med aftalt varighed", "45 minutter", proposal.Service.AppointmentTermsDA, proposal.Service.AppointmentTermsEN
				}
				for _, want := range []string{wantKind, wantDuration, wantTerms, proposal.OneTimeTerms} {
					if !strings.Contains(text, want) {
						t.Errorf("actual %s omits quoted fact %q", view, want)
					}
				}
				if strings.Contains(text, otherTerms) || strings.Contains(body, proposal.Service.AppointmentTermsSHA256) {
					t.Fatal("quote display mixed languages or exposed an internal terms hash")
				}
				for _, script := range serviceQuoteElements(doc, "script") {
					if serviceQuoteAttr(script, "src") == "" {
						t.Fatal("appointment terms became executable markup")
					}
				}
			})
		}
	}
}

func TestServiceQuoteTemplateLegacyNeverInfersAppointment(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		for _, format := range []int{0, 1} {
			proposal := serviceQuotePresentation()
			proposal.ContentFormat = format
			operator := serviceQuoteOperatorPage(lang)
			operator.Proposals = []projectProposal{proposal}
			client := serviceQuoteClientPage(lang)
			client.SharedProposal = &proposal
			for name, data := range map[string]any{"triage.html": operator, "requests.html": client} {
				doc, body := serviceQuoteRender(t, name, data)
				text := serviceQuoteText(doc)
				if !strings.Contains(text, proposal.Scope) || !strings.Contains(text, proposal.Deliverables) || !strings.Contains(text, proposal.OneTimeTerms) || !strings.Contains(text, "6000 DKK") {
					t.Fatalf("%s lost existing legacy quote content", name)
				}
				if strings.Contains(body, proposal.Service.AppointmentTermsSHA256) || strings.Contains(text, proposal.Service.AppointmentTermsEN) || strings.Contains(text, proposal.Service.AppointmentTermsDA) || strings.Contains(text, "45 minutes") || strings.Contains(text, "45 minutter") {
					t.Fatalf("%s assigned new appointment meaning to format %d", name, format)
				}
			}
		}
	}
}

func TestServiceQuoteTemplateSoftwareHasNoAppointmentFacts(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		proposal := serviceQuotePresentation()
		proposal.Service.Kind = proposalServiceSoftware
		client := serviceQuoteClientPage(lang)
		client.SharedProposal = &proposal
		doc, body := serviceQuoteRender(t, "requests.html", client)
		want := "Software work"
		if lang == "da" {
			want = "Softwarearbejde"
		}
		if !strings.Contains(serviceQuoteText(doc), want) {
			t.Fatal("typed software quote lost its explicit service type")
		}
		if strings.Contains(body, proposal.Service.AppointmentTermsSHA256) || strings.Contains(serviceQuoteText(doc), proposal.Service.AppointmentTermsEN) || strings.Contains(serviceQuoteText(doc), proposal.Service.AppointmentTermsDA) {
			t.Fatal("software quote exposed appointment-only content")
		}
	}
}

func serviceQuotePresentation() projectProposal {
	return projectProposal{
		ContentFormat: 2, Version: 1, Scope: "Fictional agreed scope", Deliverables: "Fictional deliverable", Currency: "DKK",
		OneTimeMinor: 600000, OneTimeTerms: "Unchanged general agreement terms", MonthlyTerms: "No monthly service",
		Service: proposalServiceTerms{Kind: proposalServiceAppointment, DurationMinutes: 45,
			AppointmentTermsEN: "English appointment <script>blocked()</script> & notes", AppointmentTermsDA: "Danske vilkår <script>blokeret()</script> & noter",
			AppointmentTermsSHA256: "never-expose-the-internal-appointment-terms-hash"},
		Prepared: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func serviceQuoteOperatorPage(lang string) triagePageData {
	request := httptest.NewRequest("GET", "/portal/operator/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA?lang="+lang, nil)
	page := new(browser).triagePage(request, []projectRequest{{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA", Product: "website", Title: "Fictional project"}}, "AAAAAAAAAAAAAAAAAAAAAAAAAA", "")
	page.ProposalsEnabled, page.ProposalKey, page.CSRF = true, "BBBBBBBBBBBBBBBBBBBBBBBBBB", "fictional-csrf"
	return page
}

func serviceQuoteClientPage(lang string) requestPageData {
	request := httptest.NewRequest("GET", "/portal/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA?lang="+lang, nil)
	return new(browser).requestPage(request, "review", projectRequest{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA", Product: "website", Title: "Fictional project"}, nil, "")
}

func serviceQuoteRender(t *testing.T, name string, data any) (*html.Node, string) {
	t.Helper()
	var output bytes.Buffer
	if err := pages("/portal", "").ExecuteTemplate(&output, name, data); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(output.String()))
	if err != nil {
		t.Fatal(err)
	}
	return doc, output.String()
}

func serviceQuoteElements(node *html.Node, tag string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && (tag == "" || node.Data == tag) {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return found
}

func serviceQuoteAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func serviceQuoteHasAttr(node *html.Node, name string) bool {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return true
		}
	}
	return false
}

func serviceQuoteText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func serviceQuoteControls(form *html.Node) map[string][]*html.Node {
	controls := map[string][]*html.Node{}
	for _, node := range serviceQuoteElements(form, "") {
		if name := serviceQuoteAttr(node, "name"); name != "" && (node.Data == "input" || node.Data == "select" || node.Data == "textarea") {
			controls[name] = append(controls[name], node)
		}
	}
	return controls
}

func serviceQuoteValue(control *html.Node) string {
	if control.Data == "textarea" {
		return serviceQuoteText(control)
	}
	if control.Data == "select" {
		options := serviceQuoteElements(control, "option")
		for _, option := range options {
			if serviceQuoteHasAttr(option, "selected") {
				return serviceQuoteAttr(option, "value")
			}
		}
		if len(options) > 0 {
			return serviceQuoteAttr(options[0], "value")
		}
	}
	return serviceQuoteAttr(control, "value")
}

func serviceQuoteFormValues(form *html.Node) url.Values {
	values := url.Values{}
	for name, controls := range serviceQuoteControls(form) {
		for _, control := range controls {
			values.Add(name, serviceQuoteValue(control))
		}
	}
	return values
}
