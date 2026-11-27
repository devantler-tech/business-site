package identity

import (
	"net/http"
	"strings"
	"testing"
)

// The contact-maintenance inventory compares the current home with an
// independently specified main while retaining its historical task references.
const clientHomeMainExpectation = `<main id="main"><div class="client-home">
  <h1>@TITLE@</h1>
  <p class="client-workspace">@WORKSPACE@ <strong>fictional-client</strong></p>
  <ul class="workspace-actions" role="list" aria-label="@TASKS@">
    @REQUESTS@<li><a href="/portal/contact?lang=@LANG@">
      <strong>@CONTACT@</strong>
      <span>@CONTACT_HELP@</span>
    </a></li>
  </ul>
  <p>@BOUNDARY@</p>
  <div class="portal-account-actions">
    <a class="back" href="@PUBLIC_HOME@">@BACK@</a>
    <form method="post" action="/portal/logout?lang=@LANG@">
      <input type="hidden" name="csrf_token" value="fictional-csrf">
      <button type="submit">@LOGOUT@</button>
    </form>
  </div>
</div></main>`

func ordinaryClientHomeExpectation(lang string, requests bool) string {
	words := []string{
		"@TITLE@", "Your client portal", "@WORKSPACE@", "Workspace", "@TASKS@", "Your tasks",
		"@CONTACT@", "Your contact details", "@CONTACT_HELP@", "Update how Devantler Tech can reach you.",
		"@BOUNDARY@", "Ordering and payments are not available yet.",
		"@PUBLIC_HOME@", "/", "@BACK@", "Back to the website", "@LOGOUT@", "Sign out",
	}
	requestTitle, requestHelp := "Your project requests", "Start a request or check one you’ve sent."
	if lang == "da" {
		words = []string{
			"@TITLE@", "Din kundeportal", "@WORKSPACE@", "Arbejdsområde", "@TASKS@", "Dine opgaver",
			"@CONTACT@", "Dine kontaktoplysninger", "@CONTACT_HELP@", "Opdater, hvordan Devantler Tech kan kontakte dig.",
			"@BOUNDARY@", "Bestilling og betaling er endnu ikke tilgængelig.",
			"@PUBLIC_HOME@", "/da/", "@BACK@", "Tilbage til hjemmesiden", "@LOGOUT@", "Log ud",
		}
		requestTitle, requestHelp = "Dine projektforespørgsler", "Opret en forespørgsel, eller se dem, du har sendt."
	}
	request := ""
	if requests {
		request = `<li><a href="/portal/requests?lang=` + lang + `">
      <strong>` + requestTitle + `</strong>
      <span>` + requestHelp + `</span>
    </a></li>
    `
	}
	return strings.NewReplacer(append(words, "@LANG@", lang, "@REQUESTS@", request)...).Replace(clientHomeMainExpectation)
}

func TestClientHomePutsAvailableTasksBeforeAccountActions(t *testing.T) {
	b := newBrowserFixture(t)
	b.atRoot()
	b.as(customerSubject)
	b.invite(customerSubject, "spruce-design", "client")
	callback := b.startAt("/sign-in?lang=en")
	if status, _, _ := b.request(http.MethodGet, callback, nil, ""); status != http.StatusSeeOther {
		t.Fatal("the fictional customer could not sign in")
	}
	flags := []string{"portal-identity", "portal-requests", "portal-appointment-history"}
	setFixturePortalFlags(t, b, flags, []any{true, false, false})
	status, page, _ := b.request(http.MethodGet, "/?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, `href="/contact?lang=en"`) ||
		strings.Contains(page, `href="/requests?lang=en"`) || strings.Contains(page, `href="/appointments?lang=en"`) {
		t.Fatal("the home page did not keep contact available by itself")
	}

	setFixturePortalFlags(t, b, flags, []any{true, true, true})
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			status, page, _ := b.request(http.MethodGet, "/?lang="+lang, nil, "")
			if status != http.StatusOK || !strings.Contains(page, "spruce-design") || !csrfInput.MatchString(page) {
				t.Fatal("workspace or protected sign-out is missing")
			}
			last := -1
			for _, marker := range []string{
				`href="/requests?lang=` + lang + `"`, `href="/appointments?lang=` + lang + `"`,
				`href="/contact?lang=` + lang + `"`, `action="/logout?lang=` + lang + `"`,
			} {
				next := strings.Index(page, marker)
				if next <= last {
					t.Fatalf("task or account action is missing or out of order: %s", marker)
				}
				last = next
			}
		})
	}
}

func TestClientHomeDoesNotReleaseWorkspaceNameAfterRevocation(t *testing.T) {
	b := newBrowserFixture(t)
	b.as(customerSubject)
	b.finish(b.startInvited(customerSubject, "spruce-design", "client"))
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspaces(id)
 VALUES ('spruce-design') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspace_settings(workspace_id,display_name)
 VALUES ('spruce-design','Private workshop')`); err != nil {
		t.Fatal(err)
	}
	status, page := requestAfterReadRevocation(t, b, "SELECT display_name FROM portal_workspace_settings", "/portal/", customerSubject)
	if status != http.StatusForbidden || strings.Contains(page, "Private workshop") {
		t.Fatal("a revoked customer could still read the workspace name")
	}
}
