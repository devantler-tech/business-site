package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestWorkQueueRevisionTracksCommittedChanges(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	start := fixtureQueueRevision(t, ctx, pool)
	tx := beginFixtureTransaction(ctx, t, pool)
	if _, err := tx.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES
 ('https://broker.example.test','revision-client','revision-workspace','client')`); err != nil {
		t.Fatal(err)
	}
	if fixtureQueueRevision(t, ctx, tx) != start {
		t.Fatal("the change was recorded before its transaction reached commit")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// One write changes membership and its history. It advances the revision once.
	if got := fixtureQueueRevision(t, ctx, pool); got != start+1 {
		t.Fatalf("one transaction left revision %d, expected %d", got, start+1)
	}
	tx = beginFixtureTransaction(ctx, t, pool)
	if _, err := tx.Exec(ctx, `UPDATE portal_memberships SET revoked=true WHERE subject='revision-client'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fixtureQueueRevision(t, ctx, pool); got != start+1 {
		t.Fatal("an aborted change advanced the software queue revision")
	}
	_, err := pool.Exec(ctx, `UPDATE portal_work_queue_revision SET revision=0`)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "42501" {
		t.Fatalf("the runtime role could reset the software queue revision: %v", err)
	}
}

func TestDistinctWorkQueueWritersWaitOnlyAtCommit(t *testing.T) {
	pool := fixtureDB(t)
	migrator := migrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	holder := beginFixtureTransaction(ctx, t, migrator)
	var firstRevision int64
	if err := holder.QueryRow(ctx, `SELECT revision FROM portal_work_queue_revision WHERE singleton FOR SHARE`).Scan(&firstRevision); err != nil {
		t.Fatal("hold the current queue revision", err)
	}

	first := beginFixtureTransaction(ctx, t, pool)
	id := secret()
	if _, err := first.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief)
 VALUES($1,'revision-workspace',$1,'website','Queue revision fixture','Fictional request')`, id); err != nil {
		t.Fatal("the revision lock blocked a request write before commit", err)
	}
	second := beginFixtureTransaction(ctx, t, pool)
	if _, err := second.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES
 ('https://broker.example.test','revision-other-client','revision-other-workspace','client')`); err != nil {
		t.Fatal("the revision lock blocked a membership write before commit", err)
	}
	pids := []uint32{first.Conn().PgConn().PID(), second.Conn().PgConn().PID()}
	done := make(chan error, 2)
	go func() { done <- first.Commit(ctx) }()
	go func() { done <- second.Commit(ctx) }()
	waitForFixtureDatabaseLock(ctx, t, migrator,
		`SELECT count(DISTINCT pid)=2 FROM pg_locks WHERE pid=ANY($1::integer[]) AND NOT granted`, pids)
	if fixtureQueueRevision(t, ctx, holder) != firstRevision {
		t.Fatal("the locked revision changed before either commit")
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("the deferred commit failed after release", err)
		}
	}
	if got := fixtureQueueRevision(t, ctx, pool); got != firstRevision+2 {
		t.Fatalf("two committed writers left revision %d, expected %d", got, firstRevision+2)
	}
}

func TestWorkQueueRevisionCoversEligibilityWriters(t *testing.T) {
	pool := fixtureDB(t)
	// Read the actual installed recorder and require all row change events,
	// deferred execution, and the enabled trigger on every evidence relation.
	const rowInsertUpdateDelete = 1 | 4 | 8 | 16
	var installed []string
	err := pool.QueryRow(t.Context(), `SELECT array_agg(c.relname::text ORDER BY c.relname::text COLLATE "C")
 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
 WHERE c.relnamespace='public'::regnamespace
 AND t.tgfoid='public.portal_record_work_queue_revision()'::regprocedure
 AND t.tgtype=$1 AND t.tgdeferrable AND t.tginitdeferred AND t.tgenabled='O' AND t.tgqual IS NULL`,
		rowInsertUpdateDelete).Scan(&installed)
	required := []string{
		"portal_addon_ends", "portal_addon_removals", "portal_charge_reversals",
		"portal_mail_deliveries", "portal_mail_outbox", "portal_membership_transitions", "portal_memberships",
		"portal_order_addons", "portal_order_charges", "portal_order_completions", "portal_order_withdrawals",
		"portal_orders", "portal_proposal_acceptances", "portal_proposal_visibility", "portal_proposals",
		"portal_request_closures", "portal_requests", "portal_work_approvals", "portal_work_current", "portal_work_revocations",
	}
	if err != nil || !slices.Equal(required, installed) {
		t.Fatalf("the software queue revision missed an eligibility writer: installed=%v error=%v", installed, err)
	}
}

func fixtureQueueRevision(t *testing.T, ctx context.Context, source queryRower) int64 {
	t.Helper()
	var revision int64
	if err := source.QueryRow(ctx, `SELECT revision FROM portal_work_queue_revision`).Scan(&revision); err != nil {
		t.Fatal("read the software queue revision", err)
	}
	return revision
}
