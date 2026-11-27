package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// This migration fixture uses the schema owner. The deployed web role has no
// path to start work and cannot read or write the lease records.
func insertFixtureWorkLease(ctx context.Context, db queryRower, actor, key string, duration time.Duration) (int64, error) {
	return insertFixtureSelectedWorkLease(ctx, db, actor, key, duration, nil, nil)
}

func insertFixtureSelectedWorkLease(ctx context.Context, db queryRower, actor, key string, duration time.Duration, generation, revision *int64) (int64, error) {
	var fence int64
	err := db.QueryRow(ctx, `INSERT INTO portal_work_leases
 (request_id,client_id,approval_id,approval_generation,queue_revision,actor_key,submission_key,expires_at)
 SELECT request_id,client_id,approval_id,COALESCE($5,generation),COALESCE($6,revision),
 encode(sha256(convert_to($1,'UTF8')),'hex'),$2,clock_timestamp()+make_interval(secs=>$3)
 FROM portal_work_current CROSS JOIN portal_work_queue_revision
 WHERE request_id=$4
 RETURNING fence`, actor, key, duration.Seconds(), proposalRequestID, generation, revision).Scan(&fence)
	return fence, err
}

func TestWorkLeaseStorageRefusesStaleSelection(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx := t.Context()
	var generation, revision, previousFence int64
	err := db.QueryRow(ctx, `SELECT generation,revision,issued_fence FROM portal_work_current c
 CROSS JOIN portal_work_queue_revision CROSS JOIN portal_work_lease_slot WHERE c.request_id=$1`,
		proposalRequestID).Scan(&generation, &revision, &previousFence)
	if err != nil {
		t.Fatal("read the selected work and current fence", err)
	}
	if _, err := db.Exec(ctx, `UPDATE portal_requests SET submitted_at=submitted_at WHERE id=$1`, proposalRequestID); err != nil {
		t.Fatal("commit a new eligibility revision", err)
	}
	_, err = insertFixtureSelectedWorkLease(ctx, db, "fictional-worker", secret(), time.Minute, &generation, &revision)
	requireLeaseDatabaseError(t, err, "55000")

	otherGeneration := generation + 1
	_, err = insertFixtureSelectedWorkLease(ctx, db, "fictional-worker", secret(), time.Minute, &otherGeneration, nil)
	requireLeaseDatabaseError(t, err, "55000")

	nextFence, err := insertFixtureWorkLease(ctx, db, "fictional-worker", secret(), time.Minute)
	if err != nil || nextFence != previousFence+1 {
		t.Fatalf("fresh evidence must acquire the next fence after both refusals: previous=%d got=%d: %v", previousFence, nextFence, err)
	}
}

func TestWorkLeaseStorageRefusesExpiredApproval(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx := t.Context()
	if _, err := db.Exec(ctx, `WITH at AS MATERIALIZED(SELECT clock_timestamp() AS value)
 UPDATE portal_work_approvals SET decided_at=at.value-interval '168 hours',expires_at=at.value FROM at`); err != nil {
		t.Fatal("place the selected approval at its exclusive deadline", err)
	}
	_, err := insertFixtureWorkLease(ctx, db, "fictional-worker", secret(), time.Minute)
	requireLeaseDatabaseError(t, err, "55000")
}

