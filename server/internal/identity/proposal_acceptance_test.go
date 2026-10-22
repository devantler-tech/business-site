package identity

import (
	"context"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// acceptanceFlags configures the six independent SDK gates in their fixed order.
func acceptanceFlags(t *testing.T, b *browserFixture, values ...any) {
	t.Helper()
	setFixturePortalFlags(t, b, acceptanceFlagNames(), values)
}

// acceptanceFlagNames returns a fresh gate list so fixtures cannot mutate others.
func acceptanceFlagNames() []string {
	return []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance"}
}

// acceptanceMemoryFlags provides the ordinary enabled provider around wait controls.
func acceptanceMemoryFlags() map[string]memprovider.InMemoryFlag {
	flags := map[string]memprovider.InMemoryFlag{}
	for _, name := range acceptanceFlagNames() {
		flags[name] = memprovider.InMemoryFlag{State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	return flags
}

// installAcceptanceWait releases blocked requests before fixture-server cleanup.
func installAcceptanceWait(t *testing.T, b *browserFixture, provider openfeature.FeatureProvider, channel chan struct{}) func() {
	t.Helper()
	var once sync.Once
	release := func() { once.Do(func() { close(channel) }) }
	t.Cleanup(release)
	if err := b.flags.SetProviderAndWait(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	return release
}

type lateAcceptanceProvider struct {
	openfeature.FeatureProvider
	reads            atomic.Int32
	blocked, release chan struct{}
}

// BooleanEvaluation pauses the final POST display check, retaining SDK behavior.
func (p *lateAcceptanceProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, evaluation openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if key == "portal-proposal-acceptance" && p.reads.Add(1) == 4 {
		close(p.blocked)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, evaluation)
}

// TestAcceptanceReviewRechecksMembershipAfterFinalSDKWait denies post-wait revocation.
func TestAcceptanceReviewRechecksMembershipAfterFinalSDKWait(t *testing.T) {
	b := acceptanceFixture(t)
	form := acceptanceReviewForm(t, b)
	provider := &lateAcceptanceProvider{FeatureProvider: memprovider.NewInMemoryProvider(acceptanceMemoryFlags()), blocked: make(chan struct{}), release: make(chan struct{})}
	release := installAcceptanceWait(t, b, provider, provider.release)
	result := make(chan string, 1)
	go func() {
		_, body, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-review", form, b.site.URL)
		result <- body
	}()
	select {
	case <-provider.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("final display SDK checkpoint not reached")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := b.svc.Revoke(ctx, b.f.issuer, "consent-client"); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case body := <-result:
		if strings.Contains(body, "A fictional first iteration") || strings.Contains(body, "context_key") {
			t.Fatal("revoked client received private review after final SDK wait")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review did not complete")
	}
}

// TestAcceptanceRejectsForgedFormsAndOtherClients exercises actual scoped handlers.
func TestAcceptanceRejectsForgedFormsAndOtherClients(t *testing.T) {
	b := acceptanceFixture(t)
	f := acceptanceForm(t, b)
	path := "/portal/requests/" + proposalRequestID + "/proposal-acceptance"
	for name, value := range map[string]string{"client_id": "different-client", "role": "operator", "proposal_version": "2", "content_hash": "forged", "confirm": "no", "context_key": "invalid", "scope": "replacement", "one_time_minor": "0"} {
		g := cloneValues(f)
		g.Set(name, value)
		if status, _, _ := b.request("POST", path, g, b.site.URL); status != 400 {
			t.Fatalf("forged %s admitted: %d", name, status)
		}
	}
	g := cloneValues(f)
	g.Add("confirm", "yes")
	if status, _, _ := b.request("POST", path, g, b.site.URL); status != 400 {
		t.Fatal("duplicate confirmation admitted")
	}
	for _, tc := range []struct{ subject, client, role string }{{"consent-other", "different-client", "client"}, {"consent-operator", "private-client-key", "operator"}, {"consent-same-client-actor", "private-client-key", "client"}} {
		triageSignIn(t, b, tc.subject, tc.client, tc.role)
		g := cloneValues(f)
		g.Set("csrf_token", b.csrf("/portal/"))
		status, body, _ := b.request("POST", path, g, b.site.URL)
		if status == 200 || status == 303 || strings.Contains(body, "A fictional first iteration") {
			t.Fatal("another client, operator or actor replayed consent")
		}
	}
	assertAcceptanceCount(t, b, 0)
}

// assertAcceptanceCount observes the durable ledger rather than a response claim.
func assertAcceptanceCount(t *testing.T, b *browserFixture, want int) {
	t.Helper()
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_proposal_acceptances`).Scan(&count); err != nil || count != want {
		t.Fatalf("consent ledger count=%d want=%d error=%v", count, want, err)
	}
}

// TestAcceptanceIndependentSDKGatesAndNativeCSRF retains every gate and CSRF refusal.
func TestAcceptanceIndependentSDKGatesAndNativeCSRF(t *testing.T) {
	b := acceptanceFixture(t)
	f := acceptanceForm(t, b)
	path := "/portal/requests/" + proposalRequestID + "/proposal-acceptance"
	for gate := range 6 {
		for _, off := range []any{nil, false, "true"} {
			values := []any{true, true, true, true, true, true}
			values[gate] = off
			acceptanceFlags(t, b, values...)
			for _, route := range []string{path, "/portal/requests/" + proposalRequestID + "/proposal-review"} {
				if status, _, _ := b.request("POST", route, f, b.site.URL); status != 404 {
					t.Fatalf("gate %d closed POST=%d", gate, status)
				}
			}
			_, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
			if strings.Contains(body, "Review before accepting") || strings.Contains(body, "Acceptance recorded") {
				t.Fatal("closed gate exposed consent UI")
			}
		}
	}
	acceptanceFlags(t, b, true, true, true, true, true, true)
	g := cloneValues(f)
	g.Set("csrf_token", "invalid")
	if status, _, _ := b.request("POST", path, g, b.site.URL); status != 403 {
		t.Fatal("forged CSRF admitted")
	}
	if status, _, _ := b.request("POST", path, f, "https://other.example.invalid"); status != 403 {
		t.Fatal("foreign origin admitted")
	}
	if status, _, _ := b.request("GET", path, nil, ""); status != 405 {
		t.Fatal("GET could mutate consent")
	}
	assertAcceptanceCount(t, b, 0)
}

// TestAcceptanceSDKErrorAndStaleDenyActualHandlers checks settled provider states.
func TestAcceptanceSDKErrorAndStaleDenyActualHandlers(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		t.Run(string(event), func(t *testing.T) {
			b := acceptanceFixture(t)
			f := acceptanceForm(t, b)
			client := b.flags.NewClient()
			requireFixtureSDKEvent(t, b.flags, client, acceptanceMemoryFlags(), event)
			if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", f, b.site.URL); status != 404 {
				t.Fatal("unhealthy SDK accepted consent")
			}
			assertAcceptanceCount(t, b, 0)
		})
	}
}

// TestAcceptanceContextsAreBoundedAndHistoricalRetriesSurviveReconnect distinguishes
// an original immutable decision from a rotated review or later proposal version.
func TestAcceptanceContextsAreBoundedAndHistoricalRetriesSurviveReconnect(t *testing.T) {
	b := acceptanceFixture(t)
	p := enroll(t, b.svc, b.f, "consent-direct", "private-client-key", "client")
	// Force sub-microsecond precision, rather than relying on host clock luck.
	p.expires = time.Now().Add(time.Minute).Truncate(time.Second).Add(123 * time.Nanosecond)
	first, err := b.svc.prepareProposalReview(t.Context(), p, proposalRequestID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.svc.prepareProposalReview(t.Context(), p, proposalRequestID)
	if err != nil || first.Key != again.Key || !first.Expires.Equal(again.Expires) || first.Expires.After(p.expires) {
		t.Fatal("review reuse changed original deadline or binding", err)
	}
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_proposal_reviews SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.acceptProposal(t.Context(), p, proposalRequestID, first.Key); !errors.Is(err, errProposalConflict) {
		t.Fatal("expired context admitted", err)
	}
	rotated, err := b.svc.prepareProposalReview(t.Context(), p, proposalRequestID)
	if err != nil || rotated.Key == first.Key {
		t.Fatal("expired review did not rotate", err)
	}
	saved, err := b.svc.acceptProposal(t.Context(), p, proposalRequestID, rotated.Key)
	if err != nil {
		t.Fatal(err)
	}
	op := enroll(t, b.svc, b.f, "consent-revision-operator", "operator-workspace", "operator")
	d := rotated.Proposal
	d.Key, d.Expected, d.Scope = secret(), 1, "Fictional unpublished replacement scope"
	if err := b.svc.persistProposal(t.Context(), op, proposalRequestID, d); err != nil {
		t.Fatal(err)
	}
	pool := b.reconnect(t.Context())
	svc := New(pool, b.f.broker, b.flags.NewClient())
	retry, err := svc.acceptProposal(t.Context(), p, proposalRequestID, rotated.Key)
	if err != nil || retry.ID != saved.ID || !retry.Recorded.Equal(saved.Recorded) {
		t.Fatal("reconnect changed the original receipt", err)
	}
	rows, canAccept, err := svc.readProposalAcceptances(t.Context(), p, proposalRequestID)
	if err != nil || canAccept || len(rows) != 1 || rows[0].Current || rows[0].Proposal.Scope != rotated.Proposal.Scope {
		t.Fatal("revision reused historical consent", err)
	}
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
	if status != 200 || strings.Contains(body, d.Scope) || strings.Contains(body, "Version 2") || strings.Contains(body, "Review before accepting") {
		t.Fatal("unpublished draft leaked through consent state")
	}
	assertAcceptanceCount(t, b, 1)
}

// TestAcceptanceRealConcurrentDuplicatesAndDraftWithdrawalRaces observes both
// real database lock orders and exactly-once receipt identity for duplicate retries.
func TestAcceptanceRealConcurrentDuplicatesAndDraftWithdrawalRaces(t *testing.T) {
	for _, change := range []string{"draft", "withdrawal"} {
		for _, consentFirst := range []bool{true, false} {
			t.Run(change+map[bool]string{true: "/consent-first", false: "/change-first"}[consentFirst], func(t *testing.T) {
				b, p, review := preparedConsentFixture(t, "race-consent-client")
				op := enroll(t, b.svc, b.f, "race-consent-operator", "operator-workspace", "operator")
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				migrator := migrationDB(t)
				lock := blockFixtureProposalWrite(ctx, t, migrator)
				consent, mutate := make(chan error, 1), make(chan error, 1)
				accept := func() { _, err := b.svc.acceptProposal(ctx, p, proposalRequestID, review.Key); consent <- err }
				changeProposal := func() {
					if change == "draft" {
						d := review.Proposal
						d.Key, d.Expected, d.Scope = secret(), 1, "Fictional replacement"
						mutate <- b.svc.persistProposal(ctx, op, proposalRequestID, d)
					} else {
						mutate <- b.svc.persistVisibility(ctx, op, proposalRequestID, visibilityDecision{ExpectedEvent: 1, ExpectedProposal: 1, Proposal: 1, Action: "withdraw", Key: secret()})
					}
				}
				if consentFirst {
					go accept()
				} else {
					go changeProposal()
				}
				waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
				if consentFirst {
					go changeProposal()
				} else {
					go accept()
				}
				waitForFixtureDatabaseLock(ctx, t, migrator, `SELECT count(*)=2 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=1869820518 AND objid=hashtext($1)::oid AND objsubid=2 AND NOT granted`, proposalRequestID)
				if err := lock.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if err := <-mutate; err != nil {
					t.Fatal("competing operator mutation failed", err)
				}
				err := <-consent
				if (consentFirst && err != nil) || (!consentFirst && !errors.Is(err, errProposalConflict)) {
					t.Fatal("serialization admitted stale consent or lost first decision", err)
				}
				want := 0
				if consentFirst {
					want = 1
				}
				assertAcceptanceCount(t, b, want)
				rows, canAccept, err := b.svc.readProposalAcceptances(ctx, p, proposalRequestID)
				if err != nil || canAccept || (len(rows) == 1 && rows[0].Current) {
					t.Fatal("changed proposal kept consent applicability", err)
				}
			})
		}
	}
	b := acceptanceFixture(t)
	p := enroll(t, b.svc, b.f, "duplicate-consent-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), p, proposalRequestID)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		receipt proposalAcceptance
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			d, err := b.svc.acceptProposal(t.Context(), p, proposalRequestID, review.Key)
			results <- result{d, err}
		}()
	}
	a, c := <-results, <-results
	if a.err != nil || c.err != nil || a.receipt.ID != c.receipt.ID || !a.receipt.Recorded.Equal(c.receipt.Recorded) {
		t.Fatal("concurrent identical decisions did not share original receipt")
	}
	assertAcceptanceCount(t, b, 1)
}

// TestAcceptanceRechecksAccessAndGatesAfterWaiting closes authority during lock waits.
func TestAcceptanceRechecksAccessAndGatesAfterWaiting(t *testing.T) {
	for _, operation := range []string{"review", "accept"} {
		for _, change := range []string{"revocation", "rebind", "role", "flag", "expiry", "cancel"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				b, p, review := preparedConsentFixture(t, "waiting-consent-client")
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				migrator := migrationDB(t)
				lock := blockFixtureProposalWrite(ctx, t, migrator)
				if change == "expiry" {
					p.expires = time.Now().Add(time.Second)
				}
				result := make(chan error, 1)
				go func() {
					if operation == "review" {
						_, err := b.svc.prepareProposalReview(ctx, p, proposalRequestID)
						result <- err
					} else {
						_, err := b.svc.acceptProposal(ctx, p, proposalRequestID, review.Key)
						result <- err
					}
				}()
				waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
				switch change {
				case "revocation":
					revokeFixtureAccess(ctx, t, b, p.subject)
				case "rebind":
					alterFixturePrincipal(ctx, t, b, p, change)
				case "role":
					if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='operator' WHERE subject=$1`, p.subject); err != nil {
						t.Fatal(err)
					}
				case "flag":
					acceptanceFlags(t, b, true, true, true, true, true, false)
				case "expiry":
					waitForFixtureExpiry(ctx, t, p.expires)
				case "cancel":
					cancel()
				}
				if err := lock.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := <-result; err == nil {
					t.Fatal("in-flight closure admitted consent")
				}
				assertAcceptanceCount(t, b, 0)
			})
		}
	}
}

