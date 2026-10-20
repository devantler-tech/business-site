package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// This test-only option selects ADR 0014's identical source task, not an app flag.
var maintenanceTask = flag.Bool("contact-maintenance-task", false, "check the frozen bilingual source task instead of the unchanged presentation")

// maintenanceContactViews is the frozen order shared by inventory consumers.
var maintenanceContactViews = [...]string{"", "invalid", "conflict", "verification", "confirm", "confirmed"}

// maintenanceProblem separates confirmation views from error presentation.
func maintenanceProblem(view string) string {
	if view == "confirm" || view == "confirmed" {
		return ""
	}
	return view
}

// These mutations retain the actual main body, unlike a partial-document probe.
func TestContactMaintenanceFullFrameRejections(t *testing.T) {
	for name := range maintenanceCases {
		pages, err := renderedMaintenancePages(name)
		if err != nil {
			t.Fatal(err)
		}
		for index, page := range pages {
			for _, target := range []string{"brand", "skip", "language", "stylesheet", "script", "theme-label-hidden", "theme-label-inert", "theme-select-hidden", "theme-select-inert"} {
				t.Run(name+"/"+strconv.Itoa(index)+"/"+target, func(t *testing.T) {
					bad := maintenanceAlter(t, page.markup, func(n *html.Node) bool {
						switch target {
						case "brand", "skip":
							return n.Data == "a" && linkAttribute(n, "class") == target
						case "language":
							return n.Data == "a" && linkAttribute(n, "lang") != ""
						case "stylesheet":
							return n.Data == "link" && linkAttribute(n, "rel") == "stylesheet"
						case "script":
							return n.Data == "script"
						case "theme-label-hidden", "theme-label-inert":
							return n.Data == "label" && n.LastChild != nil && n.LastChild.Data == "select" && linkAttribute(n.LastChild, "id") == "theme"
						default:
							return n.Data == "select" && linkAttribute(n, "id") == "theme"
						}
					}, func(n *html.Node) {
						switch {
						case strings.HasSuffix(target, "-hidden"):
							n.Attr = append(n.Attr, html.Attribute{Key: "hidden"})
						case strings.HasSuffix(target, "-inert"):
							n.Attr = append(n.Attr, html.Attribute{Key: "inert"})
						default:
							n.Parent.RemoveChild(n)
						}
					})
					if checkMaintenanceInventoryPage(name, index, bad, *maintenanceTask) == nil {
						t.Fatal("damaged complete private frame was accepted")
					}
				})
			}
		}
	}
}

// Normal-suite controls exercise the envelope without claiming compiler proof.
func TestContactMaintenanceCompiledEnvelope(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for index, page := range pages {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			lang := "en"
			if index >= len(maintenanceContactViews) {
				lang = "da"
			}
			data := maintenancePage(lang, maintenanceContactViews[index%len(maintenanceContactViews)])
			doc, err := html.Parse(strings.NewReader(page.markup))
			if err != nil {
				t.Fatal(err)
			}
			var fragment strings.Builder
			for n := range doc.Descendants() {
				if n.Type == html.ElementNode && n.Data == "main" {
					if err := html.Render(&fragment, n); err != nil {
						t.Fatal(err)
					}
				}
			}
			body := fragment.String()
			if err := checkMaintenanceCompiledState(body, data, *maintenanceTask); err != nil {
				t.Fatal(err)
			}
			for name, bad := range map[string]string{
				"full document": page.markup,
				"script":        body + `<script src="/unexpected.js"></script>`,
				"stylesheet":    body + `<link rel="stylesheet" href="/unexpected.css">`,
				"wrapper":       `<aside>` + body + `</aside>`,
				"text":          body + `Unexpected trailing content`,
			} {
				t.Run(name, func(t *testing.T) {
					if checkMaintenanceCompiledState(bad, data, *maintenanceTask) == nil {
						t.Fatal("content outside the body fragment was accepted")
					}
				})
			}
		})
	}
}

func TestContactMaintenanceCompleteBodyRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for index, page := range pages {
		lang := "en"
		if index >= 6 {
			lang = "da"
		}
		for _, target := range []string{"main", "intro", "heading", "section", "reload", "workspace"} {
			t.Run(strconv.Itoa(index)+"/"+target, func(t *testing.T) {
				bad := maintenanceAlterBody(t, page.markup, lang, target)
				if checkMaintenanceState(bad, maintenancePage(lang, maintenanceContactViews[index%len(maintenanceContactViews)]), *maintenanceTask) == nil {
					t.Fatal("incomplete contact body was accepted")
				}
			})
		}
	}
}

// Altered validator inputs are refusal controls, never comparative samples.
func maintenanceAlterBody(t *testing.T, markup, lang, target string) string {
	t.Helper()
	return maintenanceAlter(t, markup, func(n *html.Node) bool {
		switch target {
		case "main", "section":
			return n.Data == target
		case "intro":
			return n.Data == "div" && linkAttribute(n, "class") == "intro"
		case "heading":
			return n.Data == "h1"
		case "reload":
			return n.Data == "a" && linkAttribute(n, "href") == "/portal/contact?lang="+lang
		default:
			return n.Data == "a" && linkAttribute(n, "href") == "/portal/?lang="+lang
		}
	}, func(n *html.Node) {
		if target == "main" || target == "section" {
			for n.FirstChild != nil {
				child := n.FirstChild
				n.RemoveChild(child)
				n.Parent.InsertBefore(child, n)
			}
		}
		n.Parent.RemoveChild(n)
	})
}

func maintenanceAlter(t *testing.T, markup string, match func(*html.Node) bool, alter func(*html.Node)) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var matches []*html.Node
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && match(n) {
			matches = append(matches, n)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("mutation requires one actual target, got %d", len(matches))
	}
	alter(matches[0])
	var result strings.Builder
	if err := html.Render(&result, doc); err != nil {
		t.Fatal(err)
	}
	return result.String()
}

// TestContactMaintenanceInventory requires the real frame in every maintained view.
func TestContactMaintenanceInventory(t *testing.T) {
	for name, want := range map[string]int{"portal.html": 28, "contact.html": 12, "requests.html": 38, "triage.html": 30, "credits.html": 12} {
		t.Run(name, func(t *testing.T) {
			pages, err := renderedMaintenancePages(name)
			if err != nil || len(pages) != want {
				t.Fatalf("incomplete maintenance inventory: got=%d want=%d error=%v", len(pages), want, err)
			}
			for i, page := range pages {
				lang := "en"
				if i >= want/2 {
					lang = "da"
				}
				if !strings.HasSuffix(page.uri, "lang="+lang) {
					t.Fatal("inventory lost a language")
				}
				if err := checkMaintenanceInventoryPage(name, i, page.markup, *maintenanceTask); err != nil {
					t.Fatalf("view=%d language=%s: %v", i, lang, err)
				}
			}
			t.Logf("actual Go documents=%d", len(pages))
		})
	}
}

func checkMaintenanceInventoryPage(name string, index int, markup string, task bool) error {
	count := 2 * len(maintenanceCases[name])
	if count == 0 || index < 0 || index >= count {
		return errors.New("unknown maintained page state")
	}
	lang := "en"
	if index >= count/2 {
		lang = "da"
	}
	if err := checkMaintenanceDocument(markup, lang, name == "contact.html", task); err != nil {
		return err
	}
	frame, err := maintenanceFrameDigest(markup, lang, task)
	if err != nil || len(maintenanceFrameReferences[name]) != count || frame != maintenanceFrameReferences[name][index] {
		return errors.New("complete private frame differs from frozen reference")
	}
	if name == "contact.html" {
		view := maintenanceContactViews[index%len(maintenanceContactViews)]
		return checkMaintenanceState(markup, maintenancePage(lang, view), task)
	}
	got, err := maintenanceMainDigest(markup, maintenanceDraftKey(name, index))
	if err != nil {
		return err
	}
	want := maintenanceReferenceDigests[name]
	if len(want) != count || got != want[index] {
		return errors.New("maintained page state differs from frozen reference")
	}
	return nil
}

// The case order is frozen with portal_links_test.go at a1be787. Repeated URIs
// are different states, not duplicate evidence. Contact has explicit assertions.
var maintenanceCases = map[string][]string{
	"contact.html":  maintenanceContactViews[:],
	"portal.html":   {"sign-in", "home", "operator", "error", "unavailable", "triage-invalid", "home-links", "operator-links", "proposal-invalid", "proposal-conflict", "sharing-invalid", "sharing-conflict", "invoice-error", "credit-error"},
	"requests.html": {"website-new", "website-invalid", "website-receipt", "website-shared", "app-new", "app-invalid", "app-receipt", "app-shared", "service-new", "service-invalid", "service-receipt", "service-shared", "empty-list", "populated-list", "conflict", "review", "acceptance-conflict", "accepted-current", "accepted-historical"},
	"triage.html":   {"empty-inbox", "populated-inbox", "paginated-inbox", "website-detail", "app-detail", "service-detail", "empty-drafts", "history", "full", "hostile", "sharing", "withdrawal", "invoice", "invoice-full", "invoice-credits"},
	"credits.html":  {"empty", "confirmed", "disputed", "invalidated", "full-bound", "historical"},
}

