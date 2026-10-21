package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func workApprovalFlags() map[string]memprovider.InMemoryFlag {
	flags := acceptanceMemoryFlags()
	for _, key := range []string{"portal-invoices", "portal-credit-reconciliation", "portal-work-approvals"} {
		flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	return flags
}

func preparedApproval(t *testing.T, b *browserFixture, op Principal) workApprovalInput {
	t.Helper()
	input, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1)
	if err != nil {
		t.Fatal("approval fixture review failed", err)
	}
	return input
}

func savedApproval(t *testing.T, b *browserFixture, op Principal) workApprovalReceipt {
	t.Helper()
	input := preparedApproval(t, b, op)
	d, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if err != nil {
		t.Fatal("approval fixture decision failed", err)
	}
	return d
}

func setApprovalGate(t *testing.T, b *browserFixture, key string, value any) {
	t.Helper()
	flags := workApprovalFlags()
	if value == nil {
		delete(flags, key)
	} else {
		flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "off", Variants: map[string]any{"off": value}}
	}
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

func approvalWaitContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx, cancel
}

func blockApprovalFixture(t *testing.T, op *Principal, expires bool) (context.Context, *pgxpool.Pool, pgx.Tx) {
	t.Helper()
	ctx, cancel := approvalWaitContext(t)
	migrator := migrationDB(t)
	lock := blockFixtureProposalWrite(ctx, t, migrator)
	// Preserve cancellation before transaction cleanup on an assertion abort.
	// The earlier registration still covers a failure while obtaining the lock.
	t.Cleanup(cancel)
	if expires {
		op.expires = time.Now().Add(time.Second)
	}
	return ctx, migrator, lock
}

func pendingApproval(ctx context.Context, b *browserFixture, op Principal, input workApprovalInput) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := b.svc.persistWorkApproval(ctx, op, proposalRequestID, input)
		result <- err
	}()
	return result
}

// All three service entrypoints require independent typed feature decisions.
// No client membership, even in the request's workspace, supplies operator authority.
func TestWorkApprovalGatesAndClientDenial(t *testing.T) {
	b, op := approvalFixture(t)
	input := preparedApproval(t, b, op)
	for _, key := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation", "portal-work-approvals"} {
		for _, state := range []string{"absent", "false", "wrong type"} {
			t.Run(key+"/"+state, func(t *testing.T) {
				setApprovalGate(t, b, key, map[string]any{"absent": nil, "false": false, "wrong type": "true"}[state])
				if _, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1); !errors.Is(err, ErrDenied) {
					t.Fatal("closed gate admitted review", err)
				}
				if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); !errors.Is(err, ErrDenied) {
					t.Fatal("closed gate admitted decision", err)
				}
				if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); !errors.Is(err, ErrDenied) || valid {
					t.Fatal("closed gate admitted selection", err)
				}
			})
		}
	}
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{"private-client-key", "other-client"} {
		client := enroll(t, b.svc, b.f, "approval-client-"+workspace, workspace, "client")
		if _, err := b.svc.persistWorkApproval(t.Context(), client, proposalRequestID, input); !errors.Is(err, ErrDenied) {
			t.Fatal("client granted work authority", err)
		}
		if _, _, err := b.svc.currentWorkApproval(t.Context(), client, proposalRequestID); !errors.Is(err, ErrDenied) {
			t.Fatal("client read operator authority", err)
		}
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_approvals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("denied decision changed history", err)
	}
}

