package identity

import (
	"slices"
	"strings"
	"testing"
)

// This independent literal specifies the entire ordinary request-form main.
// It never calls the application renderer or edits a rendered document to make
// an expectation. The unchanged 26 non-form states retain their frozen bodies.
const ordinaryRequestFormExpectation = `<main id="main"><div class="intro"><h1>@TITLE@</h1>
<p>@INTRO@</p></div>
<section aria-label="@TITLE@">
@ERROR@

<form class="contact-form request-form" method="post" action="/portal/requests">
<input type="hidden" name="csrf_token" value=""><input type="hidden" name="submission_key" value="fictional-submission"><input type="hidden" name="lang" value="@LANG@">
<label for="request-product">@QUESTION@</label>
<select id="request-product" name="product" required aria-describedby="request-assessment">@OPTIONS@</select>
<p id="request-assessment">@ASSESSMENT@</p>
<label for="request-title">@PROJECT@</label><input id="request-title" name="title" value="Fictional request" maxlength="240" data-character-limit="120" required aria-describedby="title-limit"><p id="title-limit" class="field-limit">@TITLE_LIMIT@</p>
<label for="request-brief">@GOAL@</label><textarea id="request-brief" name="brief" rows="7" maxlength="2400" data-character-limit="1200" required aria-describedby="brief-limit request-privacy">Synthetic link-validation content</textarea><p id="brief-limit" class="field-limit">@BRIEF_LIMIT@</p>
<p id="request-privacy">@PRIVACY@</p>
<button type="submit">@SEND@</button>
</form>

</section><a class="back" href="/portal/requests?lang=@LANG@">@REQUESTS@</a><br><a class="back" href="/portal/?lang=@LANG@">@BACK@</a></main>`

func ordinaryRequestExpectation(lang, selected string, invalid bool) string {
	words := []string{
		"@LANG@", "en", "@TITLE@", "Your project requests", "@INTRO@", "Tell us what you would like to build. Sending a request is free and does not start paid work.",
		"@QUESTION@", "What would you like to build?", "@ASSESSMENT@", "For other requests, we first assess whether we can help. Any work, price and timeline need a separate agreement.",
		"@PROJECT@", "Project title", "@TITLE_LIMIT@", "A short title (up to 120 characters).", "@GOAL@", "What should the project help with?", "@BRIEF_LIMIT@", "Describe the goal and needs briefly (up to 1,200 characters).",
		"@PRIVACY@", "Do not include passwords, payment details or sensitive personal information.", "@SEND@", "Send request", "@REQUESTS@", "Your requests", "@BACK@", "Back to your workspace",
	}
	labels := []string{"Website", "Web app", "Service or integration", "Other requests"}
	problem := "The request was not saved. Complete all fields within the shown limits and try again."
	if lang == "da" {
		words = []string{
			"@LANG@", "da", "@TITLE@", "Dine projektforespørgsler", "@INTRO@", "Fortæl os, hvad du gerne vil bygge. Det er gratis at sende en forespørgsel og starter ikke betalt arbejde.",
			"@QUESTION@", "Hvad vil du bygge?", "@ASSESSMENT@", "Ved andre opgaver vurderer vi først, om vi kan hjælpe. Arbejde, pris og tidsplan kræver en særskilt aftale.",
			"@PROJECT@", "Projektets navn", "@TITLE_LIMIT@", "En kort titel (højst 120 tegn).", "@GOAL@", "Hvad skal projektet hjælpe med?", "@BRIEF_LIMIT@", "Beskriv mål og behov kort (højst 1.200 tegn).",
			"@PRIVACY@", "Medtag ikke adgangskoder, betalingsoplysninger eller følsomme personoplysninger.", "@SEND@", "Send forespørgsel", "@REQUESTS@", "Dine forespørgsler", "@BACK@", "Tilbage til dit arbejdsområde",
		}
		labels = []string{"Hjemmeside", "Webapp", "Service eller integration", "Andre opgaver"}
		problem = "Forespørgslen blev ikke gemt. Udfyld alle felter inden for de viste grænser, og prøv igen."
	}
	options := ""
	for i, kind := range []string{"website", "app", "service", "other"} {
		selection := ""
		if kind == selected {
			selection = "selected"
		}
		options += `<option value="` + kind + `" ` + selection + `>` + labels[i] + `</option>`
	}
	alert := ""
	if invalid {
		alert = `<p role="alert" class="form-error">` + problem + `</p>`
	}
	words = append(words, "@OPTIONS@", options, "@ERROR@", alert)
	return strings.NewReplacer(words...).Replace(ordinaryRequestFormExpectation)
}

