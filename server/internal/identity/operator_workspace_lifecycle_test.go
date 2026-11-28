package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func operatorWithWorkspaceCustomer(t *testing.T) *browserFixture {
	t.Helper()
	b := newOperatorBrowserFixture(t)
	b.invite(customerSubject, "fixture-workspace", "client")
	b.signOut()
	b.as(customerSubject)
	b.finish(b.start())
	b.signOut()
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	return b
}

func TestOperatorRenamesWorkspaceAndRemovesItsMember(t *testing.T) {
	b := operatorWithWorkspaceCustomer(t)
	token := b.csrf("/portal/")

	rename := url.Values{"workspace": {"fixture-workspace"}, "name": {"North Lab"}, "csrf_token": {token}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/rename", rename, b.site.URL); status != http.StatusOK {
		t.Fatalf("rename workspace: status %d", status)
	}
	if status, body, _ := b.request("GET", "/portal/operator/customers", nil, ""); status != http.StatusOK || !strings.Contains(body, "North Lab") {
		t.Fatalf("customer directory did not show the new workspace name: status %d", status)
	}

	remove := url.Values{"workspace": {"another-workspace"}, "subject": {customerSubject}, "csrf_token": {token}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", remove, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("removing a member from the wrong workspace returned %d", status)
	}
	remove.Set("workspace", "fixture-workspace")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", remove, b.site.URL); status != http.StatusOK {
		t.Fatalf("remove member: status %d", status)
	}
	var removed, deadline bool
	if err := b.pool.QueryRow(t.Context(), `SELECT
 NOT EXISTS (SELECT 1 FROM portal_memberships WHERE issuer=$1 AND subject=$2),
 EXISTS (SELECT 1 FROM portal_customer_account_closures c
 JOIN portal_membership_transitions h ON h.incarnation=c.membership_event
 WHERE h.issuer=$1 AND h.subject=$2 AND h.client_id='fixture-workspace' AND h.kind='delete'
 AND c.closed_at=h.recorded_at AND c.remove_by=c.closed_at+interval '720 hours')`,
		b.f.issuer, customerSubject).Scan(&removed, &deadline); err != nil || !removed || !deadline {
		t.Fatalf("member removal did not commit its exact account-removal deadline: %v", err)
	}
	assertCustomerCanSignInWithoutWorkspace(t, b, customerSubject)
}

func TestOperatorMemberRemovalRequiresItsAccountDeadline(t *testing.T) {
	b := operatorWithWorkspaceCustomer(t)
	ctx := t.Context()
	db := migrationDB(t)
	if _, err := db.Exec(ctx, `REVOKE INSERT ON portal_customer_account_closures FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(ctx, `GRANT INSERT ON portal_customer_account_closures TO portal_runtime`); err != nil {
			t.Error(err)
		}
	}()
	form := url.Values{"workspace": {"fixture-workspace"}, "subject": {customerSubject}, "csrf_token": {b.csrf("/portal/")}}
	status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", form, b.site.URL)
	var active bool
	if err := b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 AND client_id='fixture-workspace' AND NOT revoked)`,
		b.f.issuer, customerSubject).Scan(&active); err != nil || !active || status != http.StatusServiceUnavailable {
		t.Fatalf("the member removal could commit without its account record: status=%d error=%v", status, err)
	}
}

func TestCustomerAccountDeadlineRejectsAPreviousRemoval(t *testing.T) {
	b := operatorWithWorkspaceCustomer(t)
	ctx := t.Context()
	if _, err := b.pool.Exec(ctx, `DELETE FROM portal_memberships WHERE issuer=$1 AND subject=$2`,
		b.f.issuer, customerSubject); err != nil {
		t.Fatal(err)
	}
	_, err := b.pool.Exec(ctx, `INSERT INTO portal_customer_account_closures(membership_event,closed_at,remove_by)
 SELECT incarnation,recorded_at,recorded_at+interval '720 hours' FROM portal_membership_transitions
 WHERE issuer=$1 AND subject=$2 AND kind='delete'`, b.f.issuer, customerSubject)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("the existing deletion could be admitted as a new removal: %v", err)
	}
}