// Only the proposal-draft form has a random rand.Text key in these fictional
// fixtures. Its full-history case has no form. Other keys remain exact values.
func maintenanceDraftKey(name string, index int) bool {
	if name != "triage.html" {
		return false
	}
	view := index % len(maintenanceCases[name])
	return view >= 6 && view != 8
}

type maintenanceDOM struct {
	Type      html.NodeType
	Data      string
	Namespace string
	Attr      []html.Attribute
	Children  []maintenanceDOM
}

// Frame projection retains the whole document except the independently checked
// main contents. Only the validated task label is projected to its original text.
func maintenanceFrameDigest(markup, lang string, task bool) (string, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var main, theme *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "main" {
			if main != nil || !maintenanceRendered(n) {
				return "", errors.New("missing or inactive unique main")
			}
			main = n
		}
		if n.Data == "select" && linkAttribute(n, "id") == "theme" {
			if theme != nil || !maintenanceRendered(n) || n.Parent == nil || n.Parent.Data != "label" {
				return "", errors.New("missing or inactive unique theme control")
			}
			theme = n
		}
	}
	if main == nil || theme == nil {
		return "", errors.New("incomplete private frame")
	}
	original, current := "Appearance", "Colour theme"
	if lang == "da" {
		original, current = "Udseende", "Farvetema"
	}
	if !task {
		current = original
	}
	labels := 0
	var project func(*html.Node) maintenanceDOM
	project = func(n *html.Node) maintenanceDOM {
		d := maintenanceDOM{Type: n.Type, Data: n.Data, Namespace: n.Namespace, Attr: slices.Clone(n.Attr)}
		if n.Type == html.TextNode && n.Parent == theme.Parent && n.Data == current {
			d.Data = original
			labels++
		}
		if n != main {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				d.Children = append(d.Children, project(child))
			}
		}
		return d
	}
	projection := project(doc)
	if labels != 1 {
		return "", errors.New("theme label projection is incomplete")
	}
	return maintenanceDigest(projection)
}

// The whole contact main is compared with immutable ordinary Go output. Only
// compiler comments, formatting whitespace and attribute order are immaterial.
// The task's validated name hint is projected away, never removed from evidence.
func maintenanceContactDigest(markup, lang string, task bool) (string, error) {
	if err := checkMaintenanceContact(markup, lang, task); err != nil {
		return "", err
	}
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var main, name, hint *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "main" {
			if main != nil || !maintenanceRendered(n) || linkAttribute(n, "id") != "main" {
				return "", errors.New("contact main is missing, duplicated or inactive")
			}
			main = n
		}
		if linkAttribute(n, "id") == "contact-name" {
			name = n
		}
		if linkAttribute(n, "id") == "contact-name-help" {
			hint = n
		}
	}
	if main == nil {
		return "", errors.New("complete contact main is required")
	}
	if task && (hint == nil || hint.Data != "p" || hint.Parent != name.Parent || hint.Parent.Data != "form" || len(hint.Attr) != 1) {
		return "", errors.New("task hint projection is outside its native form")
	}
	var project func(*html.Node) (maintenanceDOM, bool)
	project = func(n *html.Node) (maintenanceDOM, bool) {
		if n.Type == html.CommentNode || maintenanceFormattingSpace(n) || (task && n == hint) {
			return maintenanceDOM{}, false
		}
		d := maintenanceDOM{Type: n.Type, Data: n.Data, Namespace: n.Namespace, Attr: slices.Clone(n.Attr)}
		if task && n == name {
			d.Attr = slices.DeleteFunc(d.Attr, func(a html.Attribute) bool {
				return a.Namespace == "" && a.Key == "aria-describedby" && a.Val == "contact-name-help"
			})
		}
		slices.SortFunc(d.Attr, func(a, b html.Attribute) int {
			if order := strings.Compare(a.Namespace, b.Namespace); order != 0 {
				return order
			}
			if order := strings.Compare(a.Key, b.Key); order != 0 {
				return order
			}
			return strings.Compare(a.Val, b.Val)
		})
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if value, keep := project(child); keep {
				if value.Type == html.TextNode && len(d.Children) > 0 && d.Children[len(d.Children)-1].Type == html.TextNode {
					d.Children[len(d.Children)-1].Data += value.Data
				} else {
					d.Children = append(d.Children, value)
				}
			}
		}
		return d, true
	}
	projection, _ := project(main)
	return maintenanceDigest(projection)
}

// The worker returns a body fragment, not a full Go document. Its complete main
// cannot excuse extra content that would be inserted into the surrounding frame.
func checkMaintenanceCompiledState(markup string, page contactPageData, task bool) error {
	if err := checkMaintenanceState(markup, page, task); err != nil {
		return err
	}
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return err
	}
	var main *html.Node
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && n.Data == "main" {
			main = n
		}
	}
	counts := map[string]int{}
	for n := range doc.Descendants() {
		inside := false
		for parent := n; parent != nil; parent = parent.Parent {
			inside = inside || parent == main
		}
		if inside || n.Type == html.CommentNode || (n.Type == html.TextNode && strings.Trim(n.Data, " \t\n\r\f") == "") {
			continue
		}
		if n.Type != html.ElementNode || n.Namespace != "" || len(n.Attr) != 0 || !slices.Contains([]string{"html", "head", "body"}, n.Data) {
			return errors.New("compiled body contains content outside its complete main")
		}
		if (n.Data == "html" && n.Parent != doc) || (n.Data != "html" && (n.Parent == nil || n.Parent.Data != "html")) {
			return errors.New("compiled body has a changed document envelope")
		}
		counts[n.Data]++
	}
	if counts["html"] != 1 || counts["head"] != 1 || counts["body"] != 1 || main == nil || main.Parent == nil || main.Parent.Data != "body" {
		return errors.New("compiled body has an incomplete document envelope")
	}
	return nil
}

// Discard indentation only between block-level structure. Text in labels,
// links, headings, buttons and prose remains exact, including boundary spaces.
func maintenanceFormattingSpace(n *html.Node) bool {
	if n.Type != html.TextNode || n.Parent == nil || !slices.Contains([]string{"main", "div", "section", "form"}, n.Parent.Data) {
		return false
	}
	return strings.Trim(n.Data, " \t\n\r\f") == ""
}

