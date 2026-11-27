package identity

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestHostingPeriodStorageBindsTypedAcceptance(t *testing.T) {
	ctx := t.Context()
	migrator := migrationDB(t)
	if err := Migrate(ctx, migrator); err != nil {
		t.Fatal("migrate the disposable hosting fixture", err)
	}

	var installed bool
	if err := migrator.QueryRow(ctx, `SELECT to_regclass('public.portal_hosting_period_revisions') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal("read the migrated hosting period schema", err)
	}
	if !installed {
		t.Fatal("hosting service periods are missing from the migrated schema")
	}
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)

	var client, acceptance, contentHash, acceptor, operator string
	var proposalVersion int
	if err := migrator.QueryRow(ctx, `SELECT a.client_id,a.id,a.proposal_version,a.content_hash,a.client_incarnation::text,
 (SELECT incarnation::text FROM portal_memberships WHERE subject='proposal-operator' AND role='operator')
 FROM portal_proposal_acceptances a WHERE a.request_id=$1 AND a.client_incarnation IS NOT NULL`,
		proposalRequestID).Scan(&client, &acceptance, &proposalVersion, &contentHash, &acceptor, &operator); err != nil {
		t.Fatal("read the actual typed agreement and operator evidence", err)
	}

	const otherRequest = "BBBBBBBBBBBBBBBBBBBBBBBBBB"
	const otherClient = "other-hosting-workspace"
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief)
 VALUES($1,$2,$1,'website','Fictional other website','Hosting fixture')`, otherRequest, otherClient); err != nil {
		t.Fatal("create the second workspace's request", err)
	}
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_hosting_entitlements(request_id,client_id) VALUES($1,$2),($3,$4)`,
		proposalRequestID, client, otherRequest, otherClient); err != nil {
		t.Fatal("create the two scoped entitlement records", err)
	}

	insert := func(request, workspace, hash, period, start, end, actor string) error {
		_, err := migrator.Exec(ctx, `INSERT INTO portal_hosting_period_revisions
 (request_id,client_id,period_id,revision,acceptance_id,proposal_version,content_hash,starts_at,ends_at,scope,operator_incarnation)
 VALUES($1,$2,$3,1,$4,$5,$6,$7::text::timestamptz,$8::text::timestamptz,'Agreed fictional hosting scope',$9)`,
			request, workspace, period, acceptance, proposalVersion, hash, start, end, actor)
		return err
	}

	const start = "2027-01-01T00:00:00Z"
	const end = "2027-02-01T00:00:00Z"
	const goodPeriod = "CCCCCCCCCCCCCCCCCCCCCCCCCC"
	if err := insert(proposalRequestID, client, contentHash, goodPeriod, start, end, operator); err != nil {
		t.Fatal("record the period from the matching accepted software proposal", err)
	}
	for _, control := range []struct {
		name, request, workspace, hash, from, until, actor, code string
	}{
		{"foreign acceptance", otherRequest, otherClient, contentHash, start, end, operator, "23503"},
		{"changed accepted bytes", proposalRequestID, client, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", start, end, operator, "23503"},
		{"empty agreement interval", proposalRequestID, client, contentHash, start, start, operator, "23514"},
		{"infinite end", proposalRequestID, client, contentHash, start, "infinity", operator, "23514"},
		{"client membership as operator evidence", proposalRequestID, client, contentHash, start, end, acceptor, "23514"},
	} {
		t.Run(control.name, func(t *testing.T) {
			err := insert(control.request, control.workspace, control.hash, "DDDDDDDDDDDDDDDDDDDDDDDDDD", control.from, control.until, control.actor)
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != control.code {
				t.Fatalf("expected SQL state %s, got %v", control.code, err)
			}
		})
	}
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_hosting_period_heads(request_id,client_id,period_id,revision) VALUES($1,$2,$3,1)`,
		proposalRequestID, client, goodPeriod); err != nil {
		t.Fatal("retain the matching accepted revision as the period head", err)
	}
	_, err := migrator.Exec(ctx, `INSERT INTO portal_hosting_period_heads(request_id,client_id,period_id,revision) VALUES($1,$2,$3,1)`,
		otherRequest, otherClient, goodPeriod)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("foreign workspace adopted a period revision: %v", err)
	}

	if _, err := migrator.Exec(ctx, `UPDATE portal_memberships SET revoked=true WHERE incarnation=$1`, operator); err != nil {
		t.Fatal("revoke the original operator membership", err)
	}
	err = insert(proposalRequestID, client, contentHash, "EEEEEEEEEEEEEEEEEEEEEEEEEE", start, end, operator)
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("revoked operator membership wrote a revision: %v", err)
	}

	// The initial storage migration remains dormant. It confers no runtime
	// access before the authorized operator workflow supplies its own gate.
	for _, table := range []string{"portal_hosting_entitlements", "portal_hosting_period_revisions", "portal_hosting_period_heads"} {
		var granted bool
		if err := b.pool.QueryRow(ctx, `SELECT has_table_privilege($1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER,MAINTAIN')
 OR has_any_column_privilege($1,'SELECT,INSERT,UPDATE,REFERENCES')`, table).Scan(&granted); err != nil || granted {
			t.Fatalf("the dormant table %s has unexpected runtime access: %t, %v", table, granted, err)
		}
	}
}
