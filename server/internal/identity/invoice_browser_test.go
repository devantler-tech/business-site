package identity

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
	markup "golang.org/x/net/html"
)

// httptestInvoiceForm supplies the same encoded body as the native invoice form.
func httptestInvoiceForm(f url.Values) *http.Request {
	r := httptest.NewRequest("POST", "https://fixture.invalid/invoices", strings.NewReader(f.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// invoiceFixture provisions fictional accepted scope and a current operator over TLS and SQL.
func invoiceFixture(t *testing.T) *browserFixture {
	t.Helper()
	b, _, _ := acceptedOperatorFixture(t)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices"}, []any{true, true, true, true, true, true, true})
	triageSignIn(t, b, "invoice-operator", "operator-workspace", "operator")
	return b
}

// TestInvoiceRejectsUnboundedOrForgedInput retains each rejected field as an actual HTTP control.
func TestInvoiceRejectsUnboundedOrForgedInput(t *testing.T) {
	b := invoiceFixture(t)
	for _, tc := range []struct{ field, value string }{
		{"reference", ""}, {"reference", strings.Repeat("A", 65)}, {"reference", "INV:1"},
		{"purpose", " "}, {"purpose", strings.Repeat("x", 301)}, {"purpose", "invalid\x00text"},
		{"amount_dkk", "0"}, {"amount_dkk", "-1"}, {"amount_dkk", "1e3"}, {"amount_dkk", "1.234"},
		{"amount_dkk", "1,234.56"}, {"amount_dkk", "01"}, {"amount_dkk", "10000000.01"}, {"amount_dkk", strings.Repeat("9", 30)},
		{"invoice_expected", "-1"}, {"invoice_expected", "50"}, {"invoice_key", "wrong"},
		{"invoice_acceptance", "wrong"}, {"client_id", "different-client"}, {"paid", "true"}, {"currency", "EUR"},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			f := invoiceForm(t, b)
			f.Set(tc.field, tc.value)
			status, body, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices?lang=da", f, b.site.URL)
			if status != 400 || !strings.Contains(body, "Fakturaen kunne ikke registreres") || strings.Contains(body, "/invoices?lang=en") {
				t.Fatalf("invalid invoice admitted or recovery points to POST: %d", status)
			}
		})
	}
	f := invoiceForm(t, b)
	f.Add("amount_dkk", "1")
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices", f, b.site.URL); status != 400 {
		t.Fatal("duplicate invoice fields accepted")
	}
}

// TestInvoiceExactDecimalAmounts checks stored minor units, not a reformatted input string.
func TestInvoiceExactDecimalAmounts(t *testing.T) {
	b := invoiceFixture(t)
	for _, tc := range []struct {
		raw  string
		want int64
	}{{"0.01", 1}, {"1,2", 120}, {"10000000.00", 1000000000}, {"2995", 299500}} {
		t.Run(tc.raw, func(t *testing.T) {
			f := invoiceForm(t, b)
			f.Set("reference", "AMOUNT-"+strings.ReplaceAll(strings.ReplaceAll(tc.raw, ".", "-"), ",", "-"))
			f.Set("amount_dkk", tc.raw)
			if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices", f, b.site.URL); status != 303 {
				t.Fatalf("valid decimal amount rejected: %d", status)
			}
			var amount int64
			if err := b.pool.QueryRow(t.Context(), `SELECT amount_minor FROM portal_invoices ORDER BY sequence DESC LIMIT 1`).Scan(&amount); err != nil || amount != tc.want {
				t.Fatal("invoice rounded or inferred a proposal amount", err)
			}
		})
	}
}

// TestInvoiceGatesAndClientIsolation denies closed gates and clients with genuine CSRF tokens.
func TestInvoiceGatesAndClientIsolation(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	path := "/portal/operator/requests/" + proposalRequestID
	for _, off := range []any{nil, false, "true"} {
		setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices"}, []any{true, true, true, true, true, true, off})
		if status, _, h := b.request("POST", path+"/invoices", f, b.site.URL); status != 404 || h.Get("Cache-Control") != "no-store" {
			t.Fatal("closed invoice gate admitted direct write")
		}
		if status, body, _ := b.request("GET", path, nil, ""); status != 200 || strings.Contains(body, "External invoices") || !strings.Contains(body, "Fictional proposal request") {
			t.Fatal("closed invoice gate leaked history or hid ordinary triage")
		}
	}
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices"}, []any{true, true, true, true, true, true, true})
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 303 {
		t.Fatal("positive gate control failed")
	}
	for _, client := range []string{"private-client-key", "other-client"} {
		triageSignIn(t, b, "invoice-client-"+client, client, "client")
		_, shell, _ := b.request("GET", "/portal/", nil, "")
		f.Set("csrf_token", html.UnescapeString(csrfInput.FindStringSubmatch(shell)[1]))
		if status, body, _ := b.request("GET", path, nil, ""); status != 403 || strings.Contains(body, "TEST-2026-01") {
			t.Fatal("client read operator invoice")
		}
		if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 403 {
			t.Fatal("client wrote operator invoice")
		}
	}
}

