package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/devantler-tech/business-site/server/internal/zitadel"
)

func TestOperatorWritesStopBeforeUnrecordedProviderEffects(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	creates := inviteDirectory(t, b, false, false, nil)
	form := invitationForm(b)
	restoreAudit := pauseOperatorAudit(t)
	status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL)
	count := b.invitationCount()
	if status != 503 || creates.Load() != 0 || count != 0 {
		t.Fatalf("unrecorded write had effects: status=%d providerCreates=%d invitations=%d", status, creates.Load(), count)
	}
	restoreAudit()
	status, _, _ = b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL)
	count = b.invitationCount()
	if status != 200 || creates.Load() != 1 || count != 1 {
		t.Fatalf("recorded write positive control failed: status=%d providerCreates=%d invitations=%d", status, creates.Load(), count)
	}
}

func inviteDirectory(t *testing.T, b *browserFixture, existing, fail bool, wait func()) *atomic.Int32 {
	t.Helper()
	creates := new(atomic.Int32)
	matches := []map[string]any{}
	if existing {
		matches = append(matches, map[string]any{"userId": "customer-42", "details": map[string]string{"resourceOwner": "customers"}, "human": map[string]any{"email": map[string]string{"email": "ada@example.test"}}})
	}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-zitadel-orgid") != "customers" {
			t.Error("provider organization missing")
			w.WriteHeader(403)
			return
		}
		if fail {
			w.WriteHeader(503)
			return
		}
		if wait != nil {
			wait()
		}
		switch r.URL.Path {
		case "/v2/users":
			if err := json.NewEncoder(w).Encode(map[string]any{"result": matches}); err != nil {
				t.Error(err)
			}
		case "/management/v1/users/human":
			creates.Add(1)
			_, _ = fmt.Fprint(w, `{"userId":"customer-42","details":{"resourceOwner":"customers"}}`)
		default:
			t.Error("unexpected provider route")
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	c, err := zitadel.New(s.URL, "customers", func() (string, error) { return "fixture-token", nil }, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkCustomerDirectory(c); err != nil {
		t.Fatal(err)
	}
	return creates
}

func invitationForm(b *browserFixture) url.Values {
	return url.Values{"name": {"Ada Lovelace"}, "email": {"ada@example.test"}, "workspace": {"client-b"}, "csrf_token": {b.csrf("/portal/operator/invitations/new")}}
}

func TestOperatorInvitesCustomerAndFirstSignInNeedsNoCode(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			b := newOperatorBrowserFixture(t)
			creates := inviteDirectory(t, b, existing, false, nil)
			status, body, _ := b.request("POST", operatorAPIRoot+"/invitations", invitationForm(b), b.site.URL)
			if status != 200 || !strings.Contains(body, `"workspace":"client-b"`) {
				t.Fatalf("invite failed: status=%d", status)
			}
			if (existing && creates.Load() != 0) || (!existing && creates.Load() != 1) {
				t.Fatal("customer identity was duplicated")
			}
			if got := b.auditRows(); got[len(got)-1] != "operator invitations.create allowed" {
				t.Fatalf("invitation was not audited: %q", got)
			}
			b.signOut()
			b.as(customeridentity.DexSubject("customer-42", customeridentity.ZitadelConnectorID))
			b.finish(b.start())
			status, body, _ = b.request("GET", "/portal/", nil, "")
			if status != 200 || !strings.Contains(body, "client-b") {
				t.Fatalf("first sign-in did not accept bound invitation: %d", status)
			}
		})
	}
}

func TestOperatorInviteProviderErrorsRecordNoInvitation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	inviteDirectory(t, b, false, true, nil)
	status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", invitationForm(b), b.site.URL)
	count := b.invitationCount()
	if status != 503 || count != 0 {
		t.Fatalf("provider error persisted an invitation: status=%d rows=%d", status, count)
	}
}

func TestConcurrentOperatorInvitesHaveOnePendingInvitation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	inviteDirectory(t, b, true, false, nil)
	form := invitationForm(b)
	migrator := migrationDB(t)
	holder := beginFixtureTransaction(t.Context(), t, migrator)
	subject := customeridentity.DexSubject("customer-42", customeridentity.ZitadelConnectorID)
	if err := lockIdentity(t.Context(), holder, b.f.issuer, subject); err != nil {
		t.Fatal(err)
	}
	resume := sync.OnceFunc(func() {
		fresh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = holder.Rollback(fresh)
	})
	statuses := make(chan int, 2)
	done := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			s, _, _ := b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL)
			statuses <- s
		}()
	}
	go func() { workers.Wait(); close(done) }()
	retainOperatorWorker(t, resume, done)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	waitForFixtureDatabaseLock(ctx, t, migrator, `SELECT count(*)=2 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=$1::oid AND objid=hashtext($2 || ' ' || $3)::oid AND objsubid=2 AND NOT granted`, identityLock, b.f.issuer, subject)
	resume()
	a, c := <-statuses, <-statuses
	count := b.invitationCount()
	if count != 1 || (a != 200 && c != 200) || (a == 200 && c == 200) {
		t.Fatalf("concurrent invitation was not single-use: statuses=%d,%d rows=%d", a, c, count)
	}
}

func TestOperatorInviteRequiresCurrentAuthorityAfterProviderWait(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	var revoked atomic.Bool
	inviteDirectory(t, b, true, false, func() {
		if revoked.CompareAndSwap(false, true) {
			if _, err := b.pool.Exec(t.Context(), `DELETE FROM portal_sessions`); err != nil {
				t.Error(err)
			}
		}
	})
	status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", invitationForm(b), b.site.URL)
	count := b.invitationCount()
	if status != 503 || count != 0 {
		t.Fatalf("expired operator persisted an invitation: status=%d rows=%d", status, count)
	}
}

func TestOperatorInviteValidatesBeforeProviderAndRetainsCSRF(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	creates := inviteDirectory(t, b, false, false, nil)
	for field, value := range map[string]string{"name": "Ada", "email": "Ada <ada@example.test>", "workspace": operatorWorkspace, "csrf_token": "invalid"} {
		form := invitationForm(b)
		form.Set(field, value)
		status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL)
		want := 400
		if field == "csrf_token" {
			want = 403
		}
		if status != want || creates.Load() != 0 {
			t.Fatalf("invalid %s reached provider: status=%d", field, status)
		}
	}
}

func TestCustomerCannotInviteAnotherWorkspace(t *testing.T) {
	b := newBrowserFixture(t)
	creates := inviteDirectory(t, b, false, false, nil)
	b.finish(b.startInvited("client-a", "client-a", "client"))
	form := url.Values{"name": {"Ada Lovelace"}, "email": {"ada@example.test"}, "workspace": {"client-b"}, "csrf_token": {b.csrf("/portal/contact")}}
	status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL)
	if status != 403 || creates.Load() != 0 {
		t.Fatalf("customer invited another workspace: %d", status)
	}
	if got := b.auditRows(); got[len(got)-1] != "other invitations.create refused" {
		t.Fatal("refusal was not audited")
	}
}
