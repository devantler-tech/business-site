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
)

const creditPath = "/portal/operator/requests/" + proposalRequestID + "/invoices/1/credits"

// creditFixture reuses the signed-in proposal operator while real service calls
// prepare the client's consent. Credit controls do not need unrelated extra logins.
func creditFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := sharingFixture(t)
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	creditFixtureFlags(t, b)
	client := enroll(t, b.svc, b.f, "credit-fixture-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), client, proposalRequestID)
	if err != nil {
		t.Fatal("fixture consent review failed", err)
	}
	if _, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key); err != nil {
		t.Fatal("fixture consent failed", err)
	}
	return issueCreditFixtureInvoice(t, b)
}

// creditFixtureFlags enables only the same synthetic decisions in both setups.
func creditFixtureFlags(t *testing.T, b *browserFixture) {
	t.Helper()
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation"}, []any{true, true, true, true, true, true, true, true})
}

// issueCreditFixtureInvoice retains the real native invoice form and TLS writer.
func issueCreditFixtureInvoice(t *testing.T, b *browserFixture) *browserFixture {
	t.Helper()
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices", invoiceForm(t, b), b.site.URL); status != 303 {
		t.Fatal("fixture invoice unavailable")
	}
	return b
}

// creditForm reads the native server-issued retry identity and records fictional bank evidence only.
func creditForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	status, body, _ := b.request("GET", creditPath, nil, "")
	f := url.Values{"source": {"fixture-bank"}, "reference": {"transfer-01"}, "credit_expected": {"0"}, "state": {"confirmed"}, "amount_dkk": {"500.00"}, "note": {"Fictional credited funds observed by the operator"}, "lang": {"en"}}
	for _, name := range []string{"csrf_token", "credit_key"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
		if status != 200 || len(match) != 2 {
			t.Fatalf("operator has no native bank-credit form: %d", status)
		}
		f.Set(name, html.UnescapeString(match[1]))
	}
	return f
}

// TestCreditNativePartialAndFull uses the real TLS writer and exact stored minor units.
func TestCreditNativePartialAndFull(t *testing.T) {
	// Keep the complete upstream client/operator journey as well as the focused setups.
	b := invoiceFixture(t)
	creditFixtureFlags(t, b)
	issueCreditFixtureInvoice(t, b)
	f := creditForm(t, b)
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 303 {
		t.Fatalf("credit rejected: %d", status)
	}
	status, body, h := b.request("GET", creditPath, nil, "")
	if status != 200 || !strings.Contains(body, "Partially credited") || !strings.Contains(body, "500.00 DKK") || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("partial credit missing: %d", status)
	}
	f = creditForm(t, b)
	f.Set("reference", "transfer-02")
	f.Set("amount_dkk", "734,56")
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 303 {
		t.Fatal("second transfer failed")
	}
	b.reconnect(t.Context())
	_, body, _ = b.request("GET", creditPath+"?lang=da", nil, "")
	if !strings.Contains(body, "Fuldt krediteret") || !strings.Contains(body, "1234,56 DKK") || !strings.Contains(body, "ikke tilladelse til at starte arbejde") {
		t.Fatal("full exact credit lost after reconnect or implies execution")
	}
	var total int64
	if err := b.pool.QueryRow(t.Context(), `SELECT sum(amount_minor) FROM portal_credit_revisions`).Scan(&total); err != nil || total != 123456 {
		t.Fatal("stored transfer amounts were rounded or not durable", err)
	}
}

// TestCreditInputRefusals submits malformed, forged and ambiguous native forms.
func TestCreditInputRefusals(t *testing.T) {
	b := creditFixture(t)
	for _, tc := range []struct{ field, value string }{
		{"source", ""}, {"source", " bank"}, {"source", strings.Repeat("A", 65)}, {"source", "bank:account"},
		{"reference", ""}, {"reference", "a b"}, {"reference", "é"}, {"reference", strings.Repeat("A", 65)},
		{"state", "paid"}, {"state", ""}, {"amount_dkk", "0"}, {"amount_dkk", "-1"}, {"amount_dkk", "1e3"},
		{"amount_dkk", "1.001"}, {"amount_dkk", "1,234.56"}, {"amount_dkk", "10000000.01"},
		{"note", " "}, {"note", strings.Repeat("x", 301)}, {"note", "bad\x00note"},
		{"credit_expected", "-1"}, {"credit_expected", "16"}, {"credit_expected", "15"}, {"credit_key", "bad"},
		{"client_id", "forged"}, {"invoice", "2"}, {"currency", "EUR"}, {"execution", "true"},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			f := creditForm(t, b)
			f.Set(tc.field, tc.value)
			status, body, _ := b.request("POST", creditPath+"?lang=da", f, b.site.URL)
			if status != 400 || !strings.Contains(body, "Krediteringen kunne ikke registreres") {
				t.Fatalf("invalid credit accepted: %d", status)
			}
		})
	}
	f := creditForm(t, b)
	f.Add("reference", "other")
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 400 {
		t.Fatal("duplicate form field admitted")
	}
	f = creditForm(t, b)
	f.Set("state", "invalidated")
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 400 {
		t.Fatal("invalidation with a positive amount admitted")
	}
	for _, path := range []string{strings.Replace(creditPath, "/1/", "/2/", 1), strings.Replace(creditPath, "/1/", "/0/", 1), strings.Replace(creditPath, proposalRequestID, secret(), 1)} {
		if status, _, _ := b.request("POST", path, creditForm(t, b), b.site.URL); status != 404 {
			t.Fatal("missing invoice accepted credit")
		}
	}
}

