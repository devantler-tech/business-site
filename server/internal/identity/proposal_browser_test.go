package identity

import (
	"context"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

const proposalRequestID = "AAAAAAAAAAAAAAAAAAAAAAAAAA"

func proposalFlags(t *testing.T, b *browserFixture, values ...any) {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{}
	for i, name := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals"} {
		if values[i] != nil {
			flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": values[i]}}
		}
	}
	if err := b.flags.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

func proposalFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := newBrowserFixture(t)
	proposalFlags(t, b, true, true, true, true)
	seedTriageRequest(t, b, proposalRequestID, "Fictional proposal request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	triageSignIn(t, b, "proposal-operator", "operator-workspace", "operator")
	return b
}

func proposalForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	key := regexp.MustCompile(`name="submission_key" value="([^"]+)"`).FindStringSubmatch(body)
	version := regexp.MustCompile(`name="expected_version" value="([^"]+)"`).FindStringSubmatch(body)
	if status != 200 || len(csrf) != 2 || len(key) != 2 || len(version) != 2 {
		t.Fatal("operator detail has no native proposal preparation form")
	}
	return url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "submission_key": {key[1]}, "expected_version": {version[1]}, "scope": {"A fictional first iteration"}, "deliverables": {"A website and accessible contact page"}, "currency": {"DKK"}, "one_time_dkk": {"2995"}, "monthly_dkk": {"99"}, "one_time_terms": {"One agreed iteration"}, "monthly_terms": {"Hosting and routine maintenance"}, "lang": {"en"}}
}

