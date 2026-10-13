package identity

import (
	"context"
	"errors"
	"html"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func sharingFlags(t *testing.T, b *browserFixture, values ...any) {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{}
	for i, name := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing"} {
		if values[i] != nil {
			flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": values[i]}}
		}
	}
	if err := b.flags.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

func sharingFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := proposalFixture(t)
	sharingFlags(t, b, true, true, true, true, true)
	saveFixtureProposal(t, b, proposalForm(t, b))
	return b
}

func TestSharingMissingWrongTypeAndFalseStayClosed(t *testing.T) {
	b := sharingFixture(t)
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	for _, value := range []any{nil, "true", false} {
		sharingFlags(t, b, true, true, true, true, value)
		assertClientProposal(t, b, "", "Proposal shared for review")
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
		if status != 200 || strings.Contains(body, "Share version") || strings.Contains(body, "Withdraw shared proposal") {
			t.Fatal("unavailable sharing flag exposed controls")
		}
	}
}

func TestSharingFinalVersionRetainsWithdrawalCapacity(t *testing.T) {
	b := sharingFixture(t)
	for version := 2; version <= 50; version++ {
		if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_proposals(request_id,client_id,version,expected_version,submission_key,operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms) VALUES($1,'private-client-key',$2,$3,$4,$5,'proposal-operator','Fictional bounded scope','Fictional deliverables','DKK',299500,9900,'One iteration','Hosting')`, proposalRequestID, version, version-1, secret(), b.f.issuer); err != nil {
			t.Fatal(err)
		}
	}
	p := enroll(t, b.svc, b.f, "bounded-review-operator", "operator-workspace", "operator")
	for version := 1; version <= 50; version++ {
		for index, action := range []string{"share", "withdraw"} {
			d := visibilityDecision{ExpectedEvent: (version-1)*2 + index, ExpectedProposal: 50, Proposal: version, Action: action, Key: secret()}
			if err := b.svc.persistVisibility(b.f.ctx, p, proposalRequestID, d); err != nil {
				t.Fatalf("version %d %s: %v", version, action, err)
			}
		}
	}
	v, err := b.svc.readVisibility(b.f.ctx, p, proposalRequestID)
	if err != nil || v.Event != 100 || v.Active != 0 || len(v.Shared) != 50 {
		t.Fatal("complete bounded history lost final withdrawal", err)
	}
}

func visibilityForm(t *testing.T, b *browserFixture, event, latest, version, action string) url.Values {
	t.Helper()
	f := proposalForm(t, b)
	return url.Values{"csrf_token": {f.Get("csrf_token")}, "submission_key": {secret()}, "expected_event": {event}, "expected_proposal": {latest}, "proposal_version": {version}, "action": {action}, "lang": {"en"}}
}

func cloneValues(values url.Values) url.Values {
	copy := make(url.Values, len(values))
	for key, values := range values {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

func saveVisibility(t *testing.T, b *browserFixture, form url.Values) {
	t.Helper()
	form.Set("csrf_token", proposalForm(t, b).Get("csrf_token"))
	status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", form, b.site.URL)
	if status != 303 {
		t.Fatalf("visibility status=%d", status)
	}
}

func TestSharingSerializesConcurrentDecisionsAndDraftRevisions(t *testing.T) {
	b := sharingFixture(t)
	p := enroll(t, b.svc, b.f, "race-review-operator", "operator-workspace", "operator")
	d := visibilityDecision{ExpectedEvent: 0, ExpectedProposal: 1, Proposal: 1, Action: "share", Key: secret()}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- b.svc.persistVisibility(b.f.ctx, p, proposalRequestID, d) }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("identical concurrent retry rejected", err)
		}
	}
	var recorded time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT recorded_at FROM portal_proposal_visibility`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	other := d
	other.Key = secret()
	if err := b.svc.persistVisibility(b.f.ctx, p, proposalRequestID, other); !errors.Is(err, errProposalConflict) {
		t.Fatal("stale distinct decision admitted", err)
	}
	f := proposalForm(t, b)
	saveFixtureProposal(t, b, f)
	stale := visibilityDecision{ExpectedEvent: 1, ExpectedProposal: 1, Proposal: 1, Action: "withdraw", Key: secret()}
	if err := b.svc.persistVisibility(b.f.ctx, p, proposalRequestID, stale); !errors.Is(err, errProposalConflict) {
		t.Fatal("stale draft view admitted", err)
	}
	if err := b.svc.persistVisibility(b.f.ctx, p, proposalRequestID, d); err != nil {
		t.Fatal("original retry rejected after revision", err)
	}
	var unchanged bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*)=1 AND min(recorded_at)=$1 FROM portal_proposal_visibility`, recorded).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("retry rewrote original decision", err)
	}
}

func TestSharingV7MigrationPreservesExistingDraftsAndAllChecksums(t *testing.T) {
	b := sharingFixture(t)
	migrator := migrationDB(t)
	var before string
	if err := migrator.QueryRow(b.f.ctx, `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=6`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(b.f.ctx, `DROP TABLE portal_proposal_visibility; DELETE FROM portal_schema_version WHERE version=7`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(b.f.ctx, migrator); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(b.f.ctx, migrator); err != nil {
		t.Fatal("repeat migration failed", err)
	}
	var after string
	var count int
	if err := migrator.QueryRow(b.f.ctx, `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=6`).Scan(&after); err != nil || after != before {
		t.Fatal("old migration bytes changed", err)
	}
	if err := migrator.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposals WHERE request_id=$1 AND scope='A fictional first iteration'`, proposalRequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal("draft not preserved", err)
	}
}