// TestCreditRevisionsDoNotDoubleCount retains corrections, disputed evidence and invalidations.
func TestCreditRevisionsDoNotDoubleCount(t *testing.T) {
	b := creditFixture(t)
	for i, tc := range []struct{ state, amount, status, total string }{
		{"confirmed", "1234.56", "Fully credited", "1234.56"},
		{"disputed", "1234.56", "Unresolved evidence", "0.00"},
		{"unverified", "1234.56", "Unresolved evidence", "0.00"},
		{"confirmed", "1234.57", "Excess credit", "1234.57"},
		{"confirmed", "400", "Partially credited", "400.00"},
		{"invalidated", "", "No confirmed credits", "0.00"},
	} {
		f := creditForm(t, b)
		f.Set("credit_expected", fmt.Sprint(i))
		f.Set("state", tc.state)
		f.Set("amount_dkk", tc.amount)
		f.Set("note", fmt.Sprintf("<script>fictional correction %d</script>", i))
		if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 303 {
			t.Fatalf("revision %d failed: %d", i, status)
		}
		_, body, _ := b.request("GET", creditPath, nil, "")
		if !strings.Contains(body, tc.status) || !strings.Contains(body, tc.total+" DKK") || strings.Count(body, `class="credit-record"`) != i+1 || strings.Contains(body, "<script>fictional") || !strings.Contains(body, "&lt;script&gt;fictional") {
			t.Fatal("credit fold, complete history or escaping lost")
		}
	}
	// Unresolved evidence is not silently discarded even beside a matching confirmed transfer.
	f := creditForm(t, b)
	f.Set("reference", "transfer-02")
	f.Set("amount_dkk", "1234.56")
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 303 {
		t.Fatal("confirmed transfer failed")
	}
	f = creditForm(t, b)
	f.Set("reference", "transfer-03")
	f.Set("state", "unverified")
	if status, _, _ := b.request("POST", creditPath, f, b.site.URL); status != 303 {
		t.Fatal("unverified transfer failed")
	}
	_, body, _ := b.request("GET", creditPath, nil, "")
	if strings.Contains(body, "Fully credited") || !strings.Contains(body, "Unresolved evidence") {
		t.Fatal("unknown evidence inferred full payment")
	}
}

