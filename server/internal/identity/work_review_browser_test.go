package identity

import (
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func TestWorkReviewSingleConnection(t *testing.T) {
	b := workBrowserFixture(t)
	config := b.pool.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	b.mu.Lock()
	b.handler = browserHandler(t, New(pool, b.f.broker, b.flags.NewClient()), b.site.URL+"/portal/callback")
	b.mu.Unlock()
	status, body, _ := b.request("GET", workReviewPath+"?invoice=1", nil, "")
	if status != 200 {
		t.Fatalf("one-connection review could not finish: %d", status)
	}
	form := workNativeForm(t, body, "work-approvals")
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-approvals", form, b.site.URL); status != 303 {
		t.Fatalf("one-connection approval could not finish: %d", status)
	}
	_, body, _ = b.request("GET", workReviewPath, nil, "")
	form = workNativeForm(t, body, "work-revocations")
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-revocations", form, b.site.URL); status != 303 {
		t.Fatalf("one-connection revocation could not finish: %d", status)
	}
}

const workReviewPath = "/portal/operator/requests/" + proposalRequestID + "/work-review"

// workBrowserFixture retains native TLS/session/CSRF and restricted PostgreSQL.
// The only money is fictional evidence in the disposable fixture.
func workBrowserFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := creditFixture(t)
	credit := creditForm(t, b)
	credit.Set("amount_dkk", "1234.56")
	if status, _, _ := b.request("POST", creditPath, credit, b.site.URL); status != 303 {
		t.Fatal("fictional full credit failed")
	}
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWorkReviewOriginalSessionAtFinalWait removes durable authority while the
// real SDK is waiting. Cached SCS values must neither release data nor commit a
// decision, and a failed read must not recreate the deleted session.
func TestWorkReviewOriginalSessionAtFinalWait(t *testing.T) {
	for _, change := range []string{"deleted", "expired", "replacement identity", "extended deadline", "unchanged"} {
		for _, operation := range []string{"review", "approve", "revoke"} {
			t.Run(change+"/"+operation, func(t *testing.T) {
				b := workBrowserFixture(t)
				_, body, _ := b.request("GET", workReviewPath+"?invoice=1", nil, "")
				form := workNativeForm(t, body, "work-approvals")
				path, method, at := workReviewPath+"?invoice=1", "GET", int32(4)
				if operation != "review" {
					path, method = strings.TrimSuffix(workReviewPath, "work-review")+"work-approvals", "POST"
				}
				if operation == "revoke" {
					if status, _, _ := b.request("POST", path, form, b.site.URL); status != 303 {
						t.Fatal("fixture approval unavailable")
					}
					_, body, _ = b.request("GET", workReviewPath, nil, "")
					form, path, at = workNativeForm(t, body, "work-revocations"), strings.TrimSuffix(workReviewPath, "work-review")+"work-revocations", 3
				}
				provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(workApprovalFlags()), key: "portal-work-approvals", blockAt: at, blocked: make(chan struct{}), release: make(chan struct{})}
				if err := b.flags.SetProviderAndWait(t.Context(), provider); err != nil {
					t.Fatal(err)
				}
				release := onceRelease(provider.release)
				done := make(chan contactResponse, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					status, body, _ := b.request(method, path, form, b.site.URL)
					done <- contactResponse{status, body}
				}()
				t.Cleanup(func() {
					release()
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
						t.Error("work operation worker did not finish")
					}
				})
				select {
				case <-provider.blocked:
				case <-time.After(5 * time.Second):
					t.Fatal("work operation missed its final SDK checkpoint")
				}
				switch change {
				case "deleted":
					if _, err := b.pool.Exec(t.Context(), `DELETE FROM portal_sessions`); err != nil {
						t.Fatal(err)
					}
				case "expired":
					if _, err := b.pool.Exec(t.Context(), `UPDATE portal_sessions SET expiry=now()-interval '1 minute'`); err != nil {
						t.Fatal(err)
					}
				case "replacement identity", "extended deadline":
					var data []byte
					if err := b.pool.QueryRow(t.Context(), `SELECT data FROM portal_sessions`).Scan(&data); err != nil {
						t.Fatal(err)
					}
					codec := scs.New().Codec
					deadline, values, err := codec.Decode(data)
					if err != nil {
						t.Fatal(err)
					}
					if change == "replacement identity" {
						values["subject"] = "replacement-work-operator"
					} else {
						deadline = deadline.Add(time.Minute)
					}
					data, err = codec.Encode(deadline, values)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := b.pool.Exec(t.Context(), `UPDATE portal_sessions SET data=$1`, data); err != nil {
						t.Fatal(err)
					}
				}
				release()
				select {
				case got := <-done:
					if change == "unchanged" {
						want := 303
						if operation == "review" {
							want = 200
						}
						if got.status != want {
							t.Errorf("unchanged session refused: %d", got.status)
						}
					} else if got.status < 400 || strings.Contains(got.body, "TEST-2026-01") || strings.Contains(got.body, `class="work-history-item"`) {
						t.Errorf("deleted session retained work authority: %s status=%d", operation, got.status)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("work response did not terminate")
				}
				var approvals, revocations, sessions int
				if err := b.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_work_approvals),(SELECT count(*) FROM portal_work_revocations),(SELECT count(*) FROM portal_sessions)`).Scan(&approvals, &revocations, &sessions); err != nil {
					t.Fatal(err)
				}
				want := 0
				if operation == "revoke" {
					want = 1
				}
				wantRevocations, wantSessions := 0, 1
				if change == "deleted" {
					wantSessions = 0
				}
				if change == "unchanged" && operation == "approve" {
					want = 1
				}
				if change == "unchanged" && operation == "revoke" {
					wantRevocations = 1
				}
				if approvals != want || revocations != wantRevocations || sessions != wantSessions {
					t.Errorf("deleted session changed durable work/session state: %d/%d/%d", approvals, revocations, sessions)
				}
			})
		}
	}
}

func workNativeForm(t *testing.T, body, action string) url.Values {
	t.Helper()
	form := regexp.MustCompile(`(?s)<form[^>]+action="[^"]*/` + action + `"[^>]*>(.*?)</form>`).FindStringSubmatch(body)
	if len(form) != 2 {
		t.Fatalf("native %s action missing", action)
	}
	f := url.Values{}
	for _, input := range regexp.MustCompile(`<input[^>]+name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(form[1], -1) {
		f.Add(input[1], html.UnescapeString(input[2]))
	}
	return f
}

// TestWorkReviewNativeDecisionAndRevocation proves a read cannot approve, the
// explicit decision is durable and revocation survives invalidated old funding.
func TestWorkReviewNativeDecisionAndRevocation(t *testing.T) {
	b := workBrowserFixture(t)
	var body string
	for _, lang := range []string{"en", "da"} {
		status, document, h := b.request("GET", workReviewPath+"?invoice=1&lang="+lang, nil, "")
		if status != 200 || h.Get("Cache-Control") != "no-store" || !strings.Contains(document, `lang="`+lang+`"`) || !strings.Contains(document, "TEST-2026-01") || !strings.Contains(document, "transfer-01") {
			t.Fatalf("exact private bilingual review unavailable: %s status=%d", lang, status)
		}
		for _, price := range []string{`data-work-price="one-time">2995 DKK`, `data-work-price="monthly">99 DKK`} {
			if !strings.Contains(document, price) {
				t.Errorf("exact agreed price missing from %s review: %s", lang, price)
			}
		}
		body = document
	}
	if status, _, _ := b.request("HEAD", workReviewPath+"?invoice=1", nil, ""); status != 200 {
		t.Fatal("safe HEAD unavailable")
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_approvals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("GET/HEAD created authority", err)
	}
	f := workNativeForm(t, body, "work-approvals")
	for range 2 {
		if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-approvals", f, b.site.URL); status != 303 {
			t.Fatalf("explicit decision or identical retry failed: %d", status)
		}
	}
	var decided, expires time.Time
	if err := b.pool.QueryRow(t.Context(), `SELECT decided_at,expires_at FROM portal_work_approvals`).Scan(&decided, &expires); err != nil || expires.Sub(decided) != 168*time.Hour {
		t.Fatal("retry duplicated or renewed approval", err)
	}
	b.reconnect(t.Context())
	status, body, _ := b.request("GET", workReviewPath+"?lang=en", nil, "")
	if status != 200 || !strings.Contains(body, "Current approval") || !strings.Contains(body, decided.UTC().Format("2006-01-02T15:04:05.999999Z")) || !strings.Contains(body, expires.UTC().Format("2006-01-02T15:04:05.999999Z")) {
		t.Fatal("original durable decision/deadline missing")
	}
	revoke := workNativeForm(t, body, "work-revocations")
	flags := workApprovalFlags()
	delete(flags, "portal-credit-reconciliation")
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-revocations", revoke, b.site.URL); status != 303 {
		t.Fatalf("closed old funding trapped revocation: %d", status)
	}
	status, body, _ = b.request("GET", workReviewPath+"?lang=en", nil, "")
	if status != 200 || !strings.Contains(body, "Revoked") || strings.Contains(body, "Current approval") {
		t.Fatal("terminal history lost or revoked approval revived")
	}
}

func TestWorkReviewForgedFieldsCSRFAndClientDenial(t *testing.T) {
	b := workBrowserFixture(t)
	_, body, _ := b.request("GET", workReviewPath+"?invoice=1", nil, "")
	form := workNativeForm(t, body, "work-approvals")
	path := strings.TrimSuffix(workReviewPath, "work-review") + "work-approvals"
	for _, field := range []string{"client_id", "operator", "currency", "amount_minor", "execution", "work_expected", "invoice", "evidence_hash", "acceptance", "work_key"} {
		f := form.Clone()
		f.Set(field, "forged")
		if status, _, _ := b.request("POST", path, f, b.site.URL); status != 400 {
			t.Errorf("forged %s admitted: %d", field, status)
		}
	}
	f := form.Clone()
	f.Add("invoice", "1")
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 400 {
		t.Fatal("ambiguous native field admitted")
	}
	for _, origin := range []string{"https://foreign.example.invalid", "null"} {
		if status, _, _ := b.request("POST", path, form, origin); status != 403 {
			t.Fatal("foreign origin admitted")
		}
	}
	f = form.Clone()
	f.Set("csrf_token", "forged")
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 403 {
		t.Fatal("forged CSRF admitted")
	}
	for _, workspace := range []string{"private-client-key", "other-client"} {
		triageSignIn(t, b, "work-client-"+workspace, workspace, "client")
		f = form.Clone()
		f.Set("csrf_token", b.csrf("/portal/"))
		if status, document, _ := b.request("GET", workReviewPath+"?invoice=1", nil, ""); status != 403 || strings.Contains(document, "TEST-2026-01") {
			t.Fatal("client read operator funding")
		}
		if status, _, _ := b.request("POST", path, f, b.site.URL); status != 403 {
			t.Fatal("client approved work")
		}
	}
}

func TestWorkReviewGateIsIndependentAndClosedRoutesDoNotLeak(t *testing.T) {
	b := workBrowserFixture(t)
	for _, off := range []any{nil, false, "true"} {
		flags := workApprovalFlags()
		if off == nil {
			delete(flags, "portal-work-approvals")
		} else {
			flags["portal-work-approvals"] = memprovider.InMemoryFlag{Key: "portal-work-approvals", State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": off}}
		}
		if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(flags)); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"work-review", "work-approvals", "work-revocations"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				status, document, h := b.request(method, strings.TrimSuffix(workReviewPath, "work-review")+suffix, nil, "")
				if status != 404 || strings.Contains(document, "TEST-2026-01") || len(h.Values("Set-Cookie")) != 0 {
					t.Fatalf("closed independent gate exposed %s %s", method, suffix)
				}
			}
		}
		if status, document, _ := b.request("GET", strings.TrimSuffix(workReviewPath, "/work-review"), nil, ""); status != 200 || strings.Contains(document, "/work-review") {
			t.Fatal("closed gate leaked navigation or hid existing detail")
		}
	}
}

func TestWorkReviewUnhealthyProviderNeverReleasesPrivateFrame(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		t.Run(string(event), func(t *testing.T) {
			b := workBrowserFixture(t)
			requireFixtureSDKEvent(t, b.flags, b.svc.flags, workApprovalFlags(), event)
			for _, suffix := range []string{"work-review", "work-approvals", "work-revocations"} {
				for _, method := range []string{"GET", "POST"} {
					status, body, _ := b.request(method, strings.TrimSuffix(workReviewPath, "work-review")+suffix, nil, "")
					if status != 404 || strings.Contains(body, "TEST-2026-01") {
						t.Fatal("unhealthy SDK released work data/action")
					}
				}
			}
		})
	}
}

func TestWorkReviewRevocationAfterActorConsentAndDeadlineLoss(t *testing.T) {
	b := workBrowserFixture(t)
	_, body, _ := b.request("GET", workReviewPath+"?invoice=1", nil, "")
	form := workNativeForm(t, body, "work-approvals")
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-approvals", form, b.site.URL); status != 303 {
		t.Fatal("fixture approval unavailable")
	}
	triageSignIn(t, b, "replacement-work-operator", "replacement-workspace", "operator")
	for _, subject := range []string{"proposal-operator", "credit-fixture-client"} {
		if err := b.svc.Revoke(t.Context(), b.f.issuer, subject); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := migrationDB(t).Exec(t.Context(), `UPDATE portal_work_approvals SET decided_at=now()-interval '169 hours',expires_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	status, body, _ := b.request("GET", workReviewPath, nil, "")
	if status != 200 || !strings.Contains(body, `data-work-state="expired"`) {
		t.Fatal("original authority loss hid a revocable expired decision")
	}
	form = workNativeForm(t, body, "work-revocations")
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-revocations", form, b.site.URL); status != 303 {
		t.Fatal("old approver/acceptor/deadline loss trapped revocation")
	}
}

func TestWorkReviewConcurrentDecisionsAndChangedEvidence(t *testing.T) {
	b := workBrowserFixture(t)
	_, body, _ := b.request("GET", workReviewPath+"?invoice=1", nil, "")
	a := workNativeForm(t, body, "work-approvals")
	_, body, _ = b.request("GET", workReviewPath+"?invoice=1", nil, "")
	c := workNativeForm(t, body, "work-approvals")
	path := strings.TrimSuffix(workReviewPath, "work-review") + "work-approvals"
	first, second := concurrentPortalPosts(b, path, a, c)
	if (first != 303 || second != 409) && (first != 409 || second != 303) {
		t.Fatalf("distinct stale comparisons=%d/%d", first, second)
	}
	_, body, _ = b.request("GET", workReviewPath, nil, "")
	staleRevocation := workNativeForm(t, body, "work-revocations")
	_, body, _ = b.request("GET", workReviewPath+"?invoice=1", nil, "")
	fresh := workNativeForm(t, body, "work-approvals")
	first, second = concurrentPortalPosts(b, path, fresh, fresh)
	if first != 303 || second != 303 {
		t.Fatal("identical native retries failed")
	}
	if status, _, _ := b.request("POST", strings.TrimSuffix(workReviewPath, "work-review")+"work-revocations", staleRevocation, b.site.URL); status != 409 {
		t.Fatal("stale native revocation cleared a newer decision")
	}
	// Equal totals after a changed observation are still different evidence.
	_, body, _ = b.request("GET", workReviewPath+"?invoice=1", nil, "")
	stale := workNativeForm(t, body, "work-approvals")
	correction := creditForm(t, b)
	correction.Set("credit_expected", "1")
	correction.Set("amount_dkk", "1234.56")
	correction.Set("note", "A different fictional observation with equal funds")
	if status, _, _ := b.request("POST", creditPath, correction, b.site.URL); status != 303 {
		t.Fatal("fixture correction failed")
	}
	if status, _, _ := b.request("POST", path, stale, b.site.URL); status != 409 {
		t.Fatal("changed evidence admitted stale review")
	}
	if status, document, _ := b.request("GET", workReviewPath, nil, ""); status != 200 || !strings.Contains(document, `data-work-state="paused"`) {
		t.Fatal("changed evidence left a valid displayed approval")
	}
	triageSignIn(t, b, "another-work-operator", "other-operator-workspace", "operator")
	fresh.Set("csrf_token", b.csrf("/portal/operator"))
	if status, _, _ := b.request("POST", path, fresh, b.site.URL); status != 409 {
		t.Fatal("another operator retried original actor's decision")
	}
}

func TestWorkReviewBoundedHistoryAndFailedRead(t *testing.T) {
	b, op := approvalFixture(t)
	var ids []string
	for range 23 {
		input := preparedApproval(t, b, op)
		d, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	// Migration-owner-only synthetic timestamps create real equal-time ties.
	tied := time.Date(2026, 10, 21, 22, 0, 0, 123456000, time.UTC)
	if _, err := migrationDB(t).Exec(t.Context(), `UPDATE portal_work_approvals SET decided_at=$1,expires_at=$1::timestamptz+interval '168 hours'`, tied); err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	page, err := b.svc.readWorkReview(t.Context(), op, proposalRequestID, 0, triageCursor{})
	if err != nil || len(page.History) != 20 || page.Next.ID == "" {
		t.Fatal("first bounded history unavailable", err)
	}
	var seen []string
	for _, d := range page.History {
		seen = append(seen, d.ID)
	}
	page, err = b.svc.readWorkReview(t.Context(), op, proposalRequestID, 0, page.Next)
	if err != nil || len(page.History) != 3 || page.Next.ID != "" {
		t.Fatal("second bounded history unavailable", err)
	}
	for _, d := range page.History {
		seen = append(seen, d.ID)
	}
	if !slices.Equal(seen, ids) {
		t.Fatal("equal-time history skipped, duplicated or reordered decisions")
	}
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `REVOKE SELECT ON portal_work_revocations FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	page, err = b.svc.readWorkReview(t.Context(), op, proposalRequestID, 0, triageCursor{})
	if err == nil || len(page.History) != 0 {
		t.Fatal("failed history read was presented as a successful empty result")
	}
	if _, err := migrator.Exec(t.Context(), `GRANT SELECT ON portal_work_revocations TO portal_runtime`); err != nil {
		t.Fatal("fixture read grant restoration failed", err)
	}
	for _, raw := range []string{"invoice=0", "invoice=51", "invoice=1&invoice=2", "cursor=bad", "client_id=forged", fmt.Sprintf("invoice=%d", 1<<31)} {
		_, _, valid := workReviewQuery(httptest.NewRequest("GET", "https://example.invalid/portal/?"+raw, nil))
		if valid {
			t.Errorf("ambiguous review query admitted: %s", raw)
		}
	}
}