func TestOperatorDeletesWorkspaceAndClosesItsInvitations(t *testing.T) {
	b := operatorWithWorkspaceCustomer(t)
	b.invite("waiting-client", "fixture-workspace", "client")
	b.invite("unrelated-client", "other-workspace", "client")
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES
 ('AAAAAAAAAAAAAAAAAAAAAAAAAA','fixture-workspace','AAAAAAAAAAAAAAAAAAAAAAAAAA','website','Fixture work','Fixture brief')`); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"workspace": {"fixture-workspace"}, "confirm": {"wrong workspace"}, "csrf_token": {b.csrf("/portal/")}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", form, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("deletion without the exact name returned %d", status)
	}
	form.Set("confirm", "fixture-workspace")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", form, b.site.URL); status != http.StatusOK {
		t.Fatalf("delete workspace: status %d", status)
	}
	if status, body, _ := b.request("GET", "/portal/operator/customers", nil, ""); status != http.StatusOK ||
		strings.Contains(body, "fixture-workspace") || !strings.Contains(body, "other-workspace") {
		t.Fatalf("workspace deletion affected the wrong directory entries: status %d", status)
	}
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, "new-client", "fixture-workspace", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
		t.Fatalf("new invitation can restore a deleted workspace: %v", err)
	}
	var retained bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_requests WHERE client_id='fixture-workspace')`).Scan(&retained); err != nil || !retained {
		t.Fatalf("deletion erased retained business records: %v", err)
	}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", form, b.site.URL); status != http.StatusOK {
		t.Fatalf("retry workspace closure: status %d", status)
	}
	var exact, total bool
	if err := b.pool.QueryRow(t.Context(), `SELECT
 EXISTS(SELECT 1 FROM portal_customer_account_closures q
 JOIN portal_membership_transitions h ON h.incarnation=q.membership_event
 JOIN portal_workspace_settings w ON w.workspace_id=h.client_id
 WHERE h.issuer=$1 AND h.subject=$2 AND h.kind='delete' AND h.client_id='fixture-workspace'
 AND q.closed_at=w.closed_at AND q.remove_by=q.closed_at+interval '720 hours'),
 (SELECT count(*)=1 FROM portal_customer_account_closures)`, b.f.issuer, customerSubject).Scan(&exact, &total); err != nil || !exact || !total {
		t.Fatalf("workspace closure did not record exactly one deadline for its closed sign-in account: %v", err)
	}
	assertCustomerCanSignInWithoutWorkspace(t, b, customerSubject)
	b.signOut()
	b.as("waiting-client")
	b.refused(b.start())
	b.as("unrelated-client")
	b.finish(b.start())
}

func assertCustomerCanSignInWithoutWorkspace(t *testing.T, b *browserFixture, subject string) {
	t.Helper()
	b.signOut()
	b.as(subject)
	b.finish(b.start())
	if status, body, _ := b.request("GET", "/portal/?lang=en", nil, ""); status != http.StatusOK || !strings.Contains(body, "No workspaces yet") {
		t.Fatalf("ending workspace access left the customer unable to sign in: %d", status)
	}
}

func markLegacyMembershipRevoked(t *testing.T, b *browserFixture) {
	t.Helper()
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE subject=$1`, customerSubject); err != nil {
		t.Fatal(err)
	}
}

func TestRemovingLegacyBlockedMemberRestoresOnlySignIn(t *testing.T) {
	longSubject := customeridentity.DexSubject(strings.Repeat("a", 160), customeridentity.ZitadelConnectorID)
	for _, tc := range []struct {
		name, subject, auditTarget string
	}{
		{"plain target", customerSubject, customerSubject},
		{"hashed target", longSubject, "sha256:" + hex.EncodeToString(digest(longSubject))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var restored atomic.Bool
			b := newOperatorBrowserFixture(t)
			directory := &managementDirectory{change: func(_ context.Context, user string, active bool) error {
				expected := customeridentity.DexUser(tc.subject, customeridentity.ZitadelConnectorID)
				restored.Store(active && user == expected)
				return nil
			}}
			if err := b.svc.LinkCustomerDirectory(directory); err != nil {
				t.Fatal(err)
			}
			_, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role,revoked)
 VALUES ($1,$2,'client-a','client',true)`, b.f.issuer, tc.subject)
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.pool.Exec(t.Context(), `INSERT INTO portal_operator_audit(actor_issuer,actor_subject,actor_kind,action,target,outcome)
 VALUES ($1,$2,'operator','members.deactivate',$3,'allowed')`, b.f.issuer, operatorSubject, tc.auditTarget)
			if err != nil {
				t.Fatal(err)
			}
			form := operatorMembershipForm(b, tc.subject, "client-a")
			if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", form, b.site.URL); status != http.StatusOK || !restored.Load() {
				t.Fatalf("removing a legacy-blocked customer did not restore provider sign-in: %d", status)
			}
			assertCustomerCanSignInWithoutWorkspace(t, b, tc.subject)
		})
	}
}

