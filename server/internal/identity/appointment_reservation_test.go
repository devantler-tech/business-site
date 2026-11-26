package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const insertAppointmentRevision = `INSERT INTO portal_appointment_revisions
 (reservation_id, client_id, revision, starts_at, ends_at, held_until)
 VALUES ($1,$2,$3,$4,$5,$6)`

func TestAppointmentReservationsKeepClientBoundRevisions(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	root := secret()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('fictional-booking-client'),('fictional-other-client')`); err != nil {
		t.Fatal(err)
	}
	migrator := migrationDB(t)
	if _, err := migrator.Exec(ctx, `DROP TABLE portal_appointment_revisions,portal_appointment_reservations;
 DELETE FROM portal_schema_version WHERE version=34`); err != nil {
		t.Fatal("prepare the previous fixture schema", err)
	}
	if err := MigrateWithRuntimeRole(ctx, migrator, "portal_runtime"); err != nil {
		t.Fatal("apply the reservation migration", err)
	}
	if err := CheckRuntimeRole(ctx, pool); err != nil {
		t.Fatal("verify the migrated runtime grants", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portal_appointment_reservations(id,client_id) VALUES($1,'fictional-booking-client')`, root); err != nil {
		t.Fatal("open the fictional reservation", err)
	}
	start := time.Now().UTC().Truncate(time.Minute).Add(48 * time.Hour)
	expiry := time.Now().UTC().Truncate(time.Second).Add(10 * time.Minute)
	for i, at := range []time.Time{start, start.Add(time.Hour)} {
		if _, err := pool.Exec(ctx, insertAppointmentRevision, root, "fictional-booking-client", i+1, at, at.Add(30*time.Minute), expiry); err != nil {
			t.Fatal("append the fictional reservation revision", err)
		}
	}
	requireCode := func(err error, code string) {
		t.Helper()
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != code {
			t.Fatalf("wanted PostgreSQL error code %s, got %v", code, err)
		}
	}
	_, err := pool.Exec(ctx, insertAppointmentRevision, root, "fictional-other-client", 3, start, start.Add(time.Hour), expiry)
	requireCode(err, "23503")
	_, err = pool.Exec(ctx, insertAppointmentRevision, root, "fictional-booking-client", 3, start, start.Add(time.Minute+time.Second), expiry)
	requireCode(err, "23514")
	_, err = pool.Exec(ctx, insertAppointmentRevision, root, "fictional-booking-client", 3, start, start.Add(time.Hour), start.Add(time.Second))
	requireCode(err, "23514")
	_, err = pool.Exec(ctx, insertAppointmentRevision, root, "fictional-booking-client", 2, start, start.Add(time.Hour), expiry)
	requireCode(err, "23505")
	_, err = pool.Exec(ctx, `UPDATE portal_appointment_revisions SET starts_at=ends_at WHERE reservation_id=$1`, root)
	requireCode(err, "42501")
	_, err = pool.Exec(ctx, `DELETE FROM portal_appointment_reservations WHERE id=$1`, root)
	requireCode(err, "42501")

	reopened, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var revision int
	var original, latest, heldUntil time.Time
	err = reopened.QueryRow(ctx, `SELECT r.revision,r.starts_at,r.held_until,first.starts_at
 FROM portal_appointment_revisions r JOIN portal_appointment_revisions first
 ON first.reservation_id=r.reservation_id AND first.client_id=r.client_id AND first.revision=1
 WHERE r.reservation_id=$1 AND r.client_id=$2 ORDER BY r.revision DESC LIMIT 1`, root, "fictional-booking-client").Scan(&revision, &latest, &heldUntil, &original)
	if err != nil || revision != 2 || !original.Equal(start) || !latest.Equal(start.Add(time.Hour)) || !heldUntil.Equal(expiry) {
		t.Fatalf("reservation history changed after reconnect: revision=%d error=%v", revision, err)
	}
}

