package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAppointmentIntroductionCommitsOneFreshHoldPerClient(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	const client, other = "fictional-intro-client", "fictional-other-client"
	if _, err := pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1),($2)`, client, other); err != nil {
		t.Fatal(err)
	}
	start, policy := appointmentHoldFixture(t)
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	next := booking.Interval{Start: start.Add(2 * time.Hour), End: start.Add(150 * time.Minute)}
	hold := func(id, who string, expected int, slot booking.Interval) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := policy.append(ctx, tx, id, who, expected, slot, appointmentQuoteRef{})
			return err
		})
	}
	confirm := func(id, who string, revision int) (appointmentHold, error) {
		var saved appointmentHold
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			saved, err = policy.confirmIntroduction(ctx, tx, id, who, revision)
			return err
		})
		return saved, err
	}

	id := secret()
	if err := hold(id, client, 0, slot); err != nil {
		t.Fatal("eligible introduction could not be held", err)
	}
	for _, tc := range []struct {
		client   string
		revision int
	}{{other, 1}, {client, 2}} {
		if _, err := confirm(id, tc.client, tc.revision); !errors.Is(err, errProposalConflict) {
			t.Fatalf("a different client or revision confirmed the hold: %v", err)
		}
	}
	available := policy.provider
	policy.provider = appointmentBusyFunc(func(_ context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
		return booking.BusySnapshot{ObservedAt: time.Now(), Coverage: coverage, Complete: true, Busy: []booking.Interval{slot}}, nil
	})
	if _, err := confirm(id, client, 1); !errors.Is(err, errProposalConflict) {
		t.Fatalf("a newly occupied calendar slot was committed: %v", err)
	}
	policy.provider = available
	saved, err := confirm(id, client, 1)
	if err != nil || !saved.IntroductionConfirmed || saved.Revision != 1 || !saved.Slot.Start.Equal(slot.Start) {
		t.Fatal("the current free introduction was not committed", err)
	}
	if err := hold(id, client, 1, next); !errors.Is(err, errProposalConflict) {
		t.Fatalf("a provisional reschedule changed the committed introduction: %v", err)
	}

	migrator := migrationDB(t)
	if _, err := migrator.Exec(ctx, `UPDATE portal_appointment_revisions
 SET recorded_at=clock_timestamp()-interval '2 minutes',held_until=clock_timestamp()-interval '1 minute'
 WHERE reservation_id=$1`, id); err != nil {
		t.Fatal("prepare the fixture's elapsed hold", err)
	}
	policy.provider = nil
	retry, err := confirm(id, client, 1)
	if err != nil || !retry.IntroductionConfirmed || retry.Revision != saved.Revision || !retry.Slot.Start.Equal(saved.Slot.Start) {
		t.Fatal("retry lost the committed introduction after hold expiry", err)
	}

	policy.provider = available
	policy.policy.WeeklyCapacity = 30 * time.Minute
	if err := hold(secret(), other, 0, next); !errors.Is(err, errProposalConflict) {
		t.Fatalf("the committed introduction no longer counted toward weekly capacity: %v", err)
	}
	policy.policy.WeeklyCapacity = 150 * time.Minute
	second := secret()
	if err := hold(second, client, 0, next); err != nil {
		t.Fatal("prepare the second provisional reservation", err)
	}
	if _, err := confirm(second, client, 1); !errors.Is(err, errProposalConflict) {
		t.Fatalf("a second reservation consumed the same client's introduction: %v", err)
	}
	var failure *pgconn.PgError
	_, err = pool.Exec(ctx, `INSERT INTO portal_appointment_introductions(client_id,reservation_id,revision) VALUES($1,$2,1)`, client, second)
	if !errors.As(err, &failure) || failure.Code != "23505" {
		t.Fatalf("PostgreSQL admitted multiple introductions for the same client: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM portal_appointment_introductions WHERE client_id=$1`, client)
	if !errors.As(err, &failure) || failure.Code != "42501" {
		t.Fatalf("the runtime could remove the confirmed introduction: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_appointment_introductions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("the confirmation ledger has %d rows, err=%v", count, err)
	}
}
