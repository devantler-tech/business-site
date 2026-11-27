package identity

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/devantler-tech/business-site/server/internal/booking/caldav"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type calendarCreationFixture struct {
	create, read func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error)
	remove       func(context.Context, caldav.OwnedEvent, string) error
}

func (f calendarCreationFixture) CreateOwnedEvent(ctx context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
	return f.create(ctx, event)
}

func (f calendarCreationFixture) ReadOwnedEvent(ctx context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
	if f.read == nil {
		return caldav.OwnedEventObservation{}, booking.ErrUnavailable
	}
	return f.read(ctx, event)
}

func (f calendarCreationFixture) DeleteOwnedEvent(ctx context.Context, event caldav.OwnedEvent, tag string) error {
	if f.remove == nil {
		return booking.ErrUnavailable
	}
	return f.remove(ctx, event, tag)
}

func TestCalendarCreationSerializesProviderWritesForAReservation(t *testing.T) {
	pool := fixtureDB(t)
	reservation, _ := confirmedCalendarIntroduction(t, pool)
	cfg := pool.Config()
	cfg.MaxConns = 1
	oneConnection, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer oneConnection.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	entered, resume := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	const tag = `"serialized-version"`
	create := func(ctx context.Context, _ caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
		if attempts.Add(1) == 1 {
			close(entered)
			select {
			case <-resume:
			case <-ctx.Done():
				return caldav.OwnedEventObservation{}, ctx.Err()
			}
		}
		return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
	}
	results := make(chan error, 2)
	start := func(database *pgxpool.Pool) {
		workers.Go(func() {
			results <- reconcileAppointmentCalendar(ctx, database, reservation, calendarCreationFixture{create: create})
		})
	}
	start(oneConnection)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first worker did not reach the provider using one connection")
	}
	start(pool)
	waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND objid=hashtext($1)::oid AND objsubid=2 AND NOT granted)`, reservation)
	close(resume)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("calendar worker did not reconcile cleanly", err)
		}
	}
	var observedTag string
	err = pool.QueryRow(ctx, `SELECT provider_tag FROM portal_appointment_calendar_observations
 WHERE reservation_id=$1 AND outcome='created'`, reservation).Scan(&observedTag)
	if err != nil || attempts.Load() != 1 || observedTag != tag {
		t.Fatalf("concurrent reconciliation repeated or lost the provider write: attempts=%d error=%v", attempts.Load(), err)
	}
}

func TestCalendarCreationRecoversAfterProviderAccepts(t *testing.T) {
	pool := fixtureDB(t)
	reservation, _ := confirmedCalendarIntroduction(t, pool)
	interrupted, stop := context.WithCancel(t.Context())
	defer stop()
	const tag = `"provider-version"`
	var accepted caldav.OwnedEvent
	writes, reads := 0, 0
	provider := calendarCreationFixture{
		create: func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			writes++
			accepted = event
			stop()
			return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
		},
		read: func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			reads++
			if event.Key != accepted.Key || event.Revision != accepted.Revision ||
				event.CalendarBinding != accepted.CalendarBinding || !event.Slot.Start.Equal(accepted.Slot.Start) || !event.Slot.End.Equal(accepted.Slot.End) {
				t.Fatal("recovery selected a different calendar resource or revision")
			}
			return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
		},
	}
	if err := reconcileAppointmentCalendar(interrupted, pool, reservation, provider); err == nil || accepted.Key == "" {
		t.Fatalf("the interrupted run did not leave a recoverable calendar write: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(t.Context(), pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	for range 2 {
		if err := reconcileAppointmentCalendar(t.Context(), fresh, reservation, provider); err != nil {
			t.Fatal(err)
		}
	}
	var retained string
	err = fresh.QueryRow(t.Context(), `SELECT provider_tag FROM portal_appointment_calendar_observations
 WHERE reservation_id=$1 AND outcome='created'`, reservation).Scan(&retained)
	if err != nil || retained != tag || writes != 1 || reads != 1 {
		t.Fatalf("recovery did not retain the provider version: writes=%d reads=%d error=%v", writes, reads, err)
	}
}

func TestCalendarCreationDoesNotRepeatAnUnconfirmedWrite(t *testing.T) {
	pool := fixtureDB(t)
	reservation, _ := confirmedCalendarIntroduction(t, pool)
	writes, reads := 0, 0
	visible := false
	const tag = `"late-provider-version"`
	provider := calendarCreationFixture{
		create: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			writes++
			return caldav.OwnedEventObservation{}, booking.ErrUnavailable
		},
		read: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			reads++
			if visible {
				return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
			}
			return caldav.OwnedEventObservation{}, nil
		},
	}
	if err := reconcileAppointmentCalendar(t.Context(), pool, reservation, provider); err != booking.ErrUnavailable {
		t.Fatalf("the provider did not confirm creation: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(t.Context(), pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := reconcileAppointmentCalendar(t.Context(), fresh, reservation, provider); err != booking.ErrUnavailable {
		t.Fatalf("an unconfirmed write should stay unresolved: %v", err)
	}
	var unresolved bool
	err = fresh.QueryRow(t.Context(), `SELECT EXISTS(
 SELECT 1 FROM portal_appointment_calendar_attempts WHERE reservation_id=$1)
 AND NOT EXISTS(SELECT 1 FROM portal_appointment_calendar_observations WHERE reservation_id=$1)`, reservation).Scan(&unresolved)
	if err != nil || !unresolved || writes != 1 || reads != 1 {
		t.Fatalf("provider absence must leave one unresolved attempt: writes=%d reads=%d error=%v", writes, reads, err)
	}
	visible = true
	if err := reconcileAppointmentCalendar(t.Context(), fresh, reservation, provider); err != nil {
		t.Fatal("recover the late provider version", err)
	}
	var retained string
	err = fresh.QueryRow(t.Context(), `SELECT provider_tag FROM portal_appointment_calendar_observations
 WHERE reservation_id=$1 AND outcome='created'`, reservation).Scan(&retained)
	if err != nil || retained != tag || writes != 1 || reads != 2 {
		t.Fatalf("recovery must observe the late version by reading: writes=%d reads=%d error=%v", writes, reads, err)
	}
}

func TestCalendarCreationDiscardsObservationAfterCancellation(t *testing.T) {
	pool := fixtureDB(t)
	reservation, client := confirmedCalendarIntroduction(t, pool)
	calls := 0
	create := func(ctx context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
		calls++
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := cancelIntroduction(ctx, tx, reservation, client, event.Revision)
			return err
		})
		return caldav.OwnedEventObservation{Exists: true, ETag: `"stale-version"`}, err
	}
	for range 2 {
		if err := reconcileAppointmentCalendar(t.Context(), pool, reservation, calendarCreationFixture{create: create}); err != nil {
			t.Fatal(err)
		}
	}
	var observed, attempted bool
	err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_appointment_calendar_observations WHERE reservation_id=$1),
 EXISTS(SELECT 1 FROM portal_appointment_calendar_attempts WHERE reservation_id=$1)`, reservation).Scan(&observed, &attempted)
	if err != nil || observed || !attempted || calls != 1 {
		t.Fatalf("a cancelled appointment retained an observation or retried creation: calls=%d error=%v", calls, err)
	}
	// Cancellation keeps the immutable identity for later owned-event cleanup.
	requireCalendarCreation(t, pool, reservation, 1)
}