// TestCreditConcurrentRetriesAndActorBinding arbitrates identical retries and stale corrections.
func TestCreditConcurrentRetriesAndActorBinding(t *testing.T) {
	b := creditFixture(t)
	f := creditForm(t, b)
	a, c := concurrentPortalPosts(b, creditPath, f, f)
	if a != 303 || c != 303 {
		t.Fatalf("identical retry=%d/%d", a, c)
	}
	changed := f.Clone()
	changed.Set("note", "Different evidence")
	if status, _, _ := b.request("POST", creditPath, changed, b.site.URL); status != 409 {
		t.Fatal("changed retry admitted")
	}
	correction, other := creditForm(t, b), creditForm(t, b)
	correction.Set("credit_expected", "1")
	other.Set("credit_expected", "1")
	other.Set("amount_dkk", "501")
	a, c = concurrentPortalPosts(b, creditPath, correction, other)
	if (a != 303 || c != 409) && (a != 409 || c != 303) {
		t.Fatalf("stale correction=%d/%d", a, c)
	}
	triageSignIn(t, b, "other-credit-operator", "other-operator-workspace", "operator")
	retry := creditForm(t, b)
	retry.Set("credit_key", f.Get("credit_key"))
	if status, _, _ := b.request("POST", creditPath, retry, b.site.URL); status != 409 {
		t.Fatal("other actor retried original evidence")
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_credit_revisions`).Scan(&count); err != nil || count != 2 {
		t.Fatal("retry duplicated history", err)
	}
}

// TestCreditIndependentGateAndClientDenial uses genuine sessions and CSRF, never forged role claims.
func TestCreditIndependentGateAndClientDenial(t *testing.T) {
	b := creditFixture(t)
	f := creditForm(t, b)
	for _, off := range []any{nil, false, "true"} {
		setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation"}, []any{true, true, true, true, true, true, true, off})
		for _, method := range []string{"GET", "POST"} {
			if status, _, h := b.request(method, creditPath, f, b.site.URL); status != 404 || h.Get("Cache-Control") != "no-store" {
				t.Fatal("closed credit gate admitted access")
			}
		}
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
		if status != 200 || strings.Contains(body, "/credits?") || !strings.Contains(body, "TEST-2026-01") {
			t.Fatal("credit gate leaked controls or hid ordinary invoices")
		}
	}
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation"}, []any{true, true, true, true, true, true, true, true})
	for _, client := range []string{"private-client-key", "other-client"} {
		triageSignIn(t, b, "credit-client-"+client, client, "client")
		_, shell, _ := b.request("GET", "/portal/", nil, "")
		f.Set("csrf_token", html.UnescapeString(csrfInput.FindStringSubmatch(shell)[1]))
		for _, method := range []string{"GET", "POST"} {
			if status, body, _ := b.request(method, creditPath, f, b.site.URL); status != 403 || strings.Contains(body, "TEST-2026-01") {
				t.Fatal("client read or wrote operator credit")
			}
		}
	}
}

// TestCreditWriteRechecksBlockedAuthority changes authority while the real SQL writer waits.
func TestCreditWriteRechecksBlockedAuthority(t *testing.T) {
	b := creditFixture(t)
	for _, change := range []string{"flag", "revocation", "downgrade", "expiry", "cancel"} {
		t.Run(change, func(t *testing.T) {
			creditFixtureFlags(t, b)
			f := creditForm(t, b)
			r := httptest.NewRequest(http.MethodPost, "https://fixture.invalid/credits", strings.NewReader(f.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			d, ok := parseCredit(r)
			if !ok {
				t.Fatal("invalid fixture credit")
			}
			op := timedFixtureOperator(t, b, "blocked-credit-operator-"+change, change)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			lock := blockFixtureProposalWrite(ctx, t, migrator)
			result := make(chan error, 1)
			go func() { result <- b.svc.persistCredit(ctx, op, proposalRequestID, 1, d) }()
			waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
			switch change {
			case "flag":
				setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation"}, []any{true, true, true, true, true, true, true, false})
			case "revocation", "downgrade":
				alterFixturePrincipal(ctx, t, b, op, change)
			case "expiry":
				time.Sleep(350 * time.Millisecond)
			case "cancel":
				cancel()
			}
			if err := lock.Rollback(t.Context()); err != nil {
				t.Fatal("release fixture lock", err)
			}
			if err := <-result; err == nil {
				t.Fatal("changed authority admitted credit")
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_credit_revisions`).Scan(&count); err != nil || count != 0 {
				t.Fatal("refused writer committed evidence", err)
			}
		})
	}
}

// TestCreditMigrationPreservesInvoices reapplies only v11 and retains v1-v10 bytes.
func TestCreditMigrationPreservesInvoices(t *testing.T) {
	creditFixture(t)
	m := migrationDB(t)
	var before, after, reference string
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=10`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	rewindCreditsFixture(t, m)
	for range 2 {
		if err := Migrate(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=10`).Scan(&after); err != nil || before != after {
		t.Fatal("prior ledger changed", err)
	}
	if err := m.QueryRow(t.Context(), `SELECT reference FROM portal_invoices`).Scan(&reference); err != nil || reference != "TEST-2026-01" {
		t.Fatal("invoice lost during credit migration", err)
	}
}

// TestCreditMissingInvoiceCannotRead preserves missing-record semantics for trusted internal callers.
func TestCreditMissingInvoiceCannotRead(t *testing.T) {
	b := creditFixture(t)
	op := enroll(t, b.svc, b.f, "missing-credit-reader", "operator-workspace", "operator")
	if _, err := b.svc.readCredits(t.Context(), op, proposalRequestID, 2); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing invoice returned credit history", err)
	}
}