// Revocation is a distinct entrypoint: invalid old evidence may not trap an
// approval, but neither clients nor a closed revocation gate may clear it.
func TestWorkApprovalRevocationGateAndActorBinding(t *testing.T) {
	b, op := approvalFixture(t)
	d := savedApproval(t, b, op)
	input := workRevocationInput{Key: secret(), Approval: d.ID, Expected: 1}
	for _, key := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-work-approvals"} {
		for _, value := range []any{nil, false, "true"} {
			setApprovalGate(t, b, key, value)
			if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, input); !errors.Is(err, ErrDenied) {
				t.Fatal("closed gate admitted revocation", err)
			}
		}
	}
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	client := enroll(t, b.svc, b.f, "revoking-client", "private-client-key", "client")
	if err := b.svc.revokeWorkApproval(t.Context(), client, proposalRequestID, input); !errors.Is(err, ErrDenied) {
		t.Fatal("client revoked operator decision", err)
	}
	other := enroll(t, b.svc, b.f, "another-operator", "independent-workspace", "operator")
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, input); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), other, proposalRequestID, input); !errors.Is(err, errProposalConflict) {
		t.Fatal("another actor reused revocation identity", err)
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_revocations`).Scan(&count); err != nil || count != 1 {
		t.Fatal("denied revocation changed terminal history", err)
	}
}

// Equality is closed, including at offsets either side of a daylight-saving
// transition. These exercise the same predicate applied to database-clock reads.
func TestWorkApprovalExclusiveDeadline(t *testing.T) {
	start := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.FixedZone("before-transition", 2*60*60))
	deadline := start.Add(168 * time.Hour)
	if !workDeadlineValid(deadline.Add(-time.Nanosecond), deadline) ||
		workDeadlineValid(deadline, deadline) ||
		workDeadlineValid(deadline.Add(time.Nanosecond), deadline) ||
		workDeadlineValid(deadline.In(time.FixedZone("after-transition", 60*60)), deadline) {
		t.Fatal("exclusive elapsed-time deadline failed")
	}
}

// This changes only the disposable migrator's synthetic row and uses the actual
// v14 constraint under a DST-observing PostgreSQL timezone.
func TestWorkApprovalElapsedDatabaseConstraint(t *testing.T) {
	b, op := approvalFixture(t)
	savedApproval(t, b, op)
	tx := beginFixtureTransaction(t.Context(), t, migrationDB(t))
	if _, err := tx.Exec(t.Context(), `SET LOCAL TIME ZONE 'Europe/Copenhagen'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `UPDATE portal_work_approvals SET decided_at='2026-10-24T12:00:00+02',expires_at='2026-10-31T11:00:00+01'`); err != nil {
		t.Fatal("168-hour DST-spanning control refused", err)
	}
	_, err := tx.Exec(t.Context(), `UPDATE portal_work_approvals SET expires_at=decided_at+interval '7 days'`)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatal("calendar days bypassed actual elapsed-hour constraint", err)
	}
}

