package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func eligibilityFlags() map[string]memprovider.InMemoryFlag {
	flags := workApprovalFlags()
	flags["portal-work-eligibility"] = memprovider.InMemoryFlag{Key: "portal-work-eligibility", State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	return flags
}

func TestWorkEligibilityOriginalOrderAndBoundedCursor(t *testing.T) {
	b, op := approvalFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// Reverse insertion order and equal instants cannot replace the immutable
	// original submission order and request-ID tie breaker by byte order.
	for i := 22; i > 0; i-- {
		id := string(rune('A'+i)) + strings.Repeat("A", 25)
		seedTriageRequest(t, b, id, "Inert fictional inquiry", at)
	}
	page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if err != nil || len(page.Items) != 20 || page.Next.ID != "T"+strings.Repeat("A", 25) || !page.Next.Submitted.Equal(at) {
		t.Fatalf("first eligibility page lost its bound or original cursor: %+v %v", page, err)
	}
	for i, item := range page.Items {
		if item.Request != string(rune('A'+i))+strings.Repeat("A", 25) || !item.Submitted.Equal(at) || item.State != eligibilityApprovalRequired {
			t.Fatalf("equal-time request was reordered or approved: %+v", item)
		}
	}
	next, err := b.svc.readWorkEligibility(t.Context(), op, page.Next)
	if err != nil || len(next.Items) != 3 || next.Next.ID != "" {
		t.Fatalf("continuation duplicated or omitted requests: %+v %v", next, err)
	}
	for i, item := range next.Items {
		if item.Request != string(rune('U'+i))+strings.Repeat("A", 25) {
			t.Fatalf("continuation changed original order: %+v", item)
		}
	}
}

func TestWorkEligibilityReusesCurrentApprovalPolicy(t *testing.T) {
	for _, change := range []string{"refunded", "disputed", "withdrawn", "reference", "unsettled", "no-early-start"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			saved := savedApproval(t, b, op)
			setEligibilityFlags(t, b, eligibilityFlags())
			before, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
			if err != nil || len(before.Items) != 1 || before.Items[0].State != eligibilityConfirmationPending || before.Items[0].Approval != saved.ID || before.Items[0].Generation != 1 {
				t.Fatalf("selected approval was lost or overstated without a confirmation: %+v %v", before, err)
			}
			changeFixtureFunding(t, change)
			after, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
			if err != nil || len(after.Items) != 1 || after.Items[0].State != eligibilityApprovalPaused || after.Items[0].Approval != saved.ID || after.Items[0].Generation != 1 {
				t.Fatalf("changed funding remained available for service: %+v %v", after, err)
			}
		})
	}
}