func maintenanceDigest(projection maintenanceDOM) (string, error) {
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Recorded from ordinary a1be787 source, independently of either source task.
var maintenanceFrameReferences = map[string][]string{
	"contact.html": {
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"a5d89b8a16c31f9fc15b086e6e68fdeeca5f0ea42bd121a5515bef53cdcd70fc",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
		"05494f267b6fbd2fe1bda8da0b8527bb424eba1256cbc33a8e8a38602be5097f",
	},
	"credits.html": {
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"203362b05d25eced543ad1fbe066bf55b5e8f71912ade6f644be6ed8f1ab0618",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
		"2f924f6904911899870b0eda7cbab35ea9499d4738033c411614e04c31d72334",
	},
	"portal.html": {
		"9aa418e549b64fc3a3f2b6df896107d9f334a3f195059cbb75373cbfd9f79df1",
		"03bcdc9ac3d0970bfc526ebd1202968625798f14e783929a4b15a3d684ecd533",
		"ee6daf32d5a6e1d2bce2aa2b29e9c32961490a422db816d2cd63ee619d8de511",
		"9aa418e549b64fc3a3f2b6df896107d9f334a3f195059cbb75373cbfd9f79df1",
		"9aa418e549b64fc3a3f2b6df896107d9f334a3f195059cbb75373cbfd9f79df1",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"03bcdc9ac3d0970bfc526ebd1202968625798f14e783929a4b15a3d684ecd533",
		"ee6daf32d5a6e1d2bce2aa2b29e9c32961490a422db816d2cd63ee619d8de511",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"5c8edcdcf7fe1bd44406265f5ad583df6e93de69d9eb6c4198e19a4da08216fd",
		"26bb2cdffc500ba751af8e9aba076c92f62c3cb865ee9d7b7b503222af48bb43",
		"99ba466be152ba1746c90df5a2b77a309de4b966ca7afdc94ae68c45c164ba93",
		"9d0a89caa7f160527ade13df6ceb2af5dff34cad1f695af4824a260675b94aad",
		"26bb2cdffc500ba751af8e9aba076c92f62c3cb865ee9d7b7b503222af48bb43",
		"26bb2cdffc500ba751af8e9aba076c92f62c3cb865ee9d7b7b503222af48bb43",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"99ba466be152ba1746c90df5a2b77a309de4b966ca7afdc94ae68c45c164ba93",
		"9d0a89caa7f160527ade13df6ceb2af5dff34cad1f695af4824a260675b94aad",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
		"a09ca51470e7724548e823a69985797ccc07aecd53ace3ccbf6010c2c78fd58e",
	},
	"requests.html": {
		"35f031a4707023d06d4cc6d3418129068d248fe3165b73e9dfd69ace1977bba5",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"35f031a4707023d06d4cc6d3418129068d248fe3165b73e9dfd69ace1977bba5",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"35f031a4707023d06d4cc6d3418129068d248fe3165b73e9dfd69ace1977bba5",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"988ac7dbf5b39939830271d43e6fc4c10ae88b04cb42f39d1f5f4a3bfe16f314",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"4b26fbdc2a10f952d5552487de2cbe61926b438ce90c44fa36990ece94d97854",
		"2c94a511f54cce381980f3017311dc9e564bb480d1bd7beb0a11148e11e63178",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"2c94a511f54cce381980f3017311dc9e564bb480d1bd7beb0a11148e11e63178",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"2c94a511f54cce381980f3017311dc9e564bb480d1bd7beb0a11148e11e63178",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"0635b968b6c3f570806eba02f09d6d89260e9f2413660fe4b6b7d5c988f88413",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
		"51f222e2b6867bdc99645ab91dcd6fbc661c98d42eb409afe20310047337effd",
	},
	"triage.html": {
		"bef031f3c27c40b9bc917595ae59b00fd7593eae3459c7de9e73e8ce6a39269e",
		"bef031f3c27c40b9bc917595ae59b00fd7593eae3459c7de9e73e8ce6a39269e",
		"bef031f3c27c40b9bc917595ae59b00fd7593eae3459c7de9e73e8ce6a39269e",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"d74dec31314bf0102be0b6788957e0efb02e2c1b75fc07bb291205cee89a9c89",
		"67a9d246f76c59d66e11d2715d607baea1303fff5f1201b639f01593713390bc",
		"67a9d246f76c59d66e11d2715d607baea1303fff5f1201b639f01593713390bc",
		"67a9d246f76c59d66e11d2715d607baea1303fff5f1201b639f01593713390bc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
		"4b33bc38523bbb82c44c42abc5e975e71a227b6e3ccc67573b52936eb1630edc",
	},
}
var maintenanceContactReferences = []string{
	"f8d6306c79871812f4b11d9277bdbe0c6d0505d44e89793f0a6eef586f4352f1",
	"1fa2b9ddc9c05daaf9ef030818ff07e3f61d78690fec672b885e8198329414eb",
	"a3f96b85467de66d4a7118068781661bd70557c31d32be115dcc65ba54886367",
	"40d887eb0511b4ad17dce08f98b9f11234137b3575aa918a4da9f074ba147c4c",
	"1ea661668f25a7e1ebb5d92b9e8babe5f0da6edcf12f649e8da26df73536a1af",
	"63d4ce9c1b0432e99ee852fdb98cb44b1af1ea1a2d0e069ab9e39725926c66c3",
	"247acdb93c5175d0d4790c4a8ab630a2d642beb174e51cd1b96259b2598fc127",
	"61c412a85c7c88d645f4d40e3d5ffdf66f18c83b34bbf15cc178062f138200fb",
	"56e14e455f7955179181cc33e94afb9389dde4a9c6db5750c7054672bb856e00",
	"60fb257b249912911cb7bb1735e6e0b8ff462dc68065c406e4b1af224ad43496",
	"37607be5da132ea95d5ccea26ba5fd3f10242cc872174cb97848aa089c384381",
	"5acac781124124a87c6723a49b50d31e080b20642c22ddd0c72b1c550c2b2582",
}

// maintenanceMainDigest fingerprints the complete parsed main tree, including
// text, option labels, namespaces, all attributes and child order. The raw HTML
// and parsed tree are never rewritten. Only a strictly located, validated draft
// key is projected to a marker in this separate oracle representation.
func maintenanceMainDigest(markup string, draftKey bool) (string, error) {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return "", err
	}
	var main *html.Node
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && n.Data == "main" {
			if main != nil || !maintenanceRendered(n) {
				return "", errors.New("reference main is duplicated, hidden or inert")
			}
			main = n
		}
	}
	if main == nil {
		return "", errors.New("reference main is missing")
	}
	keys := 0
	var project func(*html.Node) (maintenanceDOM, error)
	project = func(n *html.Node) (maintenanceDOM, error) {
		d := maintenanceDOM{Type: n.Type, Data: n.Data, Namespace: n.Namespace, Attr: slices.Clone(n.Attr)}
		if draftKey && n.Type == html.ElementNode && n.Namespace == "" && n.Data == "input" && linkAttribute(n, "name") == "submission_key" && n.Parent != nil && n.Parent.Data == "form" && n.Parent.Namespace == "" && linkAttribute(n.Parent, "method") == "post" && linkAttribute(n.Parent, "action") == "/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals" {
			key := linkAttribute(n, "value")
			if linkAttribute(n, "type") != "hidden" || len(key) != 26 || strings.Trim(key, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
				return d, errors.New("fictional draft key has an unexpected shape")
			}
			values := 0
			for i := range d.Attr {
				if d.Attr[i].Key == "value" && d.Attr[i].Namespace == "" {
					values++
					d.Attr[i].Val = "validated-fictional-draft-key"
				}
			}
			if values != 1 {
				return d, errors.New("fictional draft key value is missing or duplicated")
			}
			keys++
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			c, err := project(child)
			if err != nil {
				return d, err
			}
			d.Children = append(d.Children, c)
		}
		return d, nil
	}
	projection, err := project(main)
	if err != nil {
		return "", err
	}
	if (draftKey && keys != 1) || (!draftKey && keys != 0) {
		return "", errors.New("fictional draft key coverage changed")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Generated only from the ordinary immutable a1be787 source, never from the
// maintenance task under test. See CONTACT-MAINTENANCE.md for its provenance.
var maintenanceReferenceDigests = map[string][]string{
	"credits.html": {
		"3575e88878d4f6e886a60b12c3bd2f8fa3b29ab456237d36b8ed5782b297ae27",
		"6aee0bfd9320df558563fcd84dfa4ec4c3eafaffda039040b53a5407882e3f63",
		"9addcd1df510a5fb2af0e0154b399d630dd8a1beb57d5894dd7de34c19f183a7",
		"179c565da41c3e452d4ef0cbf467106d85d9501de9775b6b2ca2d330960b1c42",
		"73070a2b426d84c20eef99e0aacdd74da5de7afbc778f93e382e6112697d8aa2",
		"bed8527c03701f5eb52649f996ef914260a826f2497694abd489998fe84d5a83",
		"b230236a262bf281f433638395c6c3085514272a8cecc27cca4f8de04f07579d",
		"e427aaa9bebb2c2627647ebd9ad19abc4238e21c8558d4809ffb6f3aef2abaec",
		"82dd76552f394d44b50dfcc06c29753efe84f15e4d20bf9df4509b68f71cbf23",
		"665a646e8224432393f17d8b198782912cbeb2ae159feb8b64c4aab389152d81",
		"174f46a14bd96f43c869466c2cf522a603477887f532f7b5612d4c7428d0b7e9",
		"75a27c9abd4c6cfc1a4943804df305324e91a34997333aa2f117be7903a16597",
	},
	"portal.html": {
		"2c3b37fbca59ddb85d649dcb775fc8535a7633e4e9f92cddace170ecd8554512",
		"37bee4a39a255dec1eae234ecb6dd440a06c29825c104fb10da6f7687e972019",
		"3df2a16aa47778d274dc3a73ddc32a699f711f5378ec14134997ce1ca7e082ce",
		"9344d0eeb84f089172dfe23fa97e85910ec6e45b610e3687bb8abc1f6009d11a",
		"714589569c2a38ec2c4c54580fbaa21c0eb686443820cd4fede0034d0788841d",
		"866cc312594810a95f015b98576659cea17c9570b85a9350b4638c5e9a42d46a",
		"8ef29bc10d8443a8aa754175c04f4c3869ecbbffaccbca56c274615107b79621",
		"66f140387ab99e2350a1bdca72ca6d5f63ff45669d41b65f7651a6e7aeb30cf6",
		"b18386f821608e1d92891131d5ddbe0e972f1d89aeaf8eb4c7badb54773b50ee",
		"5d19e77d7c5399566bf5afad843ef5fe357cdfa1bec32a4f7e1c51dc799973ca",
		"3163a4456c9995b01518313d80843cc3609e1ade45b637196e898caa7136ad1b",
		"3163a4456c9995b01518313d80843cc3609e1ade45b637196e898caa7136ad1b",
		"2091965848c296695f0e0c86f880f2a26b00e5918fa204e447c6ee9c986908bb",
		"9bc08d143dfca308344d056026c3ff20f15e9cec8b52acc8b9aed86bf4380cea",
		"97517c38d4a151f1e2d1e780a7100533f595d8151791caffccdf53f841542e77",
		"717acfefd67649d54421e4180e227277389a5c2cc1bf43fb392f2537422b49b0",
		"49ed961f44a9e595bcee42da5dab29fb20fe60efedfd29abbdc4380eedcbd626",
		"e6164087b70dbc034f1b2358035b4b8dad47f0b0d6b0c012cffacd31356c50c6",
		"e4a2200b80ddf50af967eb031f73dc5193d2845ee6c438443b4ce5f4eacea0b3",
		"819650480189de2574b5e7d171bfbd0157b10ff926d50ac712d18e9dca52c966",
		"4c80b64b7d3137dc2a730d5da6cbf2e4efbf3d429f0185c7299e52448e82f4b1",
		"5bb159b38db70207fcbf709a36d7180ae068294db323b4f663aa1dc84cefc4dd",
		"b4bca74b088abfd08c5f9023e7e45ea856c648dfd009fb8d5e432a06baa9b213",
		"a19d7e30316b1be3b7a461044dd4cbfbb5481a2492528b3afbc90ea0527afde0",
		"22fe594eabcc1252342ef696d06c04aecbd24f0e348dab247895bdcf103799cb",
		"22fe594eabcc1252342ef696d06c04aecbd24f0e348dab247895bdcf103799cb",
		"f8ab8c7327f6cbd16d15edf3fb836c6af086c4181febe22e8375d44a213c4314",
		"1870d6ec724cc978c369b534c97c38c37ffbd813ca87e7631c75b9b4b96acc34",
	},
	"requests.html": {
		"2c6c8d8098ea574e7df5748556e250fc4b5fe9e5cce2cdb5898b96907a1bb245",
		"b55cb20b8404da1a9e56cd596579345e0f65398ac388fdaee4045b4793409135",
		"07e61d5b9220f6d92e19b40b98ec7d8bb5b2a8d087ce09fb87765442d8351013",
		"cfd468b013a861bfa77042fec75dd08f353af7a30777b13c11aecbda6c4a3c23",
		"fb85f03d6b1902c049779df1667d60b6413c857cb772547a14a9e6c1ab86c4d6",
		"57ddfc64f6c8c14ec456c7d3b658c3b9e8cb2ee497b7063be6fb6474e7579236",
		"a5a22c0149dea17765cde619ef7d3cfd5f168dec767338f20fb4823f2a2ee9c6",
		"6cec17fe690b73f2f2ea9ab7a28360dd7ff9611f6313a5c66e5826c06e00e652",
		"24c8dbf7b97e0cae3fecebcbfdfaa5bd9ad21f45ae637ae6f45cc8baa145440e",
		"6b03e9dead29606199772c98ef12b8214d165e767ea0abb243166a66c49a3545",
		"a3a0091b4ac5ab614a9f94b32cb9c4cc6c4f411a434e06fae8d58cb8a36cc6c1",
		"a952dd2a8ec2000e4cd775a2b51fc7f608756cb533d738820e15449aec7da2bf",
		"b90ff927c0e77d26d28930175f89139d210bb7f002368d2dc4eb1060d076a21f",
		"1251ef71b4a258508bcd986bcc0866bb8cb5c207570cc0b7e65ac7685cc5bc11",
		"6916eeae6164262f7d4a3afcafbe2b19942377c7db39d58af5f053457b9a3b74",
		"69aa37c6716487c0b473544263a695d6325bd59e2716f2b041692ee6999f2236",
		"e9c5d465df4fd8b9d8af9883a1dd78e594d176d05c1c02ce9496d4276b4c8683",
		"d6764b0b637474f5756cf6532e18ee82d508bcd33d4214f2e6c50761b8d450d5",
		"fcf881baac490553d9acd7fccfa5c9e3ce312f375f377fe42d4e7edbe0a3c16b",
		"613903e021b6db4002cc8c2f2f231e60c0ef98d810d309879e3e7dbc41984e9c",
		"6d7f2d5816670f8c776e8fb4803d17ec8399530be032ae260f34c8f0bd4447b3",
		"e530fca557a6d039a51fde4614dfd8785593bbcce31d16430eed96d4448d00e1",
		"3dce67dfe3ef40d4fd8d62f03c9a6e2a55f94e18c5821e3a6e1171ed64a71821",
		"c34970671bec16971cb6488b0eaeede1610319747e7c309d3dfcb5324431d75f",
		"af15e0740ae2466a79328f1b54afea09ea1d538fdec9fc60541bfb77c98433d0",
		"022c60700547842dc5dd1ea8522ab89cc97a38d5e14b9109e961af7abd9d2324",
		"3b63d25611eac5d2676acd24c4240e6f8aba7228b0be81dd7180c1ea943640da",
		"8358faa9c6b159300327d18b7edec820509ce9b312348c282c2cebd82b6b8433",
		"04d45d2fdf3adfdd7960f3aa5a58ba64f4159999947d2cca6fc31abbad676421",
		"802cb16f64ddc85931bcf3cdcde156fe922117338873d5fe5efa3056daa3ebfe",
		"150e268b97c7c9e078bbd4e713053ac96a613f17b66a814fd84fa6c27124f541",
		"fcec25678a5f5295c52ce5dfa9243a4e17f900b1d65e49267666c42e93c156d6",
		"64d7079796e89999942ebab21002f366fae538049a39f715f2c935b88ccfce1c",
		"0766a418166b08aa1a9315069645f6ec4391514e011095b8276c9b3bfd1b2328",
		"3ae42beb4b2c6e2833bbbd6952ee6f74a5ebda12e4b5962833f9994674ed7d36",
		"265bb8fbfaa1b85c30be3ee9a5450ba77983ba35cfee1541266e13aaa934cd50",
		"43bf5374c9ad13393e29f58d017649835045d0f23eb202e39aeffff0dd25191e",
		"38db284e92e6ac91cd9831103f28f0621449ac77b02dad3e9d5e12eb3c2899a2",
	},
	"triage.html": {
		"8dabbc55a29a43559fe6b5ba27cd759a31eaff7119ae162664ea7b3238569673",
		"c6d297689afbba79eef47ee712b15e91f7d6bafee1e5592e05dbab74f098db52",
		"33a6af2890cb399106b54aa43cf453606427debc9aea08a8ab15b594ced7296d",
		"0622fd91f7e9af9c42d5759481cecd2da84936acc21b173d1c813b1ed20246b6",
		"030c63d05649fd0c3d2346f3afe0fb38317fce26ce0f2f2524bf02cbcf80eb1e",
		"68f119a129ce9acfff0d66c62ff3e324a75c2a95a8cd18dd428a31928bb2b493",
		"a726b3568e13bf1917827063416c27232f52adfbc0aafb4b0f44c67ac1074856",
		"bfe30b905bcafac1f589c55bf9b63011dd9b475f29cf99248eb0ffd7510dedbb",
		"bdd64bfca46461e50cdcd7ba6397c6c11a62e9b913f5fdce1d4505fbf1904980",
		"1d00ea15686a90add4cafe5853358005b958a0621acfb2b01f1a2ebb92eb1e9b",
		"41d46d9f603d4f9e601adda2af056bf1ebcdcd83ea965fcbc653bf423465fb76",
		"e9e34df25350c9ad10d12af63b036fbfaf6ec932ac3abb34ea1cb447673aee37",
		"26da58801f11793648c26f5a0e206e29e2329484d9f84b0e8b60df5d1d1fca76",
		"685994fee03f603d04edd5c912b2386dc049b23d6e2eed81065ec219c964b9fe",
		"4c2744e152eeebd6bea425d704042beae2d51e6df3e374026ab30da7506208a9",
		"c191a924fb08fbc09a245df740a42be494534cf11352aa234ac399e848c67153",
		"3344f0e632cefa65f526aaf4c12a3920d7b2c21d07dcdd2d3ffec0b363a24a5b",
		"bd37eb7b628dc67ad07df539b3ef8edfc738e6cbeaaa9024a3b957e00d834c91",
		"78d71798b3c918af41eac556fb85b1f0c1397e22a3c36c962603ebe63ca87946",
		"4e9d71c15a412a76ddadbfd48179ebe5bb31a471e859aedb0a1b611feb4a2cce",
		"6558f914752c4eee555256063c58fb963fb779165dc6b228b989c81cb9cfb742",
		"54dca502a052cdba07370df7aea51254ce4357ae0742900606a389a1514f47d6",
		"606ee1722e70aae976ab60a2d08148f7d3f663998f0f49b4030e0338d962bfb5",
		"a2f1ae5f008547cb7133b85e3ac8609a0f13bb6003870d2d524d480f6118b5ba",
		"1d507c49c44f968e5ae5eeef185dce5b32d205918259f95ce342f35f4ae34b28",
		"c554a70b28a6cd51e33a4008182868ec1558e4e603ec07a3ad162761f997006b",
		"c97472f834d21ab72e25aa2a593d877c890ce921093a33a8e4030b05b96e789f",
		"fd573225638ed03449ef6f68bd159fcd364c7756fedab7b27791a7a9f9812ff5",
		"ca0a4b50ea19c05cc48f964aa6156a2bfa86d4c35cbe3398d8900dee84664eb4",
		"bf30f5144bb203a060bd11030b8c9dbe9e407a42ab6e2be1b064715ea0993a51",
	},
}

// TestContactMaintenanceAncestorRejections preserves active forms and their
// messages even when an existing HTML ancestor retains its allowed tag name.
func TestContactMaintenanceAncestorRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for i, page := range pages {
		for _, tag := range []string{"html", "body", "main", "section"} {
			for _, attr := range []string{"hidden", "inert"} {
				t.Run(strconv.Itoa(i)+"/"+tag+"/"+attr, func(t *testing.T) {
					bad := strings.Replace(page.markup, "<"+tag, "<"+tag+" "+attr, 1)
					if bad == page.markup {
						t.Fatal("refusal control did not alter its fixture")
					}
					if checkMaintenanceInventoryPage("contact.html", i, bad, *maintenanceTask) == nil {
						t.Fatal("hidden or inert contact interface was accepted")
					}
				})
			}
		}
	}
}

