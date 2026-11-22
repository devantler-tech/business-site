package identity

import (
	"bytes"
	"errors"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

var workRenderViews = []string{"absent", "current", "paused", "expired", "selected", "eligible", "ineligible", "history", "closed"}

// workRenderFixture is fictional presentation input, never authorization.
func workRenderFixture(view string) (workReviewData, bool) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	d := workReviewData{Request: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", State: workAbsent, CanReview: view != "closed"}
	if view == "current" || view == "paused" || view == "expired" || view == "selected" || view == "closed" {
		d.State = workReviewState(view)
		if view == "closed" {
			d.State = workPaused
		}
		d.Pointer = workPointer{ID: "BBBBBBBBBBBBBBBBBBBBBBBBBB", Generation: 3}
		d.Current = workApprovalReceipt{ID: d.Pointer.ID, Decided: at, Expires: at.Add(168 * time.Hour)}
	}
	selected := view == "eligible" || view == "ineligible" || view == "selected"
	if view == "eligible" {
		proposal := projectProposal{Scope: "<script>inert</script>", Deliverables: "Fictional deliverables", OneTimeMinor: 375000, MonthlyMinor: 12500, OneTimeTerms: "Fictional iteration terms", MonthlyTerms: "Fictional monthly terms", Version: 1}
		order := workOrder{ID: "FFFFFFFFFFFFFFFFFFFFFFFFFF", Collection: "invoice", OneTimeMinor: 375000, MonthlyMinor: 12500}
		funding := workFunding{Period: "acceptance", Reference: "fixture-payment-01", GrossMinor: 375000, NetMinor: 300000, VATMinor: 75000, Settled: at}
		d.Candidate = &workCandidate{Evidence: workEvidence{Acceptance: "CCCCCCCCCCCCCCCCCCCCCCCCCC", Proposal: proposal, Order: order, Funding: funding}, Input: workApprovalInput{Key: "DDDDDDDDDDDDDDDDDDDDDDDDDD", Acceptance: "CCCCCCCCCCCCCCCCCCCCCCCCCC", Order: order.ID, EvidenceHash: strings.Repeat("a", 64)}}
	}
	if view == "history" {
		alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
		for i := range workHistorySize {
			d.History = append(d.History, workHistoryItem{ID: strings.Repeat("A", 25) + string(alphabet[i]), Period: "acceptance", Version: 1, Decided: at, Expires: at.Add(168 * time.Hour), Terminal: "superseded", Ended: &at})
		}
		d.History[0].Terminal = "revoked"
		d.Next = triageCursor{ID: d.History[19].ID, Submitted: at}
	}
	return d, selected
}

// renderedWorkLinkPagesFor exercises both languages of each explicit work state
// through the actual template set for method-aware route and asset validation.
func renderedWorkLinkPagesFor(templates *template.Template, b *browser) ([]linkPage, error) {
	var pages []linkPage
	for _, lang := range []string{"en", "da"} {
		for _, view := range workRenderViews {
			d, review := workRenderFixture(view)
			r := httptest.NewRequest("GET", "https://portal.example.invalid/portal/operator/requests/"+d.Request+"/work-review?lang="+lang, nil)
			if review {
				q := r.URL.Query()
				q.Set("review", "funding")
				r.URL.RawQuery = q.Encode()
			}
			page := b.workPage(r, d, review)
			page.CSRF, page.RevocationKey = "fictional-form-nonce", "EEEEEEEEEEEEEEEEEEEEEEEEEE"
			var body bytes.Buffer
			if err := templates.ExecuteTemplate(&body, "work-review.html", page); err != nil {
				return nil, err
			}
			pages = append(pages, linkPage{uri: r.URL.RequestURI(), markup: body.String()})
		}
	}
	return pages, nil
}

// checkWorkDocument uses explicit state/form expectations, separate from the
// renderer. It does not regenerate or replace the 98 historical body oracles.
func checkWorkDocument(markup, lang, view string) error {
	if err := checkMaintenanceDocument(markup, lang, false, false); err != nil {
		return err
	}
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return err
	}
	state := view
	if view == "eligible" || view == "ineligible" || view == "history" {
		state = "absent"
	}
	if view == "closed" {
		state = "paused"
	}
	counts := map[string]int{}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "p" && linkAttribute(n, "data-work-state") == state && maintenanceRendered(n) {
			counts["state"]++
		}
		if n.Data == "section" {
			counts[linkAttribute(n, "aria-labelledby")]++
		}
		if n.Data == "h2" && maintenanceRendered(n) {
			counts["heading-"+linkAttribute(n, "id")]++
		}
		if n.Data == "form" {
			suffix := "work-revocations"
			fields := map[string]string{"csrf_token": "fictional-form-nonce", "lang": lang, "work_key": "EEEEEEEEEEEEEEEEEEEEEEEEEE", "work_expected": "3", "approval": "BBBBBBBBBBBBBBBBBBBBBBBBBB"}
			if view == "eligible" {
				suffix = "work-approvals"
				fields = map[string]string{"csrf_token": "fictional-form-nonce", "lang": lang, "work_key": "DDDDDDDDDDDDDDDDDDDDDDDDDD", "work_expected": "0", "acceptance": "CCCCCCCCCCCCCCCCCCCCCCCCCC", "order": "FFFFFFFFFFFFFFFFFFFFFFFFFF", "evidence_hash": strings.Repeat("a", 64)}
			}
			if !maintenanceRendered(n) || linkAttribute(n, "method") != "post" || linkAttribute(n, "action") != "/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/"+suffix {
				return errors.New("work form is inactive or has a different action")
			}
			seen, buttons := map[string]int{}, 0
			for child := range n.Descendants() {
				if child.Type != html.ElementNode {
					continue
				}
				if child.Data == "input" {
					field := linkAttribute(child, "name")
					value, known := fields[field]
					if !known || linkAttribute(child, "type") != "hidden" || linkAttribute(child, "value") != value {
						return errors.New("work form has an unexpected comparison")
					}
					seen[field]++
				}
				if child.Data == "button" && linkAttribute(child, "type") == "submit" && maintenanceRendered(child) && linkAttribute(child, "formaction") == "" {
					buttons++
				}
			}
			for field := range fields {
				if seen[field] != 1 {
					return errors.New("work comparison is missing or duplicated")
				}
			}
			if buttons != 1 {
				return errors.New("explicit work submit action missing")
			}
			counts["form"]++
		}
	}
	for _, field := range []string{"state", "work-current", "work-review", "work-history"} {
		if counts[field] != 1 || (field != "state" && counts["heading-"+field] != 1) {
			return errors.New("complete work state/section missing")
		}
	}
	forms := 1
	if view == "absent" || view == "ineligible" || view == "history" {
		forms = 0
	}
	if counts["form"] != forms || strings.Contains(markup, "<script>inert</script>") {
		return errors.New("wrong state actions or escaped evidence")
	}
	if view == "eligible" {
		for _, want := range []string{`data-work-price="one-time">3750 DKK`, `data-work-price="monthly">125 DKK`, "Fictional deliverables", "Fictional iteration terms", "Fictional monthly terms", `data-work-funding="acceptance"`, ": 3750 DKK</dd>", "<code>FFFFFFFFFFFFFFFFFFFFFFFFFF</code>", "<code>fixture-payment-01</code>", "2026-10-01 12:00:00 UTC", "&lt;script&gt;inert&lt;/script&gt;"} {
			if !strings.Contains(markup, want) {
				return errors.New("exact agreement or funding fact missing")
			}
		}
	}
	return nil
}

// TestWorkReviewRenderedInventory preserves all eighteen explicit documents and
// catches missing evidence, wrong states and forged native-form compositions.
func TestWorkReviewRenderedInventory(t *testing.T) {
	pages, err := renderedLinkPages("work-review.html")
	if err != nil || len(pages) != 18 {
		t.Fatal("complete 18-document work inventory required", err)
	}
	for i, page := range pages {
		lang := "en"
		if i >= 9 {
			lang = "da"
		}
		view := workRenderViews[i%9]
		if err := checkWorkDocument(page.markup, lang, view); err != nil {
			t.Fatalf("%s/%s: %v", lang, view, err)
		}
		for _, bad := range []string{strings.Replace(page.markup, `data-work-state=`, `hidden data-work-state=`, 1), strings.Replace(page.markup, `id="work-review"`, `id="lost-review"`, 1), strings.Replace(page.markup, `name="work_expected"`, `name="lost-comparison"`, 1)} {
			if bad != page.markup && checkWorkDocument(bad, lang, view) == nil {
				t.Fatalf("%s/%s accepted damaged state/section/form", lang, view)
			}
		}
	}
}
