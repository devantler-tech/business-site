package identity

import (
	"context"
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

func seedAppointmentWorkspaces(t *testing.T, pool *pgxpool.Pool, clients ...string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `INSERT INTO portal_workspaces(id) SELECT unnest($1::text[]) ON CONFLICT DO NOTHING`, clients); err != nil {
		t.Fatal(err)
	}
}

func requireAppointmentClientErasure(t *testing.T, client string) {
	t.Helper()
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE client_id=$1`, client); err != nil {
		t.Fatal(err)
	}
	if erased := purgeAged(t, migrator); erased != 1 {
		t.Fatalf("retention could not erase the appointment's former client: %d", erased)
	}
}

func TestAppointmentReservationsKeepClientBoundRevisions(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	root := secret()
	seedAppointmentWorkspaces(t, pool, "fictional-booking-client", "fictional-other-client")
	migrator := migrationDB(t)
	rewindAppointmentReservationsFixture(t, migrator)
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
	seedAppointmentWorkspaces(t, pool, "fictional-first-hold", "fictional-second-hold")
	first, second := beginFixtureTransaction(ctx, t, pool), beginFixtureTransaction(ctx, t, pool)
	start, policy := appointmentHoldFixture(t)
	policy.policy.WeeklyCapacity = 30 * time.Minute
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	later := booking.Interval{Start: start.Add(2 * time.Hour), End: start.Add(150 * time.Minute)}
	root := secret()
	if _, err := policy.append(ctx, first, root, "fictional-first-hold", 0, slot, appointmentQuoteRef{}); err != nil {
		t.Fatal("first hold failed", err)
	}
	var competingPID int
	if err := second.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&competingPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := policy.append(ctx, second, secret(), "fictional-second-hold", 0, later, appointmentQuoteRef{})
		result <- err
	}()
	defer func() {
		cancel()
		<-done
	}()
	waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE pid=$1::int AND locktype='advisory' AND NOT granted)`, competingPID)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, errProposalConflict) {
		t.Fatalf("the second client should see the first hold consuming the rest of the week after waiting: %v", err)
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
		_, err := policy.append(ctx, tx, secret(), "fictional-second-hold", 0, later, appointmentQuoteRef{})
		return err
	})
	if err != nil {
		t.Fatal("an expired hold still occupied the slot", err)
	}
}

func TestAppointmentHoldRetriesPreserveTheSelectedRevision(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	seedAppointmentWorkspaces(t, pool, "fictional-booking-client", "fictional-other-client")
	root := secret()
	start, policy := appointmentHoldFixture(t)
	policy.policy.WeeklyCapacity = 60 * time.Minute
	original := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	adjacent := booking.Interval{Start: original.End, End: original.End.Add(30 * time.Minute)}
	reserve := func(id, client string, expected int, slot booking.Interval) (appointmentHold, error) {
		var saved appointmentHold
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			saved, err = policy.append(ctx, tx, id, client, expected, slot, appointmentQuoteRef{})
			return err
		})
		return saved, err
	}
	first, err := reserve(root, "fictional-booking-client", 0, original)
	if err != nil || first.Revision != 1 {
		t.Fatal("first hold did not open the reservation", err)
	}
	provider := policy.provider
	policy.provider = nil
	replayed, err := reserve(root, "fictional-booking-client", 0, original)
	if err != nil || replayed.Revision != 1 || !replayed.HeldUntil.Equal(first.HeldUntil) {
		t.Fatal("an identical retry changed the original hold", err)
	}
	policy.provider = provider
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
	seedAppointmentWorkspaces(t, pool, "fictional-booking-client")
	start, policy := appointmentHoldFixture(t)
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		_, err := policy.append(ctx, tx, secret(), "fictional-booking-client", 0, slot, appointmentQuoteRef{})
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