// TestContactMaintenanceNonContactRejections preserves each case's main content,
// not merely the number of documents or the shared frame around an empty main.
func TestContactMaintenanceNonContactRejections(t *testing.T) {
	for _, name := range []string{"portal.html", "requests.html", "triage.html", "credits.html"} {
		pages, err := renderedMaintenancePages(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, page := range pages {
			start, end := maintenanceMainRange(t, page.markup)
			variants := map[string]string{
				"removed":         "",
				"changed heading": strings.Replace(page.markup[start:end], "<h1>", "<h1>Changed ", 1),
			}
			// Preserve this frame/language, replacing only the state with a
			// genuinely different frozen main. Identical reference aliases
			// are not claimed to be distinguishable states.
			for j, other := range pages {
				if i/len(maintenanceCases[name]) == j/len(maintenanceCases[name]) && maintenanceDraftKey(name, i) == maintenanceDraftKey(name, j) && maintenanceReferenceDigests[name][i] != maintenanceReferenceDigests[name][j] {
					a, b := maintenanceMainRange(t, other.markup)
					variants["collapsed"] = other.markup[a:b]
					break
				}
			}
			if len(variants) != 3 {
				t.Fatal("different-state refusal fixture unavailable")
			}
			for variant, content := range variants {
				t.Run(name+"/"+strconv.Itoa(i)+"/"+variant, func(t *testing.T) {
					bad := page.markup[:start] + content + page.markup[end:]
					if bad == page.markup {
						t.Fatal("refusal control did not alter its fixture")
					}
					if checkMaintenanceInventoryPage(name, i, bad, *maintenanceTask) == nil {
						t.Fatal("missing or collapsed maintained state content was accepted")
					}
				})
			}
		}
	}
}

func maintenanceMainRange(t *testing.T, markup string) (int, int) {
	t.Helper()
	start := strings.Index(markup, `<main id="main">`)
	end := strings.Index(markup, `</main>`)
	if start < 0 || end <= start+len(`<main id="main">`) {
		t.Fatal("actual main content unavailable")
	}
	return start + len(`<main id="main">`), end
}

// TestContactMaintenanceReferenceKeys bounds the oracle's only varying field.
// No missing or malformed input may disappear behind key canonicalization.
func TestContactMaintenanceReferenceKeys(t *testing.T) {
	pages, err := renderedMaintenancePages("triage.html")
	if err != nil || len(pages) != 30 {
		t.Fatal("actual triage inventory unavailable")
	}
	for i, page := range pages {
		if !maintenanceDraftKey("triage.html", i) {
			continue
		}
		doc, err := html.Parse(strings.NewReader(page.markup))
		if err != nil {
			t.Fatal(err)
		}
		key := ""
		for n := range doc.Descendants() {
			if n.Data == "input" && linkAttribute(n, "name") == "submission_key" && n.Parent != nil && strings.HasSuffix(linkAttribute(n.Parent, "action"), "/proposals") {
				key = linkAttribute(n, "value")
			}
		}
		if key == "" {
			t.Fatal("actual fictional draft key unavailable")
		}
		variants := map[string]string{}
		for variant, input := range map[string]string{
			"empty":    `value=""`,
			"short":    `value="` + strings.Repeat("A", 25) + `"`,
			"long":     `value="` + strings.Repeat("A", 27) + `"`,
			"alphabet": `value="` + strings.Repeat("a", 26) + `"`,
			"missing":  "",
		} {
			variants[variant] = strings.Replace(page.markup, `value="`+key+`"`, input, 1)
		}
		input := `<input type="hidden" name="submission_key" value="` + key + `">`
		variants["duplicate input"] = strings.Replace(page.markup, input, input+input, 1)
		for variant, bad := range variants {
			t.Run(strconv.Itoa(i)+"/"+variant, func(t *testing.T) {
				if bad == page.markup {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceInventoryPage("triage.html", i, bad, *maintenanceTask) == nil {
					t.Fatal("missing or malformed draft key was accepted")
				}
			})
		}
	}
	// Independently rendered random keys must not make the reference unstable.
	other, err := renderedMaintenancePages("triage.html")
	if err != nil || len(other) != len(pages) {
		t.Fatal("second actual inventory unavailable")
	}
	for i, page := range other {
		if err := checkMaintenanceInventoryPage("triage.html", i, page.markup, *maintenanceTask); err != nil {
			t.Fatalf("second actual inventory view=%d: %v", i, err)
		}
	}
}

// maintenancePage derives fictional state expectations from the unchanged builder.
func maintenancePage(lang, view string) contactPageData {
	r := httptest.NewRequest("GET", "https://portal.example.invalid/portal/contact?lang="+lang, nil)
	d := contactData{Name: "Fictional client", Email: "fixture@example.invalid", Company: "Fictional company", Revision: 1, EmailVerified: view == "confirmed"}
	page := (&browser{}).contactPage(r, d, maintenanceProblem(view), view == "confirm")
	// This presentation fixture has no middleware or request authority. Both
	// real renderers receive the same nonempty, explicitly fictional nonce.
	page.CSRF = "fictional-form-nonce"
	return page
}

// renderedMaintenancePages keeps the complete real frame inventory while
// rendering contact source with the same nonempty values used by the worker.
func renderedMaintenancePages(name string) ([]linkPage, error) {
	if name != "contact.html" {
		return renderedLinkPages(name)
	}
	var pages []linkPage
	for _, lang := range []string{"en", "da"} {
		for _, view := range maintenanceContactViews {
			var markup bytes.Buffer
			if err := browserPage.ExecuteTemplate(&markup, name, maintenancePage(lang, view)); err != nil {
				return nil, err
			}
			pages = append(pages, linkPage{"/portal/contact?lang=" + lang, markup.String()})
		}
	}
	return pages, nil
}

// TestContactMaintenanceStateRejections prevents normal output masking other states.
func TestContactMaintenanceStateRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for _, view := range maintenanceContactViews[1:] {
		t.Run(view, func(t *testing.T) {
			if checkMaintenanceState(pages[0].markup, maintenancePage("en", view), *maintenanceTask) == nil {
				t.Fatal("normal body passed as a different state")
			}
		})
	}
	for name, bad := range map[string]string{
		"missing nonce":        strings.Replace(pages[0].markup, `name="csrf_token"`, `name="missing"`, 1),
		"missing revision":     strings.Replace(pages[0].markup, `name="revision"`, `name="missing"`, 1),
		"wrong language input": strings.Replace(pages[0].markup, `name="lang" value="en"`, `name="lang" value="da"`, 1),
		"missing submitter":    strings.Replace(pages[0].markup, `<button type="submit">`, `<button type="button">`, 1),
		"submit override":      strings.Replace(pages[0].markup, `<button type="submit">`, `<button type="submit" formaction="/portal/elsewhere">`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if checkMaintenanceState(bad, maintenancePage("en", ""), *maintenanceTask) == nil {
				t.Fatal("incomplete native form was accepted")
			}
		})
	}
}