func TestWorkEligibilityStorageFailureIsNotAnEmptyQueue(t *testing.T) {
	b, op := approvalFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	owner := migrationDB(t)
	if _, err := owner.Exec(t.Context(), `REVOKE SELECT ON portal_memberships FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `GRANT SELECT ON portal_memberships TO portal_runtime`); err != nil {
			t.Error(err)
		}
	})
	page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if !errors.Is(err, errTriageStorage) || len(page.Items) != 0 {
		t.Fatalf("unreadable authority became an empty/denied queue: %+v %v", page, err)
	}
}

func TestWorkEligibilityDiscardsPartialObservations(t *testing.T) {
	b, op := approvalFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	seedTriageRequest(t, b, "BBBBBBBBBBBBBBBBBBBBBBBBBB", "Fictional second inquiry", time.Now().UTC())
	checks := 0
	page, err := b.svc.readWorkEligibilityChecked(t.Context(), op, triageCursor{}, func(context.Context, pgx.Tx, string) error {
		checks++
		if checks == 2 {
			return errTriageStorage
		}
		return nil
	})
	if checks != 2 || !errors.Is(err, errTriageStorage) || len(page.Items) != 0 || page.Next.ID != "" {
		t.Fatalf("later uncertainty released a partial queue: checks=%d page=%+v err=%v", checks, page, err)
	}
}

func TestWorkEligibilityNeverRestartsClosedWork(t *testing.T) {
	for _, change := range []string{"request-closed", "order-completed"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			saved := savedApproval(t, b, op)
			setEligibilityFlags(t, b, eligibilityFlags())
			statement := `INSERT INTO portal_request_closures(request_id,client_id,operator_issuer,operator_subject) SELECT request_id,client_id,operator_issuer,operator_subject FROM portal_work_approvals WHERE id=$1`
			if change == "order-completed" {
				statement = `INSERT INTO portal_order_completions(order_id,operator_issuer,operator_subject) SELECT order_id,operator_issuer,operator_subject FROM portal_work_approvals WHERE id=$1`
			}
			if _, err := migrationDB(t).Exec(t.Context(), statement, saved.ID); err != nil {
				t.Fatal(err)
			}
			page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
			if err != nil || len(page.Items) != 1 || page.Items[0].State != workEligibilityState("closed") || page.Items[0].Approval != saved.ID {
				t.Fatalf("historical approval reopened finished work: %+v %v", page, err)
			}
			// Starting eligibility must not revoke the operator's durable history.
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || !valid {
				t.Fatalf("eligibility rewrote approval history: valid=%v err=%v", valid, err)
			}
		})
	}
}

func TestWorkEligibilityChangedAgreementAndMembershipPause(t *testing.T) {
	for _, change := range []string{"scope", "acceptor-revoked", "expired"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			saved := savedApproval(t, b, op)
			setEligibilityFlags(t, b, eligibilityFlags())
			switch change {
			case "scope":
				proposal := projectProposal{Key: secret(), Expected: 1, Scope: "Changed fictional scope", Deliverables: "Another iteration", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One agreed iteration", MonthlyTerms: "Hosting and routine maintenance"}
				if err := b.svc.persistProposal(t.Context(), op, proposalRequestID, proposal); err != nil {
					t.Fatal(err)
				}
			case "acceptor-revoked":
				if err := b.svc.Revoke(t.Context(), b.f.issuer, "credit-fixture-client"); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := migrationDB(t).Exec(t.Context(), `WITH at AS MATERIALIZED(SELECT clock_timestamp() AS value) UPDATE portal_work_approvals SET decided_at=at.value-interval '168 hours',expires_at=at.value FROM at`); err != nil {
					t.Fatal(err)
				}
			}
			page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
			if err != nil || len(page.Items) != 1 || page.Items[0].State != eligibilityApprovalPaused || page.Items[0].Approval != saved.ID {
				t.Fatalf("changed authority remained available for service: %+v %v", page, err)
			}
		})
	}
}

// The final SDK evaluation of a row and the final evaluation of the complete
// page both precede fresh authority checks. No partial page survives denial.
func TestWorkEligibilityFinalWaitRefusesStaleRead(t *testing.T) {
	for _, at := range []int32{3, 4} {
		for _, change := range []string{"unchanged", "flag", "cancel", "expiry"} {
			t.Run(string(rune('0'+at))+"/"+change, func(t *testing.T) {
				b, op := approvalFixture(t)
				if change == "expiry" {
					op.expires = time.Now().Add(time.Second)
				}
				checkpoint := newEligibilityCheckpoint(t, b, op, at)
				switch change {
				case "flag":
					checkpoint.provider.denied.Store(true)
				case "cancel":
					checkpoint.cancel()
				case "expiry":
					waitForFixtureExpiry(checkpoint.ctx, t, op.expires)
				}
				checkpoint.release()
				select {
				case got := <-checkpoint.result:
					if change == "unchanged" {
						if got.err != nil || len(got.page.Items) != 1 {
							t.Fatalf("unchanged authority positive control failed: %v", got.err)
						}
					} else if got.err == nil || len(got.page.Items) != 0 {
						t.Fatalf("closed checkpoint released work: %+v %v", got.page, got.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("eligibility read did not finish")
				}
			})
		}
	}
}

func TestWorkEligibilityRejectsUnhealthySDK(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		t.Run(string(event), func(t *testing.T) {
			b, op := approvalFixture(t)
			requireFixtureSDKEvent(t, b.flags, b.svc.flags, eligibilityFlags(), event)
			page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
			if !errors.Is(err, ErrDenied) || len(page.Items) != 0 {
				t.Fatalf("unhealthy SDK released eligibility: %+v %v", page, err)
			}
		})
	}
}

func TestWorkEligibilityRechecksSelectedDatabaseDeadline(t *testing.T) {
	b, op := approvalFixture(t)
	savedApproval(t, b, op)
	owner := migrationDB(t)
	var expires time.Time
	if err := owner.QueryRow(t.Context(), `WITH at AS MATERIALIZED(SELECT clock_timestamp()+interval '1 second' AS value) UPDATE portal_work_approvals SET decided_at=at.value-interval '168 hours',expires_at=at.value FROM at RETURNING expires_at`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	checkpoint := newEligibilityCheckpoint(t, b, op, 3)
	waitForFixtureExpiry(checkpoint.ctx, t, expires)
	var expired bool
	if err := owner.QueryRow(checkpoint.ctx, `SELECT clock_timestamp()>=$1::timestamptz`, expires).Scan(&expired); err != nil || !expired {
		t.Fatal("database deadline control did not expire", err)
	}
	checkpoint.release()
	select {
	case got := <-checkpoint.result:
		if !errors.Is(got.err, ErrDenied) || len(got.page.Items) != 0 {
			t.Fatalf("expired selected approval released observations: %+v %v", got.page, got.err)
		}
	case <-checkpoint.ctx.Done():
		t.Fatal("expired eligibility observation did not finish")
	}
}

// Sender acceptance of a legacy, uncertified confirmation is insufficient.
// The billing producer's exact frozen-agreement proof is a separate prerequisite.
func TestWorkEligibilityLegacySenderReceiptIsInsufficient(t *testing.T) {
	b, op := approvalFixture(t)
	saved := savedApproval(t, b, op)
	setEligibilityFlags(t, b, eligibilityFlags())
	m := mailFixture(t, b)
	assertEligibilityState(t, b, op, eligibilityConfirmationPending)
	wrong := secret()
	if _, err := b.pool.Exec(t.Context(), `WITH queued AS (INSERT INTO portal_mail_outbox(id,order_id,kind,recipient,subject,body) VALUES($1,$2,'withdrawal-receipt','fictional@example.invalid','Fictional receipt','Inert fixture') RETURNING id) INSERT INTO portal_mail_deliveries(outbox_id) SELECT id FROM queued`, wrong, saved.Order); err != nil {
		t.Fatal(err)
	}
	assertEligibilityState(t, b, op, eligibilityConfirmationPending)
	original := secret()
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_mail_outbox(id,order_id,kind,recipient,subject,body) VALUES($1,$2,'order-confirmation','fictional@example.invalid','Fictional original confirmation','Inert frozen fixture')`, original, saved.Order); err != nil {
		t.Fatal(err)
	}
	assertEligibilityState(t, b, op, eligibilityConfirmationPending)
	exerciseEligibilitySender(t, b, op, m, eligibilityConfirmationPending)
	var deliveries int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_mail_deliveries WHERE outbox_id=$1`, original).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatal("legacy sender receipt positive control failed", err)
	}
}

// The positive path uses the native order producer, the real immutable format-1
// proof, the fictional Numina HTTP fixture and the ordinary sender receipt.
// No external customer, provider or money connection is involved.
func TestWorkEligibilityCertifiedOriginalConfirmation(t *testing.T) {
	b := billingFixture(t)
	form := orderForm(t, b, "en")
	form.Set("early_start", "yes")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", form, b.site.URL); status != 303 {
		t.Fatal("native certified order fixture was refused", status)
	}
	fake, cfg := newFakeNumina(t)
	syncEligibilityInvoice(t, b, cfg)
	fake.confirmDenmark()
	syncEligibilityInvoice(t, b, cfg)
	fake.pay("paid", "0")
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "settled" {
		t.Fatal("fictional Numina fixture did not settle its exact order", err)
	}
	setEligibilityFlags(t, b, eligibilityFlags())
	op := enroll(t, b.svc, b.f, "certified-approval-operator", "operator-workspace", "operator")
	savedApproval(t, b, op)
	assertEligibilityState(t, b, op, eligibilityConfirmationPending)
	m := mailFixture(t, b)
	exerciseEligibilitySender(t, b, op, m, eligibilityEligible)
	owner := migrationDB(t)
	if _, err := owner.Exec(t.Context(), `REVOKE SELECT ON portal_mail_outbox FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if _, err := owner.Exec(context.Background(), `GRANT SELECT ON portal_mail_outbox TO portal_runtime`); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(restore)
	page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if !errors.Is(err, errTriageStorage) || len(page.Items) != 0 {
		t.Fatalf("unreadable confirmation became missing or eligible: %+v %v", page, err)
	}
	restore()
	assertEligibilityState(t, b, op, eligibilityEligible)
}

