package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionInsertPause stops the actual maintained store immediately before its
// authenticated INSERT. It changes neither the codec nor database result.
type sessionInsertPause struct {
	started          atomic.Bool
	blocked, release chan struct{}
}

func (p *sessionInsertPause) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(query.SQL, "INSERT INTO portal_sessions") || len(query.Args) < 2 {
		return ctx
	}
	data, ok := query.Args[1].([]byte)
	if !ok {
		return ctx
	}
	_, values, err := (scs.GobCodec{}).Decode(data)
	if err == nil && values["subject"] == customerSubject && p.started.CompareAndSwap(false, true) {
		close(p.blocked)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	return ctx
}

func (*sessionInsertPause) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TestLateSessionCommitCannotRestoreRevokedMembership catches a callback whose
// old authenticated payload reaches SCS after deactivation AND reactivation.
func TestLateSessionCommitCannotRestoreRevokedMembership(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	operatorClient, csrf := b.client, b.csrf("/portal/")
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{change: func(context.Context, string, bool) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	pause := &sessionInsertPause{blocked: make(chan struct{}), release: make(chan struct{})}
	config := b.pool.Config()
	config.ConnConfig.Tracer = pause
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	b.svc.pool = pool
	b.mu.Lock()
	b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
	b.mu.Unlock()
	customerClient := *operatorClient
	customerClient.Jar, _ = cookiejar.New(nil)
	b.client = &customerClient
	b.as(customerSubject)
	callback := b.startInvited(customerSubject, "client-a", "client")
	resume := onceRelease(pause.release)
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.finish(callback)
	}()
	t.Cleanup(func() {
		resume()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("delayed customer callback did not finish")
		}
	})
	select {
	case <-pause.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not reach authenticated session persistence")
	}
	for _, action := range []string{"deactivate", "reactivate"} {
		if status := memberClientRequest(t, b, operatorClient, csrf, action); status != http.StatusOK {
			t.Fatal("member transition failed while the old callback was paused", action, status)
		}
	}
	resume()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old callback did not return")
	}
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status == http.StatusOK {
		t.Fatal("a session committed after revocation regained private access")
	}
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK {
		t.Fatal("a genuinely new sign-in could not use the reactivated membership", status)
	}
}

// memberClientRequest retains an independent operator cookie jar while a
// customer's callback is running through the same real HTTP server.
func memberClientRequest(t *testing.T, b *browserFixture, client *http.Client, csrf, action string) int {
	t.Helper()
	form := url.Values{"workspace": {"client-a"}, "subject": {customerSubject}, "csrf_token": {csrf}}
	req, err := http.NewRequestWithContext(t.Context(), "POST", b.site.URL+operatorAPIRoot+"/members/"+action, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", b.site.URL)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, res.Body)
	closeErr := res.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("member action response cleanup failed", readErr, closeErr)
	}
	return res.StatusCode
}

// TestUnboundAuthenticatedSessionRequiresNewSignIn catches legacy session
// payloads being admitted without a server-issued membership incarnation.
func TestUnboundAuthenticatedSessionRequiresNewSignIn(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.startInvited("client-a", "client-a", "client"))
	key := base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
	var data []byte
	if err := b.pool.QueryRow(t.Context(), `SELECT data FROM portal_sessions WHERE token=$1`, key).Scan(&data); err != nil {
		t.Fatal(err)
	}
	codec := scs.GobCodec{}
	deadline, values, err := codec.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	delete(values, "membership_incarnation")
	legacy, err := codec.Encode(deadline, values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_sessions SET data=$2 WHERE token=$1`, key, legacy); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status == http.StatusOK {
		t.Fatal("an unbound authenticated session retained private access")
	}
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK {
		t.Fatal("legacy-session refusal prevented a new sign-in", status)
	}
}

// TestOperatorSessionIncarnationIsRecheckedAfterWait catches a private response
// that outlives revocation/reactivation while the opaque session row stays put.
func TestOperatorSessionIncarnationIsRecheckedAfterWait(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("pending-customer", "epoch-private", "client")
	pause := installCompletedRead(t, b, "FROM inventory i LEFT JOIN")
	b.mu.Lock()
	b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
	b.mu.Unlock()
	resume := onceRelease(pause.release)
	done := make(chan struct{})
	result := make(chan contactResponse, 1)
	go func() {
		defer close(done)
		status, body, _ := b.request("GET", operatorAPIRoot+"/workspaces", nil, "")
		result <- contactResponse{status, body}
	}()
	retainOperatorWorker(t, resume, done)
	select {
	case <-pause.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("operator inventory missed its final authority checkpoint")
	}
	for range 2 {
		if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=NOT revoked WHERE subject=$1`, operatorSubject); err != nil {
			t.Fatal(err)
		}
	}
	resume()
	got := <-result
	if got.status != http.StatusServiceUnavailable || strings.Contains(got.body, "epoch-private") {
		t.Fatal("a superseded membership released private inventory", got.status)
	}
}

// TestBoundMembershipCannotWriteAfterReactivation catches a write admitted by
// an earlier membership check but waiting before its final row lock.
func TestBoundMembershipCannotWriteAfterReactivation(t *testing.T) {
	cases := []struct {
		name, table string
		write       func(*browserFixture, Principal) error
	}{
		{"contact", "portal_contacts", func(b *browserFixture, p Principal) error {
			return b.svc.saveContact(t.Context(), p, contactData{Name: "Current customer", Email: "customer@example.test"})
		}},
		{"request", "portal_requests", func(b *browserFixture, p Principal) error {
			_, err := b.svc.persistRequest(t.Context(), p, projectRequest{Key: secret(), Product: "website", Title: "Current project", Brief: "Fictional request"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBrowserFixture(t)
			setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests"}, []any{true, true})
			p := enroll(t, b.svc, b.f, "epoch-writer", "client-a", "client")
			pause := installCompletedRead(t, b, "SELECT client_id,role,incarnation::text FROM portal_memberships")
			resume := onceRelease(pause.release)
			done := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				defer close(done)
				result <- tc.write(b, p)
			}()
			t.Cleanup(func() {
				resume()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("membership-bound writer did not finish")
				}
			})
			select {
			case <-pause.blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("writer missed its initial membership check")
			}
			for range 2 {
				if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=NOT revoked WHERE subject='epoch-writer'`); err != nil {
					t.Fatal(err)
				}
			}
			resume()
			if err := <-result; !errors.Is(err, ErrDenied) {
				t.Fatal("a superseded membership committed private data", err)
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+tc.table).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale writer left private records", count, err)
			}
			attempt, err := b.svc.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			current, err := b.svc.Complete(t.Context(), attempt.State, attempt.BrowserBinding, b.f.token(t, "epoch-writer", attempt.Nonce, nil))
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.write(b, current); err != nil {
				t.Fatal("a newly authenticated membership lost its write", err)
			}
		})
	}
}