// TestContactMaintenanceFixtureCoverage requires meaningful saved-value and
// anti-forgery presentation inputs in both languages and every contact state.
func TestContactMaintenanceFixtureCoverage(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		for _, view := range maintenanceContactViews {
			page := maintenancePage(lang, view)
			t.Run(lang+"/"+view+"/company", func(t *testing.T) {
				if page.Contact.Company == "" {
					t.Fatal("empty saved-company fixture")
				}
			})
			t.Run(lang+"/"+view+"/nonce", func(t *testing.T) {
				if page.CSRF == "" {
					t.Fatal("empty form-nonce fixture")
				}
			})
		}
	}
}

// TestContactMaintenanceFrozenViews keeps an independent characterization of
// the frozen order and problem mapping while inventory consumers share a list.
func TestContactMaintenanceFrozenViews(t *testing.T) {
	want := [...]struct {
		view, problem     string
		confirm, verified bool
	}{
		{"", "", false, false},
		{"invalid", "invalid", false, false},
		{"conflict", "conflict", false, false},
		{"verification", "verification", false, false},
		{"confirm", "", true, false},
		{"confirmed", "", false, true},
	}
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for language, lang := range []string{"en", "da"} {
		for index, view := range want {
			t.Run(lang+"/"+view.view, func(t *testing.T) {
				if maintenanceProblem(view.view) != view.problem {
					t.Fatal("frozen contact problem mapping changed")
				}
				page := maintenancePage(lang, view.view)
				r := httptest.NewRequest("GET", "https://portal.example.invalid/portal/contact?lang="+lang, nil)
				original := (&browser{}).contactPage(r, contactData{Revision: 1, EmailVerified: view.verified}, view.problem, view.confirm)
				if page.FormError != original.FormError || page.VerificationEnabled != view.confirm || page.Contact.EmailVerified != view.verified {
					t.Fatal("frozen contact state mapping changed")
				}
				if err := checkMaintenanceState(pages[language*len(want)+index].markup, page, *maintenanceTask); err != nil {
					t.Fatalf("frozen contact order changed: %v", err)
				}
			})
		}
	}
}

