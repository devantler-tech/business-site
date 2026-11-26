package identity

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestServiceFulfillmentRejectsCompetingAppointment(t *testing.T) {
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)
	placeEarlyStartOrder(t, b)
	ctx, cancel := approvalWaitContext(t)
	defer cancel()

	const insertRoot = `INSERT INTO portal_service_fulfillments(id,request_id,client_id,order_id,acceptance_id,proposal_version,proposal_sha256,service_kind)
 SELECT $1,o.request_id,o.client_id,o.id,o.acceptance_id,a.proposal_version,a.content_hash,$2
 FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id WHERE o.request_id=$3`
	requireSQLState := func(err error, want string) {
		t.Helper()
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != want {
			t.Fatalf("wanted PostgreSQL error code %s, got %v", want, err)
		}
	}
	_, err := b.pool.Exec(ctx, insertRoot, secret(), "appointment", proposalRequestID)
	requireSQLState(err, "23503")

	software := beginFixtureTransaction(ctx, t, b.pool)
	defer rollbackProposalWrite(ctx, software)
	root := secret()
	if _, err := software.Exec(ctx, insertRoot, root, "software", proposalRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := software.Exec(ctx, `INSERT INTO portal_service_consumers(fulfillment_id,service_kind,aggregate_id)
 SELECT $1,'software',id FROM portal_orders WHERE request_id=$2`, root, proposalRequestID); err != nil {
		t.Fatal(err)
	}

	competing, err := b.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer competing.Release()
	pid := competing.Conn().PgConn().PID()
	result := make(chan error, 1)
	go func() {
		defer close(result)
		_, err := competing.Exec(ctx, insertRoot, secret(), "appointment", proposalRequestID)
		result <- err
	}()
	defer func() {
		cancel()
		<-result
	}()
	waitForFixtureDatabaseLock(ctx, t, b.pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
	 WHERE pid=$1::bigint AND NOT granted)`, int64(pid))
	if err := software.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	requireSQLState(<-result, "23505")

	var roots, consumers int
	var correct bool
	err = b.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_service_fulfillments),
 (SELECT count(*) FROM portal_service_consumers), EXISTS(
   SELECT 1 FROM portal_service_fulfillments f JOIN portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind=f.service_kind
   WHERE f.id=$1 AND f.service_kind='software' AND c.aggregate_id=f.order_id)`, root).Scan(&roots, &consumers, &correct)
	if err != nil || roots != 1 || consumers != 1 || !correct {
		t.Fatalf("competition changed the software binding: roots=%d consumers=%d correct=%t err=%v", roots, consumers, correct, err)
	}
}