func TestConcurrentWorkspaceClosuresRecordFinalCustomerRemoval(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	subject := customeridentity.DexSubject("concurrent-close", customeridentity.ZitadelConnectorID)
	_ = enroll(t, svc, f, subject, "first", "client")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES($1,$2,'second','client')`, f.issuer, subject); err != nil {
		t.Fatal(err)
	}
	remove := func(workspace string) (pgx.Tx, []string) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		var incarnation string
		if err := tx.QueryRow(ctx, `DELETE FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND client_id=$3
 RETURNING incarnation::text`, f.issuer, subject, workspace).Scan(&incarnation); err != nil {
			t.Fatal(err)
		}
		return tx, []string{incarnation}
	}
	// The last removal began first, so its closure event is stamped earlier.
	second, secondEvents := remove("second")
	first, firstEvents := remove("first")
	if err := recordClosedCustomerAccounts(ctx, first, f.issuer, "first", firstEvents); err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan error, 1)
	go func() {
		secondResult <- recordClosedCustomerAccounts(ctx, second, f.issuer, "second", secondEvents)
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		select {
		case err := <-secondResult:
			t.Fatalf("second removal recorded its decision before the first committed: %v", err)
		case <-ctx.Done():
			t.Fatal("second removal did not wait for the first")
		case <-tick.C:
			var waiting bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=$1::oid AND NOT granted)`, customerClosureLock).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break waiting
			}
		}
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	if err := second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var workspace string
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(min(h.client_id),'') FROM portal_customer_account_closures c
 JOIN portal_membership_transitions h ON h.incarnation=c.membership_event
 WHERE h.issuer=$1 AND h.subject=$2`, f.issuer, subject).Scan(&count, &workspace); err != nil || count != 1 || workspace != "second" {
		t.Fatalf("expected one removal deadline from the final membership, got %d for %q: %v", count, workspace, err)
	}
	rows, err := pool.Query(ctx, customerAccountAccessSQL, f.issuer, []string{subject}, operatorWorkspace, previewWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	access, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[customerWorkspaceAccess])
	if err != nil || !access.Former || access.Deadline == nil {
		t.Fatalf("the directory did not show the recorded retention deadline: %v", err)
	}
}

func TestCustomerDeadlineSurvivesAnInvitationWithoutMembership(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	for _, ending := range []string{"expires", "revoked", "workspace-closed"} {
		t.Run(ending, func(t *testing.T) {
			id := "pending-" + ending
			subject := customeridentity.DexSubject(id, customeridentity.ZitadelConnectorID)
			memberWorkspace := id + "-member"
			inviteWorkspace := id + "-invite"
			if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES($1,$2,$3,'client')`, b.f.issuer, subject, memberWorkspace); err != nil {
				t.Fatal(err)
			}
			b.invite(subject, inviteWorkspace, "client")
			form := operatorMembershipForm(b, subject, memberWorkspace)
			if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", form, b.site.URL); status != http.StatusOK {
				t.Fatalf("removing the last membership returned %d", status)
			}
			readAccount := func() customerAccountRow {
				t.Helper()
				rows, err := b.pool.Query(t.Context(), customerAccountAccessSQL, b.f.issuer, []string{subject}, operatorWorkspace, previewWorkspace)
				if err != nil {
					t.Fatal(err)
				}
				access, err := pgx.CollectRows(rows, pgx.RowToStructByPos[customerWorkspaceAccess])
				if err != nil || len(access) == 0 {
					t.Fatalf("could not read the customer directory: %v", err)
				}
				return accountRow(customeridentity.Account{ID: id}, access, "en")
			}
			if account := readAccount(); account.Status != "invited" || account.Deadline != nil {
				t.Fatalf("a pending invitation should take precedence in the directory, got %s", account.Status)
			}
			switch ending {
			case "expires":
				_, err := b.pool.Exec(t.Context(), `UPDATE portal_invitations SET expires_at=now()-interval '1 minute'
 WHERE issuer=$1 AND subject=$2 AND client_id=$3`, b.f.issuer, subject, inviteWorkspace)
				if err != nil {
					t.Fatal(err)
				}
			case "revoked":
				var expires time.Time
				err := b.pool.QueryRow(t.Context(), `SELECT expires_at FROM portal_invitations
 WHERE issuer=$1 AND subject=$2 AND client_id=$3`, b.f.issuer, subject, inviteWorkspace).Scan(&expires)
				if err != nil {
					t.Fatal(err)
				}
				revoke := url.Values{"workspace": {inviteWorkspace}, "subject": {subject},
					"expires": {expires.Format(time.RFC3339Nano)}, "csrf_token": form["csrf_token"]}
				if status, _, _ := b.request("POST", operatorAPIRoot+"/invitations/revoke?focus=workspace", revoke, b.site.URL); status != http.StatusOK {
					t.Fatalf("revoking the invitation returned %d", status)
				}
			case "workspace-closed":
				close := url.Values{"workspace": {inviteWorkspace}, "confirm": {inviteWorkspace}, "csrf_token": form["csrf_token"]}
				if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", close, b.site.URL); status != http.StatusOK {
					t.Fatalf("closing the workspace with the pending invitation returned %d", status)
				}
			}
			if account := readAccount(); account.Status != "former" || account.Deadline == nil {
				t.Fatalf("the former customer's retention deadline was lost after %s", ending)
			}
		})
	}
}

