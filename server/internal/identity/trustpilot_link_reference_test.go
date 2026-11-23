package identity

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The rest of the client home still has to match its existing frozen body.
const trustpilotLinkExpectation = `<a class="trustpilot-link" href="https://www.trustpilot.com/evaluate/devantler.tech" rel="noreferrer"><span>@LABEL@ <strong>Trustpilot</strong></span><span class="trustpilot-link-arrow" aria-hidden="true">↗</span></a>`

func checkClientHomeTrustpilotLink(markup, lang string) (string, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var badge *html.Node
	for node := range doc.Descendants() {
		if node.Type == html.ElementNode && node.Data == "a" && linkAttribute(node, "class") == "trustpilot-link" {
			if badge != nil {
				return "", errors.New("Trustpilot link must occur once in the client home footer")
			}
			badge = node
		}
	}
	if badge == nil || badge.Parent == nil || badge.Parent.Data != "footer" {
		return "", errors.New("Trustpilot link must be in the client home footer")
	}
	label := "Review us on"
	if lang == "da" {
		label = "Anmeld os på"
	}
	want, err := maintenanceMainDigest("<main>"+strings.Replace(trustpilotLinkExpectation, "@LABEL@", label, 1)+"</main>", false)
	if err != nil {
		return "", err
	}
	var actual strings.Builder
	if err := html.Render(&actual, badge); err != nil {
		return "", err
	}
	got, err := maintenanceMainDigest("<main>"+actual.String()+"</main>", false)
	if err != nil || got != want {
		return "", errors.New("Trustpilot link differs from the independent client home reference")
	}

	badge.Parent.RemoveChild(badge)
	var rest strings.Builder
	if err := html.Render(&rest, doc); err != nil {
		return "", err
	}
	return rest.String(), nil
}

func TestContactMaintenanceTrustpilotLinkReferenceBoundary(t *testing.T) {
	pages, err := renderedMaintenancePages("portal.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{
		{`class="trustpilot-link"`, `class="missing-trustpilot-link"`},
		{`/evaluate/devantler.tech`, `/evaluate/example.invalid`},
		{`rel="noreferrer"`, `rel="noopener"`},
		{`<span>Review us on`, `<span>5 stars on`},
	} {
		bad := strings.Replace(pages[1].markup, change[0], change[1], 1)
		if bad == pages[1].markup || checkMaintenanceInventoryPage("portal.html", 1, bad, false) == nil {
			t.Errorf("accepted changed client home Trustpilot link: %s", change[1])
		}
	}
}
