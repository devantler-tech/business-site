package identity

import (
	"encoding/base64"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	preview := operator
	preview.subject, preview.membershipIncarnation = previewSubject, incarnation
	preview.preview = previewOperator{subject: operator.subject, incarnation: operator.membershipIncarnation}
	t.Run("proposal transaction holds preview owner membership", func(t *testing.T) {
		tx, _, _, err := b.svc.beginClientServiceRequest(ctx, preview, proposalRequestID, b.svc.acceptanceReady)
		if err != nil {
			t.Fatal("open preview proposal transaction", err)
		}
		defer rollbackProposalWrite(ctx, tx)
		other := beginFixtureTransaction(ctx, t, b.pool)
		if _, err := other.Exec(ctx, `SET LOCAL lock_timeout='50ms'`); err != nil {
			t.Fatal("bound the competing membership lock", err)
		}
		var subject string
		err = other.QueryRow(ctx, `SELECT subject FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 FOR UPDATE`, operator.issuer, operator.subject).Scan(&subject)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "55P03" {
			t.Fatal("preview proposal transaction did not hold the owner's membership lock", err)
		}
	})
	sessionKey := base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
	var data []byte
	var deadline time.Time
	if err := b.pool.QueryRow(ctx, `SELECT data,expiry FROM portal_sessions WHERE token=$1`, sessionKey).Scan(&data, &deadline); err != nil {
		t.Fatal("could not load the disposable preview session", err)
	}
	session := durableResponseSnapshot{
		original: contactAuthority{principal: preview, deadline: deadline},
		key:      sessionKey,
		data:     data,
	}
	expired := session
	expired.original.deadline = time.Now().Add(-time.Second)
	if err := b.svc.recordPreviewPurchase(ctx, expired, proposalRequestID, acceptance[1]); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired browser session completed checkout with a current broker token: %v", err)
	}
	if _, err := b.pool.Exec(ctx, `DELETE FROM portal_sessions WHERE token=$1`, sessionKey); err != nil {
		t.Fatal("could not retire the disposable preview session", err)
	}
	if err := b.svc.recordPreviewPurchase(ctx, session, proposalRequestID, acceptance[1]); !errors.Is(err, ErrDenied) {
		t.Fatalf("retired preview session was accepted at checkout: %v", err)
	}
	var recorded bool
	if err := b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_preview_receipts)`).Scan(&recorded); err != nil || recorded {
		t.Fatalf("denied preview checkout left a receipt: %v", err)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_sessions(token,data,expiry) VALUES($1,$2,$3)`, sessionKey, data, deadline); err != nil {
		t.Fatal("could not restore the fixture for the authorized checkout controls", err)
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

	t.Run("service contract upgrade preserves the receipt", func(t *testing.T) {
		migrator := migrationDB(t)
		rewindServiceContractsFixture(t, migrator)
		const publishedPreviewChecksum = "eaa884a519b7c1ba69223b49d1b186b41c6dc7324138d833a8a8ded252a1d2b0"
		var checksum string
		if err := migrator.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=31`).Scan(&checksum); err != nil {
			t.Fatal("read the published preview migration ledger", err)
		}
		if checksum != publishedPreviewChecksum {
			t.Fatal("migration 31 no longer matches the published preview receipts migration")
		}
		for _, name := range []string{"portal_mail_outbox_producer_format_check", "portal_mail_complete_producer"} {
			var definition string
			if err := migrator.QueryRow(ctx, `SELECT c.check_clause
 FROM information_schema.check_constraints c
 JOIN information_schema.table_constraints t USING (constraint_catalog,constraint_schema,constraint_name)
 WHERE t.table_schema='public' AND t.table_name='portal_mail_outbox' AND t.constraint_name=$1`, name).Scan(&definition); err != nil {
				t.Fatal("read the published producer constraint", err)
			}
			if !strings.Contains(definition, "producer_format = 1") {
				t.Errorf("published preview schema permits a later producer format in %s: %s", name, definition)
			}
		}
		if err := Migrate(ctx, migrator); err != nil {
			t.Fatal("upgrade the published preview schema", err)
		}
		var upgraded, retained bool
		if err := migrator.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM portal_schema_version WHERE version=33),
 EXISTS (SELECT 1 FROM portal_preview_receipts WHERE acceptance_id=$1 AND request_id=$2
  AND client_id='preview-demo' AND one_time_minor=299500 AND monthly_minor=9900)`,
			acceptance[1], proposalRequestID).Scan(&upgraded, &retained); err != nil || !upgraded || !retained {
			t.Fatalf("service contract migration applied=%t, preview receipt retained=%t: %v", upgraded, retained, err)
		}
	})
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
