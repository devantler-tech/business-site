package identity

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The rest of the client home still has to match its existing frozen body.
const reviewCollectorExpectation = `<section class="review-collector" aria-labelledby="review-title" data-review-collector>
  <h2 id="review-title">@TITLE@</h2>
  <p>@PROMPT@</p>
  <p id="review-privacy">@DISCLOSURE@ <a href="https://legal.trustpilot.com/for-everyone/end-user-privacy-terms" rel="noreferrer">@PRIVACY@</a></p>
  <div class="review-actions">
    <button type="button" hidden aria-controls="review-frame" aria-expanded="false" aria-describedby="review-privacy" data-review-toggle data-hide-label="@HIDE@">@LOAD@</button>
    <a href="https://www.trustpilot.com/review/devantler.tech" rel="noreferrer">@LINK@</a>
  </div>
  <div id="review-frame" hidden></div>
  <template data-review-frame><iframe data-src="https://widget.trustpilot.com/trustboxes/56278e9abfbbba0bdcd568bc/index.html?templateId=56278e9abfbbba0bdcd568bc&amp;businessunitId=6b00bb09a7dbf30ad4d33478#locale=@LOCALE@&amp;styleHeight=52px&amp;styleWidth=100%25" title="@FRAME_TITLE@" width="340" height="52" sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox" referrerpolicy="no-referrer"></iframe></template>
  <script type="module" src="/portal/assets/review-collector.mjs"></script>
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
				return "", errors.New("review collector heading must occur once in the client home")
			}
			collector = node.Parent
		}
	}
	if collector == nil || collector.Parent == nil || collector.Parent.Data != "main" ||
		collector.PrevSibling == nil || collector.PrevSibling.Data != "section" ||
		collector.NextSibling == nil || linkAttribute(collector.NextSibling, "class") != "back" {
		return "", errors.New("review collector must follow the client actions and precede the return link")
	}
	words := []string{
		"@TITLE@", "Review Devantler Tech", "@PROMPT@", "Have you used our services? Share your experience on Trustpilot.",
		"@DISCLOSURE@", "Loading the widget shares your IP address and browser details with Trustpilot, which may use cookies.",
		"@PRIVACY@", "Trustpilot’s privacy policy", "@HIDE@", "Hide Trustpilot widget", "@LOAD@", "Load Trustpilot widget",
		"@LINK@", "Open on Trustpilot", "@LOCALE@", "en-US", "@FRAME_TITLE@", "Review Devantler Tech on Trustpilot",
	}
	if lang == "da" {
		words = []string{
			"@TITLE@", "Anmeld Devantler Tech", "@PROMPT@", "Har du brugt vores ydelser? Del din oplevelse på Trustpilot.",
			"@DISCLOSURE@", "Hvis du viser widgetten, modtager Trustpilot din IP-adresse og browseroplysninger og kan bruge cookies.",
			"@PRIVACY@", "Trustpilots privatlivspolitik", "@HIDE@", "Skjul Trustpilot-widget", "@LOAD@", "Vis Trustpilot-widget",
			"@LINK@", "Åbn på Trustpilot", "@LOCALE@", "da-DK", "@FRAME_TITLE@", "Anmeld Devantler Tech på Trustpilot",
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
		{`rel="noreferrer">Open on Trustpilot`, `rel="noopener">Open on Trustpilot`},
		{`referrerpolicy="no-referrer"`, `referrerpolicy="unsafe-url"`},
		{`src="/portal/assets/review-collector.mjs"`, `src="/portal/assets/missing.mjs"`},
	} {
		bad := strings.Replace(pages[1].markup, change[0], change[1], 1)
		if bad == pages[1].markup || checkMaintenanceInventoryPage("portal.html", 1, bad, false) == nil {
			t.Errorf("accepted changed client home collector: %s", change[1])
		}
	}
}