// TestInvoiceRetriesConcurrencyAndScopeChanges arbitrates stale forms and preserves historical retries.
func TestInvoiceRetriesConcurrencyAndScopeChanges(t *testing.T) {
	b := invoiceFixture(t)
	f, other := invoiceForm(t, b), invoiceForm(t, b)
	other.Set("reference", "TEST-2026-02")
	path := "/portal/operator/requests/" + proposalRequestID + "/invoices"
	a, c := concurrentPortalPosts(b, path, f, other)
	if (a != 303 || c != 409) && (a != 409 || c != 303) {
		t.Fatalf("stale concurrent writes=%d/%d", a, c)
	}
	var savedKey string
	if err := b.pool.QueryRow(t.Context(), `SELECT submission_key FROM portal_invoices`).Scan(&savedKey); err != nil {
		t.Fatal(err)
	}
	if savedKey == other.Get("invoice_key") {
		f = other
	}
	if a, c = concurrentPortalPosts(b, path, f, f); a != 303 || c != 303 {
		t.Fatal("identical concurrent retry lost its receipt")
	}
	stale := invoiceForm(t, b)
	draft := proposalForm(t, b)
	saveFixtureProposal(t, b, draft)
	if status, _, _ := b.request("POST", path, stale, b.site.URL); status != 409 {
		t.Fatal("new invoice reused consent after re-quote")
	}
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatal("historical retry stopped returning its original record")
	}
	f.Set("purpose", "Different invoiced work")
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 409 {
		t.Fatal("content-changing retry overwrote invoice")
	}
	status, body, _ := b.request("GET", strings.TrimSuffix(path, "/invoices"), nil, "")
	if status != 200 || strings.Contains(body, `name="invoice_key"`) || !strings.Contains(body, "Historical acceptance") {
		t.Fatal("historical invoice claims current consent")
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_invoices`).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry or conflict duplicated invoice", err)
	}
}

// TestInvoiceCrossOperatorAndDuplicateReferenceConflict binds retries to actors and unique references.
func TestInvoiceCrossOperatorAndDuplicateReferenceConflict(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	path := "/portal/operator/requests/" + proposalRequestID + "/invoices"
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatal("initial invoice failed")
	}
	duplicate := invoiceForm(t, b)
	duplicate.Set("reference", "test-2026-01")
	if status, _, _ := b.request("POST", path, duplicate, b.site.URL); status != 409 {
		t.Fatal("normalized duplicate reference admitted")
	}
	triageSignIn(t, b, "other-invoice-operator", "other-operator-workspace", "operator")
	other := invoiceForm(t, b)
	other.Set("invoice_key", f.Get("invoice_key"))
	other.Set("invoice_expected", f.Get("invoice_expected"))
	if status, _, _ := b.request("POST", path, other, b.site.URL); status != 409 {
		t.Fatal("another operator reused original retry authority")
	}
}

// TestInvoiceWriteRechecksBlockedAuthority changes authority while the actual SQL writer is blocked.
func TestInvoiceWriteRechecksBlockedAuthority(t *testing.T) {
	for _, change := range []string{"flag", "revocation", "downgrade", "expiry", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b := invoiceFixture(t)
			f := invoiceForm(t, b)
			raw := httptestInvoiceForm(f)
			d, valid := parseInvoice(raw)
			if !valid {
				t.Fatal("fixture invoice invalid")
			}
			op := enroll(t, b.svc, b.f, "blocked-invoice-operator", "operator-workspace", "operator")
			if change == "expiry" {
				op.expires = time.Now().Add(300 * time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			lock := blockFixtureProposalWrite(ctx, t, migrator)
			writeCtx, stop := context.WithCancel(ctx)
			defer stop()
			done := make(chan error, 1)
			go func() { done <- b.svc.persistInvoice(writeCtx, op, proposalRequestID, d) }()
			waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
			switch change {
			case "flag":
				setFixturePortalFlags(t, b, []string{"portal-identity"}, []any{true})
			case "cancel":
				stop()
			default:
				alterFixturePrincipal(ctx, t, b, op, change)
			}
			if err := lock.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err := <-done
			if change == "cancel" {
				if err == nil {
					t.Fatal("cancelled invoice committed")
				}
			} else if !errors.Is(err, ErrDenied) {
				t.Fatalf("invoice survived %s: %v", change, err)
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_invoices`).Scan(&count); err != nil || count != 0 {
				t.Fatal("denied invoice persisted", err)
			}
		})
	}
}