// The native SDK may still return true from ERROR/STALE providers. Both settled
// admission and a state change during the final wait must remain closed.
func TestWorkApprovalSDKStateDenial(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		for _, duringWait := range []bool{false, true} {
			for _, revocation := range []bool{false, true} {
				t.Run(string(event)+"/"+map[bool]string{false: "admission", true: "final wait"}[duringWait]+"/"+map[bool]string{false: "approval", true: "revocation"}[revocation], func(t *testing.T) {
					b, op := approvalFixture(t)
					input := preparedApproval(t, b, op)
					var d workApprovalReceipt
					if revocation {
						d = savedApproval(t, b, op)
					}
					inner := &unsettledProvider{FeatureProvider: memprovider.NewInMemoryProvider(workApprovalFlags()), events: make(chan openfeature.Event, 1)}
					at := int32(0)
					if duringWait {
						at = 3
						if revocation {
							at = 2
						}
					}
					provider := &finalRequestProvider{FeatureProvider: inner, key: "portal-work-approvals", blockAt: at, blocked: make(chan struct{}), release: make(chan struct{})}
					// Keep the event channel on the installed provider rather than the
					// embedded interface, whose method set intentionally omits events.
					eventProvider := &approvalEventProvider{finalRequestProvider: provider, events: inner.events}
					release := installAcceptanceWait(t, b, eventProvider, provider.release)
					settled := make(chan struct{})
					callback := func(openfeature.EventDetails) { close(settled) }
					b.flags.NewClient().AddHandler(event, &callback)
					signal := func() {
						inner.events <- openfeature.Event{EventType: event}
						select {
						case <-settled:
						case <-time.After(time.Second):
							t.Fatal("SDK state did not settle")
						}
					}
					if !duringWait {
						signal()
					}
					result := make(chan error, 1)
					go func() {
						if revocation {
							result <- b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, workRevocationInput{Key: secret(), Approval: d.ID, Expected: 1})
						} else {
							_, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
							result <- err
						}
					}()
					if duringWait {
						select {
						case <-provider.blocked:
						case <-time.After(5 * time.Second):
							t.Fatal("final state checkpoint not reached")
						}
						signal()
					}
					release()
					select {
					case err := <-result:
						if err == nil {
							t.Fatal("unhealthy SDK committed authority change")
						}
					case <-time.After(5 * time.Second):
						t.Fatal("SDK-state refusal did not finish")
					}
					var approvals, revocations int
					if err := b.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_work_approvals),(SELECT count(*) FROM portal_work_revocations)`).Scan(&approvals, &revocations); err != nil || revocations != 0 || (revocation && approvals != 1) || (!revocation && approvals != 0) {
						t.Fatal("unhealthy SDK changed evidence", err)
					}
				})
			}
		}
	}
}

type approvalEventProvider struct {
	*finalRequestProvider
	events <-chan openfeature.Event
}

func (p *approvalEventProvider) EventChannel() <-chan openfeature.Event { return p.events }

// Identity restoration restores eligibility for a new operator decision, never
// the old approval. The client's existing consent is not rewritten or invented.
func TestWorkApprovalMembershipRestorationRequiresNewDecision(t *testing.T) {
	for _, actor := range []string{"credit-fixture-client", "approval-operator"} {
		t.Run(actor, func(t *testing.T) {
			b, op := approvalFixture(t)
			first := savedApproval(t, b, op)
			viewer := enroll(t, b.svc, b.f, "independent-approval-reader", "other-operator-space", "operator")
			if err := b.svc.Revoke(t.Context(), b.f.issuer, actor); err != nil {
				t.Fatal(err)
			}
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), viewer, proposalRequestID); err != nil || valid {
				t.Fatal("revoked original actor remained valid", err)
			}
			if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=false WHERE issuer=$1 AND subject=$2`, b.f.issuer, actor); err != nil {
				t.Fatal(err)
			}
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), viewer, proposalRequestID); err != nil || valid {
				t.Fatal("restoration revived old approval", err)
			}
			input, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1)
			if err != nil || input.Expected != 1 {
				t.Fatal("restored exact principal cannot receive new review", err)
			}
			next, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
			if err != nil || next.ID == first.ID {
				t.Fatal("new explicit decision missing", err)
			}
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), viewer, proposalRequestID); err != nil || !valid {
				t.Fatal("independent current operator cannot validate renewed decision", err)
			}
			var acceptances int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_proposal_acceptances`).Scan(&acceptances); err != nil || acceptances != 1 {
				t.Fatal("restoration invented client consent", err)
			}
		})
	}
}

// Equivalent totals are not equivalent evidence: corrections invalidate the old
// watermark, including when a later observation returns to the original amount.
func TestWorkApprovalFundingWatermarkAndRevocation(t *testing.T) {
	b, op := approvalFixture(t)
	first := savedApproval(t, b, op)
	for i, state := range []string{"disputed", "confirmed"} {
		if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: state, Expected: i + 1, AmountMinor: 123456, Note: "Fictional revised observation"}); err != nil {
			t.Fatal(err)
		}
		if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || valid {
			t.Fatal("changed evidence preserved old authority", err)
		}
	}
	next := savedApproval(t, b, op)
	if next.EvidenceHash == first.EvidenceHash {
		t.Fatal("corrected history lost its watermark")
	}
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: "invalidated", Expected: 3, Note: "Fictional invalidation"}); err != nil {
		t.Fatal(err)
	}
	// Revocation does not depend on funding, client access or the optional credit gate.
	if err := b.svc.Revoke(t.Context(), b.f.issuer, "credit-fixture-client"); err != nil {
		t.Fatal(err)
	}
	flags := workApprovalFlags()
	delete(flags, "portal-credit-reconciliation")
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, workRevocationInput{Key: secret(), Approval: next.ID, Expected: 2}); err != nil {
		t.Fatal("invalid evidence prevented terminal revocation", err)
	}
}

// A fresh invoice changes the complete request watermark even if invoice one
// remains fully funded.
func TestWorkApprovalInvoiceWatermark(t *testing.T) {
	b, op := approvalFixture(t)
	first := savedApproval(t, b, op)
	input := preparedApproval(t, b, op)
	d, err := scanInvoice(b.pool.QueryRow(t.Context(), `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE sequence=1`))
	if err != nil {
		t.Fatal(err)
	}
	d.Key, d.Reference, d.Expected = secret(), "FICTIONAL-02", 1
	if err := b.svc.persistInvoice(t.Context(), op, proposalRequestID, d); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || valid {
		t.Fatal("new invoice did not invalidate old watermark", err)
	}
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); !errors.Is(err, errProposalConflict) {
		t.Fatal("stale review survived new invoice", err)
	}
	renewed := savedApproval(t, b, op)
	if renewed.ID == first.ID || renewed.EvidenceHash == first.EvidenceHash {
		t.Fatal("complete invoice history omitted")
	}
}

// Replaying actual migrations preserves an old invoice's unknown components;
// neither matching credit nor a fresh operator may fill in those tax facts.
func TestWorkApprovalUnknownLegacyTaxDenied(t *testing.T) {
	b, op := approvalFixture(t)
	migrator := migrationDB(t)
	rewindInvoiceTaxFixture(t, migrator)
	if err := Migrate(t.Context(), migrator); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(t.Context(), `GRANT SELECT ON portal_membership_transitions TO portal_runtime;
 GRANT SELECT,INSERT ON portal_work_approvals,portal_work_revocations,portal_work_current TO portal_runtime;
 GRANT UPDATE(approval_id,generation) ON portal_work_current TO portal_runtime`); err != nil {
		t.Fatal("restored fixture grants failed", err)
	}
	if _, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1); !errors.Is(err, errProposalConflict) {
		t.Fatal("unknown historical tax admitted a work review", err)
	}
	var unknown bool
	if err := b.pool.QueryRow(t.Context(), `SELECT net_minor IS NULL AND vat_minor IS NULL AND amount_minor=123456 FROM portal_invoices WHERE sequence=1`).Scan(&unknown); err != nil || !unknown {
		t.Fatal("approval inferred historical tax", err)
	}
}

func TestWorkApprovalIneligibleFundingDenied(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		amount      int64
	}{
		{"partial", "confirmed", 123455}, {"excess", "confirmed", 123457},
		{"unverified", "unverified", 123456}, {"disputed", "disputed", 123456},
		{"invalidated", "invalidated", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, op := approvalFixture(t)
			if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", Expected: 1, State: tc.state, AmountMinor: tc.amount, Note: "Fictional ineligible evidence"}); err != nil {
				t.Fatal(err)
			}
			if _, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1); !errors.Is(err, errProposalConflict) {
				t.Fatal("ineligible funding admitted review", err)
			}
		})
	}
}

// Compare-and-append serializes distinct decisions while identical retries
// return one receipt. A losing request creates no orphan decision or terminal row.
func TestWorkApprovalConcurrentCompareAndRetry(t *testing.T) {
	for _, identical := range []bool{true, false} {
		t.Run(map[bool]string{true: "same retry", false: "distinct decisions"}[identical], func(t *testing.T) {
			b, op := approvalFixture(t)
			a, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1)
			if err != nil {
				t.Fatal(err)
			}
			c := a
			if !identical {
				c.Key = secret()
			}
			results := make(chan error, 2)
			for _, input := range []workApprovalInput{a, c} {
				go func() { _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); results <- err }()
			}
			successes := 0
			for range 2 {
				if err := <-results; err == nil {
					successes++
				} else if !errors.Is(err, errProposalConflict) {
					t.Fatal(err)
				}
			}
			want := 1
			if identical {
				want = 2
			}
			if successes != want {
				t.Fatal("compare token or idempotency failed", successes)
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_approvals`).Scan(&count); err != nil || count != 1 {
				t.Fatal("concurrent decision left duplicate evidence", err)
			}
		})
	}
}

