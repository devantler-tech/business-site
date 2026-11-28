package identity

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// The preprocessor runs this test binary only. No diagnostic or fixture entrypoint
// is exported from the production identity package or reachable over HTTP.
var linkSource = flag.String("portal-link-source", "", "source selected by the link checker")
var linkOutput = flag.String("portal-link-output", "", "new private output in the wrapper's scratch directory")

type linkPage struct {
	uri, markup string
}

func TestPortalLinkPreprocessor(t *testing.T) {
	if *linkSource != "" || *linkOutput != "" {
		if *linkSource == "" || *linkOutput == "" {
			t.Fatal("both preprocessor paths are required")
		}
		output, err := preprocessLinkInput(*linkSource)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(*linkOutput, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("cannot create new private preprocessor output")
		}
		_, writeErr := file.Write(output)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("cannot complete preprocessor output")
		}
		return
	}
	for name, want := range map[string]int{"portal.html": 24, "contact.html": 12, "requests.html": 38, "triage.html": 24, "work-review.html": 18, "operator.html": 40} {
		t.Run(name, func(t *testing.T) {
			pages, err := renderedLinkPages(name)
			if err != nil || len(pages) != want {
				t.Fatalf("incomplete rendered cases: count=%d want=%d error=%v", len(pages), want, err)
			}
			for _, page := range pages {
				output, count, err := validateRenderedLinks(page)
				if err != nil || count < 5 || strings.Contains(output, "{{") {
					t.Fatalf("rendered link contract: targets=%d error=%v", count, err)
				}
				publicPath := "/"
				if strings.Contains(page.uri, "lang=da") {
					publicPath = "/da/"
				}
				if !strings.Contains(output, `href="https://devantler.tech`+publicPath+`"`) || !strings.Contains(output, "data-route-checked-") {
					t.Fatal("public homepage or explicit method-aware coverage missing")
				}
			}
		})
	}
}

