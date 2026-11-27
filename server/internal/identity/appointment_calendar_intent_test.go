package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fixtureCalendarDestination = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestAppointmentCalendarIntentCommitsWithConfirmation(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	const client = "fictional-calendar-intent-client"
	seedAppointmentWorkspaces(t, pool, client)
	start, holds := appointmentHoldFixture(t)
	reservation := secret()
	slot := booking.Interval{Start: start, End: start.Add(introductionDuration)}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := holds.append(ctx, tx, reservation, client, 0, slot, appointmentQuoteRef{})
		return err
	}); err != nil {
		t.Fatal("create the fixture hold", err)
	}

	var denied *pgconn.PgError
	_, err := pool.Exec(ctx, `INSERT INTO portal_appointment_calendar_intents
 (reservation_id,revision,calendar_binding,event_key) VALUES($1,1,$2,$3)`,
		reservation, fixtureCalendarDestination, strings.Repeat("b", 64))
	if !errors.As(err, &denied) || denied.Code != "23514" {
		t.Fatalf("an unconfirmed hold could queue a calendar create: %v", err)
	}

	confirm := func() error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := holds.confirmIntroduction(ctx, tx, reservation, client, 1)
			return err
		})
	}
	holds.calendarBinding = "invalid-fixture-binding"
	if err := confirm(); err == nil {
		t.Fatal("booking confirmation committed even though the intent could not be recorded")
	}
	var confirmed bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_appointment_introductions WHERE reservation_id=$1)`,
		reservation).Scan(&confirmed); err != nil || confirmed {
		t.Fatalf("the failed intent left a confirmed appointment: %v", err)
	}

	holds.calendarBinding = fixtureCalendarDestination
	if err := confirm(); err != nil {
		t.Fatal("confirmation and intent could not commit together", err)
	}
	key := requireCalendarCreation(t, pool, reservation, 1)

	reopened, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	holds.calendarBinding = strings.Repeat("c", 64)
	holds.provider = nil
	if err := confirm(); err != nil {
		t.Fatal("the committed confirmation could not be replayed", err)
	}
	if requireCalendarCreation(t, reopened, reservation, 1) != key {
		t.Fatal("a fresh connection or retried confirmation changed the event key")
	}
	if _, err := reopened.Exec(ctx, `UPDATE portal_appointment_calendar_intents SET calendar_binding=$2 WHERE reservation_id=$1`,
		reservation, holds.calendarBinding); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("the runtime could alter a recorded destination: %v", err)
	}
}

func requireCalendarCreation(t *testing.T, pool *pgxpool.Pool, reservation string, revision int) string {
	t.Helper()
	var storedRevision int
	var binding, key string
	err := pool.QueryRow(t.Context(), `SELECT revision,calendar_binding,event_key FROM portal_appointment_calendar_intents WHERE reservation_id=$1`,
		reservation).Scan(&storedRevision, &binding, &key)
	if err != nil || storedRevision != revision || binding != fixtureCalendarDestination || len(key) != 64 || key == binding {
		t.Fatalf("the recorded calendar creation lost its identity or scope: %v", err)
	}
	return key
}

func requireNoCalendarCreation(t *testing.T, pool *pgxpool.Pool, reservation string) {
	t.Helper()
	var pending bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_appointment_calendar_intents WHERE reservation_id=$1)`,
		reservation).Scan(&pending); err != nil || pending {
		t.Fatalf("an appointment unexpectedly had a calendar intent: %v", err)
	}
}