func TestAppointmentHoldsSerializeCompetingClients(t *testing.T) {
	pool := fixtureDB(t)
	ctx, cancel := approvalWaitContext(t)
	defer cancel()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('fictional-first-hold'),('fictional-second-hold')`); err != nil {
		t.Fatal(err)
	}
	first, second := beginFixtureTransaction(ctx, t, pool), beginFixtureTransaction(ctx, t, pool)
	start := time.Now().UTC().Truncate(time.Minute).Add(48 * time.Hour)
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	root := secret()
	if _, err := appendAppointmentHold(ctx, first, root, "fictional-first-hold", 0, slot, 10*time.Minute); err != nil {
		t.Fatal("first hold failed", err)
	}
	var competingPID int
	if err := second.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&competingPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		defer close(result)
		_, err := appendAppointmentHold(ctx, second, secret(), "fictional-second-hold", 0, slot, 10*time.Minute)
		result <- err
	}()
	defer func() {
		cancel()
		<-result
	}()
	waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE pid=$1::int AND locktype='advisory' AND NOT granted)`, competingPID)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, errProposalConflict) {
		t.Fatalf("the second client should see the first hold after waiting: %v", err)
	}
	if err := second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_appointment_reservations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("the competing operation left an orphan reservation: count=%d err=%v", count, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_appointment_revisions SET recorded_at=clock_timestamp()-interval '2 minutes',held_until=clock_timestamp()-interval '1 minute' WHERE reservation_id=$1`, root); err == nil {
		t.Fatal("the runtime could change the competing hold's expiry")
	}
	owner := migrationDB(t)
	if _, err := owner.Exec(ctx, `UPDATE portal_appointment_revisions SET recorded_at=clock_timestamp()-interval '2 minutes',held_until=clock_timestamp()-interval '1 minute' WHERE reservation_id=$1`, root); err != nil {
		t.Fatal("prepare an expired fixture hold", err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := appendAppointmentHold(ctx, tx, secret(), "fictional-second-hold", 0, slot, 10*time.Minute)
		return err
	})
	if err != nil {
		t.Fatal("an expired hold still occupied the slot", err)
	}
}

func TestAppointmentHoldRetriesPreserveTheSelectedRevision(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('fictional-booking-client'),('fictional-other-client')`); err != nil {
		t.Fatal(err)
	}
	root := secret()
	start := time.Now().UTC().Truncate(time.Minute).Add(48 * time.Hour)
	original := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	adjacent := booking.Interval{Start: original.End, End: original.End.Add(30 * time.Minute)}
	reserve := func(id, client string, expected int, slot booking.Interval) (appointmentHold, error) {
		var saved appointmentHold
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			saved, err = appendAppointmentHold(ctx, tx, id, client, expected, slot, 10*time.Minute)
			return err
		})
		return saved, err
	}
	first, err := reserve(root, "fictional-booking-client", 0, original)
	if err != nil || first.Revision != 1 {
		t.Fatal("first hold did not open the reservation", err)
	}
	replayed, err := reserve(root, "fictional-booking-client", 0, original)
	if err != nil || replayed.Revision != 1 || !replayed.HeldUntil.Equal(first.HeldUntil) {
		t.Fatal("an identical retry changed the original hold", err)
	}
	if _, err := reserve(root, "fictional-other-client", 1, adjacent); !errors.Is(err, errProposalConflict) {
		t.Fatalf("another client could revise the hold: %v", err)
	}
	rescheduled, err := reserve(root, "fictional-booking-client", 1, adjacent)
	if err != nil || rescheduled.Revision != 2 {
		t.Fatal("rescheduling did not append the next revision", err)
	}
	if _, err := reserve(root, "fictional-booking-client", 0, original); !errors.Is(err, errProposalConflict) {
		t.Fatalf("the original retry could change the latest slot: %v", err)
	}
	if _, err := reserve(secret(), "fictional-other-client", 0, original); err != nil {
		t.Fatal("the superseded slot still counted as occupied", err)
	}
}

func TestAppointmentHoldRefusesAStaleTransactionSnapshot(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('fictional-booking-client')`); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Minute).Add(48 * time.Hour)
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		_, err := appendAppointmentHold(ctx, tx, secret(), "fictional-booking-client", 0, slot, 10*time.Minute)
		return err
	})
	if !errors.Is(err, errTriageStorage) {
		t.Fatalf("a stale snapshot could record a hold: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_appointment_reservations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("the refused transaction left a reservation: count=%d err=%v", count, err)
	}
}
