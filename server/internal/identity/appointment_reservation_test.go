package identity

import (
	"errors"
	"testing"
	"time"

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