// TestContactMaintenanceLabelRejections preserves a unique, named native label
// for each visible field, rather than treating an input ID as an association.
func TestContactMaintenanceLabelRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for _, field := range []string{"name", "email", "company"} {
		markup := pages[0].markup
		start := strings.Index(markup, `<label for="contact-`+field+`">`)
		if start < 0 {
			t.Fatal("actual contact label missing")
		}
		end := strings.Index(markup[start:], `</label>`)
		if end < 0 {
			t.Fatal("actual contact label incomplete")
		}
		label := markup[start : start+end+len(`</label>`)]
		for name, replacement := range map[string]string{
			"missing":      "",
			"disconnected": strings.Replace(label, `for="contact-`+field+`"`, `for="absent"`, 1),
			"unnamed":      `<label for="contact-` + field + `"></label>`,
			"duplicate":    label + label,
			"wrong text":   `<label for="contact-` + field + `">Different field</label>`,
			"hidden":       strings.Replace(label, `<label `, `<label hidden `, 1),
			"inert":        `<template>` + label + `</template>`,
			"foreign":      `<svg>` + label + `</svg>`,
		} {
			t.Run(field+"/"+name, func(t *testing.T) {
				if replacement == label {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceContact(strings.Replace(markup, label, replacement, 1), "en", false) == nil {
					t.Fatal("missing or ambiguous contact label was accepted")
				}
			})
		}
	}
}

// TestContactMaintenanceSavedValueRejections requires every actual Go view to
// retain its nonempty company and nonce, including the confirmation operation.
func TestContactMaintenanceSavedValueRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for i, page := range pages {
		lang := "en"
		if i >= len(maintenanceContactViews) {
			lang = "da"
		}
		view := maintenanceContactViews[i%len(maintenanceContactViews)]
		expected := maintenancePage(lang, view)
		for name, bad := range map[string]string{
			"empty company":         strings.ReplaceAll(page.markup, `value="`+expected.Contact.Company+`"`, `value=""`),
			"wrong company":         strings.ReplaceAll(page.markup, `value="`+expected.Contact.Company+`"`, `value="Another company"`),
			"missing company value": strings.ReplaceAll(page.markup, `value="`+expected.Contact.Company+`"`, ""),
			"empty nonce":           strings.ReplaceAll(page.markup, `value="`+expected.CSRF+`"`, `value=""`),
			"wrong nonce":           strings.ReplaceAll(page.markup, `value="`+expected.CSRF+`"`, `value="wrong-fixture-nonce"`),
			"missing nonce value":   strings.ReplaceAll(page.markup, `value="`+expected.CSRF+`"`, ""),
		} {
			t.Run(strconv.Itoa(i)+"/"+name, func(t *testing.T) {
				if bad == page.markup {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceState(bad, expected, *maintenanceTask) == nil {
					t.Fatal("lost saved value or form nonce was accepted")
				}
			})
		}
	}
}