func assertClientProposal(t *testing.T, b *browserFixture, visible, hidden string) {
	t.Helper()
	operator := b.client
	triageSignIn(t, b, "review-client-"+secret(), "private-client-key", "client")
	for _, lang := range []string{"en", "da"} {
		status, body, header := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang="+lang, nil, "")
		if status != 200 || (visible != "" && !strings.Contains(body, visible)) || (hidden != "" && strings.Contains(body, hidden)) || header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s client visibility status=%d", lang, status)
		}
		if strings.Contains(body, "submission_key") || strings.Contains(body, "proposal-operator") || strings.Contains(body, "<script>alert") {
			t.Fatal("private metadata or active markup leaked")
		}
	}
	b.client = operator
}

func TestSharingDraftsWithdrawalsAndRetriesNeverRepublish(t *testing.T) {
	b := sharingFixture(t)
	assertClientProposal(t, b, "", "A fictional first iteration")
	first := visibilityForm(t, b, "0", "1", "1", "share")
	saveVisibility(t, b, first)
	secondDraft := proposalForm(t, b)
	secondDraft.Set("scope", "Private second scope <script>alert(1)</script>")
	saveFixtureProposal(t, b, secondDraft)
	assertClientProposal(t, b, "A fictional first iteration", "Private second scope")
	second := visibilityForm(t, b, "1", "2", "2", "share")
	saveVisibility(t, b, second)
	assertClientProposal(t, b, "Private second scope &lt;script&gt;", "A fictional first iteration")
	withdraw := visibilityForm(t, b, "2", "2", "2", "withdraw")
	saveVisibility(t, b, withdraw)
	saveVisibility(t, b, first)
	saveVisibility(t, b, second)
	saveVisibility(t, b, withdraw)
	assertClientProposal(t, b, "", "Proposal shared for review")
	var count int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposal_visibility`).Scan(&count); err != nil || count != 3 {
		t.Fatal("retry changed event history", err)
	}
}

func TestSharingRejectsStaleForgedAndCrossActorDecisions(t *testing.T) {
	b := sharingFixture(t)
	f := visibilityForm(t, b, "0", "1", "1", "share")
	saveVisibility(t, b, f)
	for _, field := range []string{"expected_event", "expected_proposal", "proposal_version", "action"} {
		changed := cloneValues(f)
		switch field {
		case "action":
			changed.Set(field, "withdraw")
		case "expected_event":
			changed.Set(field, "1")
		default:
			changed.Set(field, "0")
		}
		status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", changed, b.site.URL)
		if status != 400 && status != 409 {
			t.Fatalf("changed retry %s status=%d", field, status)
		}
	}
	for _, field := range []string{"client_id", "operator_subject", "paid", "accepted", "work_eligible"} {
		forged := cloneValues(f)
		forged.Set(field, "forged")
		status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", forged, b.site.URL)
		if status != 400 {
			t.Fatalf("forged %s status=%d", field, status)
		}
	}
	stale := visibilityForm(t, b, "0", "1", "1", "share")
	status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", stale, b.site.URL)
	if status != 409 {
		t.Fatalf("stale event status=%d", status)
	}
	triageSignIn(t, b, "second-review-operator", "operator-workspace", "operator")
	f.Set("csrf_token", proposalForm(t, b).Get("csrf_token"))
	status, _, _ = b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", f, b.site.URL)
	if status != 409 {
		t.Fatalf("cross-actor retry status=%d", status)
	}
	var count int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_proposal_visibility`).Scan(&count); err != nil || count != 1 {
		t.Fatal("denied decision persisted", err)
	}
}