// TestInvoiceStorageFailureIsNotEmpty refuses a real privilege failure without leaking table details.
func TestInvoiceStorageFailureIsNotEmpty(t *testing.T) {
	b := invoiceFixture(t)
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `REVOKE SELECT ON portal_invoices FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(t.Context(), `GRANT SELECT ON portal_invoices TO portal_runtime`); err != nil {
			t.Error("fixture invoice privilege cleanup failed", err)
		}
	}()
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 503 || strings.Contains(body, "No recorded invoices") || strings.Contains(body, "portal_invoices") {
		t.Fatal("invoice storage error was hidden or leaked")
	}
}

// TestInvoiceReadClosureAndMissingRequest rechecks release and access after materializing private rows.
func TestInvoiceReadClosureAndMissingRequest(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "flag", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b := invoiceFixture(t)
			f := invoiceForm(t, b)
			d, _ := parseInvoice(httptestInvoiceForm(f))
			op := enroll(t, b.svc, b.f, "invoice-reader", "operator-workspace", "operator")
			if err := b.svc.persistInvoice(t.Context(), op, proposalRequestID, d); err != nil {
				t.Fatal(err)
			}
			flags := acceptanceMemoryFlags()
			flags["portal-invoices"] = memprovider.InMemoryFlag{Key: "portal-invoices", State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
			provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(flags), key: "portal-invoices"}
			if err := b.flags.SetProviderAndWait(t.Context(), provider); err != nil {
				t.Fatal(err)
			}
			tracer := installCompletedRead(t, b, "SELECT i.sequence,i.expected_sequence")
			read := func(ctx context.Context, p Principal) (bool, error) {
				history, err := b.svc.readInvoices(ctx, p, proposalRequestID)
				return len(history.Records) != 0, err
			}
			checkChangedRead(t, b, op, provider, tracer.blocked, tracer.release, change, true, true, read)
		})
	}
	b := invoiceFixture(t)
	op := enroll(t, b.svc, b.f, "missing-invoice-reader", "operator-workspace", "operator")
	if _, err := b.svc.readInvoices(t.Context(), op, "not-an-id"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing request read invoice", err)
	}
}

// TestInvoiceMigrationPreservesConsent reapplies v10 without changing prior consent or checksums.
func TestInvoiceMigrationPreservesConsent(t *testing.T) {
	invoiceFixture(t)
	migrator := migrationDB(t)
	var before, after, acceptance string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=9`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := migrator.QueryRow(t.Context(), `SELECT id FROM portal_proposal_acceptances`).Scan(&acceptance); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(t.Context(), `DROP TABLE portal_invoices; ALTER TABLE portal_proposal_acceptances DROP CONSTRAINT portal_acceptance_invoice_binding; DELETE FROM portal_schema_version WHERE version=10`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=9`).Scan(&after); err != nil || before != after {
		t.Fatal("invoice migration changed prior schema ledger", err)
	}
	var retained bool
	if err := migrator.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_proposal_acceptances WHERE id=$1) AND (SELECT count(*)=10 FROM portal_schema_version)`, acceptance).Scan(&retained); err != nil || !retained {
		t.Fatal("invoice migration changed consent or omitted its own version", err)
	}
}