// Request-lock waiting must not retain a stale operator role or extend a token.
func TestWorkApprovalRechecksAccessAfterRequestLock(t *testing.T) {
	for _, change := range []string{"revoke", "rebind", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			input := preparedApproval(t, b, op)
			ctx, migrator, lock := blockApprovalFixture(t, &op, change == "expiry")
			result := pendingApproval(ctx, b, op, input)
			waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
			switch change {
			case "revoke":
				revokeFixtureAccess(ctx, t, b, op.subject)
			case "rebind":
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE issuer=$1 AND subject=$2`, b.f.issuer, op.subject); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				waitForFixtureExpiry(ctx, t, op.expires)
			}
			releaseFixtureAndRequireDenial(ctx, t, lock, result, change)
			var count int
			if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM portal_work_approvals`).Scan(&count); err != nil || count != 0 {
				t.Fatal("denied delayed decision committed", err)
			}
		})
	}
}

// The final external SDK wait precedes the last principal/deadline checkpoint;
// no browser confirmation or process clock can extend a persisted deadline.
func TestWorkApprovalFinalSDKAndDatabaseDeadline(t *testing.T) {
	for _, change := range []string{"expiry", "flag", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			input := preparedApproval(t, b, op)
			provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(workApprovalFlags()), key: "portal-work-approvals", blockAt: 3, blocked: make(chan struct{}), release: make(chan struct{})}
			release := installAcceptanceWait(t, b, provider, provider.release)
			ctx, cancel := approvalWaitContext(t)
			defer cancel()
			if change == "expiry" {
				op.expires = time.Now().Add(time.Second)
			}
			result := pendingApproval(ctx, b, op, input)
			select {
			case <-provider.blocked:
			case <-ctx.Done():
				t.Fatal("final SDK wait not reached")
			}
			switch change {
			case "expiry":
				waitForFixtureExpiry(ctx, t, op.expires)
			case "flag":
				provider.denied.Store(true)
			case "cancel":
				cancel()
			}
			release()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("closed final checkpoint committed")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("delayed approval did not finish")
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_approvals`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed final checkpoint left evidence", err)
			}
		})
	}
	b, op := approvalFixture(t)
	d := savedApproval(t, b, op)
	// Only this disposable migrator can alter append-only fixture rows.
	if _, err := migrationDB(t).Exec(t.Context(), `WITH at AS MATERIALIZED(SELECT clock_timestamp() AS value) UPDATE portal_work_approvals SET decided_at=at.value-interval '168 hours',expires_at=at.value FROM at`); err != nil {
		t.Fatal("owned exact expiry fixture failed", err)
	}
	if got, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || valid || got.ID != d.ID {
		t.Fatal("expired selection supplied authority or fallback", err)
	}
}
