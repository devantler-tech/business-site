package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func TestPaidAppointmentQuoteRequiresItsOwnGate(t *testing.T) {
	b := acceptedAppointmentQuoteFixture(t)
	var bound bool
	err := b.pool.QueryRow(t.Context(), `SELECT a.content_hash=p.content_sha256 AND a.client_incarnation IS NOT NULL
 FROM portal_proposal_acceptances a JOIN portal_proposals p USING (request_id,client_id)
 WHERE a.request_id=$1 AND p.version=a.proposal_version AND p.service_kind='appointment'`, proposalRequestID).Scan(&bound)
	if err != nil || !bound {
		t.Fatalf("acceptance did not retain the original appointment quote and human membership: %v", err)
	}
}

func acceptedAppointmentQuoteFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := proposalFixture(t)
	form := serviceProposalForm(t, b, proposalServiceAppointment)
	form.Set("monthly_dkk", "0")
	form.Set("monthly_terms", "No recurring charge")
	form.Set("appointment_minutes", "30")
	saveFixtureProposal(t, b, form)
	operator := authenticateExistingMember(t, b, "proposal-operator")
	draft := visibilityDecision{ExpectedProposal: 1, Proposal: 1, Action: "share", Key: secret()}
	flagNames := append(acceptanceFlagNames(), "portal-invoice-billing", "portal-paid-appointments")
	for _, option := range []any{nil, "true", false} {
		setFixturePortalFlags(t, b, flagNames, []any{true, true, true, true, true, true, true, option})
		if err := b.svc.persistVisibility(t.Context(), operator, proposalRequestID, draft); !errors.Is(err, ErrDenied) {
			t.Fatalf("appointment quote was available with gate %v: %v", option, err)
		}
	}
	setFixturePortalFlags(t, b, flagNames, []any{true, true, true, true, true, true, true, true})
	if err := b.svc.persistVisibility(t.Context(), operator, proposalRequestID, draft); err != nil {
		t.Fatalf("explicitly enabled appointment quote was refused: %v", err)
	}
	triageSignIn(t, b, "consent-client", "private-client-key", "client")
	status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", acceptanceForm(t, b), b.site.URL)
	if status != 303 {
		t.Fatalf("client could not accept the appointment quote: %d", status)
	}
	prepareServiceOrderConfirmation(t, b)
	return b
}

func TestPaidAppointmentOrderRollsBackWhenHoldExpiresDuringFinalEvaluation(t *testing.T) {
	b := acceptedAppointmentQuoteFixture(t)
	principal := authenticateExistingMember(t, b, "consent-client")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	ref := appointmentQuoteRef{Request: proposalRequestID}
	var acceptance string
	if err := b.pool.QueryRow(ctx, `SELECT id,proposal_version,content_hash FROM portal_proposal_acceptances WHERE request_id=$1`, proposalRequestID).
		Scan(&acceptance, &ref.Version, &ref.Hash); err != nil {
		t.Fatal(err)
	}
	start, policy := appointmentHoldFixture(t)
	selection := appointmentOrderSelection{Reservation: secret(), Revision: 1}
	seedAppointmentWorkspaces(t, b.pool, "private-client-key")
	if err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		_, err := policy.append(ctx, tx, selection.Reservation, "private-client-key", 0, booking.Interval{Start: start, End: start.Add(30 * time.Minute)}, ref)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	flags := map[string]memprovider.InMemoryFlag{}
	for _, name := range append(acceptanceFlagNames(), "portal-invoice-billing", "portal-paid-appointments") {
		flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	provider := &finalRequestProvider{
		FeatureProvider: memprovider.NewInMemoryProvider(flags),
		key:             "portal-paid-appointments", blockAt: 2, blocked: make(chan struct{}), release: make(chan struct{}),
	}
	if err := b.flags.SetProviderAndWait(ctx, provider); err != nil {
		t.Fatal(err)
	}
	var deadline time.Time
	if err := migrationDB(t).QueryRow(ctx, `UPDATE portal_appointment_revisions
 SET held_until=clock_timestamp()+interval '2 seconds' WHERE reservation_id=$1 RETURNING held_until`, selection.Reservation).
		Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	_, hash := b.svc.termsFor("en")
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := b.svc.placeOrder(ctx, principal, proposalRequestID, acceptance, "invoice", true, false, nil, mailContext{Lang: "en", TermsHash: hash}, selection)
		result <- err
	}()
	defer func() {
		cancel()
		<-done
	}()
	select {
	case <-provider.blocked:
	case err := <-result:
		t.Fatalf("order never reached its final flag evaluation: %v", err)
	case <-ctx.Done():
		t.Fatal("order did not reach the final flag evaluation")
	}
	var active bool
	if err := b.pool.QueryRow(ctx, `SELECT $1::timestamptz>clock_timestamp()`, deadline).Scan(&active); err != nil || !active {
		t.Fatalf("hold was already expired before pausing the final flag: %v", err)
	}
	if _, err := b.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM $1::timestamptz-clock_timestamp())))`, deadline); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	select {
	case err := <-result:
		if !errors.Is(err, errProposalConflict) {
			t.Fatalf("expired hold was not rejected at commit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("order did not finish after its held slot expired")
	}
	var stored int
	if err := b.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM portal_orders WHERE request_id=$1) +
 (SELECT count(*) FROM portal_mail_outbox WHERE kind='order-confirmation')`, proposalRequestID).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("failed order left an order or confirmation in storage: %v", err)
	}
}

