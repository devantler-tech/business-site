package identity

import (
	"context"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func TestOperatorUnifiedPreviewKeepsPurchaseAndIdentityInSeparateStores(t *testing.T) {
	b := newBrowserFixture(t)
	demo := unifiedPreviewFixture(t, b.pool)
	ctx := t.Context()
	if err := b.svc.LinkOperators([]string{sharedOperator}); err != nil {
		t.Fatal(err)
	}
	demoAPI := isolated.NewAPI()
	t.Cleanup(func() { _ = demoAPI.Shutdown(context.Background()) })
	flags := map[string]memprovider.InMemoryFlag{}
	for _, key := range []string{
		"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance",
	} {
		flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	if err := demoAPI.SetProviderAndWait(ctx, memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkClientPreview(b.pool, demoAPI.NewClient()); err == nil {
		t.Fatal("preview accepted the production session store")
	}
	if err := b.svc.LinkClientPreview(demo, demoAPI.NewClient()); err != nil {
		t.Fatal(err)
	}
	b.atRoot()
	signIn := func(subject string) {
		t.Helper()
		b.as(subject)
		callback := b.startAt("/sign-in?lang=en")
		if status, _, header := b.request(http.MethodGet, callback, nil, ""); status != http.StatusSeeOther || header.Get("Location") != "/?lang=en" {
			t.Fatalf("shared sign-in failed: %d", status)
		}
	}

	b.invite(customerSubject, "fixture-customer", "client")
	signIn(customerSubject)
	if status, _, _ := b.request(http.MethodPost, "/preview", url.Values{"csrf_token": {b.csrf("/")}}, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("ordinary customer could enter preview: %d", status)
	}
	var err error
	b.client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	signIn(sharedOperator)
	if status, page, _ := b.request(http.MethodGet, "/operator?lang=en", nil, ""); status != http.StatusOK || !strings.Contains(page, "Open client preview") {
		t.Fatalf("ordinary operator workspace does not offer the preview: %d", status)
	}
	start := url.Values{"csrf_token": {b.csrf("/operator")}, "lang": {"en"}}
	if status, _, header := b.request(http.MethodPost, "/preview", start, b.site.URL); status != http.StatusSeeOther || header.Get("Location") != "/?lang=en" {
		t.Fatalf("the existing operator session failed to switch on the same host: %d", status)
	}
	if status, _, _ := b.request(http.MethodGet, "/operator", nil, ""); status != http.StatusForbidden {
		t.Fatalf("preview kept access to actual operator routes: %d", status)
	}
	var id string
	if err := demo.QueryRow(ctx, `SELECT id FROM portal_requests WHERE client_id=$1 AND product='website'`, previewWorkspace).Scan(&id); err != nil {
		t.Fatalf("fixture did not seed a fictional website quote: %v", err)
	}
	path := "/requests/" + id
	reviewForm := url.Values{"csrf_token": {b.csrf("/contact")}, "lang": {"en"}}
	status, review, _ := b.request(http.MethodPost, path+"/proposal-review", reviewForm, b.site.URL)
	if status != http.StatusOK || !strings.Contains(review, "100%") || !strings.Contains(review, "DKK 0") || !strings.Contains(review, "2995") {
		t.Fatalf("fictional proposal lacks the regular price and zero total: %d", status)
	}
	key := regexp.MustCompile(`name="context_key" value="([^"]+)"`).FindStringSubmatch(review)
	csrf := csrfInput.FindStringSubmatch(review)
	if len(key) != 2 || len(csrf) != 2 {
		t.Fatal("fictional proposal has no confirmation form")
	}
	confirm := url.Values{"context_key": {key[1]}, "confirm": {"yes"}, "csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, path+"/proposal-acceptance", confirm, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("fictional proposal did not confirm: %d", status)
	}
	status, page, _ := b.request(http.MethodGet, path+"?lang=en", nil, "")
	acceptance := regexp.MustCompile(`name="acceptance" value="([^"]+)"`).FindStringSubmatch(page)
	if status != http.StatusOK || len(acceptance) != 2 || !strings.Contains(page, "Simulate purchase") {
		t.Fatalf("fictional purchase form unavailable: %d", status)
	}
	purchase := url.Values{"acceptance": {acceptance[1]}, "csrf_token": {b.csrf(path)}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, path+"/orders", purchase, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("preview could reach a real order route: %d", status)
	}
	if first, second := concurrentPortalPosts(b, path+"/preview-orders", purchase, purchase); first != http.StatusSeeOther || second != http.StatusSeeOther {
		t.Fatalf("fictional purchase and retry returned %d and %d", first, second)
	}
	var receipts, commercial, copiedAuthority int
	if err := demo.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM portal_preview_receipts),
 (SELECT count(*) FROM portal_orders) + (SELECT count(*) FROM portal_invoices) +
 (SELECT count(*) FROM portal_card_checkouts) + (SELECT count(*) FROM portal_mail_outbox) +
 (SELECT count(*) FROM portal_work_approvals) + (SELECT count(*) FROM portal_launch_places),
 (SELECT count(*) FROM portal_sessions) + (SELECT count(*) FROM portal_memberships WHERE role='operator')`).Scan(&receipts, &commercial, &copiedAuthority); err != nil || receipts != 1 || commercial != 0 || copiedAuthority != 0 {
		t.Fatalf("fictional store has %d receipts, %d commercial rows and %d copied authority rows: %v", receipts, commercial, copiedAuthority, err)
	}
	var previewRows int
	if err := b.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_requests WHERE client_id=$1) +
 (SELECT count(*) FROM portal_memberships WHERE client_id=$1)`, previewWorkspace).Scan(&previewRows); err != nil || previewRows != 0 {
		t.Fatalf("ordinary store acquired fictional customer rows: %d, %v", previewRows, err)
	}
	degraded := New(b.pool, b.svc.broker, b.svc.flags)
	if err := degraded.LinkOperators([]string{sharedOperator}); err != nil {
		t.Fatal(err)
	}
	if err := degraded.LinkPublicSite(fixtureSite); err != nil {
		t.Fatal(err)
	}
	degraded.EnableClientPreviewSessions()
	degradedHandler := portalHandler(t, degraded, b.callbackURL())
	b.mu.Lock()
	healthy := b.handler
	b.handler = degradedHandler
	b.mu.Unlock()
	status, page, _ = b.request(http.MethodGet, path+"?lang=en", nil, "")
	backCSRF := csrfInput.FindStringSubmatch(page)
	if status != http.StatusServiceUnavailable || len(backCSRF) != 2 || !strings.Contains(page, "Return to operator workspace") {
		t.Fatalf("preview storage outage hid the return to operator action: %d", status)
	}
	back := url.Values{"csrf_token": {html.UnescapeString(backCSRF[1])}, "lang": {"en"}}
	if status, _, header := b.request(http.MethodPost, "/preview/leave", back, b.site.URL); status != http.StatusSeeOther || header.Get("Location") != "/operator?lang=en" {
		t.Fatalf("return did not restore the operator view on the same host: %d", status)
	}
	if status, page, _ := b.request(http.MethodGet, "/operator?lang=en", nil, ""); status != http.StatusOK || strings.Contains(page, "Open client preview") {
		t.Fatalf("ordinary operator unavailable or offered an unavailable preview: %d", status)
	}
	b.mu.Lock()
	b.handler = healthy
	b.mu.Unlock()
	start.Set("csrf_token", b.csrf("/operator"))
	if status, _, _ := b.request(http.MethodPost, "/preview", start, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("could not re-enter fictional workspace: %d", status)
	}
	revokeFixtureAccess(ctx, t, b, sharedOperator)
	if status, _, _ := b.request(http.MethodGet, path, nil, ""); status == http.StatusOK {
		t.Fatal("a revoked operator could still read the fictional workspace")
	}
}

// The shared and product test runners already provide an exclusively owned
// disposable service. Create a second database only on that fixed fixture.
func unifiedPreviewFixture(t *testing.T, primary *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := primary.Config().ConnConfig
	if (cfg.Host != "127.0.0.1" && cfg.Host != "localhost") || (cfg.Port != 5432 && cfg.Port != 55444) ||
		cfg.Database != "portal_identity_test" || cfg.User != "portal_runtime" || cfg.Password != "fixture-only" {
		t.Fatal("refusing to create a second database outside the owned synthetic fixture")
	}
	adminConfig := primary.Config()
	adminConfig.ConnConfig.Database = "postgres"
	adminConfig.ConnConfig.User = "portal_fixture_owner"
	admin, err := pgxpool.NewWithConfig(t.Context(), adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(t.Context(), "CREATE ROLE portal_preview_web LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP ROLE portal_preview_web"); err != nil {
			t.Error(err)
		}
	})
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE portal_preview OWNER portal_migrator"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE portal_preview"); err != nil {
			t.Error(err)
		}
	})
	migrationConfig := primary.Config()
	migrationConfig.ConnConfig.Database = "portal_preview"
	migrationConfig.ConnConfig.User = "portal_migrator"
	migrator, err := pgxpool.NewWithConfig(t.Context(), migrationConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(migrator.Close)
	if err := MigrateWithRuntimeRole(t.Context(), migrator, "portal_preview_web"); err != nil {
		t.Fatal(err)
	}
	demoConfig := primary.Config()
	demoConfig.ConnConfig.Database = "portal_preview"
	demoConfig.ConnConfig.User = "portal_preview_web"
	demo, err := pgxpool.NewWithConfig(t.Context(), demoConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(demo.Close)
	if err := CheckPreviewReceiptRole(t.Context(), demo, true); err != nil {
		t.Fatal(err)
	}
	return demo
}
