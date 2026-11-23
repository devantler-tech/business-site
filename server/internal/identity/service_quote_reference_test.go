package identity

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const proposalServiceFieldsReference = `<fieldset aria-describedby="proposal-service-help">
<legend>@TYPE@</legend>
<label for="proposal-service-kind">@CHOOSE@</label><select id="proposal-service-kind" name="service_kind" required><option value="" selected>@PLACEHOLDER@</option><option value="software">@SOFTWARE@</option><option value="appointment">@APPOINTMENT@</option></select>
<p id="proposal-service-help" class="field-limit">@HELP@</p>
<label for="proposal-appointment-minutes">@DURATION@</label><input id="proposal-appointment-minutes" name="appointment_minutes" type="number" min="1" max="1440" step="1" aria-describedby="proposal-service-help">
<label for="proposal-appointment-terms-en">@EN_TERMS@</label><textarea id="proposal-appointment-terms-en" name="appointment_terms_en" rows="4" maxlength="4000" data-character-limit="2000" aria-describedby="proposal-service-help"></textarea>
<label for="proposal-appointment-terms-da">@DA_TERMS@</label><textarea id="proposal-appointment-terms-da" name="appointment_terms_da" rows="4" maxlength="4000" data-character-limit="2000" aria-describedby="proposal-service-help"></textarea>
<p class="field-limit">@LIMIT@</p>
</fieldset>`

// Validate the new fields and their placement before comparing the rest of the
// triage document with the existing frozen reference.
func checkTriageProposalServiceFields(markup, lang string, expected bool) (string, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var fields *html.Node
	for node := range doc.Descendants() {
		if node.Type == html.ElementNode && node.Data == "fieldset" && linkAttribute(node, "aria-describedby") == "proposal-service-help" {
			if fields != nil {
				return "", errors.New("triage proposal service fields are duplicated")
			}
			fields = node
		}
	}
	if !expected && fields == nil {
		return markup, nil
	}
	if !expected || fields == nil || !maintenanceRendered(fields) {
		return "", errors.New("triage proposal service fields do not match the draft form state")
	}
	form, previous, next := fields.Parent, fields.PrevSibling, fields.NextSibling
	if previous != nil && maintenanceFormattingSpace(previous) {
		previous = previous.PrevSibling
	}
	if form == nil || form.Data != "form" || linkAttribute(form, "method") != "post" ||
		linkAttribute(form, "action") != "/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals" ||
		previous == nil || previous.Data != "input" || linkAttribute(previous, "name") != "currency" ||
		next == nil || next.Data != "label" || linkAttribute(next, "for") != "scope" {
		return "", errors.New("triage service fields must follow currency and precede scope in the proposal form")
	}
	language := []string{
		"@TYPE@", "Service type", "@CHOOSE@", "Choose the service type", "@PLACEHOLDER@", "Choose a type",
		"@SOFTWARE@", "Software work", "@APPOINTMENT@", "Appointment",
		"@HELP@", "For an appointment, enter its duration and terms in both languages. Leave these fields empty for software work. Saving a draft does not reserve a time.",
		"@DURATION@", "Appointment duration (whole minutes)", "@EN_TERMS@", "Appointment terms (English)", "@DA_TERMS@", "Appointment terms (Danish)",
		"@LIMIT@", "Up to 2,000 characters in each appointment terms field. The other agreement terms remain unchanged.",
	}
	if lang == "da" {
		language = []string{
			"@TYPE@", "Ydelsestype", "@CHOOSE@", "Vælg ydelsestype", "@PLACEHOLDER@", "Vælg en type",
			"@SOFTWARE@", "Softwarearbejde", "@APPOINTMENT@", "Ydelse med aftalt varighed",
			"@HELP@", "Ved en ydelse med aftalt varighed skal du angive varigheden og vilkårene på både engelsk og dansk. Lad disse felter stå tomme ved softwarearbejde. Udkastet reserverer ikke et tidspunkt.",
			"@DURATION@", "Aftalt varighed (hele minutter)", "@EN_TERMS@", "Vilkår for ydelsen (engelsk)", "@DA_TERMS@", "Vilkår for ydelsen (dansk)",
			"@LIMIT@", "Højst 2.000 tegn i hvert felt med vilkår for ydelsen. De øvrige aftalevilkår er uændrede.",
		}
	}
	want, err := maintenanceMainDigest("<main>"+strings.NewReplacer(language...).Replace(proposalServiceFieldsReference)+"</main>", false)
	if err != nil {
		return "", err
	}
	var actual strings.Builder
	if err := html.Render(&actual, fields); err != nil {
		return "", err
	}
	got, err := maintenanceMainDigest("<main>"+actual.String()+"</main>", false)
	if err != nil || got != want {
		return "", errors.New("triage proposal service fields differ from their independent reference")
	}
	form.RemoveChild(fields)
	var remainder strings.Builder
	if err := html.Render(&remainder, doc); err != nil {
		return "", err
	}
	return remainder.String(), nil
}

func TestContactMaintenanceTriageServiceFieldsReference(t *testing.T) {
	pages, err := renderedMaintenancePages("triage.html")
	if err != nil || len(pages) != 24 {
		t.Fatal("actual triage inventory unavailable", err)
	}
	for i, page := range pages {
		if !maintenanceDraftKey("triage.html", i) {
			continue
		}
		for _, change := range [][2]string{
			{`name="service_kind" required`, `name="service_kind"`},
			{`max="1440"`, `max="1441"`},
			{`aria-describedby="proposal-service-help"`, `aria-describedby="unbound-service-help"`},
		} {
			bad := strings.Replace(page.markup, change[0], change[1], 1)
			if bad == page.markup || checkMaintenanceInventoryPage("triage.html", i, bad, *maintenanceTask) == nil {
				t.Fatalf("triage state %d accepted altered service fields %q", i, change[0])
			}
		}
	}
}
