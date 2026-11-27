package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// customerIdentityConnection retains the distributed identity lock across local
// commit and provider synchronization. Its caller uses this same connection for
// transactions and post-wait authority checks, including with a one-slot pool.
func (s *Foundation) customerIdentityConnection(ctx context.Context, subject string) (*pgxpool.Conn, func(), error) {
	return holdSessionLock(ctx, s.pool, identityLock, s.broker.issuer+" "+subject)
}

// holdSessionLock keeps an advisory lock on one pooled connection across
// transaction boundaries and releases or discards that connection on cleanup.
func holdSessionLock(ctx context.Context, pool *pgxpool.Pool, namespace int32, key string) (*pgxpool.Conn, func(), error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	discard := func() {
		// Hijacking removes the connection from the pool even if network cleanup
		// fails. A connection whose lock release is uncertain must never be reused.
		raw := connection.Hijack()
		fresh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = raw.Close(fresh)
	}
	if _, err = connection.Exec(ctx, `SELECT pg_advisory_lock($1::int,hashtext($2))`, namespace, key); err != nil {
		discard()
		return nil, nil, err
	}
	release := func() {
		// Request cancellation must not cancel lock cleanup or return a locked
		// connection. Only a positively confirmed unlock permits pool reuse.
		fresh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := connection.QueryRow(fresh, `SELECT pg_advisory_unlock($1::int,hashtext($2))`, namespace, key).Scan(&unlocked); err != nil || !unlocked {
			discard()
			return
		}
		connection.Release()
	}
	return connection, release, nil
}
