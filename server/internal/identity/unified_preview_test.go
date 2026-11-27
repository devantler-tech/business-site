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
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func TestOperatorCanStartAnInvitationWithoutClientPreview(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.atRoot()
	checkOperatorWorkspaceActions(t, b, false)
}

func checkOperatorWorkspaceActions(t *testing.T, b *browserFixture, previewAvailable bool) {
	t.Helper()
	for lang, inviteLabel := range map[string]string{"en": "Invite a customer", "da": "Inviter en kunde"} {
		invitePath := "/operator/invitations/new?lang=" + lang
		invitation := `href="` + invitePath + `">` + inviteLabel + `</a>`
		previewForm := `method="post" action="/preview?lang=` + lang + `"`
		for _, path := range []string{"/operator", "/operator/customers"} {
			status, page, _ := b.request(http.MethodGet, path+"?lang="+lang, nil, "")
			expectPreview := previewAvailable && path == "/operator"
			if status != http.StatusOK || !strings.Contains(page, invitation) || strings.Contains(page, previewForm) != expectPreview {
				t.Fatalf("%s (%s): invitation or preview unavailable, status=%d", path, lang, status)
			}
		}
		status, page, _ := b.request(http.MethodGet, invitePath, nil, "")
		if status != http.StatusOK || !strings.Contains(page, `action="/operator/api/invitations?lang=`+lang+`"`) {
			t.Fatalf("operator could not reach the invitation form (%s), status=%d", lang, status)
		}
	}
}

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
	checkOperatorWorkspaceActions(t, b, true)
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
	checkUnifiedPreviewRelease(t, b, demoAPI, flags, demo, path+"/preview-orders", purchase)
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
	demo.Close()
	status, page, _ = b.request(http.MethodGet, path+"?lang=en", nil, "")
	if status < http.StatusBadRequest || !strings.Contains(page, "Return to operator workspace") || !csrfInput.MatchString(page) {
		t.Errorf("a preview database lost after startup hid the return form: %d", status)
	}
	revokeFixtureAccess(ctx, t, b, sharedOperator)
	if status, _, _ := b.request(http.MethodGet, path, nil, ""); status != http.StatusForbidden {
		t.Fatalf("a revoked operator reached fictional storage: %d", status)
	}
}

func checkUnifiedPreviewRelease(t *testing.T, b *browserFixture, demoAPI *openfeature.EvaluationAPI, flags map[string]memprovider.InMemoryFlag, demo *pgxpool.Pool, path string, purchase url.Values) {
	t.Helper()
	provider := &finalRequestProvider{
		FeatureProvider: memprovider.NewInMemoryProvider(flags), key: "portal-proposal-acceptance",
		blockAt: 2, blocked: make(chan struct{}), release: make(chan struct{}),
	}
	if err := demoAPI.SetProviderAndWait(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	release := onceRelease(provider.release)
	defer release()
	result := make(chan int, 1)
	go func() {
		status, _, _ := b.request(http.MethodPost, path, purchase, b.site.URL)
		result <- status
	}()
	select {
	case <-provider.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("fictional purchase did not reach the final acceptance gate")
	}
	setFlag(t, b.flags, false)
	release()
	select {
	case status := <-result:
		if status != http.StatusForbidden {
			t.Errorf("purchase survived closure of the main portal identity gate: %d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fictional purchase did not finish after the final gate was released")
	}
	var receipts int
	if err := demo.QueryRow(t.Context(), "SELECT count(*) FROM portal_preview_receipts").Scan(&receipts); err != nil || receipts != 0 {
		t.Errorf("the closed identity gate retained a fictional receipt: %d, %v", receipts, err)
	}
	setFlag(t, b.flags, true)
	if err := demoAPI.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
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
	for _, resource := range []struct{ create, drop string }{
		{
			create: "CREATE ROLE portal_preview_web LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS",
			drop:   "DROP ROLE portal_preview_web",
		}, {
			create: "CREATE DATABASE portal_preview OWNER portal_migrator",
			drop:   "DROP DATABASE portal_preview",
		},
	} {
		if _, err := admin.Exec(t.Context(), resource.create); err != nil {
			t.Fatalf("create the fictional storage fixture: %v", err)
		}
		t.Cleanup(func() {
			cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
			defer finish()
			if _, err := admin.Exec(cleanup, resource.drop); err != nil {
				t.Errorf("remove a fictional storage fixture: %v", err)
			}
		})
	}
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
