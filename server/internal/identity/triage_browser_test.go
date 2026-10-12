package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// triageFlags keeps each release boundary independently controllable; changing
// one gate must not silently replace the other two with absent flags.
func triageFlags(t *testing.T, b *browserFixture, identity, requests, triage any) {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{}
	for name, value := range map[string]any{"portal-identity": identity, "portal-requests": requests, "portal-triage": triage} {
		if value != nil {
			flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": value}}
		}
	}
	if err := b.flags.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

// SQL fixtures use the existing immutable request table and no new authority.
func seedTriageRequest(t *testing.T, b *browserFixture, id, title string, submitted time.Time) {
	t.Helper()
	_, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief,submitted_at) VALUES($1,'private-client-key',$1,'app',$2,'<script>hostile-brief</script>\nhttps://example.invalid/inert', $3)`, id, title, submitted)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperatorInboxDetailDurabilityAndClientDenial(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	id := "AAAAAAAAAAAAAAAAAAAAAAAAAA"
	seedTriageRequest(t, b, id, "Fictional private request", time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC))
	paths := []string{"/portal/operator/requests", "/portal/operator/requests/" + id, "/portal/operator/requests/BBBBBBBBBBBBBBBBBBBBBBBBBB"}
	for _, subject := range []string{"anonymous", "client-a", "client-b"} {
		if subject != "anonymous" {
			triageSignIn(t, b, subject, subject, "client")
		}
		for _, path := range paths {
			status, body, _ := b.request("GET", path+"?role=operator&client_id=private-client-key", nil, "")
			if status != 403 || strings.Contains(body, "Fictional private request") || strings.Contains(body, "hostile-brief") {
				t.Fatalf("%s reads operator data at %s: %d", subject, path, status)
			}
		}
	}
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	pool, err := pgxpool.New(context.Background(), b.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	b.mu.Lock()
	b.handler = browserHandler(t, New(pool, b.f.broker, b.flags.NewClient()), b.site.URL+"/portal/callback")
	b.mu.Unlock()
	status, body, headers := b.request("GET", paths[1]+"?lang=da", nil, "")
	if status != 200 || !strings.Contains(body, "Ikke godkendt til arbejde") || !strings.Contains(body, "Fictional private request") || !strings.Contains(body, "2026-01-02T03:04:05.123456Z") || !strings.Contains(body, "&lt;script&gt;hostile-brief&lt;/script&gt;") || strings.Contains(body, "<script>hostile") || strings.Contains(body, "private-client-key") || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("durable escaped Danish detail missing: %d %s", status, body)
	}
	if status, _, _ = b.request("GET", paths[2], nil, ""); status != 404 {
		t.Fatal("operator missing record is not 404")
	}
	if status, _, _ = b.request("GET", "/portal/requests/"+id, nil, ""); status != 403 {
		t.Fatal("operator obtained client receipt authority")
	}
	if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "operator"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if status, _, _ = b.request("GET", path, nil, ""); status != 403 {
			t.Fatal("revoked operator retains inbox authority")
		}
	}
}

func TestOperatorInboxFlagsAreIndependentAndFailClosed(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	for _, gate := range []string{"identity", "requests", "triage"} {
		for _, value := range []any{nil, false, "true"} {
			identity, requests, triage := any(true), any(true), any(true)
			switch gate {
			case "identity":
				identity = value
			case "requests":
				requests = value
			case "triage":
				triage = value
			}
			triageFlags(t, b, identity, requests, triage)
			for _, path := range []string{"/portal/operator/requests", "/portal/operator/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA"} {
				status, _, h := b.request("GET", path, nil, "")
				if status != 404 || len(h.Values("Set-Cookie")) != 0 {
					t.Fatalf("closed %s=%v exposes route/cookie: %d", gate, value, status)
				}
			}
			_, shell, _ := b.request("GET", "/portal/operator", nil, "")
			if strings.Contains(shell, `href="/portal/operator/requests`) {
				t.Fatal("closed gate leaves inbox link")
			}
		}
	}
}