func TestProposalImmutableRevisionsAndDurableRetries(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	f.Set("scope", "<script>fictional-private-scope</script>\r\nSecond line")
	path := "/portal/operator/requests/" + proposalRequestID + "/proposals"
	for range 2 {
		status, _, headers := b.request("POST", path, f, b.site.URL)
		if status != 303 || headers.Get("Location") != "/portal/operator/requests/"+proposalRequestID+"?lang=en" {
			t.Fatalf("save/retry=%d", status)
		}
	}
	second := proposalForm(t, b)
	second.Set("scope", "A separately priced next iteration")
	second.Set("one_time_dkk", "7995")
	if status, _, _ := b.request("POST", path, second, b.site.URL); status != 303 {
		t.Fatalf("revision=%d", status)
	}
	b.reconnect(context.Background())
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatal("reconnected old identical retry did not retain original version")
	}
	f.Set("monthly_dkk", "100")
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 409 {
		t.Fatal("content-changing retry overwrote history")
	}
	status, body, h := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=da", nil, "")
	for _, want := range []string{"&lt;script&gt;fictional-private-scope&lt;/script&gt;", "A separately priced next iteration", "2995", "7995", "99", "Ikke accepteret"} {
		if status != 200 || !strings.Contains(body, want) {
			t.Fatalf("durable Danish proposal history missing %q", want)
		}
	}
	if strings.Contains(body, "<script>fictional") || strings.Contains(body, "private-client-key") || h.Get("Cache-Control") != "no-store" {
		t.Fatal("proposal leaked identity, active text or cache authority")
	}
	var count int
	var submitted time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposals WHERE request_id=$1`, proposalRequestID).Scan(&count); err != nil || count != 2 {
		t.Fatal("retry created duplicate versions", err)
	}
	if err := b.pool.QueryRow(b.f.ctx, `SELECT submitted_at FROM portal_requests WHERE id=$1`, proposalRequestID).Scan(&submitted); err != nil || !submitted.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatal("proposal changed intake age", err)
	}
}

func TestProposalRejectsInvalidAndForgedForms(t *testing.T) {
	b := proposalFixture(t)
	for _, tc := range []struct{ name, field, value string }{
		{"empty scope", "scope", " "}, {"scope bound", "scope", strings.Repeat("x", 1201)},
		{"deliverables bound", "deliverables", strings.Repeat("x", 1201)}, {"terms bound", "monthly_terms", strings.Repeat("x", 601)},
		{"currency", "currency", "EUR"}, {"fraction", "one_time_dkk", "1.99"}, {"negative", "monthly_dkk", "-1"},
		{"overflow", "one_time_dkk", "999999999999999999999"}, {"above price bound", "one_time_dkk", "10000001"}, {"version", "expected_version", "-1"},
		{"owner", "client_id", "different-client"}, {"role", "role", "operator"}, {"accepted", "accepted", "true"},
		{"duplicate", "scope", "duplicate"}, {"control", "scope", "\x00unsafe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := proposalForm(t, b)
			if tc.name == "duplicate" {
				f.Add(tc.field, tc.value)
			} else {
				f.Set(tc.field, tc.value)
			}
			if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", f, b.site.URL); status != 400 {
				t.Fatalf("invalid form admitted: %d", status)
			}
		})
	}
	var count int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid proposal persisted", err)
	}
}

func TestProposalGatesAndCurrentOperatorAuthority(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	saveFixtureProposal(t, b, f)
	path := "/portal/operator/requests/" + proposalRequestID + "/proposals"
	for gate := range 4 {
		for _, closed := range []any{nil, false, "true"} {
			values := []any{true, true, true, true}
			values[gate] = closed
			proposalFlags(t, b, values...)
			status, _, h := b.request("POST", path, f, b.site.URL)
			if status != 404 || h.Get("Cache-Control") != "no-store" {
				t.Fatalf("closed gate=%d status=%d", gate, status)
			}
			_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
			if strings.Contains(body, `name="submission_key"`) {
				t.Fatal("closed gate left preparation form")
			}
		}
	}
	proposalFlags(t, b, true, true, true, true)
	for _, subject := range []string{"proposal-client-a", "proposal-client-b"} {
		triageSignIn(t, b, subject, subject, "client")
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
		if status != 403 || strings.Contains(body, "Fictional proposal request") || strings.Contains(body, "A fictional first iteration") {
			t.Fatal("client reads proposal workspace")
		}
		// Use that session's genuine CSRF token; a client role must still deny.
		_, shell, _ := b.request("GET", "/portal/", nil, "")
		f.Set("csrf_token", html.UnescapeString(csrfInput.FindStringSubmatch(shell)[1]))
		if status, _, _ = b.request("POST", path, f, b.site.URL); status != 403 {
			t.Fatal("client writes operator proposal")
		}
	}
}

func TestProposalConcurrentVersionsAndRetries(t *testing.T) {
	b := proposalFixture(t)
	first, second := proposalForm(t, b), proposalForm(t, b)
	second.Set("scope", "Concurrent alternative")
	path := "/portal/operator/requests/" + proposalRequestID + "/proposals"
	statuses := make(chan int, 2)
	for _, form := range []url.Values{first, second} {
		go func() { status, _, _ := b.request("POST", path, form, b.site.URL); statuses <- status }()
	}
	a, c := <-statuses, <-statuses
	if (a != 303 || c != 409) && (a != 409 || c != 303) {
		t.Fatalf("concurrent expected version did not arbitrate: %d/%d", a, c)
	}
	retry := proposalForm(t, b)
	for range 2 {
		go func() { status, _, _ := b.request("POST", path, retry, b.site.URL); statuses <- status }()
	}
	if a, c = <-statuses, <-statuses; a != 303 || c != 303 {
		t.Fatalf("concurrent identical retry: %d/%d", a, c)
	}
	var versions int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposals WHERE request_id=$1`, proposalRequestID).Scan(&versions); err != nil || versions != 2 {
		t.Fatal("duplicate or overwritten concurrent versions", err)
	}
}

func TestProposalCrossOperatorRetryCollisionIsConflict(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	saveFixtureProposal(t, b, f)
	triageSignIn(t, b, "second-operator", "other-operator-workspace", "operator")
	collision := proposalForm(t, b)
	collision.Set("submission_key", f.Get("submission_key"))
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", collision, b.site.URL); status != 409 {
		t.Fatalf("request-scoped key collision became %d, want conflict", status)
	}
}

