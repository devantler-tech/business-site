package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// TestWorkApprovalSchema requires all three durable authority relations, not a
// ledger version or an in-memory approval that disappears across reconnects.
func TestWorkApprovalSchema(t *testing.T) {
	pool := fixtureDB(t)
	for _, name := range []string{"portal_work_approvals", "portal_work_revocations", "portal_work_current"} {
		var present bool
		if err := pool.QueryRow(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&present); err != nil {
			t.Fatal("approval schema read failed", err)
		}
		if !present {
			t.Errorf("durable approval relation %s is absent", name)
		}
	}
}

// Funding that is no longer settled, was refunded or disputed, or belongs to a
// withdrawn order pauses a recorded approval and admits no fresh one.
func TestWorkApprovalRequiresSettledFundingNeverReversed(t *testing.T) {
	for _, change := range []string{"unsettled", "refunded", "disputed", "withdrawn"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			savedApproval(t, b, op)
			changeFixtureFunding(t, change)
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || valid {
				t.Fatalf("changed funding preserved authority: valid=%v err=%v", valid, err)
			}
			if _, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID); !errors.Is(err, errProposalConflict) {
				t.Errorf("changed funding admitted a fresh review: %v", err)
			}
		})
	}
}

func TestWorkApprovalRefusesTypedEvidenceAsConflict(t *testing.T) {
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)
	placeEarlyStartOrder(t, b)
	fake, cfg := newFakeNumina(t)
	paidInvoiceOrder(t, b, fake, cfg)
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	operator := authenticateExistingMember(t, b, "proposal-operator")
	if _, err := b.svc.prepareWorkApproval(t.Context(), operator, proposalRequestID); !errors.Is(err, errProposalConflict) {
		t.Fatalf("unsupported typed evidence returned %v, want a proposal conflict", err)
	}
}

// approvalFixture owns real storage, a TLS broker and independent SDK state.
// Domain controls need no HTTP application or session store; native journeys
// retain their separate browser fixtures and complete actor coverage.
func approvalFixture(t *testing.T) (*browserFixture, Principal) {
	t.Helper()
	b := &browserFixture{t: t, pool: fixtureDB(t), f: fixtureBroker(t), flags: flagAPI(t, true)}
	b.svc = New(b.pool, b.f.broker, b.flags.NewClient())
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	seedTriageRequest(t, b, proposalRequestID, "Fictional proposal request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	author := enroll(t, b.svc, b.f, "proposal-operator", "operator-workspace", "operator")
	proposal := projectProposal{Key: secret(), Scope: "A fictional first iteration", Deliverables: "A website and accessible contact page", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One agreed iteration", MonthlyTerms: "Hosting and routine maintenance"}
	if err := b.svc.persistProposal(t.Context(), author, proposalRequestID, proposal); err != nil {
		t.Fatal("fixture proposal unavailable", err)
	}
	if err := b.svc.persistVisibility(t.Context(), author, proposalRequestID, visibilityDecision{ExpectedProposal: 1, Proposal: 1, Action: "share", Key: secret()}); err != nil {
		t.Fatal("fixture sharing unavailable", err)
	}
	client := enroll(t, b.svc, b.f, "credit-fixture-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), client, proposalRequestID)
	if err != nil {
		t.Fatal("fixture consent review unavailable", err)
	}
	acceptance, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key)
	if err != nil {
		t.Fatal("fixture consent unavailable", err)
	}
	if acceptance.ID == "" {
		t.Fatal("fixture consent has no identity")
	}
	fundFixtureOrder(t)
	op := enroll(t, b.svc, b.f, "approval-operator", "operator-workspace", "operator")
	return b, op
}

// TestWorkApprovalExactBindingAndTerminalHistory exercises real persistence,
// historical retry, permanent supersession and revocation without resurrection.
func TestWorkApprovalExactBindingAndTerminalHistory(t *testing.T) {
	b, op := approvalFixture(t)
	input, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil {
		t.Fatal("funded approval review unavailable", err)
	}
	a, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if err != nil || a.ID == "" || a.Expires.Sub(a.Decided) != 168*time.Hour {
		t.Fatal("exact funded approval or fixed deadline failed", err)
	}
	current, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || !valid || current.ID != a.ID {
		t.Fatal("stored approval was not current", err)
	}
	pool, err := pgxpool.New(t.Context(), b.pool.Config().ConnString())
	if err != nil {
		t.Fatal("approval reconnect failed", err)
	}
	t.Cleanup(pool.Close)
	b.pool.Close()
	b.pool = pool
	b.svc = New(pool, b.f.broker, b.flags.NewClient())
	retry, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if err != nil || retry.ID != a.ID || !retry.Decided.Equal(a.Decided) || !retry.Expires.Equal(a.Expires) {
		t.Fatal("retry renewed or lost the original approval", err)
	}
	next, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || next.Expected != 1 {
		t.Fatal("next comparison token unavailable", err)
	}
	c, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, next)
	if err != nil || c.ID == a.ID {
		t.Fatal("fresh decision did not supersede the previous approval", err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, workRevocationInput{Key: secret(), Approval: a.ID, Expected: 1}); !errors.Is(err, errProposalConflict) {
		t.Fatal("stale revocation cleared a newer selection", err)
	}
	changed := input
	changed.EvidenceHash = c.EvidenceHash + "0"
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, changed); !errors.Is(err, errProposalConflict) {
		t.Fatal("changed retry replaced original evidence", err)
	}
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); err != nil {
		t.Fatal("historical retry lost its original receipt", err)
	}
	current, valid, err = b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || !valid || current.ID != c.ID {
		t.Fatal("old retry revived a superseded pointer", err)
	}
	revocation := workRevocationInput{Key: secret(), Approval: c.ID, Expected: 2}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, revocation); err != nil {
		t.Fatal("revocation failed", err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, revocation); err != nil {
		t.Fatal("identical revocation retry failed", err)
	}
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, next); err != nil {
		t.Fatal("revoked approval receipt retry failed", err)
	}
	current, valid, err = b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || valid || current.ID != "" {
		t.Fatal("revoked or superseded approval was resurrected", err)
	}
	var terminals int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_revocations`).Scan(&terminals); err != nil || terminals != 2 {
		t.Fatal("terminal history was lost or duplicated", err)
	}
}
