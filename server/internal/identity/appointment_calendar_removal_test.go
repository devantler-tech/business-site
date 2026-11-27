package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/devantler-tech/business-site/server/internal/booking/caldav"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCalendarRemovalRecoversAnUnconfirmedCancelledIntroduction(t *testing.T) {
	ctx := t.Context()
	pool := fixtureDB(t)
	reservation, client := confirmedCalendarIntroduction(t, pool)
	const tag = `"late-provider-version"`
	var original caldav.OwnedEvent
	creates, reads, removals := 0, 0, 0
	visible := false
	provider := calendarCreationFixture{
		create: func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			creates++
			original = event
			return caldav.OwnedEventObservation{}, booking.ErrUnavailable
		},
		read: func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			reads++
			if event != original {
				t.Fatal("recovery read a different calendar event")
			}
			if !visible {
				return caldav.OwnedEventObservation{}, nil
			}
			return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
		},
		remove: func(_ context.Context, event caldav.OwnedEvent, expected string) error {
			removals++
			if event != original || expected != tag {
				t.Fatal("recovery removed a different event or ignored the observed validator")
			}
			return nil
		},
	}
	cancelUnconfirmedCalendarCreationFixture(t, pool, reservation, client, provider)

	restarted, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := reconcileAppointmentCalendar(ctx, restarted, reservation, provider); err != nil {
		t.Fatal("reconcile a temporary absence", err)
	}
	if creates != 1 || reads != 1 || removals != 0 {
		t.Fatalf("an absent event must stay pending: creates=%d reads=%d removals=%d", creates, reads, removals)
	}
	var observed bool
	err = restarted.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_appointment_calendar_observations WHERE reservation_id=$1)`, reservation).Scan(&observed)
	if err != nil || observed {
		t.Fatalf("temporary absence created an observation: %v", err)
	}

	visible = true
	for range 2 {
		if err := reconcileAppointmentCalendar(ctx, restarted, reservation, provider); err != nil {
			t.Fatal("reconcile the late calendar event", err)
		}
	}
	var outcome string
	err = restarted.QueryRow(ctx, `SELECT outcome FROM portal_appointment_calendar_removals WHERE reservation_id=$1`, reservation).Scan(&outcome)
	if err != nil || outcome != "removed" || creates != 1 || reads != 2 || removals != 1 {
		t.Fatalf("the late event must be removed once: outcome=%q creates=%d reads=%d removals=%d: %v", outcome, creates, reads, removals, err)
	}
}

func TestCalendarRemovalRecordsConflictFromAnUnconfirmedCreation(t *testing.T) {
	ctx := t.Context()
	pool := fixtureDB(t)
	reservation, client := confirmedCalendarIntroduction(t, pool)
	reads := 0
	provider := calendarCreationFixture{
		create: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			return caldav.OwnedEventObservation{}, booking.ErrUnavailable
		},
		read: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
			reads++
			return caldav.OwnedEventObservation{}, caldav.ErrOwnedEventConflict
		},
		remove: func(context.Context, caldav.OwnedEvent, string) error {
			t.Fatal("a conflicting event was sent to the provider for removal")
			return nil
		},
	}
	cancelUnconfirmedCalendarCreationFixture(t, pool, reservation, client, provider)
	for range 2 {
		if err := reconcileAppointmentCalendar(ctx, pool, reservation, provider); err != nil {
			t.Fatal(err)
		}
	}
	var outcome string
	err := pool.QueryRow(ctx, `SELECT outcome FROM portal_appointment_calendar_observations WHERE reservation_id=$1`, reservation).Scan(&outcome)
	if err != nil || outcome != "conflict" || reads != 1 {
		t.Fatalf("the conflict should be recorded once: outcome=%q reads=%d: %v", outcome, reads, err)
	}
}

func cancelUnconfirmedCalendarCreationFixture(t *testing.T, pool *pgxpool.Pool, reservation, client string, provider calendarCreationFixture) {
	t.Helper()
	if err := reconcileAppointmentCalendar(t.Context(), pool, reservation, provider); !errors.Is(err, booking.ErrUnavailable) {
		t.Fatalf("the first creation should be unconfirmed: %v", err)
	}
	cancelCalendarRemovalFixture(t, pool, reservation, client)
}

func TestCalendarRemovalFollowsAnObservedCancelledIntroduction(t *testing.T) {
	for _, test := range []struct {
		name       string
		created    bool
		cancelled  bool
		firstError error
		wantCalls  int
		outcome    string
	}{
		{name: "active event", created: true},
		{name: "unknown creation", cancelled: true},
		{name: "verified absence", created: true, cancelled: true, wantCalls: 1, outcome: "removed"},
		{name: "edited event", created: true, cancelled: true, firstError: caldav.ErrOwnedEventConflict, wantCalls: 1, outcome: "conflict"},
		{name: "temporary outage", created: true, cancelled: true, firstError: booking.ErrUnavailable, wantCalls: 2, outcome: "removed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			pool := fixtureDB(t)
			reservation, client := confirmedCalendarIntroduction(t, pool)
			const tag = `"saved-version"`
			var created caldav.OwnedEvent
			create := calendarCreationFixture{create: func(_ context.Context, event caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
				if !test.created {
					return caldav.OwnedEventObservation{}, booking.ErrUnavailable
				}
				created = event
				return caldav.OwnedEventObservation{Exists: true, ETag: tag}, nil
			}}
			err := reconcileAppointmentCalendar(ctx, pool, reservation, create)
			if test.created && err != nil || !test.created && !errors.Is(err, booking.ErrUnavailable) {
				t.Fatalf("creation fixture failed: %v", err)
			}
			if test.cancelled {
				cancelCalendarRemovalFixture(t, pool, reservation, client)
			}

			calls := 0
			remove := func(_ context.Context, event caldav.OwnedEvent, expected string) error {
				if event != created || expected != tag {
					t.Fatal("removal selected a different calendar event or validator")
				}
				calls++
				if calls == 1 {
					return test.firstError
				}
				return nil
			}
			provider := calendarCreationFixture{
				remove: remove,
				read: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
					return caldav.OwnedEventObservation{}, nil
				},
			}
			for pass := range 2 {
				err := reconcileAppointmentCalendar(ctx, pool, reservation, provider)
				if pass == 0 && test.firstError == booking.ErrUnavailable {
					if !errors.Is(err, booking.ErrUnavailable) {
						t.Fatal("the provider outage did not leave cleanup pending")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if calls != test.wantCalls {
				t.Fatalf("provider was called %d times, want %d", calls, test.wantCalls)
			}
			var outcome string
			err = pool.QueryRow(ctx, `SELECT COALESCE(
 (SELECT outcome FROM portal_appointment_calendar_removals WHERE reservation_id=$1),'')`, reservation).Scan(&outcome)
			if err != nil || outcome != test.outcome {
				t.Fatalf("recorded outcome %q, want %q: %v", outcome, test.outcome, err)
			}
		})
	}
}

func TestCalendarRemovalSerializesWithAnotherWorker(t *testing.T) {
	pool := fixtureDB(t)
	reservation, client := confirmedCalendarIntroduction(t, pool)
	provider := calendarCreationFixture{create: func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error) {
		return caldav.OwnedEventObservation{Exists: true, ETag: `"verified-version"`}, nil
	}}
	if err := reconcileAppointmentCalendar(t.Context(), pool, reservation, provider); err != nil {
		t.Fatal(err)
	}
	cancelCalendarRemovalFixture(t, pool, reservation, client)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	entered := make(chan struct{}, 2)
	unblock := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	provider.remove = func(ctx context.Context, _ caldav.OwnedEvent, _ string) error {
		entered <- struct{}{}
		select {
		case <-unblock:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	run := func() { results <- reconcileAppointmentCalendar(ctx, pool, reservation, provider) }
	workers.Go(run)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the first removal did not reach the provider")
	}
	workers.Go(run)
	waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND objid=hashtext($1)::oid AND objsubid=2 AND NOT granted)`, reservation)
	close(unblock)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(entered) != 0 {
		t.Fatal("the waiting worker repeated a verified removal")
	}
}

func cancelCalendarRemovalFixture(t *testing.T, pool *pgxpool.Pool, reservation, client string) {
	t.Helper()
	ctx := t.Context()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := cancelIntroduction(ctx, tx, reservation, client, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