func TestAppointmentHoldRequiresCurrentCalendarAndBuffers(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	seedAppointmentWorkspaces(t, pool, "fictional-booking-client")
	start, policy := appointmentHoldFixture(t)
	buffer := 15 * time.Minute
	policy.policy.BufferBefore = &buffer
	policy.policy.BufferAfter = &buffer
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	id := secret()
	reserve := func(id string, expected int, slot booking.Interval) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := policy.append(ctx, tx, id, "fictional-booking-client", expected, slot, appointmentQuoteRef{})
			return err
		})
	}
	beyond := booking.Interval{Start: slot.Start.AddDate(0, 0, 30), End: slot.End.AddDate(0, 0, 30)}
	current := policy.provider
	policy.provider = appointmentBusyFunc(func(context.Context, booking.Interval) (booking.BusySnapshot, error) {
		t.Error("the provider was queried outside the booking horizon")
		return booking.BusySnapshot{}, booking.ErrUnavailable
	})
	if err := reserve(id, 0, beyond); !errors.Is(err, booking.ErrUnavailable) {
		t.Fatalf("a hold outside the booking horizon was not refused: %v", err)
	}
	policy.provider = current
	if err := reserve(id, 0, slot); err != nil {
		t.Fatal("eligible slot was refused", err)
	}
	adjacent := booking.Interval{Start: slot.End.Add(buffer), End: slot.End.Add(3 * buffer)}
	if err := reserve(secret(), 0, adjacent); !errors.Is(err, errProposalConflict) {
		t.Fatalf("the existing reservation's buffer was ignored: %v", err)
	}
	candidate := booking.Interval{Start: start.Add(2 * time.Hour), End: start.Add(150 * time.Minute)}
	policy.provider = appointmentBusyFunc(func(_ context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
		if !coverage.Start.Equal(candidate.Start.Add(-buffer)) || !coverage.End.Equal(candidate.End.Add(buffer)) {
			t.Fatal("the requested calendar interval did not include both buffers")
		}
		busy := booking.Interval{Start: coverage.Start, End: candidate.Start}
		return booking.BusySnapshot{ObservedAt: time.Now(), Coverage: coverage, Complete: true, Busy: []booking.Interval{busy}}, nil
	})
	if err := reserve(id, 1, candidate); !errors.Is(err, errProposalConflict) {
		t.Fatalf("the outside-calendar buffer was ignored: %v", err)
	}
	policy.provider = appointmentBusyFunc(func(_ context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
		return booking.BusySnapshot{ObservedAt: time.Now().Add(-time.Minute), Coverage: coverage, Complete: true}, nil
	})
	if err := reserve(id, 1, candidate); !errors.Is(err, booking.ErrUnavailable) {
		t.Fatalf("stale calendar data admitted a hold: %v", err)
	}
	policy.provider = appointmentBusyFunc(func(_ context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
		return booking.BusySnapshot{ObservedAt: time.Now().Add(time.Minute), Coverage: coverage, Complete: true}, nil
	})
	if err := reserve(id, 1, candidate); !errors.Is(err, booking.ErrUnavailable) {
		t.Fatalf("a future calendar timestamp admitted a hold: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_appointment_revisions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("a refused hold replaced the original revision: count=%d err=%v", count, err)
	}
}

