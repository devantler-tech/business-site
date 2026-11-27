package identity

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestCustomerCanNavigateBetweenAvailablePortalTasks(t *testing.T) {
	b := newBrowserFixture(t)
	b.atRoot()
	flags := []string{"portal-identity", "portal-requests", "portal-appointment-history"}
	setFixturePortalFlags(t, b, flags, []any{true, true, true})
	status, markup, _ := b.request(http.MethodGet, "/contact?lang=en", nil, "")
	if status != http.StatusUnauthorized || strings.Contains(markup, "client-task-navigation") {
		t.Fatal("customer task navigation appeared before sign-in")
	}
	b.as(customerSubject)
	b.invite(customerSubject, "spruce-design", "client")
	callback := b.startAt("/sign-in?lang=en")
	if status, _, headers := b.request(http.MethodGet, callback, nil, ""); status != http.StatusSeeOther || headers.Get("Location") != "/?lang=en" {
		t.Fatal("the fictional customer could not sign in")
	}

	for _, lang := range []string{"en", "da"} {
		for _, area := range []string{"/contact", "/appointments", "/requests"} {
			t.Run(lang+area, func(t *testing.T) {
				checkCustomerTaskNavigation(t, b, lang, area, []string{"/", "/requests", "/appointments", "/contact"})
			})
		}
	}

	setFixturePortalFlags(t, b, flags, []any{true, false, false})
	checkCustomerTaskNavigation(t, b, "en", "/contact", []string{"/", "/contact"})
	status, markup, _ = b.request(http.MethodGet, "/?lang=en", nil, "")
	if status != http.StatusOK || strings.Contains(markup, "client-task-navigation") {
		t.Fatal("home should use its existing task cards without duplicate navigation")
	}
}

func TestCustomerTaskNavigationIsHiddenFromOperators(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.atRoot()
	for path, want := range map[string]int{"/operator": http.StatusOK, "/contact": http.StatusForbidden} {
		status, markup, _ := b.request(http.MethodGet, path+"?lang=en", nil, "")
		if status != want || strings.Contains(markup, "client-task-navigation") {
			t.Fatalf("unexpected operator response at %s, status=%d, want=%d without customer navigation", path, status, want)
		}
	}
}

func checkCustomerTaskNavigation(t *testing.T, b *browserFixture, lang, area string, destinations []string) {
	t.Helper()
	status, markup, _ := b.request(http.MethodGet, area+"?lang="+lang, nil, "")
	if status != http.StatusOK {
		t.Fatalf("%s returned status %d", area, status)
	}
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var current, paths []string
	label := "Workspace"
	if lang == "da" {
		label = "Arbejdsområde"
	}
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Data != "nav" || linkAttribute(node, "aria-label") != label {
			continue
		}
		for link := range node.Descendants() {
			if link.Type != html.ElementNode || link.Data != "a" {
				continue
			}
			href := linkAttribute(link, "href")
			paths = append(paths, href)
			if linkAttribute(link, "aria-current") == "page" {
				current = append(current, href)
			}
		}
	}
	var want []string
	for _, dest := range destinations {
		want = append(want, dest+"?lang="+lang)
	}
	if !slices.Equal(paths, want) || !slices.Equal(current, []string{area + "?lang=" + lang}) {
		t.Fatalf("customer task navigation paths=%v, current=%v; want %v with %s current", paths, current, want, area)
	}
}