func saveFixtureProposal(t *testing.T, b *browserFixture, form url.Values) {
	t.Helper()
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", form, b.site.URL); status != 303 {
		t.Fatal("fixture proposal save failed")
	}
}

func TestProposalMutationRejectsInFlightAuthorityClosure(t *testing.T) {
	for _, change := range []string{"flag", "revocation", "downgrade", "expiry", "storage"} {
		t.Run(change, func(t *testing.T) {
			b := proposalFixture(t)
			p := enroll(t, b.svc, b.f, "blocking-operator", "operator-workspace", "operator")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			tx := beginFixtureTransaction(ctx, t, migrator)
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, proposalRequestID); err != nil {
				t.Fatal(err)
			}
			d := projectProposal{Key: "BBBBBBBBBBBBBBBBBBBBBBBBBB", Scope: "Blocked draft", Deliverables: "Fictional deliverables", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One iteration", MonthlyTerms: "Hosting"}
			if change == "expiry" {
				p.expires = time.Now().Add(time.Second)
			}
			result := make(chan error, 1)
			go func() { result <- b.svc.persistProposal(ctx, p, proposalRequestID, d) }()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				if err := migrator.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal("fixture did not reach locked write")
				}
			}
			switch change {
			case "flag":
				proposalFlags(t, b, true, true, true, false)
			case "revocation":
				revokeFixtureAccess(ctx, t, b, "blocking-operator")
			case "downgrade":
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE subject='blocking-operator'`); err != nil {
					t.Fatal(err)
				}
			case "storage":
				if _, err := migrator.Exec(ctx, `REVOKE SELECT ON portal_memberships FROM portal_runtime`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := migrator.Exec(context.Background(), `GRANT SELECT ON portal_memberships TO portal_runtime`); err != nil {
						t.Error("fixture membership privilege cleanup failed")
					}
				}()
			case "expiry":
				waitForFixtureExpiry(ctx, t, p.expires)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			want := ErrDenied
			if change == "storage" {
				want = errTriageStorage
			}
			if err := <-result; !errors.Is(err, want) {
				t.Fatalf("in-flight %s admitted: %v", change, err)
			}
			var count int
			if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM portal_proposals`).Scan(&count); err != nil || count != 0 {
				t.Fatal("denied write left history", err)
			}
		})
	}
}

func TestProposalReadStorageFailureIsNotEmptyHistory(t *testing.T) {
	b := proposalFixture(t)
	migrator := migrationDB(t)
	if _, err := migrator.Exec(b.f.ctx, `REVOKE SELECT ON portal_proposals FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(b.f.ctx, `GRANT SELECT ON portal_proposals TO portal_runtime`); err != nil {
			t.Error("fixture read privilege cleanup failed")
		}
	}()
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 503 || strings.Contains(body, "No saved drafts") || strings.Contains(body, "permission denied") || strings.Contains(body, "portal_proposals") {
		t.Fatal("proposal storage error masked or leaked")
	}
}

func TestProposalHistoryRejectsInFlightClosure(t *testing.T) {
	for _, change := range []string{"flag", "revocation", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b := proposalFixture(t)
			p := enroll(t, b.svc, b.f, "history-operator", "operator-workspace", "operator")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			tx, relation := lockFixtureRead(ctx, t, migrator, "portal_proposals")
			if change == "expiry" {
				p.expires = time.Now().Add(time.Second)
			}
			result := make(chan error, 1)
			go func() { _, err := b.svc.readProposals(ctx, p, proposalRequestID); result <- err }()
			waitForTriageRead(ctx, t, migrator, relation)
			switch change {
			case "flag":
				proposalFlags(t, b, true, true, true, false)
			case "revocation":
				revokeFixtureAccess(ctx, t, b, "history-operator")
			case "expiry":
				waitForFixtureExpiry(ctx, t, p.expires)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrDenied) {
				t.Fatalf("history released after %s: %v", change, err)
			}
		})
	}
}

func TestProposalWriteStorageFailureAndMissingRequest(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	for _, id := range []string{"not-an-id", "BBBBBBBBBBBBBBBBBBBBBBBBBB"} {
		if status, _, _ := b.request("POST", "/portal/operator/requests/"+id+"/proposals", f, b.site.URL); status != 404 {
			t.Fatalf("missing request status=%d", status)
		}
	}
	migrator := migrationDB(t)
	if _, err := migrator.Exec(b.f.ctx, `REVOKE INSERT ON portal_proposals FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(b.f.ctx, `GRANT INSERT ON portal_proposals TO portal_runtime`); err != nil {
			t.Error("fixture write privilege cleanup failed")
		}
	}()
	status, body, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", f, b.site.URL)
	if status != 503 || strings.Contains(body, "permission denied") || strings.Contains(body, "portal_proposals") {
		t.Fatal("failed write masked or leaked")
	}
}