func TestCalendarCreationHoldsVerifiedProviderConflict(t *testing.T) {
	pool := fixtureDB(t)
	reservation, _ := confirmedCalendarIntroduction(t, pool)
	calls := 0
	create := func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
		calls++
		return caldav.OwnedEventObservation{}, caldav.ErrOwnedEventConflict
	}
	for range 2 {
		if err := reconcileAppointmentCalendar(t.Context(), pool, reservation, calendarCreationFixture{create: create}); err != nil {
			t.Fatal(err)
		}
	}
	var conflict bool
	err := pool.QueryRow(t.Context(), `SELECT outcome='conflict' AND provider_tag IS NULL
 FROM portal_appointment_calendar_observations WHERE reservation_id=$1`, reservation).Scan(&conflict)
	if err != nil || !conflict || calls != 1 {
		t.Fatalf("a provider conflict did not hold creation for operator review: calls=%d error=%v", calls, err)
	}
}

func TestPaidCalendarCreationFollowsCommittedFunding(t *testing.T) {
	for _, scenario := range []struct {
		name                         string
		reverseBefore, reverseDuring bool
		wantCalls                    int
		wantObservation              bool
	}{
		{name: "settled", wantCalls: 1, wantObservation: true},
		{name: "reversed before creation", reverseBefore: true},
		{name: "reversed during creation", reverseDuring: true, wantCalls: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			b, reservation, slot := confirmedCalendarConsultation(t)
			key := requireCalendarCreation(t, b.pool, reservation, 1)
			if scenario.reverseBefore {
				changeFixtureFunding(t, "refunded")
			}
			const tag = `"consultation-version"`
			calls := 0
			create := func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
				calls++
				if event.Key != key || event.CalendarBinding != fixtureCalendarDestination || event.Revision != 1 ||
					!event.Slot.Start.Equal(slot.Start) || !event.Slot.End.Equal(slot.End) {
					t.Fatal("calendar operation used a different resource or consultation slot")
				}
				if scenario.reverseDuring {
					changeFixtureFunding(t, "refunded")
				}
				return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
			}
			for range 2 {
				if err := reconcileAppointmentCalendar(t.Context(), b.pool, reservation, calendarCreationFixture{create: create}); err != nil {
					t.Fatal(err)
				}
			}
			var observations int
			var retainedTag string
			err := b.pool.QueryRow(t.Context(), `SELECT count(*), COALESCE(max(provider_tag),'')
 FROM portal_appointment_calendar_observations WHERE reservation_id=$1`, reservation).Scan(&observations, &retainedTag)
			if err != nil || calls != scenario.wantCalls {
				t.Fatalf("unexpected provider calls: %d error=%v", calls, err)
			}
			if scenario.wantObservation && (observations != 1 || retainedTag != tag) ||
				!scenario.wantObservation && (observations != 0 || retainedTag != "") {
				t.Fatal("calendar reconciliation retained the wrong observation")
			}
			if requireCalendarCreation(t, b.pool, reservation, 1) != key {
				t.Fatal("funding changes altered the reserved calendar identity")
			}
		})
	}
}