func TestOperatorWorkspaceDeletionSerializesWithInvitation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("existing-customer", "fixture-workspace", "client")
	form := url.Values{"workspace": {"fixture-workspace"}, "confirm": {"fixture-workspace"}, "csrf_token": {b.csrf("/portal/")}}
	pause := installCompletedRead(t, b, "INSERT INTO portal_invitations")
	resume := onceRelease(pause.release)
	inviteDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	defer resume()
	go func() {
		inviteDone <- b.svc.Invite(ctx, Invitation{b.f.issuer, "concurrent-customer", "fixture-workspace", "client", time.Now().Add(time.Hour)})
	}()
	select {
	case <-pause.blocked:
	case <-ctx.Done():
		t.Fatal("invitation did not reach the database")
	}
	deleted := make(chan int, 1)
	go func() {
		status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", form, b.site.URL)
		deleted <- status
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := b.pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=$1::oid AND NOT granted)`, workspaceLock).Scan(&waiting); err != nil {
			t.Fatal("workspace deletion did not wait for the invitation", err)
		}
		if waiting {
			break
		}
		select {
		case status := <-deleted:
			t.Fatalf("workspace deletion bypassed the invitation lock: %d", status)
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("workspace deletion did not reach the invitation lock")
		}
	}
	resume()
	select {
	case err := <-inviteDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("invitation did not finish")
	}
	select {
	case status := <-deleted:
		if status != http.StatusOK {
			t.Fatalf("delete workspace: status %d", status)
		}
	case <-ctx.Done():
		t.Fatal("workspace deletion did not finish")
	}
	b.signOut()
	b.as("concurrent-customer")
	b.refused(b.start())
}

func TestOperatorWorkspaceDeletionPreservesActiveBillingAccess(t *testing.T) {
	b, _ := subscribedCardOrder(t)
	b.signOut()
	configureOperatorBrowser(t, b)
	form := url.Values{"workspace": {"private-client-key"}, "confirm": {"private-client-key"}, "csrf_token": {b.csrf("/portal/")}}
	status, body, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", form, b.site.URL)
	if status != http.StatusConflict || !strings.Contains(body, "active billing") {
		t.Fatalf("delete workspace with a subscription: status %d", status)
	}
	var staysOpen bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(
 SELECT 1 FROM portal_memberships WHERE client_id='private-client-key' AND NOT revoked)`).Scan(&staysOpen); err != nil || !staysOpen {
		t.Fatalf("refused workspace deletion ended client access: %v", err)
	}
}