func TestSharingMutationRejectsInFlightClosure(t *testing.T) {
	for _, change := range []string{"flag", "revocation", "downgrade", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b := sharingFixture(t)
			p := enroll(t, b.svc, b.f, "blocked-review-operator", "operator-workspace", "operator")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			tx := beginFixtureTransaction(ctx, t, migrator)
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, proposalRequestID); err != nil {
				t.Fatal(err)
			}
			if change == "expiry" {
				p.expires = time.Now().Add(time.Second)
			}
			result := make(chan error, 1)
			go func() {
				result <- b.svc.persistVisibility(ctx, p, proposalRequestID, visibilityDecision{Proposal: 1, ExpectedProposal: 1, Action: "share", Key: secret()})
			}()
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
					t.Fatal("fixture did not reach blocked sharing write")
				}
			}
			switch change {
			case "flag":
				sharingFlags(t, b, true, true, true, true, false)
			case "revocation":
				revokeFixtureAccess(ctx, t, b, p.subject)
			case "downgrade":
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE subject=$1`, p.subject); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				waitForFixtureExpiry(ctx, t, p.expires)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrDenied) {
				t.Fatalf("inflight %s allowed sharing: %v", change, err)
			}
			var count int
			if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM portal_proposal_visibility`).Scan(&count); err != nil || count != 0 {
				t.Fatal("denied write left visibility event", err)
			}
		})
	}
}

func TestSharingIndependentGatesAndClientScopes(t *testing.T) {
	b := sharingFixture(t)
	f := visibilityForm(t, b, "0", "1", "1", "share")
	saveVisibility(t, b, f)
	for off := range 5 {
		values := []any{true, true, true, true, true}
		values[off] = false
		sharingFlags(t, b, values...)
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
		if status == 200 && strings.Contains(body, "Share version") {
			t.Fatalf("gate %d retained sharing UI", off)
		}
		status, _, _ = b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", f, b.site.URL)
		if status != 404 {
			t.Fatalf("gate %d POST status=%d", off, status)
		}
	}
	sharingFlags(t, b, true, true, true, true, true)
	for _, identity := range []struct{ subject, client, role string }{{"other-client", "different-client", "client"}, {"client-shaped-operator", "private-client-key", "operator"}} {
		triageSignIn(t, b, identity.subject, identity.client, identity.role)
		status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
		if status == 200 || strings.Contains(body, "A fictional first iteration") {
			t.Fatal("cross-client or operator read admitted")
		}
	}
	triageSignIn(t, b, "review-client", "private-client-key", "client")
	_, shell, _ := b.request("GET", "/portal/", nil, "")
	token := csrfInput.FindStringSubmatch(shell)
	if len(token) != 2 {
		t.Fatal("client shell has no genuine CSRF token")
	}
	f.Set("csrf_token", html.UnescapeString(token[1]))
	status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", f, b.site.URL)
	if status != 403 {
		t.Fatalf("client sharing status=%d", status)
	}
}

type lateSharingProvider struct {
	openfeature.FeatureProvider
	reads            atomic.Int32
	blocked, release chan struct{}
}