func TestPortalLinkRejections(t *testing.T) {
	for name, body := range map[string]string{
		"unknown route":                 `<a href="/portal/unknown">Unknown</a>`,
		"unknown asset":                 `<script src="/portal/assets/unknown.js"></script>`,
		"raw template":                  `<a href="{{.Next}}">Next</a>`,
		"wrong logout method":           `<a href="/portal/logout">Sign out</a>`,
		"wrong form method":             `<form method="post" action="/portal/operator/requests"></form>`,
		"missing form method":           `<form action="/portal/logout"></form>`,
		"overridden form method":        `<form method="post"><button formmethod="get" formaction="/portal/logout">Out</button></form>`,
		"inherited action override":     `<form method="post" action="/portal/logout"><button formmethod="get">Out</button></form>`,
		"associated form override":      `<form id="logout" method="post" action="/portal/logout"></form><button form="logout" formmethod="get">Out</button>`,
		"non-canonical router redirect": `<a href="/portal//contact">Contact</a>`,
		"invalid ID":                    `<a href="/portal/operator/requests/not-an-id">Request</a>`,
		"invalid nested ID":             `<form method="post" action="/portal/operator/requests/not-an-id/proposals"></form>`,
		"invalid invoice":               `<a href="/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/invoices/0/credits">Credits</a>`,
		"invalid cursor":                `<a href="/portal/operator/requests?cursor=invalid">Next</a>`,
		"duplicate language":            `<a href="/portal/?lang=en&amp;lang=da">Home</a>`,
		"unknown language":              `<a href="/portal/?lang=de">Home</a>`,
		"unknown query":                 `<a href="/portal/?next=elsewhere">Home</a>`,
		"misplaced cursor":              `<a href="/portal/contact?cursor=invalid">Contact</a>`,
		"misplaced workspace":           `<a href="/portal/contact?workspace=sample">Contact</a>`,
		"invalid invitation workspace":  `<a href="/portal/operator/invitations/new?workspace=%0A">Invite</a>`,
		"missing fragment":              `<a href="#absent">Skip</a>`,
		"encoded route":                 `<a href="/portal%2f/logout">Out</a>`,
		"empty target":                  `<a href="">Nothing</a>`,
		"base redirect":                 `<base href="https://example.invalid/"><a href="/portal/">Home</a>`,
		"no observed target":            `<p>No navigation was observed.</p>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := validateRenderedLinks(linkPage{"/portal/?lang=en", "<!doctype html><html><body>" + body + "</body></html>"})
			if err == nil {
				t.Fatal("invalid rendered target was accepted")
			}
		})
	}
}

func TestPortalLinkNestedProposalMethod(t *testing.T) {
	markup := `<html><body><a href="/">Home</a><form method="post" action="/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals"><button>Save</button></form></body></html>`
	output, count, err := validateRenderedLinks(linkPage{"/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ?lang=en", markup})
	if err != nil || count != 3 || !strings.Contains(output, `data-route-checked-action="/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals"`) {
		t.Fatalf("registered nested proposal POST rejected: count=%d error=%v", count, err)
	}
}

func TestPortalLinkRequestContexts(t *testing.T) {
	pages, err := renderedLinkPages("requests.html")
	if err != nil {
		t.Fatal(err)
	}
	for langIndex, lang := range []string{"en", "da"} {
		for productIndex := range 3 {
			for viewIndex, path := range []string{"/portal/requests/new", "/portal/requests", "/portal/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ", "/portal/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
				page := pages[langIndex*19+productIndex*4+viewIndex]
				if page.uri != path+"?lang="+lang {
					t.Errorf("render context=%s want=%s", page.uri, path+"?lang="+lang)
				}
			}
		}
	}
}

func TestPortalLinkRenderFailure(t *testing.T) {
	original := browserPage
	defer func() { browserPage = original }()
	for _, name := range []string{"contact.html", "requests.html"} {
		t.Run(name, func(t *testing.T) {
			data, err := browserFiles.ReadFile("web/" + name)
			if err != nil {
				t.Fatal(err)
			}
			clone, err := template.New("portal").Funcs(templateFuncs(internalRoot, "")).ParseFS(browserFiles, "web/*.html")
			if err != nil {
				t.Fatal(err)
			}
			browserPage, err = clone.New(name).Parse(string(data) + "{{.AbsentField}}")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := renderedLinkPages(name); err == nil {
				t.Fatal("template execution failure was reported as successful rendering")
			}
			browserPage = original
		})
	}
}

func TestPortalLinkCoverageSplit(t *testing.T) {
	page := linkPage{"/portal/?lang=en", `<html><body><a href="https://example.invalid/broken">Broken public link</a><form method="post" action="/portal/logout"><button>Out</button></form></body></html>`}
	output, count, err := validateRenderedLinks(page)
	if err != nil || count != 3 || !strings.Contains(output, `href="https://example.invalid/broken"`) || !strings.Contains(output, `data-route-checked-action="/portal/logout"`) {
		t.Fatalf("target accounting or ordinary Lychee target changed: count=%d error=%v", count, err)
	}
}

// preprocessLinkInput accepts only regular repository-local inputs. Only the four
// exact embedded templates are rendered; ordinary documents remain byte-identical.
func preprocessLinkInput(source string) ([]byte, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, errors.New("cannot establish module root")
	}
	path, err := filepath.Abs(source)
	if err != nil {
		return nil, errors.New("invalid source path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, errors.New("source is absent or linked")
	}
	rel, err := filepath.Rel(filepath.Dir(root), path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, errors.New("source is outside the repository")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 512*1024 {
		return nil, errors.New("source is not a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("source read failed")
	}
	name := filepath.Base(path)
	if filepath.ToSlash(rel) != "server/internal/identity/web/"+name {
		return data, nil
	}
	embedded, err := browserFiles.ReadFile("web/" + name)
	if err != nil || !bytes.Equal(data, embedded) {
		return nil, errors.New("input differs from the compiled source")
	}
	renderedName := name
	if name == "operator-workspaces.html" || name == "operator-customers.html" {
		renderedName = "operator.html"
	}
	pages, err := renderedLinkPages(renderedName)
	if err != nil {
		return nil, err
	}
	var result strings.Builder
	for _, page := range pages {
		output, count, err := validateRenderedLinks(page)
		if err != nil || count < 5 {
			return nil, fmt.Errorf("incomplete rendered link coverage for %s: %w", name, err)
		}
		fmt.Fprintf(&result, "<!-- method-aware link contract: %s targets=%d -->\n%s\n", name, count, output)
	}
	return []byte(result.String()), nil
}

func renderedLinkPages(name string) ([]linkPage, error) {
	return renderedLinkPagesUsing(name, browserPage)
}

// renderedLinkPagesUsing applies the complete fictional view inventory to one
// compiled template set without replacing the process-wide production renderer.
func renderedLinkPagesUsing(name string, templates *template.Template) ([]linkPage, error) {
	return renderedLinkPagesFor(name, templates, &browser{})
}

// renderedLinkPagesFor renders the same inventory through a chosen browser, so a
// root-mounted portal can be compared with the shared-host one.
func renderedLinkPagesFor(name string, templates *template.Template, b *browser) ([]linkPage, error) {
	if name == "work-review.html" {
		return renderedWorkLinkPagesFor(templates, b)
	}
	request := projectRequest{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Key: "fictional-submission", Product: "website", Title: "Fictional request", Brief: "Synthetic link-validation content", Submitted: time.Date(2026, 10, 1, 12, 0, 0, 123000, time.UTC)}
	var pages []linkPage
	for _, lang := range []string{"en", "da"} {
		add := func(uri string, render func(*httptest.ResponseRecorder, *http.Request) error) error {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "https://portal.example.invalid"+uri+"?lang="+lang, nil)
			if err := render(w, r); err != nil {
				return err
			}
			pages = append(pages, linkPage{r.URL.RequestURI(), w.Body.String()})
			return nil
		}
		switch name {
		case "operator.html":
			for _, view := range []string{"list", "active", "former", "remove"} {
				err := add(customerAccountsPath, func(w *httptest.ResponseRecorder, r *http.Request) error {
					d := b.operatorData(r, "operator-customer-accounts", "fictional-operator")
					d.CSRF = "fictional-csrf"
					index := b.visible(customerAccountsPath) + "?lang=" + lang
					account := customerAccountRow{Subject: customeridentity.DexSubject("fictional-customer", customeridentity.ZitadelConnectorID), Name: "Ada Example", Email: "ada@example.test",
						Workspace: "fictional-workspace", WorkspaceName: "Fictional workspace", Status: "active", ManageAccess: true}
					account.URL = index + "&account=" + account.Subject
					page := customerAccountsPage{pageData: d, Query: "Ada", BackURL: index,
						Confirmation: account.URL + "&view=remove", Previous: index + "&page=1", Next: index + "&page=3"}
					if view == "list" {
						page.Accounts = []customerAccountRow{account}
					} else {
						page.Account = &account
						page.Title = account.Name
						page.ConfirmRemoval = view == "remove"
						if view == "former" {
							account.Status = view
							account.ManageAccess = false
						}
					}
					return templates.ExecuteTemplate(w, name, page)
				})
				if err != nil {
					return nil, err
				}
			}
			for _, view := range []struct {
				path, kind     string
				triage, events bool
			}{
				{"/operator", "operator", false, false},
				{"/operator", "operator", true, false},
				{"/operator/customers", "operator-customers", true, false},
				{"/operator/invitations/new", "operator-invite", true, false},
				{"/operator/audit", "operator-audit", true, false},
				{"/operator/audit", "operator-audit", true, true},
				{"/operator/support", "operator-support", true, false},
			} {
				err := add("/portal"+view.path, func(w *httptest.ResponseRecorder, r *http.Request) error {
					d := b.operatorData(r, view.kind, "fictional-operator")
					d.CSRF, d.TriageEnabled = "fictional-csrf", view.triage
					if view.kind == "operator-customers" {
						return templates.ExecuteTemplate(w, name, managementPage{
							pageData: d, SupportAvailable: true,
							Workspaces: []workspaceView{{ID: "fictional-workspace", Name: "Fictional workspace", Manageable: true, Members: []workspaceMember{
								{Subject: strings.Repeat("FictionalMember", 9), Role: "client"},
								{Subject: "fictional-disabled", Role: "client", Revoked: true},
							}}},
							Invitations: []pendingInvitation{{Subject: "fictional-invitation", Workspace: "fictional-workspace", Expires: request.Submitted}},
						})
					}
					if view.kind == "operator-support" {
						d.RequestsEnabled = true
						return templates.ExecuteTemplate(w, name, supportPage{
							pageData: d, Case: "CASE-1234", ContactSaved: true, SharingOpen: true,
							Requests: []supportRequest{{Product: "website", Submitted: request.Submitted, ProposalShared: true}},
						})
					}
					if view.kind == "operator-invite" {
						return templates.ExecuteTemplate(w, name, managementPage{
							pageData: d, Workspaces: []workspaceView{{ID: "fictional-workspace", Name: "Fictional workspace", Manageable: true}},
						})
					}
					if view.events {
						d.Audit = []auditEntry{
							{At: request.Submitted, Kind: "operator", Subject: "fictional-operator", Action: "invitations.create", Target: "fictional-workspace", Outcome: "allowed"},
							{At: request.Submitted.Add(-time.Minute), Kind: "agent", Subject: "fictional-agent", Action: "workspaces.list", Outcome: "refused"},
						}
					}
					if view.kind == "operator-audit" {
						page := auditSearchPage{pageData: d}
						if view.events {
							event := auditSearchEvent{auditEntry: d.Audit[0], ID: "f0000000-0000-4000-8000-000000000001"}
							page.Older = page.Search.pageLink(b.visible("/portal/operator/audit"), lang, "before", event)
							page.Newer = page.Search.pageLink(b.visible("/portal/operator/audit"), lang, "after", event)
						}
						return templates.ExecuteTemplate(w, name, page)
					}
					return templates.ExecuteTemplate(w, name, d)
				})
				if err != nil {
					return nil, err
				}
			}
			for _, view := range []string{"workspace", "close", "remove", "closed", "cleanup", "cleanup-confirm", "purge-confirm", "new", "support"} {
				err := add(customerWorkspacePath, func(w *httptest.ResponseRecorder, r *http.Request) error {
					d := b.operatorData(r, "operator-customers", "fictional-operator")
					d.CSRF = "fictional-csrf"
					member := workspaceMember{Subject: "fictional-member", Role: "client", Person: customeridentity.Profile{Name: "Ada Example", Email: "ada@example.test"}}
					detail := workspaceView{ID: "fictional-workspace", Name: "Fictional workspace", Manageable: true, Members: []workspaceMember{member}}
					closed := closedCustomerWorkspace{ID: detail.ID, Name: detail.Name, PurgeState: "eligible", At: request.Submitted, RetentionStart: request.Submitted}
					d.Title, d.Intro = detail.Name, ""
					data := managementPage{pageData: d, View: view, Detail: &detail, SelectedMember: &member, SupportAvailable: true, SupportMembers: true,
						Invitations: []pendingInvitation{{Subject: "fictional-invitation", Workspace: detail.ID, Expires: request.Submitted, Person: customeridentity.Profile{Name: "Robin Example", Email: "robin@example.test"}}},
						Closed:      []closedCustomerWorkspace{closed},
						Purge:       &closed,
						Stale:       []staleWorkspaceInvitations{{Workspace: detail.ID, Name: detail.Name, Count: 1}},
					}
					return templates.ExecuteTemplate(w, name, cleanupPage{managementPage: data, CleanupCount: 1, CleanupFingerprint: "fictional-preview"})
				})
				if err != nil {
					return nil, err
				}
			}
		case "portal.html":
			for _, kind := range []string{"no-access", "home", "operator", "error", "unavailable", "triage-invalid", "home-links", "operator-links", "proposal-invalid", "proposal-conflict", "sharing-invalid", "sharing-conflict"} {
				uri := "/portal/"
				switch kind {
				case "no-access", "unavailable":
					uri = "/portal/sign-in"
				case "operator", "operator-links":
					uri = "/portal/operator"
				case "triage-invalid":
					uri = "/portal/operator/requests"
				case "error":
					uri = "/portal/logout"
				}
				err := add(uri, func(w *httptest.ResponseRecorder, r *http.Request) error {
					d := b.data(r, strings.TrimSuffix(kind, "-links"), "fictional-client", "fictional-csrf")
					d.RequestsEnabled = kind == "home-links"
					d.TriageEnabled = kind == "operator-links"
					return templates.ExecuteTemplate(w, name, d)
				})
				if err != nil {
					return nil, err
				}
			}
		case "contact.html":
			for _, problem := range []string{"", "invalid", "conflict", "verification", "confirm", "confirmed"} {
				if err := add("/portal/contact", func(w *httptest.ResponseRecorder, r *http.Request) error {
					d := contactData{Name: "Fictional client", Email: "fixture@example.invalid", Revision: 1, EmailVerified: problem == "confirmed"}
					view := problem
					if problem == "confirm" || problem == "confirmed" {
						view = ""
					}
					return templates.ExecuteTemplate(w, name, b.contactPage(r, d, view, problem == "confirm"))
				}); err != nil {
					return nil, err
				}
			}
		case "requests.html":
			for _, product := range []string{"website", "app", "service"} {
				request.Product = product
				for _, view := range []string{"new", "invalid", "receipt", "shared"} {
					uri := "/portal/requests"
					switch view {
					case "new":
						uri += "/new"
					case "receipt", "shared":
						uri += "/" + request.ID
					}
					if err := add(uri, func(w *httptest.ResponseRecorder, r *http.Request) error {
						problem, selected := "", view
						if view == "invalid" {
							problem, selected = "invalid", "new"
							r.Method = http.MethodPost
						}
						page := b.requestPage(r, selected, request, nil, problem)
						if view == "shared" {
							page.View = "receipt"
							page.SharedProposal = &projectProposal{Version: 1, Scope: "Fictional shared scope", Deliverables: "Fictional deliverables", OneTimeMinor: 299500, MonthlyMinor: 9900}
						}
						return templates.ExecuteTemplate(w, name, page)
					}); err != nil {
						return nil, err
					}
				}
			}
			for _, rows := range [][]projectRequest{nil, {request}} {
				if err := add("/portal/requests", func(w *httptest.ResponseRecorder, r *http.Request) error {
					return templates.ExecuteTemplate(w, name, b.requestPage(r, "list", request, rows, ""))
				}); err != nil {
					return nil, err
				}
			}
			if err := add("/portal/requests", func(w *httptest.ResponseRecorder, r *http.Request) error {
				r.Method = http.MethodPost
				return templates.ExecuteTemplate(w, name, b.requestPage(r, "conflict", request, nil, "conflict"))
			}); err != nil {
				return nil, err
			}
			for _, view := range []string{"review", "acceptance-conflict", "accepted-current", "accepted-historical"} {
				if err := add("/portal/requests/"+request.ID, func(w *httptest.ResponseRecorder, r *http.Request) error {
					page := b.requestPage(r, view, request, nil, "")
					page.AcceptanceEnabled = true
					proposal := projectProposal{Version: 1, Scope: "Fictional scope", Deliverables: "Fictional deliverables", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One iteration", MonthlyTerms: "Hosting"}
					if view == "review" {
						page.Review = &proposalReview{Key: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Proposal: proposal}
						page.SharedProposal = &proposal
					} else if view != "acceptance-conflict" {
						page.View = "receipt"
						page.Acceptances = []proposalAcceptance{{ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Proposal: proposal, Recorded: request.Submitted, Current: view == "accepted-current"}}
					}
					return templates.ExecuteTemplate(w, name, page)
				}); err != nil {
					return nil, err
				}
			}
		case "triage.html":
			many := make([]projectRequest, triagePageSize+1)
			for i := range many {
				many[i] = request
			}
			for _, rows := range [][]projectRequest{nil, {request}, many} {
				if err := add("/portal/operator/requests", func(w *httptest.ResponseRecorder, r *http.Request) error {
					return templates.ExecuteTemplate(w, name, b.triagePage(r, rows, "", ""))
				}); err != nil {
					return nil, err
				}
			}
			for _, product := range []string{"website", "app", "service"} {
				request.Product = product
				if err := add("/portal/operator/requests/"+request.ID, func(w *httptest.ResponseRecorder, r *http.Request) error {
					return templates.ExecuteTemplate(w, name, b.triagePage(r, []projectRequest{request}, request.ID, ""))
				}); err != nil {
					return nil, err
				}
			}
			for _, view := range []string{"empty", "history", "full", "hostile", "sharing", "withdrawal"} {
				if err := add("/portal/operator/requests/"+request.ID, func(w *httptest.ResponseRecorder, r *http.Request) error {
					page := b.triagePage(r, []projectRequest{request}, request.ID, "")
					var proposals []projectProposal
					if view != "empty" {
						proposals = []projectProposal{{Version: 1, Scope: "Fictional scope", Deliverables: "Fictional deliverables", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One iteration", MonthlyTerms: "Hosting and maintenance", Prepared: request.Submitted}}
					}
					if view == "full" {
						proposals[0].Version = 50
					}
					if view == "hostile" {
						proposals[0].Scope = "<script>inert</script>"
					}
					b.proposalPresentation(r, &page, proposals)
					if view == "sharing" || view == "withdrawal" {
						page.SharingEnabled = true
						page.SharingKey = "fictional-sharing-key"
						page.Visibility.Shared = make(map[int]bool)
						if view == "withdrawal" {
							page.Visibility = proposalVisibility{Event: 1, Active: 1, Shared: map[int]bool{1: true}}
						}
					}
					return templates.ExecuteTemplate(w, name, page)
				}); err != nil {
					return nil, err
				}
			}
		default:
			return nil, errors.New("template has no complete rendered-case contract")
		}
	}
	return pages, nil
}

func linkAttribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// validateRenderedLinks accounts for every URL-bearing attribute. Checked private
// targets and the runtime's fixed review handoff remain explicit data
// attributes. Other public and external targets retain normal Lychee checking.
func validateRenderedLinks(page linkPage) (string, int, error) {
	if strings.Contains(page.markup, "{{") {
		return "", 0, errors.New("unexpanded template field")
	}
	if !strings.HasSuffix(strings.TrimSpace(page.markup), "</html>") {
		return "", 0, errors.New("incomplete rendered document")
	}
	doc, err := html.Parse(strings.NewReader(page.markup))
	if err != nil {
		return "", 0, err
	}
	mux, err := (&browser{}).routes()
	if err != nil {
		return "", 0, err
	}
	ids := map[string]*html.Node{}
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && n.Data == "base" {
			return "", 0, errors.New("unexpected base element")
		}
		if id := linkAttribute(n, "id"); id != "" {
			if ids[id] != nil {
				return "", 0, errors.New("duplicate fragment identity")
			}
			ids[id] = n
		}
	}
	base, err := url.Parse("https://portal.example.invalid" + page.uri)
	if err != nil {
		return "", 0, err
	}
	count := 0
	// A submitter may override its form's method without having a formaction.
	// Account for effective submissions before rewriting any original attributes.
	for n := range doc.Descendants() {
		kind := strings.ToLower(linkAttribute(n, "type"))
		submitter := n.Data == "button" && (kind == "" || kind == "submit") || n.Data == "input" && (kind == "submit" || kind == "image")
		if n.Type != html.ElementNode || !submitter {
			continue
		}
		form, method, err := linkForm(n, ids)
		if err != nil {
			return "", count, err
		}
		action := linkAttribute(form, "action")
		for _, attr := range n.Attr {
			if attr.Key == "formaction" {
				action = attr.Val
			}
		}
		ref, err := url.Parse(action)
		if err != nil {
			return "", count, errors.New("malformed submission target")
		}
		target := base.ResolveReference(ref)
		if target.Host == base.Host && (target.Path == "/portal" || strings.HasPrefix(target.Path, "/portal/")) {
			if err := registeredLinkTarget(mux, method, target); err != nil {
				return "", count, err
			}
		}
		count++
		n.Attr = append(n.Attr, html.Attribute{Key: "data-route-checked-submission", Val: method + " " + target.String()})
	}
	for n := range doc.Descendants() {
		for i, attr := range n.Attr {
			if attr.Key != "href" && attr.Key != "src" && attr.Key != "action" && attr.Key != "formaction" && attr.Key != "poster" && attr.Key != "data" {
				continue
			}
			count++
			ref, err := url.Parse(attr.Val)
			if err != nil || attr.Val == "" || strings.ContainsAny(attr.Val, "{}") {
				return "", count, errors.New("malformed rendered target")
			}
			if strings.HasPrefix(attr.Val, "#") {
				if ids[ref.Fragment] == nil {
					return "", count, errors.New("missing fragment target")
				}
				n.Attr[i].Key = "data-route-checked-" + attr.Key
				continue
			}
			if ref.IsAbs() || ref.Host != "" {
				continue
			}
			if n.Data == "a" && attr.Key == "href" && attr.Val == "/trustpilot/" {
				if err := checkTrustpilotHandoff(); err != nil {
					return "", count, err
				}
				n.Attr[i].Key = "data-route-checked-href"
				continue
			}
			target := base.ResolveReference(ref)
			if target.Path != "/portal" && !strings.HasPrefix(target.Path, "/portal/") {
				target.Host = "devantler.tech"
				n.Attr[i].Val = target.String()
				continue
			}
			method := http.MethodGet
			if attr.Key == "action" || attr.Key == "formaction" {
				_, selected, err := linkForm(n, ids)
				if err != nil {
					return "", count, err
				}
				method = selected
			}
			if err := registeredLinkTarget(mux, method, target); err != nil {
				return "", count, err
			}
			n.Attr[i].Key = "data-route-checked-" + attr.Key
		}
	}
	if count == 0 {
		return "", 0, errors.New("no observed targets")
	}
	var output strings.Builder
	if err := html.Render(&output, doc); err != nil {
		return "", count, err
	}
	return output.String(), count, nil
}

func linkForm(n *html.Node, ids map[string]*html.Node) (*html.Node, string, error) {
	form := n
	if id := linkAttribute(n, "form"); id != "" {
		form = ids[id]
	} else {
		for form != nil && form.Data != "form" {
			form = form.Parent
		}
	}
	if form == nil || form.Data != "form" {
		return nil, "", errors.New("submission target has no form")
	}
	method := strings.ToUpper(linkAttribute(form, "method"))
	for _, attr := range n.Attr {
		if attr.Key == "formmethod" {
			method = strings.ToUpper(attr.Val)
		}
	}
	if method == "" {
		method = http.MethodGet
	}
	return form, method, nil
}

func registeredLinkTarget(mux *http.ServeMux, method string, target *url.URL) error {
	if target.RawPath != "" || target.Fragment != "" || (method != http.MethodGet && method != http.MethodPost) {
		return errors.New("non-canonical private target")
	}
	r := httptest.NewRequest(method, target.String(), nil)
	// Exercise the production public/private path boundary, not the mux's
	// canonicalizing redirect. This synthetic artifact checks delegation only;
	// it is never a replacement for public-page or authenticated HTTP proof.
	artifact, err := webapp.Load(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("English fixture")},
		"da/index.html": &fstest.MapFile{Data: []byte("Danish fixture")},
	})
	if err != nil {
		return err
	}
	delegated := false
	artifact.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { delegated = true })).ServeHTTP(httptest.NewRecorder(), r)
	if !delegated {
		return errors.New("target is rejected by the unified site boundary")
	}
	handler, pattern := mux.Handler(r)
	if pattern == "" {
		return errors.New("target is not registered for its HTTP method")
	}
	if strings.Contains(pattern, "{id}") {
		registered := strings.Split(strings.SplitN(pattern, " ", 2)[1], "/")
		actual := strings.Split(target.Path, "/")
		for index, segment := range registered {
			if segment == "{id}" && (index >= len(actual) || !opaqueRequestID.MatchString(actual[index])) {
				return errors.New("target has an invalid request identity")
			}
			if segment == "{invoice}" {
				sequence, valid := proposalNumber(actual[index], 50)
				if !valid || sequence < 1 {
					return errors.New("target has an invalid invoice sequence")
				}
			}
		}
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return errors.New("malformed target query")
	}
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("duplicate target query")
		}
		switch key {
		case "lang":
			if values[0] != "en" && values[0] != "da" {
				return errors.New("invalid target language")
			}
		case "cursor":
			_, valid := parseTriageCursor(values[0])
			if !valid || (pattern != "GET /portal/operator/requests" && pattern != "GET /portal/operator/requests/{id}/work-review") {
				return errors.New("invalid target cursor")
			}
		case "review":
			if values[0] != "funding" || pattern != "GET /portal/operator/requests/{id}/work-review" {
				return errors.New("invalid target funding review")
			}
		case "workspace":
			switch pattern {
			case "GET /portal/operator/invitations/new", "GET /portal/operator/customers":
				if validCustomerWorkspace(values[0]) || pattern == "GET /portal/operator/customers" && values[0] == operatorWorkspace {
					continue
				}
			}
			return errors.New("invalid target workspace")
		case "view":
			if pattern == "GET "+customerAccountsPath && values[0] == "remove" && customerAccountID(query.Get("account")) != "" {
				continue
			}
			switch values[0] {
			case "closed", "cleanup", "cleanup-confirm", "purge-confirm", "close", "remove", "new", "support":
				if pattern == "GET /portal/operator/customers" {
					continue
				}
			}
			return errors.New("invalid workspace view")
		case "member":
			if pattern != "GET /portal/operator/customers" || query.Get("view") != "remove" ||
				values[0] == "" || len(values[0]) > 512 || strings.ContainsFunc(values[0], unicode.IsControl) {
				return errors.New("invalid workspace member")
			}
		case "focus":
			if values[0] == "customer" && pattern == "POST /portal/operator/api/members/remove" {
				continue
			}
			if values[0] != "workspace" || !slices.Contains([]string{
				"POST /portal/operator/api/invitations", "POST /portal/operator/api/invitations/revoke",
				"POST /portal/operator/api/workspaces/rename", "POST /portal/operator/api/workspaces/delete",
				"POST /portal/operator/api/members/deactivate", "POST /portal/operator/api/members/reactivate",
				"POST /portal/operator/api/members/remove",
			}, pattern) {
				return errors.New("invalid workspace action return")
			}
		case "account":
			if pattern != "GET "+customerAccountsPath || customerAccountID(values[0]) == "" {
				return errors.New("invalid target customer account")
			}
		case "page", "q":
			if pattern == "GET "+customerAccountsPath {
				if _, _, err := readCustomerSearch(query); err != nil {
					return errors.New("invalid customer search query")
				}
				continue
			}
			if values := query["page"]; len(values) != 0 {
				return errors.New("invalid target customer page")
			}
			fallthrough
		case "actor", "outcome", "from", "to", "before", "after":
			if pattern != "GET /portal/operator/audit" {
				return errors.New("invalid audit search target")
			}
			if _, err := readAuditSearch(target.RawQuery); err != nil {
				return errors.New("invalid audit search query")
			}
		default:
			return errors.New("unexpected target query")
		}
	}
	if strings.HasPrefix(target.Path, "/portal/assets/") {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			return errors.New("registered asset is not served")
		}
	}
	return nil
}