func TestPaidAppointmentOrderKeepsItsSelectedSlot(t *testing.T) {
	for _, tt := range []struct{ lang, label string }{
		{"en", "Service: Consultancy"},
		{"da", "Ydelse: Rådgivning"},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			b := acceptedAppointmentQuoteFixture(t)
			p := authenticateExistingMember(t, b, "consent-client")
			ctx := t.Context()
			const client = "private-client-key"
			seedAppointmentWorkspaces(t, b.pool, client)
			ref := appointmentQuoteRef{Request: proposalRequestID}
			var acceptance, termsText string
			if err := b.pool.QueryRow(ctx, `SELECT a.id,p.version,p.content_sha256,CASE WHEN $2='da'
 THEN p.appointment_terms_da ELSE p.appointment_terms_en END FROM portal_proposal_acceptances a
 JOIN portal_proposals p USING (request_id,client_id) WHERE a.request_id=$1 AND p.version=a.proposal_version`,
				proposalRequestID, tt.lang).Scan(&acceptance, &ref.Version, &ref.Hash, &termsText); err != nil {
				t.Fatal(err)
			}
			start, policy := appointmentHoldFixture(t)
			reservation := secret()
			hold := func(start time.Time, prior int) {
				t.Helper()
				err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
					_, err := policy.append(ctx, tx, reservation, client, prior, booking.Interval{Start: start, End: start.Add(30 * time.Minute)}, ref)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			hold(start, 0)
			selectedStart := start.Add(time.Hour)
			hold(selectedStart, 1)
			_, hash := b.svc.termsFor(tt.lang)
			place := func(slot appointmentOrderSelection) (projectOrder, error) {
				return b.svc.placeOrder(ctx, p, proposalRequestID, acceptance, "invoice", true, false, nil, mailContext{Lang: tt.lang, TermsHash: hash}, slot)
			}
			if _, err := place(appointmentOrderSelection{}); !errors.Is(err, ErrDenied) {
				t.Fatalf("generic checkout placed an appointment order: %v", err)
			}
			if _, err := place(appointmentOrderSelection{Reservation: reservation, Revision: 1}); !errors.Is(err, errProposalConflict) {
				t.Fatalf("superseded hold was accepted: %v", err)
			}
			slot := appointmentOrderSelection{Reservation: reservation, Revision: 2}
			first, err := place(slot)
			if err != nil || first.appointment != slot || first.MonthlyMinor != 0 {
				t.Fatalf("exact held appointment could not be ordered: %v", err)
			}
			if err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
				moved := selectedStart.Add(time.Hour)
				_, err := policy.append(ctx, tx, reservation, client, slot.Revision, booking.Interval{Start: moved, End: moved.Add(30 * time.Minute)}, ref)
				return err
			}); !errors.Is(err, errProposalConflict) {
				t.Fatalf("an order did not preserve its held revision: %v", err)
			}
			retry, err := place(slot)
			if err != nil || retry.ID != first.ID {
				t.Fatalf("identical retry did not keep the original order: %v", err)
			}
			var body string
			if err := b.pool.QueryRow(ctx, `SELECT body FROM portal_mail_outbox WHERE order_id=$1 AND producer_format=3`, first.ID).Scan(&body); err != nil {
				t.Fatalf("appointment confirmation was not queued: %v", err)
			}
			for _, want := range []string{tt.label, termsText, selectedStart.Format(time.RFC3339), selectedStart.Add(30 * time.Minute).Format(time.RFC3339)} {
				if !strings.Contains(body, want) {
					t.Fatalf("appointment confirmation left out %q", want)
				}
			}
			prepared, err := readOriginalConfirmation(ctx, b.pool, proposalRequestID, client, acceptance, first.ID)
			if err != nil || prepared.State != confirmationPrepared {
				t.Fatalf("appointment producer did not supply exact agreement evidence: %v", err)
			}
			confirm := func() (appointmentHold, error) {
				return b.svc.confirmPaidAppointment(ctx, p, proposalRequestID, slot, policy)
			}
			if _, err := confirm(); !errors.Is(err, errProposalConflict) {
				t.Fatalf("unfunded appointment was confirmed: %v", err)
			}
			provider, billing := newFakeNumina(t)
			paidInvoiceOrder(t, b, provider, billing)
			if _, err := confirm(); !errors.Is(err, errProposalConflict) {
				t.Fatalf("prepared mail was used as sender acceptance: %v", err)
			}
			if err := b.svc.SendQueuedMail(ctx); err != nil {
				t.Fatal(err)
			}
			sent, err := readOriginalConfirmation(ctx, b.pool, proposalRequestID, client, acceptance, first.ID)
			if err != nil || sent.State != confirmationSenderAccepted || sent.BodySHA256 != prepared.BodySHA256 {
				t.Fatalf("sender acceptance did not retain the same appointment confirmation: %v", err)
			}
			for _, alreadyConfirmed := range []bool{false, true} {
				tx := beginFixtureTransaction(ctx, t, b.pool)
				hold := appointmentHold{PaidConfirmed: alreadyConfirmed}
				if err := tx.QueryRow(ctx, `SELECT clock_timestamp() - interval '1 second'`).Scan(&hold.HeldUntil); err != nil {
					t.Fatal(err)
				}
				err := b.svc.commitPaidAppointment(ctx, p, tx, first.Collection, hold)
				if !alreadyConfirmed && !errors.Is(err, errProposalConflict) {
					t.Fatalf("an expired first confirmation could still commit: %v", err)
				}
				if alreadyConfirmed && err != nil {
					t.Fatalf("an expired hold blocked an already confirmed retry: %v", err)
				}
				rollbackProposalWrite(ctx, tx)
			}
			calendar := policy.provider
			policy.provider = nil
			if _, err := confirm(); !errors.Is(err, booking.ErrUnavailable) {
				t.Fatalf("confirmation skipped the final calendar read: %v", err)
			}
			policy.provider = calendar
			saved, err := confirm()
			if err != nil || !saved.PaidConfirmed || !saved.Slot.Start.Equal(selectedStart) {
				t.Fatalf("settled appointment could not be confirmed: %v", err)
			}
			if _, err := confirm(); err != nil {
				t.Fatalf("exact confirmation retry failed: %v", err)
			}
			err = pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
				commitments, err := readAppointmentCommitments(ctx, tx, secret(), saved.HeldUntil.Add(time.Second), saved.Slot)
				if err != nil || len(commitments) != 1 || !commitments[0].Start.Equal(selectedStart) {
					t.Fatalf("confirmed appointment stopped counting when its hold expired: %v", err)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			changeFixtureFunding(t, "refunded")
			if _, err := confirm(); !errors.Is(err, errProposalConflict) {
				t.Fatalf("refunded payment was still accepted: %v", err)
			}
			requireAppointmentClientErasure(t, client)
		})
	}
}