func TestWorkLeaseStorageRefusesRevisionChangedWhileWaitingForSlot(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	holder := beginFixtureTransaction(ctx, t, db)
	if _, err := holder.Exec(ctx, `SELECT issued_fence FROM portal_work_lease_slot WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal("hold the software lease slot", err)
	}
	claim := beginFixtureTransaction(ctx, t, db)
	pid := claim.Conn().PgConn().PID()
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := insertFixtureWorkLease(ctx, claim, "fictional-worker", secret(), time.Minute)
		finished <- err
	}()
	defer func() {
		cancel()
		<-finished
	}()
	waitForFixtureDatabaseLock(ctx, t, db, `SELECT EXISTS(
 SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, pid)

	// An evidence writer must commit while this claim waits for the slot.
	if _, err := db.Exec(ctx, `UPDATE portal_requests SET submitted_at=submitted_at WHERE id=$1`, proposalRequestID); err != nil {
		t.Fatal("waiting claim locked the queue revision ahead of the slot", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal("release the slot after the evidence writer commits", err)
	}
	select {
	case err := <-finished:
		requireLeaseDatabaseError(t, err, "55000")
	case <-ctx.Done():
		t.Fatal("claim did not refuse the revision changed during its wait")
	}
}

func TestWorkLeaseStorageSerializesCompetingClaims(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	migrator := migrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	first := beginFixtureTransaction(ctx, t, migrator)
	second := beginFixtureTransaction(ctx, t, migrator)
	key := secret()
	fence, err := insertFixtureWorkLease(ctx, first, "fictional-worker-one", key, time.Minute)
	if err != nil || fence <= 0 {
		t.Fatal("record the first software lease", err)
	}
	secondPID := second.Conn().PgConn().PID()
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := insertFixtureWorkLease(ctx, second, "fictional-worker-two", key, time.Minute)
		finished <- err
	}()
	defer func() {
		cancel()
		<-finished
	}()
	waitForFixtureDatabaseLock(ctx, t, migrator, `SELECT EXISTS(
 SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, secondPID)
	if err := first.Commit(ctx); err != nil {
		t.Fatal("commit the first claim", err)
	}
	select {
	case err := <-finished:
		requireLeaseDatabaseError(t, err, "23P01")
	case <-ctx.Done():
		t.Fatal("competing claim did not finish")
	}
	if err := second.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// The release ends the live window without removing the original receipt.
	if _, err := migrator.Exec(ctx, `UPDATE portal_work_leases SET released_at=clock_timestamp() WHERE fence=$1`, fence); err != nil {
		t.Fatal("release the software slot", err)
	}
	otherConnection := migrationDB(t)
	_, err = insertFixtureWorkLease(ctx, otherConnection, "fictional-worker-one", key, time.Minute)
	requireLeaseDatabaseError(t, err, "23505")
	next, err := insertFixtureWorkLease(ctx, otherConnection, "fictional-worker-two", key, time.Minute)
	if err != nil || next != fence+1 {
		t.Fatalf("next claim lost the committed fence after reconnect: next=%d previous=%d: %v", next, fence, err)
	}
}

func TestWorkLeaseStorageReassignsAnExpiredWindow(t *testing.T) {
	b, operator := approvalFixture(t)
	savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	fence, err := insertFixtureWorkLease(ctx, db, "fictional-worker-one", secret(), time.Second)
	if err != nil {
		t.Fatal("record the expiring lease", err)
	}
	var expires time.Time
	if err := db.QueryRow(ctx, `SELECT expires_at FROM portal_work_leases WHERE fence=$1`, fence).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	waitForFixtureExpiry(ctx, t, expires)
	_, err = db.Exec(ctx, `UPDATE portal_work_leases SET released_at=acquired_at WHERE fence=$1`, fence)
	requireLeaseDatabaseError(t, err, "55000")
	next, err := insertFixtureWorkLease(ctx, db, "fictional-worker-two", secret(), time.Minute)
	if err != nil || next != fence+1 {
		t.Fatalf("expiry blocked the next lease or reused its fence: next=%d previous=%d: %v", next, fence, err)
	}
}

func TestWorkLeaseStorageRetainsOnlyTheFenceAfterEvidenceRemoval(t *testing.T) {
	b, operator := approvalFixture(t)
	approval := savedApproval(t, b, operator)
	db := migrationDB(t)
	ctx := t.Context()
	fence, err := insertFixtureWorkLease(ctx, db, "fictional-worker", secret(), time.Minute)
	if err != nil {
		t.Fatal("record software lease", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM portal_work_current WHERE request_id=$1`, proposalRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM portal_work_approvals WHERE id=$1`, approval.ID); err != nil {
		t.Fatal("remove former customer approval evidence", err)
	}
	var retired bool
	if err := db.QueryRow(ctx, `SELECT issued_fence=$1 AND NOT EXISTS(
 SELECT 1 FROM portal_work_leases WHERE fence=$1) FROM portal_work_lease_slot WHERE singleton`, fence).Scan(&retired); err != nil || !retired {
		t.Fatalf("former customer data removal lost the fence or retained the receipt: %v", err)
	}
	savedApproval(t, b, operator)
	next, err := insertFixtureWorkLease(ctx, db, "fictional-worker", secret(), time.Minute)
	if err != nil || next <= fence {
		t.Fatalf("a new approval reused a retired fence: next=%d previous=%d: %v", next, fence, err)
	}
}

func TestWorkLeaseStorageRefusesWebRolePrivileges(t *testing.T) {
	runtime := fixtureDB(t)
	ctx := t.Context()
	if err := checkWorkApprovalRole(ctx, runtime); err != nil {
		t.Fatal("ordinary approval role lost access", err)
	}
	_, err := runtime.Exec(ctx, `SELECT issued_fence FROM portal_work_lease_slot`)
	requireLeaseDatabaseError(t, err, "42501")
	_, err = runtime.Exec(ctx, `SELECT fence FROM portal_work_leases`)
	requireLeaseDatabaseError(t, err, "42501")

	migrator := migrationDB(t)
	if _, err := migrator.Exec(ctx, `GRANT SELECT(fence) ON portal_work_leases TO portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(t.Context(), `REVOKE SELECT(fence) ON portal_work_leases FROM portal_runtime`); err != nil {
			t.Error(err)
		}
	}()
	if err := checkWorkApprovalRole(ctx, runtime); !errors.Is(err, ErrDenied) {
		t.Fatal("approval role accepted an unexpected lease grant", err)
	}
}

func requireLeaseDatabaseError(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		t.Fatalf("expected PostgreSQL lease refusal %s: %v", code, err)
	}
}