func TestProposalBoundedCompleteHistory(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	for i := range 50 {
		key := strings.Repeat("B", 24) + string(alphabet[i/32]) + string(alphabet[i%32])
		if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_proposals(request_id,client_id,version,expected_version,submission_key,operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms) VALUES($1,'private-client-key',$2,$3,$4,$5,'proposal-operator','Fictional saved scope','Fictional saved deliverables','DKK',0,1000000000,'No initial charge','Draft maintenance terms')`, proposalRequestID, i+1, i, key, b.f.issuer); err != nil {
			t.Fatal(err)
		}
	}
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 200 || strings.Count(body, "<article>") != 50 || strings.Contains(body, `name="submission_key"`) || !strings.Contains(body, "History is full") || !strings.Contains(body, "10000000 DKK") {
		t.Fatal("history truncated or preparation left open")
	}
	f.Set("expected_version", "49")
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", f, b.site.URL); status != 409 {
		t.Fatal("history limit admitted another version")
	}
}

func TestProposalSameRetryKeyRemainsRequestClientScoped(t *testing.T) {
	b := proposalFixture(t)
	f := proposalForm(t, b)
	other := "BBBBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES($1,'other-private-client',$1,'app','Fictional other request','Fictional other brief')`, other); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{proposalRequestID, other} {
		if status, _, _ := b.request("POST", "/portal/operator/requests/"+id+"/proposals", f, b.site.URL); status != 303 {
			t.Fatal("retry key leaked across requests")
		}
	}
	var scoped bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*)=2 AND bool_and(p.client_id=q.client_id AND p.operator_issuer=$1 AND p.operator_subject='proposal-operator' AND p.currency='DKK' AND p.one_time_minor=299500 AND p.monthly_minor=9900 AND p.prepared_at>=q.submitted_at) FROM portal_proposals p JOIN portal_requests q ON p.request_id=q.id`, b.f.issuer).Scan(&scoped); err != nil || !scoped {
		t.Fatal("server request/client/operator/price/time binding failed", err)
	}
}

func TestProposalMigrationPreservesPriorLedgerAndIntake(t *testing.T) {
	b := proposalFixture(t)
	migrator := migrationDB(t)
	var before, after string
	if err := migrator.QueryRow(b.f.ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=5`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(b.f.ctx, `DROP TABLE portal_proposals; ALTER TABLE portal_requests DROP CONSTRAINT portal_request_client; DELETE FROM portal_schema_version WHERE version=6`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(b.f.ctx, migrator); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrator.QueryRow(b.f.ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=5`).Scan(&after); err != nil || before != after {
		t.Fatal("proposal migration changed prior ledger", err)
	}
	var count int
	if err := migrator.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_requests WHERE id=$1 AND title='Fictional proposal request'`, proposalRequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal("proposal migration changed intake", err)
	}
	var checksum string
	if err := migrator.QueryRow(b.f.ctx, `SELECT checksum FROM portal_schema_version WHERE version=6`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(b.f.ctx, `UPDATE portal_schema_version SET checksum='changed' WHERE version=6`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(b.f.ctx, migrator); err == nil {
		t.Fatal("changed proposal schema admitted")
	}
	if _, err := migrator.Exec(b.f.ctx, `UPDATE portal_schema_version SET checksum=$1 WHERE version=6`, checksum); err != nil {
		t.Fatal(err)
	}
}
