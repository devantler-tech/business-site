package identity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// TestOperatorAcceptanceDetail follows the actual session, SQL and template path.
func TestOperatorAcceptanceDetail(t *testing.T) {
	b := acceptanceFixture(t)
	clientSession := b.client
	path := "/portal/operator/requests/" + proposalRequestID
	operatorSession := triageSignIn(t, b, "receipt-operator", "operator-workspace", "operator")
	status, body, _ := b.request("GET", path, nil, "")
	if status != 200 || !strings.Contains(body, "No recorded client acceptance") {
		t.Fatal("operator cannot distinguish no acceptance from an unavailable receipt")
	}
	b.client = clientSession
	f := acceptanceForm(t, b)
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", f, b.site.URL); status != 303 {
		t.Fatal("fictional acceptance failed")
	}
	var receipt string
	if err := b.pool.QueryRow(t.Context(), `SELECT id FROM portal_proposal_acceptances WHERE request_id=$1`, proposalRequestID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	b.client = operatorSession
	for _, lang := range []string{"en", "da"} {
		status, body, h := b.request("GET", path+"?lang="+lang, nil, "")
		want, boundary := "Current acceptance", "Payment and permission to start work remain unverified"
		if lang == "da" {
			want, boundary = "Aktuel accept", "Betaling og tilladelse til at starte arbejdet er fortsat uafklaret"
		}
		if status != 200 || !strings.Contains(body, want) || !strings.Contains(body, receipt) || !strings.Contains(body, boundary) || h.Get("Cache-Control") != "no-store" {
			t.Fatal("operator lacks an exact, localized, non-cacheable acceptance receipt")
		}
		if strings.Contains(body, "Not accepted or approved for work.") || strings.Contains(body, "Ikke accepteret eller godkendt til arbejde.") {
			t.Fatal("draft wording contradicts the recorded client acceptance")
		}
	}
}

// acceptedOperatorFixture records real consent before provisioning its reader.
func acceptedOperatorFixture(t *testing.T) (*browserFixture, Principal, proposalAcceptance) {
	t.Helper()
	b, client, review := preparedConsentFixture(t, "operator-receipt-client")
	receipt, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key)
	if err != nil {
		t.Fatal("fictional client consent failed", err)
	}
	op := enroll(t, b.svc, b.f, "operator-receipt-reader", "operator-workspace", "operator")
	return b, op, receipt
}

