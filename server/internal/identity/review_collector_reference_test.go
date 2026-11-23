package identity

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The rest of the client home still has to match its existing frozen body.
const reviewCollectorExpectation = `<div class="review-collector" data-review-collector>
  <a id="review-title" class="review-link" href="https://www.trustpilot.com/evaluate/devantler.tech" rel="noreferrer">@ACTION@ <strong>Trustpilot</strong><span aria-hidden="true">↗</span></a>
  <div id="review-frame" hidden></div>
  <button type="button" class="review-settings" data-review-settings aria-controls="review-consent" aria-expanded="false" hidden>@SETTINGS@</button>
  <section class="review-consent" id="review-consent" data-review-consent aria-labelledby="review-consent-title" hidden>
    <div class="review-consent-copy">
      <strong id="review-consent-title">@TITLE@</strong>
      <p>@PURPOSE@ <a href="@PRIVACY_URL@">@PRIVACY@</a></p>
    </div>
    <div class="review-consent-actions">
      <button type="button" data-review-decision="deny">@DENY@</button>
      <button type="button" data-review-decision="allow">@ALLOW@</button>
    </div>
  </section>
  <template data-review-frame><iframe data-src="https://widget.trustpilot.com/trustboxes/56278e9abfbbba0bdcd568bc/index.html?templateId=56278e9abfbbba0bdcd568bc&amp;businessunitId=6b00bb09a7dbf30ad4d33478#locale=@LOCALE@&amp;styleHeight=52px&amp;styleWidth=100%25" title="@FRAME_TITLE@" width="340" height="52" sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox" referrerpolicy="no-referrer"></iframe></template>
</div>`

func checkClientHomeReviewCollector(markup, lang string) (string, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var collector *html.Node
	for node := range doc.Descendants() {
		if node.Type == html.ElementNode && linkAttribute(node, "id") == "review-title" {
			if collector != nil || node.Parent == nil {
				return "", errors.New("review link must occur once in the client home footer")
			}
			collector = node.Parent
		}
	}
	if collector == nil || collector.Parent == nil || collector.Parent.Data != "footer" {
		return "", errors.New("review link and optional collector must be in the client home footer")
	}
	words := []string{
		"@ACTION@", "Review us on", "@SETTINGS@", "Cookie settings", "@TITLE@", "Optional cookies",
		"@PURPOSE@", "Trustpilot uses cookies to measure and test its review widget.",
		"@PRIVACY@", "Privacy and cookies", "@PRIVACY_URL@", "/privacy/",
		"@DENY@", "Decline", "@ALLOW@", "Allow Trustpilot",
		"@LOCALE@", "en-US", "@FRAME_TITLE@", "Review Devantler Tech on Trustpilot",
	}
	if lang == "da" {
		words = []string{
			"@ACTION@", "Anmeld os på", "@SETTINGS@", "Cookieindstillinger", "@TITLE@", "Valgfrie cookies",
			"@PURPOSE@", "Trustpilot bruger cookies til at måle og teste sin anmeldelseswidget.",
			"@PRIVACY@", "Privatliv og cookies", "@PRIVACY_URL@", "/da/privacy/",
			"@DENY@", "Afvis", "@ALLOW@", "Tillad Trustpilot",
			"@LOCALE@", "da-DK", "@FRAME_TITLE@", "Anmeld Devantler Tech på Trustpilot",
		}
	}
	want, err := maintenanceMainDigest("<main>"+strings.NewReplacer(words...).Replace(reviewCollectorExpectation)+"</main>", false)
	if err != nil {
		return "", err
	}
	var actual strings.Builder
	if err := html.Render(&actual, collector); err != nil {
		return "", err
	}
	got, err := maintenanceMainDigest("<main>"+actual.String()+"</main>", false)
	if err != nil || got != want {
		return "", errors.New("review collector differs from the independent client home reference")
	}
	collector.Parent.RemoveChild(collector)
	var rest strings.Builder
	if err := html.Render(&rest, doc); err != nil {
		return "", err
	}
	return rest.String(), nil
}

func TestContactMaintenanceReviewCollectorReferenceBoundary(t *testing.T) {
	pages, err := renderedMaintenancePages("portal.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{`id="review-title"`, `id="missing-review-title"`},
		{`data-src="https://widget.trustpilot.com/`, `src="https://widget.trustpilot.com/`},
		{`rel="noreferrer">Review us on`, `rel="noopener">Review us on`},
		{`data-review-decision="deny"`, `data-review-decision="allow"`},
		{`aria-labelledby="review-consent-title" hidden`, `aria-labelledby="review-consent-title"`},
		{`referrerpolicy="no-referrer"`, `referrerpolicy="unsafe-url"`},
	} {
		bad := strings.Replace(pages[1].markup, change[0], change[1], 1)
		if bad == pages[1].markup || checkMaintenanceInventoryPage("portal.html", 1, bad, false) == nil {
			t.Errorf("accepted changed client home collector: %s", change[1])
		}
	}
}