func (p *lateSharingProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, evaluation openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if key == "portal-proposal-sharing" && p.reads.Add(1) == 2 {
		close(p.blocked)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, evaluation)
}

func TestSharingClientExpiryDuringFinalSDKEvaluationDenies(t *testing.T) {
	b := sharingFixture(t)
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	p := enroll(t, b.svc, b.f, "late-review-client", "private-client-key", "client")
	flags := map[string]memprovider.InMemoryFlag{}
	for _, key := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing"} {
		flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	provider := &lateSharingProvider{FeatureProvider: memprovider.NewInMemoryProvider(flags), blocked: make(chan struct{}), release: make(chan struct{})}
	if err := b.flags.SetProviderAndWait(b.f.ctx, provider); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.expires = time.Now().Add(time.Second)
	result := make(chan error, 1)
	go func() { _, err := b.svc.readSharedProposal(ctx, p, proposalRequestID); result <- err }()
	select {
	case <-provider.blocked:
	case <-ctx.Done():
		t.Fatal("final SDK evaluation was not reached")
	}
	waitForFixtureExpiry(ctx, t, p.expires)
	close(provider.release)
	if err := <-result; !errors.Is(err, ErrDenied) {
		t.Fatal("content released after expiry during final SDK evaluation", err)
	}
}

func TestSharingReadFailsOnStorageAndInFlightRevocation(t *testing.T) {
	for _, change := range []string{"storage", "flag", "revocation", "rebind", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b := sharingFixture(t)
			saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
			p := enroll(t, b.svc, b.f, "inflight-review-client", "private-client-key", "client")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			if change == "storage" {
				if _, err := migrator.Exec(ctx, `REVOKE SELECT ON portal_proposal_visibility FROM portal_runtime`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := migrator.Exec(context.Background(), `GRANT SELECT ON portal_proposal_visibility TO portal_runtime`); err != nil {
						t.Error("fixture visibility privilege cleanup failed")
					}
				}()
				if _, err := b.svc.readSharedProposal(ctx, p, proposalRequestID); !errors.Is(err, errTriageStorage) {
					t.Fatal("storage failure masked", err)
				}
				return
			}
			tx, relation := lockFixtureRead(ctx, t, migrator, "portal_proposal_visibility")
			if change == "expiry" {
				p.expires = time.Now().Add(time.Second)
			}
			result := make(chan error, 1)
			go func() { _, err := b.svc.readSharedProposal(ctx, p, proposalRequestID); result <- err }()
			waitForTriageRead(ctx, t, migrator, relation)
			switch change {
			case "flag":
				sharingFlags(t, b, true, true, true, true, false)
			case "revocation":
				revokeFixtureAccess(ctx, t, b, p.subject)
			case "rebind":
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET client_id='different-client' WHERE subject=$1`, p.subject); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				waitForFixtureExpiry(ctx, t, p.expires)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrDenied) {
				t.Fatalf("inflight %s released content: %v", change, err)
			}
		})
	}
}

func TestProposalSharingExplicitlyReleasesOnlySelectedClientPreview(t *testing.T) {
	b := proposalFixture(t)
	sharingFlags(t, b, true, true, true, true, true)
	draft := proposalForm(t, b)
	draft.Set("scope", "Fictional selected proposal for private review")
	saveFixtureProposal(t, b, draft)
	form := url.Values{"csrf_token": {draft.Get("csrf_token")}, "submission_key": {"BBBBBBBBBBBBBBBBBBBBBBBBBB"}, "expected_event": {"0"}, "expected_proposal": {"1"}, "proposal_version": {"1"}, "action": {"share"}, "lang": {"en"}}
	status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposal-visibility", form, b.site.URL)
	if status != 303 {
		t.Fatalf("explicit sharing status=%d, want a successful native form redirect", status)
	}
	triageSignIn(t, b, "proposal-review-client", "private-client-key", "client")
	status, body, headers := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
	if status != 200 || !strings.Contains(body, "Fictional selected proposal for private review") || !strings.Contains(body, "Acceptance and ordering are not available here") || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("the intended client cannot privately review the exact shared preview")
	}
}