// TestAcceptanceStorageDenialAndV9PreserveHistory retains fail-closed storage and
// unchanged earlier migrations through actual disposable schema reconstruction.
func TestAcceptanceStorageDenialAndV9PreserveHistory(t *testing.T) {
	b := acceptanceFixture(t)
	f := acceptanceForm(t, b)
	migrator := migrationDB(t)
	for _, table := range []string{"portal_proposal_reviews", "portal_proposal_acceptances"} {
		for _, privilege := range []string{"SELECT", "INSERT"} {
			if table == "portal_proposal_reviews" && privilege == "INSERT" {
				continue
			}
			if _, err := migrator.Exec(t.Context(), "REVOKE "+privilege+" ON "+table+" FROM portal_runtime"); err != nil {
				t.Fatal(err)
			}
			status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", f, b.site.URL)
			if _, err := migrator.Exec(t.Context(), "GRANT "+privilege+" ON "+table+" TO portal_runtime"); err != nil {
				t.Fatal(err)
			}
			if status != 503 {
				t.Fatalf("storage denial masked: %s %s status=%d", table, privilege, status)
			}
		}
	}
	assertAcceptanceCount(t, b, 0)
	// Reset only this disposable ledger before the historical migration control.
	if _, err := migrator.Exec(t.Context(), `TRUNCATE portal_work_current,portal_work_revocations,portal_work_approvals,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=8`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	rewindAcceptanceFixture(t, migrator)
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	var preserved bool
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=8`).Scan(&after); err != nil || after != before {
		t.Fatal("v9 changed previous checksums", err)
	}
	if err := migrator.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_proposals WHERE request_id=$1 AND version=1) AND EXISTS(SELECT 1 FROM portal_proposal_visibility WHERE request_id=$1 AND event=1)`, proposalRequestID).Scan(&preserved); err != nil || !preserved {
		t.Fatal("v9 lost proposal/sharing history", err)
	}
}