func TestAppointmentHoldBindsThePersistedQuote(t *testing.T) {
	b := proposalFixture(t)
	ctx := t.Context()
	const client = "private-client-key"
	seedAppointmentWorkspaces(t, b.pool, client)
	for _, description := range []string{"First appointment scope", "Revised appointment scope"} {
		form := serviceProposalForm(t, b, proposalServiceAppointment)
		form.Set("scope", description)
		form.Set("appointment_minutes", "30")
		saveFixtureProposal(t, b, form)
	}
	quote := func(version int) appointmentQuoteRef {
		ref := appointmentQuoteRef{Request: proposalRequestID, Version: version}
		if err := b.pool.QueryRow(ctx, `SELECT content_sha256 FROM portal_proposals WHERE request_id=$1 AND client_id=$2 AND version=$3`,
			ref.Request, client, version).Scan(&ref.Hash); err != nil {
			t.Fatal(err)
		}
		return ref
	}
	original, revised := quote(1), quote(2)
	start, policy := appointmentHoldFixture(t)
	slot := booking.Interval{Start: start, End: start.Add(30 * time.Minute)}
	id := secret()
	reserve := func(who string, reference appointmentQuoteRef, expected int, slot booking.Interval) (appointmentHold, error) {
		var saved appointmentHold
		err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
			var err error
			saved, err = policy.append(ctx, tx, id, who, expected, slot, reference)
			return err
		})
		return saved, err
	}
	short := booking.Interval{Start: slot.Start, End: slot.End.Add(-15 * time.Minute)}
	if _, err := reserve(client, original, 0, short); !errors.Is(err, errProposalConflict) {
		t.Fatalf("a hold shortened the quoted duration: %v", err)
	}
	if _, err := reserve("fictional-other-client", original, 0, slot); !errors.Is(err, errProposalConflict) {
		t.Fatalf("a hold used another client's quote: %v", err)
	}
	first, err := reserve(client, original, 0, slot)
	if err != nil || first.Quote != original || first.Revision != 1 {
		t.Fatal("the original quote was not held", err)
	}
	next := booking.Interval{Start: slot.End, End: slot.End.Add(30 * time.Minute)}
	for _, changed := range []appointmentQuoteRef{revised, {}} {
		if _, err := reserve(client, changed, 1, next); !errors.Is(err, errProposalConflict) {
			t.Fatalf("a reschedule replaced the reservation's quote: %v", err)
		}
	}
	rescheduled, err := reserve(client, original, 1, next)
	if err != nil || rescheduled.Quote != original || rescheduled.Revision != 2 {
		t.Fatal("rescheduling lost the original quote", err)
	}
	var stored appointmentHold
	err = pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		var err error
		stored, err = readLatestAppointmentHold(ctx, tx, id, client)
		return err
	})
	if err != nil || stored.Quote != original || !stored.Slot.Start.Equal(next.Start) || stored.Revision != 2 {
		t.Fatal("the original quote and current revision could not be recovered", err)
	}

	// The restricted writer cannot invent a quote reference or change an
	// existing binding even when the requested duration would be identical.
	_, err = b.pool.Exec(ctx, `INSERT INTO portal_appointment_reservations(id,client_id,quote_request_id,quote_version,quote_sha256)
 VALUES($1,$2,$3,$4,$5)`, secret(), client, original.Request, revised.Version, original.Hash)
	var failure *pgconn.PgError
	if !errors.As(err, &failure) || failure.Code != "23503" {
		t.Fatalf("PostgreSQL accepted a mismatched quote version and hash: %v", err)
	}
	_, err = b.pool.Exec(ctx, insertAppointmentRevision, id, client, 3, short.Start, short.End, time.Now().Add(time.Minute))
	if !errors.As(err, &failure) || failure.Code != "23514" {
		t.Fatalf("PostgreSQL accepted a shortened slot for the held quote: %v", err)
	}
	_, err = b.pool.Exec(ctx, `UPDATE portal_appointment_reservations SET quote_version=$2,quote_sha256=$3 WHERE id=$1`, id, revised.Version, revised.Hash)
	if !errors.As(err, &failure) || failure.Code != "42501" {
		t.Fatalf("the runtime could replace a held quote: %v", err)
	}
	_, err = b.pool.Exec(ctx, `INSERT INTO portal_appointment_introductions
 (client_id,sequence,reservation_id,revision,cancellation_notice_minutes,cancellation_terms_version)
 VALUES($1,1,$2,2,1440,'synthetic-policy-v1')`, client, id)
	if !errors.As(err, &failure) || failure.Code != "23514" {
		t.Fatalf("PostgreSQL accepted a quoted reservation as a free introduction: %v", err)
	}
	enroll(t, b.svc, b.f, "quoted-appointment-client", client, "client")
	requireAppointmentClientErasure(t, client)
}

type appointmentBusyFunc func(context.Context, booking.Interval) (booking.BusySnapshot, error)

func (f appointmentBusyFunc) Busy(ctx context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
	return f(ctx, coverage)
}

func appointmentHoldFixture(t *testing.T) (time.Time, appointmentHoldPolicy) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatal(err)
	}
	// Choose the Monday after the coming week so test slots never straddle
	// the capacity boundary or fall inside the lead time.
	today := time.Now().In(loc)
	monday := time.Date(today.Year(), today.Month(), today.Day()+14-(int(today.Weekday())+6)%7, 9, 0, 0, 0, loc)
	zero, lead := time.Duration(0), 24*time.Hour
	return monday, appointmentHoldPolicy{
		policy: booking.Policy{
			Enabled: true, HorizonDays: 30, SlotStep: 15 * time.Minute, WeeklyCapacity: 150 * time.Minute,
			LeadTime: &lead, BufferBefore: &zero, BufferAfter: &zero, CancellationNotice: &lead,
			CancellationTermsVersion: "fixture-v1",
			Openings:                 []booking.Opening{{Date: monday.Format("2006-01-02"), Start: 9 * time.Hour, End: 15 * time.Hour}},
		},
		provider: appointmentBusyFunc(func(_ context.Context, coverage booking.Interval) (booking.BusySnapshot, error) {
			return booking.BusySnapshot{ObservedAt: time.Now(), Coverage: coverage, Complete: true}, nil
		}),
	}
}
