package identity

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPreviewCheckoutSimulatesThePurchaseWithoutCommercialWrites(t *testing.T) {
	b := previewOperatorFixture(t)
	creditFixtureFlags(t, b)
	operator := authenticateExistingMember(t, b, sharedOperator)
	ctx := t.Context()
	var hasAccess bool
	err := b.pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'portal_preview_receipts','SELECT,INSERT')`).Scan(&hasAccess)
	if err != nil || hasAccess {
		t.Fatal("ordinary runtime login unexpectedly has simulation receipt access")
	}
	if err := CheckPreviewReceiptRole(ctx, b.pool, false); err != nil {
		t.Fatal("ordinary runtime was refused with its actual restricted grants", err)
	}
	if _, err := migrationDB(t).Exec(ctx, "GRANT SELECT,INSERT ON portal_preview_receipts TO portal_runtime"); err != nil {
		t.Fatal("could not give the disposable preview test its scoped receipt privileges", err)
	}
	if err := CheckPreviewReceiptRole(ctx, b.pool, false); !errors.Is(err, ErrDenied) {
		t.Fatal("ordinary runtime with receipt privileges was accepted", err)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief)
 VALUES($1,$2,$3,'website','Sample website','A test project for the preview')`,
		proposalRequestID, previewWorkspace, secret()); err != nil {
		t.Fatal(err)
	}
	proposal := projectProposal{Key: secret(), Scope: "Sample site", Deliverables: "Prototype",
		Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900,
		OneTimeTerms: "One agreed iteration", MonthlyTerms: "Hosting"}
	if err := b.svc.persistProposal(ctx, operator, proposalRequestID, proposal); err != nil {
		t.Fatal(err)
	}
	share := visibilityDecision{Proposal: 1, ExpectedProposal: 1, Action: "share", Key: secret()}
	if err := b.svc.persistVisibility(ctx, operator, proposalRequestID, share); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request(http.MethodPost, "/portal/preview", url.Values{"csrf_token": {b.csrf("/portal/operator")}}, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("preview entry returned %d", status)
	}

	request := "/portal/requests/" + proposalRequestID
	status, review, _ := b.request(http.MethodPost, request+"/proposal-review", acceptanceReviewForm(t, b), b.site.URL)
	if status != http.StatusOK {
		t.Fatalf("preview proposal review returned %d", status)
	}
	for _, label := range []string{"2995", "99", "100%", "DKK 0"} {
		if !strings.Contains(review, label) {
			t.Errorf("preview agreement lacks %q", label)
		}
	}
	key := regexp.MustCompile(`name="context_key" value="([^"]+)"`).FindStringSubmatch(review)
	csrf := csrfInput.FindStringSubmatch(review)
	if len(key) != 2 || len(csrf) != 2 {
		t.Fatal("preview agreement has no confirmation form")
	}
	accept := url.Values{"context_key": {key[1]}, "confirm": {"yes"}, "csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, request+"/proposal-acceptance", accept, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("preview agreement confirmation returned %d", status)
	}

	status, page, _ := b.request(http.MethodGet, request+"?lang=en", nil, "")
	acceptance := regexp.MustCompile(`name="acceptance" value="([^"]+)"`).FindStringSubmatch(page)
	if status != http.StatusOK || len(acceptance) != 2 || !strings.Contains(page, "Simulate purchase") {
		t.Fatalf("preview purchase form is missing: status %d", status)
	}
	purchase := url.Values{"acceptance": {acceptance[1]}, "csrf_token": {b.csrf(request)}, "lang": {"en"}}
	path := request + "/preview-orders"
	incarnation, err := b.svc.startClientPreview(ctx, operator)
	if err != nil {
		t.Fatal(err)
	}
	expired := operator
	expired.subject, expired.membershipIncarnation = previewSubject, incarnation
	expired.preview = previewOperator{subject: operator.subject, incarnation: operator.membershipIncarnation}
	if err := b.svc.recordPreviewPurchase(ctx, expired, proposalRequestID, acceptance[1], time.Now().Add(-time.Second)); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired browser session completed checkout with a current broker token: %v", err)
	}
	forged := url.Values{"acceptance": purchase["acceptance"], "csrf_token": purchase["csrf_token"], "due_minor": {"1"}}
	if status, _, _ := b.request(http.MethodPost, path, forged, b.site.URL); status != http.StatusBadRequest {
		t.Errorf("browser-supplied price was not refused: status %d", status)
	}
	first, second := concurrentPortalPosts(b, path, purchase, purchase)
	if first != http.StatusSeeOther || second != http.StatusSeeOther {
		t.Fatalf("simulated purchase and retry returned %d and %d", first, second)
	}
	status, page, _ = b.request(http.MethodGet, request+"?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Simulated purchase") || !strings.Contains(page, "DKK 0") {
		t.Fatalf("simulated receipt is missing: status %d", status)
	}
	var receipts, commercial int
	err = b.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM portal_preview_receipts WHERE acceptance_id=$1),
 (SELECT count(*) FROM portal_orders) + (SELECT count(*) FROM portal_order_charges) +
 (SELECT count(*) FROM portal_invoices) + (SELECT count(*) FROM portal_card_checkouts) +
 (SELECT count(*) FROM portal_mail_outbox) + (SELECT count(*) FROM portal_launch_places) +
 (SELECT count(*) FROM portal_work_approvals)`, acceptance[1]).Scan(&receipts, &commercial)
	if err != nil || receipts != 1 || commercial != 0 {
		t.Fatalf("simulation persisted %d receipts and produced %d commercial records: %v", receipts, commercial, err)
	}
	revokeFixtureAccess(ctx, t, b, sharedOperator)
	if status, _, _ := b.request(http.MethodPost, path, purchase, b.site.URL); status != http.StatusForbidden {
		t.Errorf("a revoked operator's preview could still complete checkout: status %d", status)
	}
}

func TestPreviewCheckoutIsAbsentFromTheOrdinaryPortal(t *testing.T) {
	b := newBrowserFixture(t)
	b.as(customerSubject)
	b.finish(b.startInvited(customerSubject, "fixture-customer", "client"))
	form := url.Values{"csrf_token": {b.csrf("/portal/")}, "acceptance": {secret()}}
	path := "/portal/requests/" + proposalRequestID + "/preview-orders"
	if status, _, _ := b.request(http.MethodPost, path, form, b.site.URL); status != http.StatusNotFound {
		t.Fatalf("ordinary customer obtained a simulated checkout path: status %d", status)
	}
}
