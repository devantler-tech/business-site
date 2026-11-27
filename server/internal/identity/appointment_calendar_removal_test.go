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
			provider := calendarCreationFixture{remove: remove}
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