// TestContactMaintenanceConfirmationRejections preserves the second native operation.
func TestContactMaintenanceConfirmationRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	markup := pages[4].markup
	start := strings.Index(markup, `<form method="post" action="/portal/contact/verify">`)
	if start < 0 {
		t.Fatal("actual confirmation form missing")
	}
	end := strings.Index(markup[start:], `</form>`)
	if end < 0 {
		t.Fatal("actual confirmation form incomplete")
	}
	form := markup[start : start+end+len(`</form>`)]
	for name, replacement := range map[string]string{
		"empty form":          `<form action="/portal/contact/verify"></form>`,
		"wrong method":        strings.Replace(form, `method="post"`, `method="get"`, 1),
		"missing nonce":       strings.Replace(form, `name="csrf_token"`, `name="missing"`, 1),
		"missing nonce value": strings.Replace(form, `value="fictional-form-nonce"`, "", 1),
		"empty nonce value":   strings.Replace(form, `value="fictional-form-nonce"`, `value=""`, 1),
		"wrong nonce value":   strings.Replace(form, `value="fictional-form-nonce"`, `value="wrong-fixture-nonce"`, 1),
		"wrong revision":      strings.Replace(form, `name="revision" value="1"`, `name="revision" value="2"`, 1),
		"missing help":        strings.Replace(form, `id="contact-verification-help"`, `id="absent"`, 1),
		"disconnected help":   strings.Replace(form, `aria-describedby="contact-verification-help"`, `aria-describedby="absent"`, 1),
		"missing submitter":   strings.Replace(form, `type="submit"`, `type="button"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if checkMaintenanceState(strings.Replace(markup, form, replacement, 1), maintenancePage("en", "confirm"), *maintenanceTask) == nil {
				t.Fatal("incomplete confirmation form was accepted")
			}
		})
	}
}

// TestContactMaintenanceFormEnvelopeRejections challenges both forms' submission boundary.
func TestContactMaintenanceFormEnvelopeRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	for _, view := range []struct {
		index          int
		problem, start string
	}{
		{0, "", `<form class="contact-form"`},
		{4, "confirm", `<form method="post" action="/portal/contact/verify"`},
	} {
		markup := pages[view.index].markup
		start := strings.Index(markup, view.start)
		if start < 0 {
			t.Fatal("actual native form missing")
		}
		end := strings.Index(markup[start:], `</form>`)
		if end < 0 {
			t.Fatal("actual native form incomplete")
		}
		form := markup[start : start+end+len(`</form>`)]
		inputStart := strings.Index(form, `<input `)
		if inputStart < 0 {
			t.Fatal("actual native input missing")
		}
		inputEnd := strings.Index(form[inputStart:], `>`)
		if inputEnd < 0 {
			t.Fatal("actual native input incomplete")
		}
		input := form[inputStart : inputStart+inputEnd+1]
		for name, replacement := range map[string]string{
			"textarea":            strings.Replace(form, `</form>`, `<textarea name="extra"></textarea></form>`, 1),
			"select":              strings.Replace(form, `</form>`, `<select name="extra"><option>extra</option></select></form>`, 1),
			"output":              strings.Replace(form, `</form>`, `<output name="extra"></output></form>`, 1),
			"object":              strings.Replace(form, `</form>`, `<object name="extra"></object></form>`, 1),
			"disabled fieldset":   strings.Replace(strings.Replace(form, `>`, `><fieldset disabled>`, 1), `</form>`, `</fieldset></form>`, 1),
			"disabled ancestor":   `<fieldset disabled>` + form + `</fieldset>`,
			"fieldset owner":      strings.Replace(form, `</form>`, `<fieldset form="foreign"></fieldset></form>`, 1),
			"submit encoding":     strings.Replace(form, `<button `, `<button formenctype="text/plain" `, 1),
			"submit target":       strings.Replace(form, `<button `, `<button formtarget="_blank" `, 1),
			"submit validation":   strings.Replace(form, `<button `, `<button formnovalidate `, 1),
			"form encoding":       strings.Replace(form, `<form `, `<form enctype="text/plain" `, 1),
			"form target":         strings.Replace(form, `<form `, `<form target="_blank" `, 1),
			"form validation":     strings.Replace(form, `<form `, `<form novalidate `, 1),
			"outside association": strings.Replace(form, `<form `, `<form id="fixture-owner" `, 1) + `<input form="fixture-owner" name="revision" value="0">`,
			"named submitter":     strings.Replace(form, `<button `, `<button name="revision" value="0" `, 1),
			"unknown submitter":   strings.Replace(form, `<button `, `<button name="extra" value="0" `, 1),
			"form event":          strings.Replace(form, `<form `, `<form onsubmit="return false" `, 1),
			"input direction":     strings.Replace(form, `<input `, `<input dirname="extra" `, 1),
			"foreign input":       strings.Replace(form, input, `<svg>`+input+`</input></svg>`, 1),
			"foreign submitter":   strings.Replace(strings.Replace(form, `<button `, `<svg><button `, 1), `</button>`, `</button></svg>`, 1),
			"template input":      strings.Replace(form, input, `<template>`+input+`</template>`, 1),
			"datalist input":      strings.Replace(form, input, `<datalist>`+input+`</datalist>`, 1),
			"template form":       `<template>` + form + `</template>`,
			"datalist form":       `<datalist>` + form + `</datalist>`,
		} {
			t.Run(strconv.Itoa(view.index)+"/"+name, func(t *testing.T) {
				if replacement == form {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceState(strings.Replace(markup, form, replacement, 1), maintenancePage("en", view.problem), *maintenanceTask) == nil {
					t.Fatal("changed native form envelope was accepted")
				}
			})
		}
	}
	for name, replacement := range map[string]string{
		"name pattern":    `name="name" pattern="never-match"`,
		"name minimum":    `name="name" minlength="120"`,
		"name event":      `name="name" oninvalid="return false"`,
		"name visibility": `name="name" hidden`,
	} {
		t.Run(name, func(t *testing.T) {
			markup := pages[0].markup
			bad := strings.Replace(markup, `name="name"`, replacement, 1)
			if bad == markup {
				t.Fatal("refusal control did not alter its fixture")
			}
			if checkMaintenanceState(bad, maintenancePage("en", ""), *maintenanceTask) == nil {
				t.Fatal("changed visible input behavior was accepted")
			}
		})
	}
}

// checkMaintenanceState compares actual output with the expected native operations.
func checkMaintenanceState(markup string, page contactPageData, task bool) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse contact state")
	}
	alerts, confirmations, pending := 0, 0, 0
	var form, confirmation *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if linkAttribute(n, "role") == "alert" {
			alerts++
			if !maintenanceRendered(n) || page.FormError == "" || maintenanceText(n) != page.FormError {
				return errors.New("contact error state changed")
			}
		}
		if n.Data == "form" && linkAttribute(n, "action") == "/portal/contact/verify" {
			confirmations++
			confirmation = n
		}
		if linkAttribute(n, "id") == "contact-pending" {
			pending++
			if !maintenanceRendered(n) || maintenanceText(n) != page.Pending {
				return errors.New("contact verification display changed")
			}
		}
		if n.Data == "form" && linkAttribute(n, "class") == "contact-form" {
			if form != nil {
				return errors.New("duplicate native contact form")
			}
			form = n
		}
	}
	wantAlerts, wantConfirmations := 0, 0
	if page.FormError != "" {
		wantAlerts = 1
	}
	if page.VerificationEnabled {
		wantConfirmations = 1
	}
	if alerts != wantAlerts || confirmations != wantConfirmations || pending != 1 || form == nil {
		return errors.New("contact state is incomplete or collapsed")
	}
	inputs := map[string]string{"csrf_token": page.CSRF, "lang": page.Lang, "revision": strconv.FormatInt(page.Contact.Revision, 10), "name": page.Contact.Name, "email": page.Contact.Email, "company": page.Contact.Company}
	if err := checkMaintenanceForm(form, "/portal/contact", inputs, page.Save, ""); err != nil {
		return err
	}
	if confirmation != nil {
		inputs = map[string]string{"csrf_token": page.CSRF, "lang": page.Lang, "revision": strconv.FormatInt(page.Contact.Revision, 10)}
		if err := checkMaintenanceForm(confirmation, "/portal/contact/verify", inputs, page.Verify, "contact-verification-help"); err != nil {
			return err
		}
		help := 0
		for n := range doc.Descendants() {
			if n.Type == html.ElementNode && linkAttribute(n, "id") == "contact-verification-help" {
				help++
				if !maintenanceRendered(n) || maintenanceText(n) != page.VerificationHelp {
					return errors.New("confirmation help changed")
				}
			}
		}
		if help != 1 {
			return errors.New("confirmation help missing or duplicated")
		}
	}
	index := -1
	for i, view := range maintenanceContactViews {
		expected := maintenancePage(page.Lang, view)
		if page.FormError == expected.FormError && page.VerificationEnabled == expected.VerificationEnabled && page.Contact.EmailVerified == expected.Contact.EmailVerified {
			index = i
			break
		}
	}
	if page.Lang == "da" && index >= 0 {
		index += len(maintenanceContactViews)
	}
	if index < 0 || len(maintenanceContactReferences) != 12 {
		return errors.New("unknown frozen contact state")
	}
	got, err := maintenanceContactDigest(markup, page.Lang, task)
	if err != nil || got != maintenanceContactReferences[index] {
		return errors.New("complete contact body differs from frozen reference")
	}
	return nil
}

// Both native forms use the same closed input/submit contract; counting a form's
// action alone cannot establish that it still supports the requested operation.
func checkMaintenanceForm(form *html.Node, action string, wantInputs map[string]string, label, help string) error {
	if !maintenanceRendered(form) || linkAttribute(form, "method") != "post" || linkAttribute(form, "action") != action {
		return errors.New("native form operation changed")
	}
	// Neither frozen form has an ID, event handler or submission override.
	// An ID would admit outside controls that never appear below this node.
	if err := maintenanceAttributes(form, "class", "method", "action"); err != nil {
		return err
	}
	for parent := form.Parent; parent != nil; parent = parent.Parent {
		if parent.Type != html.ElementNode {
			continue
		}
		if parent.Namespace != "" || !slices.Contains([]string{"html", "body", "main", "section"}, parent.Data) {
			return errors.New("native form placement changed")
		}
	}
	counts := map[string]int{}
	submitters := 0
	for n := range form.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if !maintenanceRendered(n) {
			return errors.New("native form content is hidden or inert")
		}
		if n.Namespace != "" {
			return errors.New("native form gained foreign content")
		}
		// This frozen layout uses only active HTML controls and their labels
		// and prose. Inert/excluded containers must never supply counted fields.
		if !slices.Contains([]string{"input", "label", "span", "p", "button"}, n.Data) {
			return errors.New("native form content changed")
		}
		if n.Data == "input" {
			if err := maintenanceAttributes(n, "type", "id", "name", "value", "autocomplete", "maxlength", "aria-describedby", "required"); err != nil {
				return err
			}
			name := linkAttribute(n, "name")
			value, known := wantInputs[name]
			if !known || linkAttribute(n, "value") != value {
				return errors.New("native contact input value changed")
			}
			if (name == "csrf_token" || name == "lang" || name == "revision") && linkAttribute(n, "type") != "hidden" {
				return errors.New("native hidden control changed")
			}
			counts[name]++
		}
		if n.Data == "button" {
			// In particular, a named submitter would add an unexpected POST field.
			if err := maintenanceAttributes(n, "type", "aria-describedby"); err != nil {
				return err
			}
			if (linkAttribute(n, "type") != "" && linkAttribute(n, "type") != "submit") || maintenanceText(n) != label || linkAttribute(n, "aria-describedby") != help {
				return errors.New("native submit behavior, label or help changed")
			}
			submitters++
		}
	}
	for name := range wantInputs {
		if counts[name] != 1 {
			return errors.New("native input missing or duplicated")
		}
	}
	if submitters != 1 {
		return errors.New("native submitter missing or duplicated")
	}
	return nil
}

// maintenanceAttributes closes the frozen native-control boundary, including
// additional constraint validation and successful named submitter fields.
func maintenanceAttributes(n *html.Node, allowed ...string) error {
	if n.Namespace != "" {
		return errors.New("native control namespace changed")
	}
	for _, attr := range n.Attr {
		if !slices.Contains(allowed, attr.Key) {
			return errors.New("native control attributes changed")
		}
	}
	return nil
}

// TestContactMaintenanceHelpPlacementRejections moves real help outside both
// forms into inert or foreign content; those descendants are not native targets.
func TestContactMaintenanceHelpPlacementRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	markup := pages[4].markup
	if !*maintenanceTask {
		// A validator control only, never rendered task evidence.
		markup = strings.Replace(markup, `maxlength="120"`, `maxlength="120" aria-describedby="contact-name-help"`, 1)
		markup = strings.Replace(markup, `<label for="contact-email">`, `<p id="contact-name-help">You can update these details later.</p><label for="contact-email">`, 1)
	}
	page := maintenancePage("en", "confirm")
	if checkMaintenanceContact(markup, "en", true) != nil || checkMaintenanceState(markup, page, true) != nil {
		t.Fatal("complete help-placement control was rejected")
	}
	for _, id := range []string{"contact-name-help", "contact-pending", "contact-verification-help"} {
		start := strings.Index(markup, `<p id="`+id+`">`)
		if start < 0 {
			t.Fatal("actual help element missing")
		}
		end := strings.Index(markup[start:], `</p>`)
		if end < 0 {
			t.Fatal("actual help element incomplete")
		}
		help := markup[start : start+end+len(`</p>`)]
		for name, replacement := range map[string]string{
			"template": `<template>` + help + `</template>`,
			"svg":      `<svg>` + strings.Replace(strings.Replace(help, `<p `, `<text `, 1), `</p>`, `</text>`, 1) + `</svg>`,
			"math":     `<math>` + strings.Replace(strings.Replace(help, `<p `, `<mtext `, 1), `</p>`, `</mtext>`, 1) + `</math>`,
		} {
			t.Run(id+"/"+name, func(t *testing.T) {
				bad := strings.Replace(strings.Replace(markup, help, "", 1), `</main>`, replacement+`</main>`, 1)
				if bad == markup {
					t.Fatal("refusal control did not alter its fixture")
				}
				if checkMaintenanceContact(bad, "en", true) == nil && checkMaintenanceState(bad, page, true) == nil {
					t.Fatal("inert or foreign help target was accepted")
				}
			})
		}
	}
}

// TestContactMaintenanceRejections tests the validator, never comparative samples.
func TestContactMaintenanceRejections(t *testing.T) {
	pages, err := renderedMaintenancePages("contact.html")
	if err != nil || len(pages) != 12 {
		t.Fatal("actual contact inventory unavailable")
	}
	// A reviewed task fixture for the validator, not comparative evidence. The
	// real trial must edit source, compile it and render it separately.
	markup := pages[0].markup
	if !*maintenanceTask {
		markup = strings.Replace(markup, "Appearance", "Colour theme", 1)
		markup = strings.Replace(markup, `maxlength="120"`, `maxlength="120" aria-describedby="contact-name-help"`, 1)
		markup = strings.Replace(markup, `<label for="contact-email">`, `<p id="contact-name-help">You can update these details later.</p><label for="contact-email">`, 1)
	}
	if err := checkMaintenanceDocument(markup, "en", true, true); err != nil {
		t.Fatal("complete validator fixture was rejected", err)
	}
	nameStart := strings.Index(markup, `<input id="contact-name"`)
	if nameStart < 0 {
		t.Fatal("actual native name input missing")
	}
	nameEnd := strings.Index(markup[nameStart:], `>`)
	if nameEnd < 0 {
		t.Fatal("actual native name input incomplete")
	}
	nameInput := markup[nameStart : nameStart+nameEnd+1]
	for name, bad := range map[string]string{
		"old theme":           strings.Replace(markup, "Colour theme", "Appearance", 1),
		"missing hint":        strings.Replace(markup, `<p id="contact-name-help">You can update these details later.</p>`, "", 1),
		"duplicate hint":      strings.Replace(markup, `</main>`, `<p id="contact-name-help">You can update these details later.</p></main>`, 1),
		"disconnected hint":   strings.Replace(markup, `aria-describedby="contact-name-help"`, `aria-describedby="absent"`, 1),
		"wrong hint":          strings.Replace(markup, "You can update these details later.", "Different hint", 1),
		"lost name limit":     strings.Replace(markup, `maxlength="120"`, `maxlength="121"`, 1),
		"lost email help":     strings.Replace(markup, `aria-describedby="contact-pending"`, "", 1),
		"lost required":       strings.Replace(markup, ` required`, "", 1),
		"wrong native action": strings.Replace(markup, `action="/portal/contact"`, `action="/portal/elsewhere"`, 1),
		"missing document":    "<p>Partial output</p>",
		"wrong language":      strings.Replace(markup, `lang="en"`, `lang="da"`, 1),
		"duplicate field":     strings.Replace(markup, `</main>`, `<input id="contact-name"></main>`, 1),
		"lost email limit":    strings.Replace(markup, `maxlength="254"`, `maxlength="255"`, 1),
		"lost company limit":  strings.Replace(markup, `maxlength="160"`, `maxlength="161"`, 1),
		"hidden name":         strings.Replace(markup, `name="name"`, `name="name" type="hidden"`, 1),
		"checkbox name":       strings.Replace(markup, `name="name"`, `name="name" type="checkbox"`, 1),
		"required company":    strings.Replace(markup, `name="company"`, `name="company" required`, 1),
		"checkbox company":    strings.Replace(markup, `name="company"`, `name="company" type="checkbox"`, 1),
		"foreign name":        strings.Replace(markup, nameInput, `<svg>`+nameInput+`</input></svg>`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bad == markup {
				t.Fatal("refusal control did not alter its fixture")
			}
			if err := checkMaintenanceDocument(bad, "en", true, true); err == nil {
				t.Fatal("incomplete or altered maintenance output was accepted")
			}
		})
	}
}

// checkMaintenanceDocument checks the real private frame separately from its body.
func checkMaintenanceDocument(markup, lang string, contact, task bool) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse presentation")
	}
	counts := map[string]int{}
	theme := "Appearance"
	if task {
		theme = "Colour theme"
	}
	if lang == "da" {
		theme = "Udseende"
		if task {
			theme = "Farvetema"
		}
	}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "html" && linkAttribute(n, "lang") == lang {
			counts["language"]++
		}
		if n.Data == "main" && linkAttribute(n, "id") == "main" {
			counts["main"]++
		}
		if n.Data == "footer" && maintenanceText(n) == "Devantler Tech" {
			counts["footer"]++
		}
		if n.Data == "meta" && linkAttribute(n, "name") == "robots" && linkAttribute(n, "content") == "noindex,nofollow" {
			counts["private"]++
		}
		if n.Data == "select" && linkAttribute(n, "id") == "theme" {
			if !maintenanceRendered(n) || n.Parent == nil || n.Parent.Data != "label" || maintenanceText(n.Parent) != theme {
				return errors.New("appearance label is not associated with its control")
			}
			counts["theme"]++
		}
	}
	for _, key := range []string{"language", "main", "footer", "private", "theme"} {
		if counts[key] != 1 {
			return errors.New("incomplete or duplicated private frame")
		}
	}
	if contact {
		return checkMaintenanceContact(markup, lang, task)
	}
	return nil
}

// Select options have their own labels, not part of the wrapping label's name.
func maintenanceText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "select" {
			text.WriteString(maintenanceText(child))
		}
	}
	return strings.TrimSpace(text.String())
}

// checkMaintenanceContact preserves field semantics and the task's accessible hint.
func checkMaintenanceContact(markup, lang string, task bool) error {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return errors.New("cannot parse contact body")
	}
	ids := map[string][]*html.Node{}
	labels := map[string][]*html.Node{}
	var forms []*html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if id := linkAttribute(n, "id"); id != "" {
			ids[id] = append(ids[id], n)
		}
		if n.Data == "label" {
			labels[linkAttribute(n, "for")] = append(labels[linkAttribute(n, "for")], n)
		}
		if n.Data == "form" && linkAttribute(n, "class") == "contact-form" {
			forms = append(forms, n)
		}
	}
	if len(forms) != 1 || forms[0].Namespace != "" || linkAttribute(forms[0], "method") != "post" || linkAttribute(forms[0], "action") != "/portal/contact" {
		return errors.New("native contact form changed")
	}
	page := maintenancePage(lang, "")
	for _, field := range []struct{ id, name, limit, autocomplete, label string }{
		{"contact-name", "name", "120", "name", page.NameLabel},
		{"contact-email", "email", "254", "email", page.EmailLabel},
		{"contact-company", "company", "160", "organization", page.CompanyLabel},
	} {
		if len(ids[field.id]) != 1 {
			return errors.New("missing or duplicate contact field")
		}
		n := ids[field.id][0]
		limit := "(up to " + field.limit + " characters)"
		if lang == "da" {
			limit = "(højst " + field.limit + " tegn)"
		}
		if len(labels[field.id]) != 1 || labels[field.id][0].Namespace != "" || maintenanceText(labels[field.id][0]) != field.label+" "+limit {
			return errors.New("contact label is missing, unnamed or ambiguous")
		}
		if err := maintenanceAttributes(labels[field.id][0], "for"); err != nil {
			return err
		}
		for child := range labels[field.id][0].Descendants() {
			if child.Type != html.ElementNode {
				continue
			}
			if child.Data != "span" || linkAttribute(child, "class") != "field-limit" {
				return errors.New("contact label content changed")
			}
			if err := maintenanceAttributes(child, "class"); err != nil {
				return err
			}
		}
		labelParent := labels[field.id][0].Parent
		for labelParent != nil && labelParent != forms[0] {
			if labelParent.Namespace != "" || (labelParent.Type == html.ElementNode && !slices.Contains([]string{"label", "span", "p"}, labelParent.Data)) {
				return errors.New("contact label placement changed")
			}
			labelParent = labelParent.Parent
		}
		if labelParent == nil {
			return errors.New("contact label is outside its native form")
		}
		if n.Namespace != "" || n.Data != "input" || linkAttribute(n, "name") != field.name || linkAttribute(n, "maxlength") != field.limit || linkAttribute(n, "autocomplete") != field.autocomplete {
			return errors.New("contact input contract changed")
		}
		kind := linkAttribute(n, "type")
		if (field.name == "email" && kind != "email") || (field.name != "email" && kind != "" && kind != "text") {
			return errors.New("contact field type changed")
		}
		parent := n.Parent
		for parent != nil && parent != forms[0] {
			parent = parent.Parent
		}
		if parent == nil {
			return errors.New("contact input is outside its native form")
		}
		required := false
		for _, attr := range n.Attr {
			required = required || attr.Key == "required"
		}
		if required != (field.name != "company") {
			return errors.New("contact field requirement changed")
		}
	}
	if linkAttribute(ids["contact-email"][0], "type") != "email" || linkAttribute(ids["contact-email"][0], "aria-describedby") != "contact-pending" || len(ids["contact-pending"]) != 1 || !maintenanceRendered(ids["contact-pending"][0]) || maintenanceText(ids["contact-pending"][0]) == "" {
		return errors.New("email help or semantics changed")
	}
	if task {
		hint := "You can update these details later."
		if lang == "da" {
			hint = "Du kan ændre oplysningerne senere."
		}
		if linkAttribute(ids["contact-name"][0], "aria-describedby") != "contact-name-help" || len(ids["contact-name-help"]) != 1 || !maintenanceRendered(ids["contact-name-help"][0]) || maintenanceText(ids["contact-name-help"][0]) != hint {
			return errors.New("name help is missing, ambiguous or disconnected")
		}
	}
	return nil
}

// maintenanceRendered excludes template fragments, foreign namespaces and
// hidden/inert nodes or ancestors. This is not a CSS or browser visibility test.
func maintenanceRendered(n *html.Node) bool {
	if n == nil {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && (p.Namespace != "" || p.Data == "template") {
			return false
		}
		for _, attr := range p.Attr {
			if attr.Key == "hidden" || attr.Key == "inert" {
				return false
			}
		}
	}
	return true
}