// cloneCreditInvoice creates a second internally consistent fictional client invoice
// using the privileged fixture role; global allocation is still attempted through HTTP.
func cloneCreditInvoice(t *testing.T) string {
	t.Helper()
	id, acceptance := secret(), secret()
	m := migrationDB(t)
	for _, query := range []string{
		`INSERT INTO portal_requests SELECT $1,'other-credit-client',submission_key,product,title,brief,submitted_at FROM portal_requests WHERE id=$2`,
		`INSERT INTO portal_proposals SELECT $1,'other-credit-client',version,expected_version,submission_key,operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms,prepared_at FROM portal_proposals WHERE request_id=$2`,
		`INSERT INTO portal_proposal_visibility SELECT $1,'other-credit-client',event,expected_event,proposal_version,expected_proposal,action,submission_key,operator_issuer,operator_subject,recorded_at FROM portal_proposal_visibility WHERE request_id=$2`,
	} {
		if _, err := m.Exec(t.Context(), query, id, proposalRequestID); err != nil {
			t.Fatal("second fictional client setup failed", err)
		}
	}
	if _, err := m.Exec(t.Context(), `INSERT INTO portal_proposal_acceptances SELECT $3,$1,'other-credit-client',client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash,recorded_at FROM portal_proposal_acceptances WHERE request_id=$2`, id, proposalRequestID, acceptance); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Exec(t.Context(), `INSERT INTO portal_invoices SELECT $1,'other-credit-client',sequence,expected_sequence,submission_key,$3,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,'SECOND-INVOICE',purpose,currency,amount_minor,recorded_at FROM portal_invoices WHERE request_id=$2`, id, proposalRequestID, acceptance); err != nil {
		t.Fatal(err)
	}
	return "/portal/operator/requests/" + id + "/invoices/1/credits"
}