func setEligibilityFlags(t *testing.T, b *browserFixture, flags map[string]memprovider.InMemoryFlag) {
	t.Helper()
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

// The reader is a private operator observation, never a grant to a client or a
// worker. A missing, false or mistyped release gate must disclose no rows.
func TestWorkEligibilityGateAndOperatorBoundary(t *testing.T) {
	b, op := approvalFixture(t)
	for _, value := range []any{nil, false, "true"} {
		flags := eligibilityFlags()
		if value == nil {
			delete(flags, "portal-work-eligibility")
		} else {
			flags["portal-work-eligibility"] = memprovider.InMemoryFlag{Key: "portal-work-eligibility", State: memprovider.Enabled, DefaultVariant: "off", Variants: map[string]any{"off": value}}
		}
		setEligibilityFlags(t, b, flags)
		page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
		if !errors.Is(err, ErrDenied) || len(page.Items) != 0 {
			t.Fatalf("closed eligibility gate released work: %v", err)
		}
	}
	setEligibilityFlags(t, b, eligibilityFlags())
	client := enroll(t, b.svc, b.f, "eligibility-client", "private-client-key", "client")
	expired := op
	expired.expires = time.Now().Add(-time.Second)
	for _, actor := range []Principal{{}, client, expired} {
		page, err := b.svc.readWorkEligibility(t.Context(), actor, triageCursor{})
		if !errors.Is(err, ErrDenied) || len(page.Items) != 0 {
			t.Fatalf("non-current operator released work: %v", err)
		}
	}
	page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Request != proposalRequestID || page.Items[0].State != eligibilityApprovalRequired || page.Items[0].ObservedAt.IsZero() {
		t.Fatalf("operator observation missing or overstated: %+v %v", page, err)
	}
}

func TestWorkEligibilityRequiresOriginalMembershipEpoch(t *testing.T) {
	b, op := approvalFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	if op.membershipIncarnation == "" {
		t.Fatal("fixture operator has no server-issued membership epoch")
	}
	unbound := op
	unbound.membershipIncarnation = ""
	page, err := b.svc.readWorkEligibility(t.Context(), unbound, triageCursor{})
	if !errors.Is(err, ErrDenied) || len(page.Items) != 0 {
		t.Fatalf("unbound operator received eligibility observations: %+v %v", page, err)
	}
	for range 2 {
		if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=NOT revoked WHERE issuer=$1 AND subject=$2`, op.issuer, op.subject); err != nil {
			t.Fatal(err)
		}
	}
	page, err = b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if !errors.Is(err, ErrDenied) || len(page.Items) != 0 {
		t.Fatalf("reactivation restored an old operator epoch: %+v %v", page, err)
	}
	fresh := authenticateExistingMember(t, b, op.subject)
	if fresh.membershipIncarnation == "" || fresh.membershipIncarnation == op.membershipIncarnation {
		t.Fatal("fresh Begin/Complete did not issue the current membership epoch")
	}
	page, err = b.svc.readWorkEligibility(t.Context(), fresh, triageCursor{})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != eligibilityApprovalRequired {
		t.Fatalf("freshly authenticated operator lost eligibility access: %+v %v", page, err)
	}
}
