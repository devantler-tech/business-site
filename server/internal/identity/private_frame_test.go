package identity

import (
	"html/template"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestPrivateFrameChangeReachesEveryPage catches a page that retains an
// independent document/header/footer when the common frame changes. It renders
// the real page inventory with changed partials, not a mocked page or source grep.
func TestPrivateFrameChangeReachesEveryPage(t *testing.T) {
	for _, name := range []string{"portal.html", "contact.html", "requests.html", "triage.html", "credits.html", "work-review.html"} {
		t.Run(name, func(t *testing.T) {
			pages, err := template.ParseFS(browserFiles, "web/*.html")
			if err != nil {
				t.Fatal(err)
			}
			_, err = pages.Parse(`{{define "portal-head"}}<html lang="{{.Lang}}"><head data-frame-probe="head"><title>Fictional frame</title></head><body>{{end}}
{{define "portal-header"}}<header data-frame-probe="header">Fictional frame</header>{{end}}
{{define "portal-footer"}}<footer data-frame-probe="footer">Fictional frame</footer></body></html>{{end}}`)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := renderedLinkPagesUsing(name, pages)
			if err != nil || len(rendered) == 0 {
				t.Fatalf("cannot render real page inventory: %v", err)
			}
			for i, page := range rendered {
				for _, part := range []string{"head", "header", "footer"} {
					if strings.Count(page.markup, `data-frame-probe="`+part+`"`) != 1 {
						t.Fatalf("view %d does not use the common %s", i, part)
					}
				}
			}
		})
	}
}

// TestPrivateFramePreservesDocumentControls characterizes the real EN/DA
// documents: private metadata, skip target, native forms and public-only assets
// must survive moving their frame into shared templates.
func TestPrivateFramePreservesDocumentControls(t *testing.T) {
	for _, name := range []string{"portal.html", "contact.html", "requests.html", "triage.html", "credits.html", "work-review.html"} {
		t.Run(name, func(t *testing.T) {
			pages, err := renderedLinkPages(name)
			if err != nil || len(pages) == 0 {
				t.Fatalf("cannot render real page inventory: %v", err)
			}
			for i, page := range pages {
				doc, err := html.Parse(strings.NewReader(page.markup))
				if err != nil {
					t.Fatal(err)
				}
				counts := map[string]int{}
				for n := range doc.Descendants() {
					if n.Type != html.ElementNode {
						continue
					}
					counts[n.Data]++
					if n.Data == "html" && linkAttribute(n, "lang") != "en" && linkAttribute(n, "lang") != "da" {
						t.Fatal("document language lost")
					}
					if n.Data == "meta" && linkAttribute(n, "name") == "robots" && linkAttribute(n, "content") == "noindex,nofollow" {
						counts["private-robots"]++
					}
					if n.Data == "main" && linkAttribute(n, "id") == "main" {
						counts["main-target"]++
					}
					if n.Data == "a" && linkAttribute(n, "class") == "skip" && linkAttribute(n, "href") == "#main" {
						counts["skip-link"]++
					}
					if n.Data == "select" && linkAttribute(n, "id") == "theme" {
						counts["theme-control"]++
					}
					if n.Data == "link" && linkAttribute(n, "rel") == "stylesheet" && linkAttribute(n, "href") == "/portal/assets/portal.css" {
						counts["public-style"]++
					}
					if n.Data == "script" && linkAttribute(n, "src") == "/portal/assets/portal.js" {
						counts["public-script"]++
					}
					if n.Data == "form" {
						if linkAttribute(n, "method") != "post" {
							t.Fatal("native private form lost its POST method")
						}
						csrf := 0
						for child := range n.Descendants() {
							if child.Type == html.ElementNode && child.Data == "input" && linkAttribute(child, "name") == "csrf_token" && linkAttribute(child, "type") == "hidden" {
								csrf++
							}
						}
						if csrf != 1 {
							t.Fatal("native private form lost its unique CSRF field")
						}
					}
				}
				for _, control := range []string{"html", "head", "body", "title", "header", "footer", "main", "private-robots", "main-target", "skip-link", "theme-control", "public-style", "public-script", "script"} {
					if counts[control] != 1 {
						t.Fatalf("view %d: %s count=%d, want one", i, control, counts[control])
					}
				}
				if strings.Contains(page.markup, "<script>inert</script>") {
					t.Fatal("private proposal content escaped its text context")
				}
			}
		})
	}
}
