package identity

import (
	"bytes"
	"errors"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/net/html"
)

var workRenderViews = []string{"absent", "current", "paused", "expired", "selected", "eligible", "ineligible", "history", "closed"}

// workRenderFixture is fictional presentation input, never authorization.
func workRenderFixture(view string) (workReviewData, int) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	d := workReviewData{Request: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", State: workAbsent, CanReview: view != "closed"}
	invoice := projectInvoice{Sequence: 1, Reference: "TEST-01", Purpose: "Fictional iteration", Version: 1, AmountMinor: 12500, NetMinor: pgtype.Int8{Int64: 10000, Valid: true}, VATMinor: pgtype.Int8{Int64: 2500, Valid: true}, Current: true}
	if d.CanReview {
		d.Invoices = []projectInvoice{invoice}
	}
	if view == "current" || view == "paused" || view == "expired" || view == "selected" || view == "closed" {
		d.State = workReviewState(view)
		if view == "closed" {
			d.State = workPaused
		}
		d.Pointer = workPointer{ID: "BBBBBBBBBBBBBBBBBBBBBBBBBB", Generation: 3}
		d.Current = workApprovalReceipt{ID: d.Pointer.ID, Decided: at, Expires: at.Add(168 * time.Hour)}
	}
	selected := 0
	if view == "eligible" || view == "ineligible" || view == "selected" {
		selected = 1
	}
	if view == "eligible" {
		proposal := projectProposal{Scope: "<script>inert</script>", Deliverables: "Fictional deliverables", OneTimeMinor: 375000, MonthlyMinor: 12500, OneTimeTerms: "Fictional iteration terms", MonthlyTerms: "Fictional monthly terms", Version: 1}
		revision := creditRevision{Source: "fixture-bank", Reference: "transfer-01", State: "confirmed", Revision: 1, AmountMinor: 12500, Note: "<script>inert</script>", Recorded: at}
		d.Candidate = &workCandidate{Evidence: workEvidence{Acceptance: "CCCCCCCCCCCCCCCCCCCCCCCCCC", Proposal: proposal, Invoice: invoice}, Input: workApprovalInput{Key: "DDDDDDDDDDDDDDDDDDDDDDDDDD", Acceptance: "CCCCCCCCCCCCCCCCCCCCCCCCCC", EvidenceHash: strings.Repeat("a", 64), Invoice: 1}, Funding: creditHistory{Invoice: invoice, Transfers: []creditTransfer{{Latest: revision, History: []creditRevision{revision}}}}}
	}
	if view == "history" {
		alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
		for i := range workHistorySize {
			d.History = append(d.History, workHistoryItem{ID: strings.Repeat("A", 25) + string(alphabet[i]), Invoice: 1, Version: 1, Decided: at, Expires: at.Add(168 * time.Hour), Terminal: "superseded", Ended: &at})
		}
		d.History[0].Terminal = "revoked"
		d.Next = triageCursor{ID: d.History[19].ID, Submitted: at}
	}
	return d, selected
}

func renderedWorkLinkPages(templates *template.Template) ([]linkPage, error) {
	var pages []linkPage
	for _, lang := range []string{"en", "da"} {
		for _, view := range workRenderViews {
			d, invoice := workRenderFixture(view)
			r := httptest.NewRequest("GET", "https://portal.example.invalid/portal/operator/requests/"+d.Request+"/work-review?lang="+lang, nil)
			if invoice != 0 {
				q := r.URL.Query()
				q.Set("invoice", "1")
				r.URL.RawQuery = q.Encode()
			}
			page := (&browser{}).workPage(r, d, invoice)
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
// renderer. It does not regenerate or replace the 120 historical body oracles.
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
				fields = map[string]string{"csrf_token": "fictional-form-nonce", "lang": lang, "work_key": "DDDDDDDDDDDDDDDDDDDDDDDDDD", "work_expected": "0", "acceptance": "CCCCCCCCCCCCCCCCCCCCCCCCCC", "invoice": "1", "evidence_hash": strings.Repeat("a", 64)}
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
		for _, want := range []string{`data-work-price="one-time">3750 DKK`, `data-work-price="monthly">125 DKK`, "Fictional deliverables", "Fictional iteration terms", "Fictional monthly terms", "TEST-01", "100.00 DKK", "25.00 DKK", "125.00 DKK", "transfer-01", "&lt;script&gt;inert&lt;/script&gt;"} {
			if lang == "da" {
				want = strings.ReplaceAll(want, ".00 DKK", ",00 DKK")
			}
			if !strings.Contains(markup, want) {
				return errors.New("exact agreement or funding fact missing")
			}
		}
	}
	return nil
}

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