// acceptanceFixture shares a fictional proposal before signing in its own client.
func acceptanceFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := sharingFixture(t)
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	acceptanceFlags(t, b, true, true, true, true, true, true)
	triageSignIn(t, b, "consent-client", "private-client-key", "client")
	return b
}

// preparedConsentFixture creates a real actor-bound review for direct race controls.
func preparedConsentFixture(t *testing.T, subject string) (*browserFixture, Principal, proposalReview) {
	t.Helper()
	b := acceptanceFixture(t)
	p := enroll(t, b.svc, b.f, subject, "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), p, proposalRequestID)
	if err != nil {
		t.Fatal(err)
	}
	return b, p, review
}

// acceptanceReviewForm uses a real session CSRF token without browser-supplied scope.
func acceptanceReviewForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	return url.Values{"csrf_token": {b.csrf("/portal/contact")}, "lang": {"en"}}
}

// acceptanceForm obtains the context and CSRF token from the actual review response.
func acceptanceForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	status, body, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-review", acceptanceReviewForm(t, b), b.site.URL)
	key := regexp.MustCompile(`name="context_key" value="([^"]+)"`).FindStringSubmatch(body)
	csrf := csrfInput.FindStringSubmatch(body)
	if status != 200 || len(key) != 2 || len(csrf) != 2 || !strings.Contains(body, "Confirm this proposal") {
		t.Fatalf("client review status=%d, want native exact-proposal confirmation", status)
	}
	return url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "context_key": {key[1]}, "confirm": {"yes"}, "lang": {"en"}}
}

// TestAcceptanceActualReviewConfirmationAndReceipt covers localized durable retries.
func TestAcceptanceActualReviewConfirmationAndReceipt(t *testing.T) {
	b := acceptanceFixture(t)
	f := acceptanceForm(t, b)
	path := "/portal/requests/" + proposalRequestID + "/proposal-acceptance"
	for range 2 {
		status, _, h := b.request("POST", path, f, b.site.URL)
		if status != 303 || h.Get("Location") != "/portal/requests/"+proposalRequestID+"?lang=en" {
			t.Fatalf("acceptance/retry=%d", status)
		}
	}
	for _, lang := range []string{"en", "da"} {
		status, body, h := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang="+lang, nil, "")
		want := "Acceptance recorded"
		if lang == "da" {
			want = "Accept registreret"
		}
		if status != 200 || !strings.Contains(body, want) || !strings.Contains(body, "A fictional first iteration") || h.Get("Cache-Control") != "no-store" {
			t.Fatal("durable localized receipt unavailable")
		}
	}
	var count int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposal_acceptances`).Scan(&count); err != nil || count != 1 {
		t.Fatal("acceptance retry did not preserve exactly one receipt", err)
	}
}