func confirmedCalendarConsultation(t *testing.T) (*browserFixture, string, booking.Interval) {
	t.Helper()
	b := acceptedAppointmentQuoteFixture(t)
	ctx := t.Context()
	principal := authenticateExistingMember(t, b, "consent-client")
	const client = "private-client-key"
	seedAppointmentWorkspaces(t, b.pool, client)
	ref := appointmentQuoteRef{Request: proposalRequestID}
	var acceptance string
	if err := b.pool.QueryRow(ctx, `SELECT id,proposal_version,content_hash FROM portal_proposal_acceptances
 WHERE request_id=$1 AND client_id=$2`, ref.Request, client).Scan(&acceptance, &ref.Version, &ref.Hash); err != nil {
		t.Fatal(err)
	}
	start, policy := appointmentHoldFixture(t)
	policy.calendarBinding = fixtureCalendarDestination
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	reservation := secret()
	if err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		_, err := policy.append(ctx, tx, reservation, client, 0, slot, ref)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	selection := appointmentOrderSelection{Reservation: reservation, Revision: 1}
	_, terms := b.svc.termsFor("en")
	if _, err := b.svc.placeOrder(ctx, principal, ref.Request, acceptance, "invoice", true, false, nil,
		mailContext{Lang: "en", TermsHash: terms}, selection); err != nil {
		t.Fatal(err)
	}
	provider, billing := newFakeNumina(t)
	paidInvoiceOrder(t, b, provider, billing)
	if err := b.svc.SendQueuedMail(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.confirmPaidAppointment(ctx, principal, ref.Request, selection, policy); err != nil {
		t.Fatal(err)
	}
	return b, reservation, slot
}

func confirmedCalendarIntroduction(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	const client = "fictional-calendar-reconciliation-client"
	seedAppointmentWorkspaces(t, pool, client)
	start, policy := appointmentHoldFixture(t)
	policy.calendarBinding = fixtureCalendarDestination
	reservation := secret()
	slot := booking.Interval{Start: start, End: start.Add(introductionDuration)}
	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := policy.append(t.Context(), tx, reservation, client, 0, slot, appointmentQuoteRef{}); err != nil {
			return err
		}
		_, err := policy.confirmIntroduction(t.Context(), tx, reservation, client, 1)
		return err
	})
	if err != nil {
		t.Fatalf("commit the fictional introduction: %v", err)
	}
	return reservation, client
}
