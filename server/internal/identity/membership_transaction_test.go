package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMembershipReaderRetainsCallerTransaction(t *testing.T) {
	pool := fixtureDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES('https://membership.example.invalid','member','client-a','client')`); err != nil {
		t.Fatal(err)
	}
	tx := beginFixtureTransaction(ctx, t, pool)
	stamp, err := readMembershipIncarnation(ctx, tx, "https://membership.example.invalid", "member", "client-a", "client")
	if err != nil || stamp == "" || stamp != membershipStamp(t, pool) {
		t.Fatal("transaction reader did not return the exact current incarnation", err)
	}
	for _, binding := range []struct{ subject, client, role string }{
		{"member", "other-client", "client"}, {"member", "client-a", "operator"}, {"absent", "client-a", "client"},
	} {
		if _, err := readMembershipIncarnation(ctx, tx, "https://membership.example.invalid", binding.subject, binding.client, binding.role); err == nil {
			t.Fatal("foreign, absent or wrong-role membership returned approval evidence")
		}
	}
	other := beginFixtureTransaction(ctx, t, pool)
	if _, err := other.Exec(ctx, `SET LOCAL lock_timeout='50ms'`); err != nil {
		t.Fatal(err)
	}
	_, err = other.Exec(ctx, `UPDATE portal_memberships SET revoked=true`)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "55P03" {
		t.Fatal("reader did not retain its caller's membership lock", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("reader consumed its caller's transaction", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_memberships SET revoked=true`); err != nil {
		t.Fatal("caller commit did not release the membership lock", err)
	}
	closed := beginFixtureTransaction(ctx, t, pool)
	if _, err := readMembershipIncarnation(ctx, closed, "https://membership.example.invalid", "member", "client-a", "client"); err == nil {
		t.Fatal("revoked membership returned an active incarnation")
	}
	if _, err := readMembershipIncarnation(ctx, tx, "https://membership.example.invalid", "member", "client-a", "client"); err == nil {
		t.Fatal("closed transaction returned membership evidence")
	}
}
