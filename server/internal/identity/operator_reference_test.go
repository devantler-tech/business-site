package identity

import (
	"slices"
	"strings"
	"testing"
)

// This independent full main-document expectation specifies the ordinary
// operator landing page. It is authored separately from the production renderer;
// unchanged portal bodies and every historical task reference remain frozen.
const ordinaryOperatorMainExpectation = `<main id="main"><div class="intro"><h1>@TITLE@</h1>
<p>@INTRO@</p></div>
<section aria-label="@TITLE@">
<h2>@OPERATOR@</h2>
<p>@WORKSPACE@: <strong>fictional-client</strong></p><p><a href="/portal/operator/invitations/new?lang=@LANG@">@INVITE@</a></p><p><a href="/portal/operator/customers?lang=@LANG@">@CUSTOMERS@</a></p><p><a href="/portal/operator/audit?lang=@LANG@">@AUDIT@</a></p><p>@BOUNDARY@</p><form method="post" action="/portal/logout?lang=@LANG@"><input type="hidden" name="csrf_token" value="fictional-csrf"><button type="submit">@LOGOUT@</button></form>


@TRIAGE@
</section><a class="back" href="@PUBLIC_HOME@">@BACK@</a></main>`

func ordinaryOperatorExpectation(lang string, triage bool) string {
	words := []string{"@LANG@", "en", "@TITLE@", "Your client portal", "@INTRO@", "A private space for your work with Devantler Tech.", "@OPERATOR@", "Operator", "@WORKSPACE@", "Your workspace", "@INVITE@", "Invite a customer", "@CUSTOMERS@", "Customers and workspaces", "@AUDIT@", "Audit log", "@BOUNDARY@", "Ordering and payments are not available yet.", "@LOGOUT@", "Sign out", "@BACK@", "Back to the website"}
	triageLabel := "Review project requests"
	publicHome := "/"
	if lang == "da" {
		words = []string{"@LANG@", "da", "@TITLE@", "Din kundeportal", "@INTRO@", "Et privat sted for dit samarbejde med Devantler Tech.", "@OPERATOR@", "Operatør", "@WORKSPACE@", "Dit arbejdsområde", "@INVITE@", "Inviter en kunde", "@CUSTOMERS@", "Kunder og arbejdsområder", "@AUDIT@", "Revisionslog", "@BOUNDARY@", "Bestilling og betaling er endnu ikke tilgængelig.", "@LOGOUT@", "Log ud", "@BACK@", "Tilbage til hjemmesiden"}
		triageLabel = "Læs projektforespørgsler"
		publicHome = "/da/"
	}
	link := ""
	if triage {
		link = `<p><a href="/portal/operator/requests?lang=` + lang + `">` + triageLabel + `</a></p>`
	}
	return strings.NewReplacer(append(words, "@TRIAGE@", link, "@PUBLIC_HOME@", publicHome)...).Replace(ordinaryOperatorMainExpectation)
}

var maintenanceOrdinaryPortalDigests = ordinaryPortalReferences()

func ordinaryPortalReferences() []string {
	original := maintenanceReferenceDigests["portal.html"]
	if len(original) != 24 {
		return nil
	}
	result := slices.Clone(original)
	for i, lang := range []string{"en", "da"} {
		for _, state := range []int{1, 6} {
			digest, err := maintenanceMainDigest(ordinaryClientHomeExpectation(lang, state == 6), false)
			if err != nil {
				return nil
			}
			result[i*12+state] = digest
		}
		for _, state := range []int{2, 7} {
			digest, err := maintenanceMainDigest(ordinaryOperatorExpectation(lang, state == 7), false)
			if err != nil {
				return nil
			}
			result[i*12+state] = digest
		}
	}
	return result
}

func TestContactMaintenanceOperatorReferenceBoundary(t *testing.T) {
	original := maintenanceOrdinaryPortalDigests
	if len(original) != 24 || slices.Equal(original, maintenanceReferenceDigests["portal.html"]) {
		t.Fatal("complete independent operator references required")
	}
	pages, err := renderedMaintenancePages("portal.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { maintenanceOrdinaryPortalDigests = original }()
	for _, invalid := range [][]string{nil, original[:23], slices.Repeat([]string{"malformed"}, 24)} {
		maintenanceOrdinaryPortalDigests = invalid
		if !slices.Equal(maintenanceBodyReferences("portal.html", true), maintenanceReferenceDigests["portal.html"]) {
			t.Fatal("ordinary operator reference changed task oracle")
		}
		if !*maintenanceTask && checkMaintenanceInventoryPage("portal.html", 2, pages[2].markup, false) == nil {
			t.Fatal("incomplete ordinary operator references were accepted")
		}
	}
	maintenanceOrdinaryPortalDigests = original
	if *maintenanceTask {
		return
	}
	for _, index := range []int{2, 7, 14, 19} {
		for _, path := range []string{"invitations/new", "customers", "audit"} {
			bad := strings.Replace(pages[index].markup, "/operator/"+path, "/operator/missing", 1)
			if bad == pages[index].markup || checkMaintenanceInventoryPage("portal.html", index, bad, false) == nil {
				t.Fatal("missing operator action link accepted")
			}
		}
	}
}