var maintenanceOrdinaryRequestDigests = ordinaryRequestReferences()

func ordinaryRequestReferences() []string {
	// Explicit preservation of the 26 unchanged states is not a fallback: the
	// complete ordinary table is required by every inventory consumer.
	original := maintenanceReferenceDigests["requests.html"]
	if len(original) != 38 {
		return nil
	}
	result := slices.Clone(original)
	for l, lang := range []string{"en", "da"} {
		for p, product := range []string{"website", "app", "service"} {
			for v := range 2 {
				digest, err := maintenanceMainDigest(ordinaryRequestExpectation(lang, product, v == 1), false)
				if err != nil {
					return nil
				}
				result[l*19+p*4+v] = digest
			}
		}
	}
	return result
}

func TestContactMaintenanceRequestReferenceBoundary(t *testing.T) {
	original := maintenanceOrdinaryRequestDigests
	if len(original) != 38 || slices.Equal(original, maintenanceReferenceDigests["requests.html"]) {
		t.Fatal("independent complete ordinary request references required")
	}
	pages, err := renderedMaintenancePages("requests.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { maintenanceOrdinaryRequestDigests = original }()
	for _, invalid := range [][]string{nil, original[:37], slices.Repeat([]string{"malformed"}, 38)} {
		maintenanceOrdinaryRequestDigests = invalid
		if !slices.Equal(maintenanceBodyReferences("requests.html", true), maintenanceReferenceDigests["requests.html"]) {
			t.Fatal("ordinary override changed frozen task oracle")
		}
		if !*maintenanceTask && checkMaintenanceInventoryPage("requests.html", 0, pages[0].markup, false) == nil {
			t.Fatal("incomplete ordinary oracle accepted")
		}
	}
	maintenanceOrdinaryRequestDigests = original
	if *maintenanceTask {
		return
	}
	for i, page := range pages {
		if err := checkMaintenanceInventoryPage("requests.html", i, page.markup, false); err != nil {
			t.Fatalf("ordinary request %d: %v", i, err)
		}
		if original[i] == maintenanceReferenceDigests["requests.html"][i] {
			continue
		}
		markup := strings.Replace(page.markup, "Appearance", "Colour theme", 1)
		markup = strings.Replace(markup, "Udseende", "Farvetema", 1)
		if err := checkMaintenanceInventoryPage("requests.html", i, markup, true); err == nil || err.Error() != "maintained page state differs from frozen reference" {
			t.Fatalf("ordinary form entered frozen task oracle: %v", err)
		}
	}
	for name, mutate := range map[string]func(string) string{
		"missing other": func(s string) string {
			return strings.Replace(s, `<option value="other" >Other requests</option>`, "", 1)
		},
		"wrong other":     func(s string) string { return strings.Replace(s, `value="other"`, `value="misc"`, 1) },
		"wrong selection": func(s string) string { return strings.Replace(s, `value="website" selected`, `value="website"`, 1) },
		"missing assessment": func(s string) string {
			return strings.Replace(s, `<p id="request-assessment">`, `<p id="request-assessment" hidden>`, 1)
		},
		"wrong assessment": func(s string) string {
			return strings.Replace(s, "Any work, price and timeline need a separate agreement.", "Work starts immediately.", 1)
		},
		"broken association": func(s string) string {
			return strings.Replace(s, `aria-describedby="request-assessment"`, `aria-describedby="missing"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := mutate(pages[0].markup)
			if bad == pages[0].markup {
				t.Fatal("control did not mutate actual form")
			}
			if checkMaintenanceInventoryPage("requests.html", 0, bad, false) == nil {
				t.Fatal("damaged request form accepted")
			}
		})
	}
}
