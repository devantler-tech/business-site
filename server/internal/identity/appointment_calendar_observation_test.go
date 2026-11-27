package identity

import (
	"context"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/devantler-tech/business-site/server/internal/booking/caldav"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCalendarCreationRecoversAfterProviderAccepts(t *testing.T) {
	pool := fixtureDB(t)
	reservation, _ := confirmedCalendarIntroduction(t, pool)
	interrupted, stop := context.WithCancel(t.Context())
	defer stop()
	const tag = `"provider-version"`
	var accepted caldav.OwnedEvent
	calls := 0
	create := func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
		calls++
		if accepted.Key == "" {
			accepted = event
			stop()
		} else if event.Key != accepted.Key || event.Revision != accepted.Revision ||
			event.CalendarBinding != accepted.CalendarBinding || !event.Slot.Start.Equal(accepted.Slot.Start) || !event.Slot.End.Equal(accepted.Slot.End) {
			t.Fatal("recovery selected a different calendar resource or revision")
		}
		return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
	}
	if err := reconcileIntroCalendarCreation(interrupted, pool, reservation, create); err == nil || accepted.Key == "" {
		t.Fatalf("the interrupted run did not leave a recoverable calendar write: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(t.Context(), pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	for range 2 {
		if err := reconcileIntroCalendarCreation(t.Context(), fresh, reservation, create); err != nil {
			t.Fatal(err)
		}
	}
	var retained string
	err = fresh.QueryRow(t.Context(), `SELECT provider_tag FROM portal_appointment_calendar_observations
 WHERE reservation_id=$1 AND outcome='created'`, reservation).Scan(&retained)
	if err != nil || retained != tag || calls != 2 {
		t.Fatalf("recovery did not retain the provider version and stop further work: calls=%d error=%v", calls, err)
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
		if err := reconcileIntroCalendarCreation(t.Context(), pool, reservation, create); err != nil {
			t.Fatal(err)
		}
	}
	var observed bool
	err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_appointment_calendar_observations WHERE reservation_id=$1)`,
		reservation).Scan(&observed)
	if err != nil || observed || calls != 1 {
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
		if err := reconcileIntroCalendarCreation(t.Context(), pool, reservation, create); err != nil {
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
