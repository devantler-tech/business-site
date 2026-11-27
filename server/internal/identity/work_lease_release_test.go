package identity

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkLeaseStorageRefusesAReleaseOnCreation(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx := t.Context()
	var previousFence int64
	if err := db.QueryRow(ctx, `SELECT issued_fence FROM portal_work_lease_slot WHERE singleton`).Scan(&previousFence); err != nil {
		t.Fatal("read the previous software fence", err)
	}

	const worker = "fictional-worker"
	key := secret()
	holder := beginFixtureTransaction(ctx, t, db)
	if _, err := holder.Exec(ctx, `SELECT issued_fence FROM portal_work_lease_slot WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal("hold the slot while testing creation refusal", err)
	}
	refusal, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err := db.Exec(refusal, `INSERT INTO portal_work_leases
 (request_id,client_id,approval_id,approval_generation,queue_revision,actor_key,submission_key,expires_at,released_at)
 SELECT request_id,client_id,approval_id,generation,revision,encode(sha256(convert_to($1,'UTF8')),'hex'),
 $2,clock_timestamp()+interval '1 minute',clock_timestamp()+interval '5 seconds'
 FROM portal_work_current CROSS JOIN portal_work_queue_revision WHERE request_id=$3`, worker, key, proposalRequestID)
	requireLeaseDatabaseError(t, err, "55000")
	if err := holder.Commit(ctx); err != nil {
		t.Fatal("release the slot before retrying the claim", err)
	}

	fence, err := insertFixtureWorkLease(ctx, db, worker, key, time.Minute)
	if err != nil || fence != previousFence+1 {
		t.Fatalf("the refused insert consumed a fence or the submission key: next=%d prior=%d: %v", fence, previousFence, err)
	}
	var opened bool
	if err := db.QueryRow(ctx, `SELECT released_at IS NULL FROM portal_work_leases WHERE fence=$1`, fence).Scan(&opened); err != nil || !opened {
		t.Fatalf("the valid claim did not start active: %v", err)
	}
}

func TestWorkLeaseStorageKeepsReleaseAndIdentityImmutable(t *testing.T) {
	db, fence := activeWorkLeaseForRelease(t)
	ctx := t.Context()
	for _, assignment := range []string{
		"actor_key=repeat('a',64)",
		"submission_key=repeat('A',26)",
		"fence=fence+1",
		"approval_generation=approval_generation+1",
		"queue_revision=queue_revision+1",
		"expires_at=expires_at+interval '1 minute'",
	} {
		t.Run(assignment, func(t *testing.T) {
			_, err := db.Exec(t.Context(), `UPDATE portal_work_leases SET `+assignment+` WHERE fence=$1`, fence)
			requireLeaseDatabaseError(t, err, "55000")
		})
	}

	var earliest, releasedAt time.Time
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&earliest); err != nil {
		t.Fatal("read the database release clock", err)
	}
	err := db.QueryRow(ctx, `UPDATE portal_work_leases SET released_at=acquired_at
 WHERE fence=$1 RETURNING released_at`, fence).Scan(&releasedAt)
	if err != nil || releasedAt.Before(earliest) {
		t.Fatalf("a live release did not use the database clock: %v", err)
	}
	_, err = db.Exec(ctx, `UPDATE portal_work_leases SET released_at=NULL WHERE fence=$1`, fence)
	requireLeaseDatabaseError(t, err, "55000")
	_, err = db.Exec(ctx, `UPDATE portal_work_leases SET released_at=acquired_at WHERE fence=$1`, fence)
	requireLeaseDatabaseError(t, err, "55000")

	var replayAt time.Time
	err = db.QueryRow(ctx, `UPDATE portal_work_leases SET released_at=released_at
 WHERE fence=$1 RETURNING released_at`, fence).Scan(&replayAt)
	if err != nil || !replayAt.Equal(releasedAt) {
		t.Fatalf("the repeated release changed its recorded end: %v", err)
	}
	next, err := insertFixtureWorkLease(ctx, db, "next-fictional-worker", secret(), time.Minute)
	if err != nil || next != fence+1 {
		t.Fatalf("release did not make the next fence available: next=%d prior=%d: %v", next, fence, err)
	}
}

func TestWorkLeaseStorageKeepsTheFirstConcurrentRelease(t *testing.T) {
	db, fence := activeWorkLeaseForRelease(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	first := beginFixtureTransaction(ctx, t, db)
	second := beginFixtureTransaction(ctx, t, db)
	var firstRelease time.Time
	if err := first.QueryRow(ctx, `UPDATE portal_work_leases SET released_at=clock_timestamp()
 WHERE fence=$1 RETURNING released_at`, fence).Scan(&firstRelease); err != nil {
		t.Fatal("hold the first release", err)
	}
	pid := second.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		_, err := second.Exec(ctx, `UPDATE portal_work_leases SET released_at=acquired_at WHERE fence=$1`, fence)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitForFixtureDatabaseLock(ctx, t, db, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, pid)
	if err := first.Commit(ctx); err != nil {
		t.Fatal("commit the first release", err)
	}
	select {
	case err := <-done:
		requireLeaseDatabaseError(t, err, "55000")
	case <-ctx.Done():
		t.Fatal("the competing release did not complete")
	}
	var recorded time.Time
	if err := db.QueryRow(ctx, `SELECT released_at FROM portal_work_leases WHERE fence=$1`, fence).Scan(&recorded); err != nil || !recorded.Equal(firstRelease) {
		t.Fatalf("the waiting update changed the first release: %v", err)
	}
}

func TestWorkLeaseStoragePreservesPreUpgradeRelease(t *testing.T) {
	db, fence := activeWorkLeaseForRelease(t)
	ctx := t.Context()
	if _, err := db.Exec(ctx, `DROP TRIGGER portal_guard_work_lease_release ON portal_work_leases;
 DROP TRIGGER portal_admit_active_work_lease ON portal_work_leases;
 DROP TRIGGER portal_validate_work_lease_window ON portal_work_leases;
 DROP FUNCTION portal_guard_work_lease_release(),portal_require_active_work_lease(),portal_validate_work_lease_window();
 DELETE FROM portal_schema_version WHERE version IN (45,46,47)`); err != nil {
		t.Fatal("prepare the disposable pre-guard lease schema", err)
	}
	var priorRelease time.Time
	if err := db.QueryRow(ctx, `UPDATE portal_work_leases SET released_at=acquired_at
 WHERE fence=$1 RETURNING released_at`, fence).Scan(&priorRelease); err != nil {
		t.Fatal("record release with the old schema", err)
	}
	next, err := insertFixtureWorkLease(ctx, db, "fictional-work-successor", secret(), 169*time.Hour)
	if err != nil {
		t.Fatal("record the historical successor extending past its approval", err)
	}
	var priorExpiry time.Time
	if err := db.QueryRow(ctx, `SELECT expires_at FROM portal_work_leases WHERE fence=$1`, next).Scan(&priorExpiry); err != nil {
		t.Fatal("read the historical successor window", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal("upgrade existing lease records", err)
	}
	_, err = db.Exec(ctx, `UPDATE portal_work_leases SET released_at=NULL WHERE fence=$1`, fence)
	requireLeaseDatabaseError(t, err, "55000")
	var recorded time.Time
	var successorUnchanged bool
	err = db.QueryRow(ctx, `SELECT released_at,EXISTS(SELECT 1 FROM portal_work_leases
 WHERE fence=$2 AND released_at IS NULL AND expires_at=$3) FROM portal_work_leases WHERE fence=$1`, fence, next, priorExpiry).
		Scan(&recorded, &successorUnchanged)
	if err != nil || !recorded.Equal(priorRelease) || !successorUnchanged {
		t.Fatalf("the upgrade changed the original release or its successor: %v", err)
	}
}

func activeWorkLeaseForRelease(t *testing.T) (*pgxpool.Pool, int64) {
	t.Helper()
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	fence, err := insertFixtureWorkLease(t.Context(), db, "fictional-work-owner", secret(), time.Minute)
	if err != nil {
		t.Fatal("record a releasable work lease", err)
	}
	return db, fence
}
