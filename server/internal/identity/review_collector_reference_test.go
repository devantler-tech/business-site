package identity

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The rest of the client home still has to match its existing frozen body.
const reviewCollectorExpectation = `<section class="review-collector" aria-labelledby="review-title" data-review-collector>
  <a id="review-title" class="review-link" href="https://www.trustpilot.com/evaluate/devantler.tech" rel="noreferrer">@LINK@</a>
  <details data-review-options hidden>
    <summary>@OPTIONS@</summary>
    <p id="review-privacy">@DISCLOSURE@ <a href="@COOKIES_URL@" rel="noreferrer">@COOKIES@</a></p>
    <button type="button" hidden aria-controls="review-frame" aria-expanded="false" aria-describedby="review-privacy" data-review-toggle data-hide-label="@HIDE@">@LOAD@</button>
    <div id="review-frame" hidden></div>
  </details>
  <template data-review-frame><iframe data-src="https://widget.trustpilot.com/trustboxes/56278e9abfbbba0bdcd568bc/index.html?templateId=56278e9abfbbba0bdcd568bc&amp;businessunitId=6b00bb09a7dbf30ad4d33478#locale=@LOCALE@&amp;styleHeight=52px&amp;styleWidth=100%25" title="@FRAME_TITLE@" width="340" height="52" sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox" referrerpolicy="no-referrer"></iframe></template>
</section>`

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
		"@OPTIONS@", "Use the Trustpilot widget",
		"@DISCLOSURE@", "Trustpilot A/S receives your IP address and browser details when you load the widget. It uses cookies to measure and test the widget. You can withdraw consent by hiding it.",
		"@COOKIES@", "Trustpilot’s cookie policy", "@COOKIES_URL@", "https://corporate.trustpilot.com/legal/for-everyone/cookie-policy/may-2025",
		"@HIDE@", "Hide widget and withdraw consent", "@LOAD@", "Allow cookies and show widget",
		"@LINK@", "Review us on Trustpilot", "@LOCALE@", "en-US", "@FRAME_TITLE@", "Review Devantler Tech on Trustpilot",
	}
	if lang == "da" {
		words = []string{
			"@OPTIONS@", "Brug Trustpilot-widgetten",
			"@DISCLOSURE@", "Trustpilot A/S modtager din IP-adresse og browseroplysninger, når du viser widgetten. Trustpilot bruger cookies til at måle brugen og teste widgetten. Du kan trække dit samtykke tilbage ved at skjule den.",
			"@COOKIES@", "Trustpilots cookiepolitik", "@COOKIES_URL@", "https://dk.corporate.trustpilot.com/legal/for-everyone/cookie-policy/may-2025",
			"@HIDE@", "Skjul widget og træk samtykke tilbage", "@LOAD@", "Tillad cookies og vis widget",
			"@LINK@", "Anmeld os på Trustpilot", "@LOCALE@", "da-DK", "@FRAME_TITLE@", "Anmeld Devantler Tech på Trustpilot",
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
		{`rel="noreferrer">Review us on Trustpilot`, `rel="noopener">Review us on Trustpilot`},
		{`<details data-review-options hidden>`, `<details data-review-options open>`},
		{`referrerpolicy="no-referrer"`, `referrerpolicy="unsafe-url"`},
	} {
		bad := strings.Replace(pages[1].markup, change[0], change[1], 1)
		if bad == pages[1].markup || checkMaintenanceInventoryPage("portal.html", 1, bad, false) == nil {
			t.Errorf("accepted changed client home collector: %s", change[1])
		}
	}
}