// TestCreditGlobalAllocationAcrossClients races different request locks against the actual unique identity.
func TestCreditGlobalAllocationAcrossClients(t *testing.T) {
	b := creditFixture(t)
	otherPath := cloneCreditInvoice(t)
	f, other := creditForm(t, b), creditForm(t, b)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, tc := range []struct {
		path string
		form url.Values
	}{{creditPath, f}, {otherPath, other}} {
		go func() { <-start; status, _, _ := b.request("POST", tc.path, tc.form, b.site.URL); results <- status }()
	}
	close(start)
	a, c := <-results, <-results
	if (a != 303 || c != 409) && (a != 409 || c != 303) {
		t.Fatalf("cross-client allocation=%d/%d", a, c)
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_credit_transfers`).Scan(&count); err != nil || count != 1 {
		t.Fatal("global transfer duplicated", err)
	}
	// Invalidating evidence never releases the identity to the losing invoice.
	var winner, loser string
	// Channel completion order is not request order: bind to the stored immutable winner.
	var request string
	if err := b.pool.QueryRow(t.Context(), `SELECT request_id FROM portal_credit_transfers`).Scan(&request); err != nil {
		t.Fatal(err)
	}
	if request == proposalRequestID {
		winner, loser = creditPath, otherPath
	} else {
		winner, loser = otherPath, creditPath
	}
	correction := creditForm(t, b)
	correction.Set("credit_expected", "1")
	correction.Set("state", "invalidated")
	correction.Set("amount_dkk", "")
	if status, _, _ := b.request("POST", winner, correction, b.site.URL); status != 303 {
		t.Fatal("invalidation failed")
	}
	if status, _, _ := b.request("POST", loser, creditForm(t, b), b.site.URL); status != 409 {
		t.Fatal("invalidated transfer reallocated")
	}
	// Case is preserved: these are distinct external identities, not normalized aliases.
	other = creditForm(t, b)
	other.Set("reference", "Transfer-01")
	if status, _, _ := b.request("POST", loser, other, b.site.URL); status != 303 {
		t.Fatal("case-sensitive external reference was rewritten")
	}
}

// TestCreditReadClosure refuses release/expiry changes after materializing actual private rows.
func TestCreditReadClosure(t *testing.T) {
	b := creditFixture(t)
	op := enroll(t, b.svc, b.f, "credit-reader", "operator-workspace", "operator")
	d := creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: "confirmed", AmountMinor: 1, Note: "Fictional evidence"}
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"unchanged", "expiry", "flag", "cancel"} {
		t.Run(change, func(t *testing.T) {
			flags := acceptanceMemoryFlags()
			for _, key := range []string{"portal-invoices", "portal-credit-reconciliation"} {
				flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
			}
			provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(flags), key: "portal-credit-reconciliation"}
			if err := b.flags.SetProviderAndWait(t.Context(), provider); err != nil {
				t.Fatal(err)
			}
			tracer := installCompletedRead(t, b, "SELECT source,reference,submission_key,state,note,revision")
			t.Cleanup(func() { b.svc.pool = b.pool })
			read := func(ctx context.Context, p Principal) (bool, error) {
				h, err := b.svc.readCredits(ctx, p, proposalRequestID, 1)
				return len(h.Transfers) != 0, err
			}
			checkChangedRead(t, b, op, provider, tracer.blocked, tracer.release, change, true, true, read)
		})
	}
}

// TestCreditBoundsAndHistoricalInvoice keeps a reserved correction slot and old invoice evidence.
func TestCreditBoundsAndHistoricalInvoice(t *testing.T) {
	b := creditFixture(t)
	op := enroll(t, b.svc, b.f, "bounded-credit-operator", "operator-workspace", "operator")
	d := creditRevision{Source: "fixture-bank", Reference: "transfer-01", State: "confirmed", AmountMinor: 50000, Note: "Fictional evidence"}
	for i := range 15 {
		d.Key, d.Expected = secret(), i
		if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
			t.Fatal(err)
		}
	}
	d.Key, d.Expected = secret(), 15
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); !errors.Is(err, errProposalConflict) {
		t.Fatal("last slot accepted positive credit", err)
	}
	d.Key, d.State, d.AmountMinor = secret(), "invalidated", 0
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
		t.Fatal("reserved invalidation refused", err)
	}
	d.Key, d.Expected = secret(), 16
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); !errors.Is(err, errProposalConflict) {
		t.Fatal("revision bound exceeded", err)
	}
	for i := 1; i < 32; i++ {
		d = creditRevision{Key: secret(), Source: "fixture-bank", Reference: fmt.Sprintf("transfer-%02d", i+1), State: "confirmed", AmountMinor: 1, Note: "Fictional evidence"}
		if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
			t.Fatal(err)
		}
	}
	d.Key, d.Reference = secret(), "transfer-33"
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); !errors.Is(err, errProposalConflict) {
		t.Fatal("transfer bound exceeded", err)
	}
	h, err := b.svc.readCredits(t.Context(), op, proposalRequestID, 1)
	if err != nil || len(h.Transfers) != 32 || len(h.Transfers[0].History) != 16 || h.Total != 31 {
		t.Fatal("bounded complete fold failed", err)
	}
	// A re-quote changes agreement status, not the recorded destination of earlier money.
	saveFixtureProposal(t, b, proposalForm(t, b))
	d = creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-02", Expected: 1, State: "confirmed", AmountMinor: 2, Note: "Observed after re-quote"}
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
		t.Fatal("historical invoice could not record money", err)
	}
	h, err = b.svc.readCredits(t.Context(), op, proposalRequestID, 1)
	if err != nil || h.Invoice.Current || h.Total != 32 {
		t.Fatal("re-quote rewrote credit or current consent", err)
	}
	for _, query := range []string{`UPDATE portal_credit_revisions SET amount_minor=1`, `DELETE FROM portal_credit_revisions`, `UPDATE portal_credit_transfers SET reference='replacement'`, `DELETE FROM portal_credit_transfers`} {
		if _, err := b.pool.Exec(t.Context(), query); err == nil {
			t.Fatal("runtime mutated audit history")
		}
	}
}

// TestCreditStorageFailureIsNotAnEmptyLedger refuses real privilege failures without private diagnostics.
func TestCreditStorageFailureIsNotAnEmptyLedger(t *testing.T) {
	b := creditFixture(t)
	f := creditForm(t, b)
	m := migrationDB(t)
	for _, table := range []string{"portal_credit_transfers", "portal_credit_revisions"} {
		for _, privilege := range []string{"SELECT", "INSERT"} {
			if _, err := m.Exec(t.Context(), "REVOKE "+privilege+" ON "+table+" FROM portal_runtime"); err != nil {
				t.Fatal(err)
			}
			status, body, _ := b.request("POST", creditPath, f, b.site.URL)
			if _, err := m.Exec(t.Context(), "GRANT "+privilege+" ON "+table+" TO portal_runtime"); err != nil {
				t.Fatal(err)
			}
			if status != 503 || strings.Contains(body, table) {
				t.Fatal("storage refusal hidden or leaked")
			}
		}
	}
	if _, err := m.Exec(t.Context(), `REVOKE SELECT ON portal_credit_revisions FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	status, body, _ := b.request("GET", creditPath, nil, "")
	if _, err := m.Exec(t.Context(), `GRANT SELECT ON portal_credit_revisions TO portal_runtime`); err != nil {
		t.Fatal(err)
	}
	if status != 503 || strings.Contains(body, "No confirmed credits") || strings.Contains(body, "portal_credit_revisions") {
		t.Fatal("storage failure became zero credit")
	}
}