func TestOperatorInboxStableBoundedPagination(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	tied := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	var ids []string
	for i := range 45 {
		id := strings.Repeat("A", 24) + string(alphabet[i/32]) + string(alphabet[i%32])
		ids = append(ids, id)
		seedTriageRequest(t, b, id, fmt.Sprintf("Fictional request %02d", i), tied)
	}
	// Expected ordering follows PostgreSQL's text collation, not insertion order.
	// Use the fixture IDs sorted independently below, retaining time precision.
	sort.Strings(ids)
	link := regexp.MustCompile(`href="/portal/operator/requests/([A-Z2-7]{26})\?lang=en"`)
	next := regexp.MustCompile(`href="([^"]+)" rel="next"`)
	path, seen, page := "/portal/operator/requests", []string{}, 0
	for {
		status, body, _ := b.request("GET", path, nil, "")
		if status != 200 {
			t.Fatalf("page %d=%d", page, status)
		}
		matches := link.FindAllStringSubmatch(body, -1)
		if len(matches) > 20 || (page < 2 && len(matches) != 20) {
			t.Fatalf("unbounded/wrong page size: %d", len(matches))
		}
		for _, match := range matches {
			seen = append(seen, match[1])
		}
		page++
		if page == 1 {
			seedTriageRequest(t, b, "ZZZZZZZZZZZZZZZZZZZZZZZZZZ", "Newer request", tied.Add(time.Microsecond))
		}
		m := next.FindStringSubmatch(body)
		if len(m) == 0 {
			break
		}
		if page > 3 {
			t.Fatal("pagination never terminates")
		}
		path = html.UnescapeString(m[1])
		_, translated, _ := b.request("GET", path, nil, "")
		if !strings.Contains(translated, "cursor=") || !strings.Contains(translated, "lang=da") {
			t.Fatal("language switch drops page cursor")
		}
	}
	ids = append(ids, "ZZZZZZZZZZZZZZZZZZZZZZZZZZ")
	if len(seen) != 46 || strings.Join(seen, ",") != strings.Join(ids, ",") || page != 3 {
		t.Fatalf("records duplicated/skipped/reordered across ties: %v", seen)
	}
}

func TestOperatorInboxRejectsMalformedCursorWithoutDisclosure(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	for _, query := range []string{"cursor=", "cursor=invalid", "cursor=a&cursor=b", "cursor=" + strings.Repeat("a", 400), "cursor=%ZZ"} {
		status, body, _ := b.request("GET", "/portal/operator/requests?"+query, nil, "")
		if status != 400 || strings.Contains(body, "SELECT") {
			t.Fatalf("bad cursor accepted/leaked: %s %d", query, status)
		}
	}
	for _, raw := range []string{"2|2026-01-02T03:04:05Z|AAAAAAAAAAAAAAAAAAAAAAAAAA", "1|bad-time|AAAAAAAAAAAAAAAAAAAAAAAAAA", "1|2026-01-02T03:04:05.123456789Z|AAAAAAAAAAAAAAAAAAAAAAAAAA", "1|2026-01-02T04:04:05+01:00|AAAAAAAAAAAAAAAAAAAAAAAAAA", "1|2026-01-02T03:04:05Z|../bad-id"} {
		query := base64.RawURLEncoding.EncodeToString([]byte(raw))
		if status, _, _ := b.request("GET", "/portal/operator/requests?cursor="+query, nil, ""); status != 400 {
			t.Fatalf("malformed cursor payload accepted: %q", raw)
		}
	}
}

func TestOperatorInboxDeniesDowngradeAndExpiredSession(t *testing.T) {
	for _, change := range []string{"downgrade", "expiry", "logout"} {
		t.Run(change, func(t *testing.T) {
			b := newBrowserFixture(t)
			triageFlags(t, b, true, true, true)
			triageSignIn(t, b, "operator", "operator-workspace", "operator")
			if status, _, _ := b.request("GET", "/portal/operator/requests", nil, ""); status != 200 {
				t.Fatal("operator baseline unavailable")
			}
			switch change {
			case "downgrade":
				if _, err := b.pool.Exec(b.f.ctx, `UPDATE portal_memberships SET role='client' WHERE subject='operator'`); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				if _, err := b.pool.Exec(b.f.ctx, `UPDATE portal_sessions SET expiry=now()-interval '1 minute'`); err != nil {
					t.Fatal(err)
				}
			case "logout":
				if status, _, _ := b.request("POST", "/portal/logout", url.Values{"csrf_token": {b.csrf("/portal/operator")}}, b.site.URL); status != 303 {
					t.Fatal("logout failed")
				}
			}
			for _, path := range []string{"/portal/operator/requests", "/portal/operator/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA"} {
				if status, _, _ := b.request("GET", path, nil, ""); status != 403 {
					t.Fatalf("%s retains inbox: %d", change, status)
				}
			}
		})
	}
}

