package identity

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The native tracer observes pool reacquisition without replacing any database
// operation or changing the production dispatcher. Query callbacks are inert.
type mailPoolAcquireTrace struct{ starts atomic.Int32 }

func (trace *mailPoolAcquireTrace) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	trace.starts.Add(1)
	return ctx
}

func (*mailPoolAcquireTrace) TraceAcquireEnd(context.Context, *pgxpool.Pool, pgxpool.TraceAcquireEndData) {
}
func (*mailPoolAcquireTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (*mailPoolAcquireTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// An empty pass still must query the actual outbox while retaining the sender
// lock. The two-connection control distinguishes pool starvation from a broken
// fixture; a table barrier and native advisory waiter establish the reached phase.
func TestMailDispatchEmptyOutboxUsesOneConnection(t *testing.T) {
	for _, test := range []struct {
		name string
		max  int32
	}{{"max-conns-1", 1}, {"max-conns-2", 2}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := fixtureDB(t)
			observer := migrationDB(t)
			if observer.Config().MaxConns < 2 {
				t.Fatal("mail fixture needs two independent observer connections")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var queued, delivered int
			if err := fixture.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_mail_outbox),(SELECT count(*) FROM portal_mail_deliveries)`).Scan(&queued, &delivered); err != nil || queued != 0 || delivered != 0 {
				t.Fatal("mail pool fixture must begin with an empty real outbox")
			}
			trace := &mailPoolAcquireTrace{}
			config := fixture.Config()
			config.MaxConns, config.MinConns, config.MinIdleConns = test.max, 0, 0
			config.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pool.Close)
			service := enabled(t, pool, fixtureBroker(t))
			sender := &fakeMailer{}
			if err := service.EnableMail(sender); err != nil || !service.ready(ctx) {
				t.Fatal("actual mail identity policy did not admit the fixture")
			}
			barrier, err := observer.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if err := barrier.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Error("mail outbox barrier cleanup failed")
				}
			}()
			if _, err = barrier.Exec(ctx, `LOCK TABLE portal_mail_outbox IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			waiter, err := observer.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var waiterPID int32
			if err := waiter.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
				waiter.Release()
				t.Fatal(err)
			}
			dispatchContext, stopDispatch := context.WithTimeout(ctx, 4*time.Second)
			waiterContext, stopWaiter := context.WithCancel(ctx)
			dispatchResult, waiterResult := make(chan error, 1), make(chan error, 1)
			var dispatchRead, waiterStarted, waiterRead, waiterUnlocked bool
			defer func() {
				stopDispatch()
				stopWaiter()
				cleanup, stop := context.WithTimeout(context.Background(), 9*time.Second)
				defer stop()
				if err := barrier.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Error("mail outbox barrier cleanup failed")
				}
				if !dispatchRead {
					select {
					case <-dispatchResult:
					case <-cleanup.Done():
						t.Error("mail dispatcher did not stop during bounded cleanup")
					}
				}
				if waiterStarted && !waiterRead {
					select {
					case <-waiterResult:
					case <-cleanup.Done():
						t.Error("native mail advisory waiter did not stop during bounded cleanup")
					}
				}
				if !waiterUnlocked && !waiter.Conn().IsClosed() {
					if _, err := waiter.Exec(cleanup, `SELECT pg_advisory_unlock($1::bigint)`, mailLock); err != nil {
						t.Error("native mail waiter cleanup could not unlock its connection")
						if err := waiter.Conn().Close(cleanup); err != nil {
							t.Error("native mail waiter cleanup could not close its connection")
						}
					}
				}
				waiter.Release()
			}()
			go func() { dispatchResult <- service.SendQueuedMail(dispatchContext) }()
			var ownerPID int32
			if err := waitForMailPoolPhase(ctx, func() (bool, error) {
				err := barrier.QueryRow(ctx, `SELECT coalesce((SELECT pid FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=$1::oid AND objsubid=1 AND granted),0)`, mailLock).Scan(&ownerPID)
				return ownerPID != 0, err
			}); err != nil {
				t.Fatal("dispatcher did not acquire the actual sender lock")
			}
			waiterStarted = true
			go func() {
				_, err := waiter.Exec(waiterContext, `SELECT pg_advisory_lock($1::bigint)`, mailLock)
				waiterResult <- err
			}()
			if err := waitForMailPoolPhase(ctx, func() (bool, error) {
				var waiting bool
				err := barrier.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=$1::oid AND objsubid=1 AND pid=$2 AND NOT granted) AND EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=0 AND objid=$1::oid AND objsubid=1 AND pid=$3 AND granted)`, mailLock, waiterPID, ownerPID).Scan(&waiting)
				return waiting, err
			}); err != nil {
				t.Fatal("native contender did not wait for the observed sender lock")
			}
			var readerPID int32
			if err := waitForMailPoolPhase(ctx, func() (bool, error) {
				err := barrier.QueryRow(ctx, `SELECT coalesce((SELECT pid FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND relation='public.portal_mail_outbox'::regclass AND mode='AccessShareLock' AND NOT granted LIMIT 1),0)`).Scan(&readerPID)
				return readerPID != 0 || (test.max == 1 && trace.starts.Load() >= 2), err
			}); err != nil {
				t.Fatal("dispatcher reached neither a real outbox read nor native pool reacquisition")
			}
			if test.max == 1 && (pool.Stat().MaxConns() != 1 || pool.Stat().AcquiredConns() != 1 || pool.Stat().IdleConns() != 0) {
				t.Fatal("one-connection observation did not retain the constrained pool")
			}
			// The native two-connection positive control must have reached the
			// actual SELECT, not merely the pool tracer or an elapsed-time guess.
			if test.max == 2 && readerPID == 0 {
				t.Fatal("two-connection control did not reach the actual outbox table wait")
			}
			t.Logf("observed native mail owner=%d blocked advisory waiter=%d outbox table waiter=%d pool max=%d acquired=%d idle=%d native acquire starts=%d", ownerPID, waiterPID, readerPID, pool.Stat().MaxConns(), pool.Stat().AcquiredConns(), pool.Stat().IdleConns(), trace.starts.Load())
			if err := barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var dispatchErr error
			select {
			case dispatchErr = <-dispatchResult:
				dispatchRead = true
			case <-ctx.Done():
				t.Fatal("observed dispatcher did not finish within its bound")
			}
			select {
			case err := <-waiterResult:
				waiterRead = true
				if err != nil {
					t.Fatal("sender lock was not released to its native waiter")
				}
			case <-ctx.Done():
				t.Fatal("native sender-lock waiter exceeded its bound")
			}
			var unlocked bool
			if err := waiter.QueryRow(ctx, `SELECT pg_advisory_unlock($1::bigint)`, mailLock).Scan(&unlocked); err != nil || !unlocked {
				t.Fatal("native sender-lock waiter did not release its lock")
			}
			waiterUnlocked = true
			if len(sender.messages()) != 0 {
				t.Fatal("empty outbox invoked the fake sender")
			}
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_mail_outbox),(SELECT count(*) FROM portal_mail_deliveries)`).Scan(&queued, &delivered); err != nil || queued != 0 || delivered != 0 {
				t.Fatal("empty dispatch changed mail state or retained its pool connection")
			}
			t.Logf("completed dispatch result=%v context=%v canceled pool acquires=%d; native waiter released, zero fake sends, unchanged empty outbox and reusable pool", dispatchErr, dispatchContext.Err(), pool.Stat().CanceledAcquireCount())
			if dispatchErr != nil {
				t.Errorf("empty outbox dispatch must succeed with MaxConns=%d after its observed sender lock; canceled pool acquires=%d, context=%v, result=%v", test.max, pool.Stat().CanceledAcquireCount(), dispatchContext.Err(), dispatchErr)
			}
			if readerPID != ownerPID {
				t.Errorf("outbox read must use the held sender connection; owner=%d reader=%d native acquire starts=%d", ownerPID, readerPID, trace.starts.Load())
			}
		})
	}
}

func waitForMailPoolPhase(ctx context.Context, observe func() (bool, error)) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		reached, err := observe()
		if err != nil || reached {
			return err
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