// TestOperatorAcceptanceHistory retains exact content, identity and intake order
// after a private revision, a replacement share, withdrawal and pool reconnect.
func TestOperatorAcceptanceHistory(t *testing.T) {
	b, op, receipt := acceptedOperatorFixture(t)
	before, err := b.svc.readTriage(t.Context(), op, proposalRequestID, triageCursor{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.svc.readOperatorProposalAcceptances(t.Context(), op, proposalRequestID)
	if err != nil || len(first) != 1 || !first[0].Current || first[0].ID != receipt.ID || first[0].Event != 1 || first[0].Proposal.Version != 1 || first[0].Proposal.OneTimeMinor != 299500 {
		t.Fatal("exact current receipt unavailable", err)
	}
	draft := first[0].Proposal
	draft.Key, draft.Expected, draft.Scope = secret(), 1, "Fictional revised scope <script>not executable</script>"
	if err := b.svc.persistProposal(t.Context(), op, proposalRequestID, draft); err != nil {
		t.Fatal(err)
	}
	historical, err := b.svc.readOperatorProposalAcceptances(t.Context(), op, proposalRequestID)
	first[0].Current = false
	if err != nil || !reflect.DeepEqual(first, historical) {
		t.Fatal("unshared re-quote changed or reapplied the original acceptance", err)
	}
	if err := b.svc.persistVisibility(t.Context(), op, proposalRequestID, visibilityDecision{ExpectedEvent: 1, ExpectedProposal: 2, Proposal: 2, Action: "share", Key: secret()}); err != nil {
		t.Fatal(err)
	}
	client := enroll(t, b.svc, b.f, "replacement-receipt-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), client, proposalRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key); err != nil {
		t.Fatal(err)
	}
	current, err := b.svc.readOperatorProposalAcceptances(t.Context(), op, proposalRequestID)
	if err != nil || len(current) != 2 || !current[0].Current || current[1].Current || current[0].Event != 2 || current[0].Proposal.Version != 2 || !reflect.DeepEqual(current[1], first[0]) || current[0].Proposal.Scope != draft.Scope {
		t.Fatal("replacement consent or bounded newest-first history is incorrect", err)
	}
	if err := b.svc.persistVisibility(t.Context(), op, proposalRequestID, visibilityDecision{ExpectedEvent: 2, ExpectedProposal: 2, Proposal: 2, Action: "withdraw", Key: secret()}); err != nil {
		t.Fatal(err)
	}
	current[0].Current = false
	svc := New(b.reconnect(t.Context()), b.f.broker, b.flags.NewClient())
	after, err := svc.readOperatorProposalAcceptances(t.Context(), op, proposalRequestID)
	if err != nil || !reflect.DeepEqual(current, after) {
		t.Fatal("withdrawal/reconnect lost immutable receipts or retained current consent", err)
	}
	requests, err := svc.readTriage(t.Context(), op, proposalRequestID, triageCursor{})
	if err != nil || !reflect.DeepEqual(before, requests) {
		t.Fatal("consent reads changed original intake or ordering", err)
	}
	triageSignIn(t, b, "receipt-browser-operator", "operator-workspace", "operator")
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 200 || strings.Count(body, "Historical acceptance") != 2 || strings.Contains(body, "Current acceptance") || strings.Contains(body, "<script>not executable</script>") || !strings.Contains(body, "&lt;script&gt;not executable&lt;/script&gt;") {
		t.Fatal("operator history is missing, misleading or executable")
	}
}

// TestOperatorAcceptanceReaderAuthority never turns a client into an operator;
// an operator's separate workspace does not restrict its permitted inbox view.
func TestOperatorAcceptanceReaderAuthority(t *testing.T) {
	for _, role := range []string{"client", "other-client", "revoked", "expired", "operator"} {
		t.Run(role, func(t *testing.T) {
			b, op, _ := acceptedOperatorFixture(t)
			p := op
			switch role {
			case "client", "other-client":
				p = enroll(t, b.svc, b.f, role, role, "client")
			case "revoked":
				if err := b.svc.Revoke(t.Context(), op.issuer, op.subject); err != nil {
					t.Fatal(err)
				}
			case "expired":
				p.expires = time.Now().Add(-time.Second)
			}
			rows, err := b.svc.readOperatorProposalAcceptances(t.Context(), p, proposalRequestID)
			if role == "operator" {
				if err != nil || len(rows) != 1 {
					t.Fatal("cross-workspace operator lost authorized receipt", err)
				}
			} else if !errors.Is(err, ErrDenied) || len(rows) != 0 {
				t.Fatal("invalid authority received acceptance data", err)
			}
		})
	}
}

// TestOperatorAcceptanceMissingAndUnavailable keeps a failed private read distinct
// from no client decision, including the actual HTTP unavailable response.
func TestOperatorAcceptanceMissingAndUnavailable(t *testing.T) {
	b, op, _ := acceptedOperatorFixture(t)
	if rows, err := b.svc.readOperatorProposalAcceptances(t.Context(), op, "invalid"); len(rows) != 0 || !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("invalid request acquired a receipt", err)
	}
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `REVOKE SELECT ON portal_proposal_acceptances FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(t.Context(), `GRANT SELECT ON portal_proposal_acceptances TO portal_runtime`); err != nil {
			t.Error("fixture consent permission cleanup failed", err)
		}
	}()
	triageSignIn(t, b, "unavailable-browser-operator", "operator-workspace", "operator")
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 503 || strings.Contains(body, "No recorded client acceptance") || strings.Contains(body, "portal_proposal_acceptances") || strings.Contains(body, "permission denied") {
		t.Fatal("receipt storage failure was hidden or exposed private diagnostics")
	}
}

// TestOperatorAcceptanceMaterializedReadClosure exercises real SQL completion,
// not a mocked store, when a gate, session or request closes during the read.
func TestOperatorAcceptanceMaterializedReadClosure(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "flag", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b, op, _ := acceptedOperatorFixture(t)
			provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(acceptanceMemoryFlags()), key: "portal-proposal-acceptance"}
			if err := b.flags.SetProviderAndWait(t.Context(), provider); err != nil {
				t.Fatal(err)
			}
			tracer := installCompletedRead(t, b, "SELECT a.id,a.proposal_version")
			read := func(ctx context.Context, p Principal) (bool, error) {
				rows, err := b.svc.readOperatorProposalAcceptances(ctx, p, proposalRequestID)
				return len(rows) != 0, err
			}
			checkChangedRead(t, b, op, provider, tracer.blocked, tracer.release, change, true, true, read)
		})
	}
}

// TestOperatorAcceptanceGateDoesNotHideTheRequest preserves the independent gate.
func TestOperatorAcceptanceGateDoesNotHideTheRequest(t *testing.T) {
	b, op, receipt := acceptedOperatorFixture(t)
	triageSignIn(t, b, "receipt-operator", "operator-workspace", "operator")
	for _, off := range []any{nil, false, "true"} {
		acceptanceFlags(t, b, true, true, true, true, true, off)
		status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
		if status != 200 || !strings.Contains(body, "Fictional proposal request") || strings.Contains(body, "Client acceptance") || strings.Contains(body, receipt.ID) {
			t.Fatal("acceptance gate leaked consent or incorrectly hid ordinary triage")
		}
		if rows, err := b.svc.readOperatorProposalAcceptances(t.Context(), op, proposalRequestID); len(rows) != 0 || !errors.Is(err, ErrDenied) {
			t.Fatal("closed acceptance gate admitted the operator reader", err)
		}
	}
}

// TestOperatorAcceptanceLockWaitRechecksMembership revokes or downgrades the
// operator while its real database transaction waits behind a proposal writer.
func TestOperatorAcceptanceLockWaitRechecksMembership(t *testing.T) {
	for _, role := range []string{"revocation", "downgrade"} {
		t.Run(role, func(t *testing.T) {
			b, op, _ := acceptedOperatorFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			lock := blockFixtureProposalWrite(ctx, t, migrator)
			type result struct {
				rows []proposalAcceptance
				err  error
			}
			done := make(chan result, 1)
			go func() {
				rows, err := b.svc.readOperatorProposalAcceptances(ctx, op, proposalRequestID)
				done <- result{rows, err}
			}()
			waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
			if role == "revocation" {
				if err := b.svc.Revoke(ctx, op.issuer, op.subject); err != nil {
					t.Fatal(err)
				}
			} else if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE issuer=$1 AND subject=$2`, op.issuer, op.subject); err != nil {
				t.Fatal(err)
			}
			if err := lock.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-done:
				if len(got.rows) != 0 || !errors.Is(got.err, ErrDenied) {
					t.Fatal("closed operator retained receipt after the lock wait", got.err)
				}
			case <-ctx.Done():
				t.Fatal("operator reader did not finish")
			}
		})
	}
}