func TestOperatorInboxStorageFailureIsNotEmptySuccess(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	migrator := migrationDB(t)
	if _, err := migrator.Exec(b.f.ctx, `REVOKE SELECT ON portal_requests FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := migrator.Exec(context.Background(), `GRANT SELECT ON portal_requests TO portal_runtime`); err != nil {
			t.Error(err)
		}
	})
	for _, path := range []string{"/portal/operator/requests", "/portal/operator/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		status, body, _ := b.request("GET", path, nil, "")
		if status != 503 || strings.Contains(body, "permission denied") || strings.Contains(body, "portal_requests") || strings.Contains(body, "No project requests yet") {
			t.Fatalf("storage failure masked or leaked: %d %s", status, body)
		}
	}
}

func TestOperatorInboxIndexMigrationPreservesRequests(t *testing.T) {
	b := newBrowserFixture(t)
	seedTriageRequest(t, b, "AAAAAAAAAAAAAAAAAAAAAAAAAA", "Durable request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	migrator := migrationDB(t)
	var indexed bool
	if err := migrator.QueryRow(b.f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE tablename='portal_requests' AND indexname='portal_requests_triage')`).Scan(&indexed); err != nil || !indexed {
		t.Fatal("global oldest-first inbox has no matching index", err)
	}
	var original string
	if err := migrator.QueryRow(b.f.ctx, `SELECT checksum FROM portal_schema_version WHERE version=5`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(b.f.ctx, `DROP INDEX portal_requests_triage; DELETE FROM portal_schema_version WHERE version=5`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(b.f.ctx, migrator); err != nil {
		t.Fatal(err)
	}
	var kept bool
	if err := migrator.QueryRow(b.f.ctx, `SELECT EXISTS(SELECT 1 FROM portal_requests WHERE id='AAAAAAAAAAAAAAAAAAAAAAAAAA' AND title='Durable request') AND EXISTS(SELECT 1 FROM portal_schema_version WHERE version=5 AND checksum=$1)`, original).Scan(&kept); err != nil || !kept {
		t.Fatal("index migration altered immutable data/checksum", err)
	}
}

// A real table lock pauses the data read after initial authorization. This
// catches removing the final guard, unlike changing authority before a request.
func TestOperatorInboxRejectsInFlightAuthorityClosure(t *testing.T) {
	for _, change := range []string{"flag", "revocation", "downgrade", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b := newBrowserFixture(t)
			triageFlags(t, b, true, true, true)
			p := enroll(t, b.svc, b.f, "operator", "operator-workspace", "operator")
			seedTriageRequest(t, b, "AAAAAAAAAAAAAAAAAAAAAAAAAA", "Private in-flight request", time.Now())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			migrator := migrationDB(t)
			var relation uint32
			if err := migrator.QueryRow(ctx, `SELECT 'portal_requests'::regclass::oid`).Scan(&relation); err != nil {
				t.Fatal(err)
			}
			tx, err := migrator.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Error(err)
				}
			}()
			if _, err := tx.Exec(ctx, `LOCK TABLE portal_requests IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			if change == "expiry" {
				p.expires = time.Now().Add(time.Second)
			}
			result := make(chan error, 1)
			go func() {
				_, err := b.svc.readTriage(ctx, p, "", triageCursor{})
				result <- err
			}()
			waitForTriageRead(t, ctx, migrator, relation)
			switch change {
			case "flag":
				triageFlags(t, b, true, true, false)
			case "revocation":
				if err := b.svc.Revoke(ctx, b.f.issuer, "operator"); err != nil {
					t.Fatal(err)
				}
			case "downgrade":
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE subject='operator'`); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				timer := time.NewTimer(time.Until(p.expires) + time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					t.Fatal("fixture expiry wait exceeded bound")
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("%s during data read was admitted: %v", change, err)
				}
			case <-ctx.Done():
				t.Fatal("fixture data read exceeded bound")
			}
		})
	}
}

func waitForTriageRead(t *testing.T, ctx context.Context, pool *pgxpool.Pool, relation uint32) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation=$1 AND mode='AccessShareLock' AND NOT granted)`, relation).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("fixture never reached the blocked data read")
		}
	}
}

func triageSignIn(t *testing.T, b *browserFixture, subject, client, role string) *http.Client {
	t.Helper()
	copy := *b.site.Client()
	var err error
	copy.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	b.client = &copy
	b.mu.Lock()
	b.subject = subject
	b.mu.Unlock()
	b.finish(b.start(b.invite(subject, client, role)))
	return &copy
}

// A missing operator route, a client-scoped aggregate query or raw brief HTML
// must fail this actual TLS/session/database flow, not a mocked handler.
func TestOperatorInboxReadsBothClientsWithoutApprovingWork(t *testing.T) {
	b := newBrowserFixture(t)
	triageFlags(t, b, true, true, true)
	for _, client := range []string{"client-a", "client-b"} {
		triageSignIn(t, b, client, client, "client")
		f := requestForm(t, b)
		f.Set("title", "Fictional "+client+" request")
		f.Set("brief", "<script>hostile-brief</script>\nA fictional need.")
		status, _, _ := b.request("POST", "/portal/requests", f, b.site.URL)
		if status != 303 {
			t.Fatalf("client submission=%d want 303", status)
		}
	}
	triageSignIn(t, b, "operator", "operator-workspace", "operator")
	status, body, headers := b.request("GET", "/portal/operator/requests", nil, "")
	if status != 200 {
		t.Fatalf("operator inbox=%d want 200", status)
	}
	for _, text := range []string{"Fictional client-a request", "Fictional client-b request", "Not approved for work"} {
		if !strings.Contains(body, text) {
			t.Errorf("inbox missing %q", text)
		}
	}
	if headers.Get("Cache-Control") != "no-store" || strings.Contains(body, "operator-workspace") || strings.Contains(body, "hostile-brief") {
		t.Fatal("list caches or exposes internal identity/full brief")
	}
	_, shell, _ := b.request("GET", "/portal/operator", nil, "")
	if !strings.Contains(shell, `href="/portal/operator/requests?lang=en"`) {
		t.Fatal("enabled operator workspace has no inbox link")
	}
}