// TestInvoiceCompleteBoundedImmutableHistory verifies all 50 rows and actual runtime mutation refusal.
func TestInvoiceCompleteBoundedImmutableHistory(t *testing.T) {
	b := invoiceFixture(t)
	d, valid := parseInvoice(httptestInvoiceForm(invoiceForm(t, b)))
	if !valid {
		t.Fatal("invalid synthetic invoice")
	}
	op := enroll(t, b.svc, b.f, "bounded-invoice-operator", "operator-workspace", "operator")
	for i := range 50 {
		d.Key, d.Reference, d.Expected = secret(), fmt.Sprintf("BOUNDED-%02d", i), i
		if err := b.svc.persistInvoice(t.Context(), op, proposalRequestID, d); err != nil {
			t.Fatal(err)
		}
	}
	history, err := b.svc.readInvoices(t.Context(), op, proposalRequestID)
	if err != nil || len(history.Records) != 50 || history.Expected != 50 || history.Records[0].Reference != "BOUNDED-49" || history.Records[49].Reference != "BOUNDED-00" {
		t.Fatal("invoice history truncated or incorrectly ordered", err)
	}
	d.Key, d.Reference, d.Expected = secret(), "BOUNDED-50", 50
	if err := b.svc.persistInvoice(t.Context(), op, proposalRequestID, d); !errors.Is(err, errProposalConflict) {
		t.Fatal("invoice history bound admitted new record", err)
	}
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 200 || strings.Count(body, `class="invoice-record"`) != 50 || strings.Contains(body, `name="invoice_key"`) {
		t.Fatal("full invoice history hid records or left entry enabled")
	}
	for _, query := range []string{`UPDATE portal_invoices SET amount_minor=1`, `DELETE FROM portal_invoices`} {
		if _, err := b.pool.Exec(t.Context(), query); err == nil {
			t.Fatal("runtime mutated immutable invoice history")
		}
	}
}

// invoiceForm reads server-issued identity, sequence and consent from the rendered operator form.
func invoiceForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	f := url.Values{"reference": {"TEST-2026-01"}, "purpose": {"Fictional agreed iteration installment"}, "amount_dkk": {"1234.56"}, "lang": {"en"}}
	for _, name := range []string{"csrf_token", "invoice_key", "invoice_acceptance", "invoice_expected"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
		if status != 200 || len(match) != 2 {
			t.Fatal("operator has no native acceptance-bound invoice form")
		}
		f.Set(name, html.UnescapeString(match[1]))
	}
	return f
}

// TestInvoiceReferenceNativeNormalization keeps native fields compatible with server-side trimming.
func TestInvoiceReferenceNativeNormalization(t *testing.T) {
	b := invoiceFixture(t)
	path := "/portal/operator/requests/" + proposalRequestID
	status, body, _ := b.request("GET", path, nil, "")
	doc, err := markup.Parse(strings.NewReader(body))
	if err != nil || status != 200 {
		t.Fatal("invoice form unavailable", err)
	}
	found := false
	for n := range doc.Descendants() {
		if linkAttribute(n, "id") == "invoice-reference" {
			found = true
			for _, a := range n.Attr {
				if a.Key == "pattern" || a.Key == "maxlength" {
					t.Fatal("raw native constraint prevents normalized references")
				}
			}
		}
	}
	if !found {
		t.Fatal("native invoice reference field missing")
	}
	f := invoiceForm(t, b)
	f.Set("reference", " \t"+strings.Repeat("a", 64)+"\u00a0 ")
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 303 {
		t.Fatalf("normalized reference refused: %d", status)
	}
	var reference string
	if err := b.pool.QueryRow(t.Context(), "SELECT reference FROM portal_invoices").Scan(&reference); err != nil || reference != strings.Repeat("A", 64) {
		t.Fatal("reference was not trimmed and normalized", err)
	}
}

// TestInvoiceDurablePrivateRecord checks stored binding, locale display, escaping and reconnect.
func TestInvoiceDurablePrivateRecord(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	f.Set("purpose", "<script>fictional-private-invoice</script>")
	path := "/portal/operator/requests/" + proposalRequestID
	for range 2 {
		if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 303 {
			t.Fatalf("invoice save/retry status=%d", status)
		}
	}
	b.reconnect(t.Context())
	for _, lang := range []string{"en", "da"} {
		amount := "1234.56 DKK"
		if lang == "da" {
			amount = "1234,56 DKK"
		}
		status, body, h := b.request("GET", path+"?lang="+lang, nil, "")
		if status != 200 || !strings.Contains(body, "TEST-2026-01") || !strings.Contains(body, amount) || h.Get("Cache-Control") != "no-store" {
			t.Fatal("durable localized private invoice record unavailable")
		}
		if strings.Contains(body, "<script>fictional-private-invoice</script>") || !strings.Contains(body, "&lt;script&gt;fictional-private-invoice&lt;/script&gt;") {
			t.Fatal("invoice purpose was executable or lost")
		}
	}
	var count int
	var bound bool
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*),bool_and(i.client_id=a.client_id AND i.request_id=a.request_id AND i.proposal_version=a.proposal_version AND i.content_hash=a.content_hash AND i.amount_minor=123456) FROM portal_invoices i JOIN portal_proposal_acceptances a ON a.id=i.acceptance_id`).Scan(&count, &bound); err != nil || count != 1 || !bound {
		t.Fatal("invoice duplicated or lost immutable acceptance/amount binding", err)
	}
}
