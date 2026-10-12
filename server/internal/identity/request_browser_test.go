package identity

import (
	"context"
	"html"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func requestFlag(t *testing.T, b *browserFixture, value any) {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{
		"portal-identity": {Key: "portal-identity", State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": true}},
	}
	if value != nil {
		flags["portal-requests"] = memprovider.InMemoryFlag{Key: "portal-requests", State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": value}}
	}
	if err := b.flags.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRequestInvalidFormsNeverPersist(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	for _, kind := range []string{"empty", "long title", "long brief", "control", "product", "key", "duplicate", "owner", "timestamp", "origin", "csrf", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			f := requestForm(t, b)
			origin, want := b.site.URL, 400
			switch kind {
			case "empty":
				f.Set("brief", " ")
			case "long title":
				f.Set("title", strings.Repeat("x", 121))
			case "long brief":
				f.Set("brief", strings.Repeat("x", 1201))
			case "control":
				f.Set("title", "Bad\x00title")
			case "product":
				f.Set("product", "paid-work")
			case "key":
				f.Set("submission_key", "../unsafe")
			case "duplicate":
				f.Add("title", "other")
			case "owner":
				f.Set("client_id", "client-b")
			case "timestamp":
				f.Set("submitted_at", "2000-01-01")
			case "origin":
				origin, want = "https://other.example.invalid", 403
			case "csrf":
				f.Set("csrf_token", "bad")
				want = 403
			case "oversized":
				f.Set("brief", strings.Repeat("x", 20001))
				want = 403
			}
			s, _, _ := b.request("POST", "/portal/requests", f, origin)
			if s != want {
				t.Fatalf("invalid %s=%d want %d", kind, s, want)
			}
			var n int
			if err := b.pool.QueryRow(b.f.ctx, "SELECT count(*) FROM portal_requests").Scan(&n); err != nil || n != 0 {
				t.Fatal("invalid form persisted", err)
			}
		})
	}
}

func TestProjectRequestFlagAndCurrentMembership(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	f := requestForm(t, b)
	s, _, h := b.request("POST", "/portal/requests", f, b.site.URL)
	if s != 303 {
		t.Fatalf("create=%d", s)
	}
	for _, value := range []any{nil, false, "true"} {
		requestFlag(t, b, value)
		for _, path := range []string{"/portal/requests", "/portal/requests/new", h.Get("Location")} {
			s, _, _ := b.request("GET", path, nil, "")
			if s != 404 {
				t.Fatalf("closed flag exposes %s: %d", path, s)
			}
		}
		s, _, _ = b.request("POST", "/portal/requests", f, b.site.URL)
		if s != 404 {
			t.Fatal("closed flag accepts a write")
		}
		_, body, _ := b.request("GET", "/portal/", nil, "")
		if strings.Contains(body, `href="/portal/requests`) {
			t.Fatal("closed flag leaves an intake link")
		}
	}
	requestFlag(t, b, true)
	if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "client-a"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portal/requests", "/portal/requests/new", h.Get("Location")} {
		s, body, _ := b.request("GET", path, nil, "")
		if s != 403 || strings.Contains(body, "Fictional booking") {
			t.Fatal("revoked member reads requests")
		}
	}
	s, _, _ = b.request("POST", "/portal/requests", f, b.site.URL)
	if s != 403 {
		t.Fatal("revoked member writes")
	}
}

func TestProjectRequestClientIsolationAndEscaping(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	a := b.client
	f := requestForm(t, b)
	f.Set("title", `<script>private-a</script>`)
	s, _, h := b.request("POST", "/portal/requests", f, b.site.URL)
	if s != 303 {
		t.Fatalf("create=%d", s)
	}
	location := h.Get("Location")
	_, body, _ := b.request("GET", location, nil, "")
	if strings.Contains(body, `<script>private-a</script>`) || !strings.Contains(body, "&lt;script&gt;private-a&lt;/script&gt;") {
		t.Fatal("receipt did not escape client text")
	}
	other := *a
	var err error
	other.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b.client = &other
	b.mu.Lock()
	b.subject = "client-b"
	b.mu.Unlock()
	b.finish(b.start(b.invite("client-b", "client-b", "client")))
	s, body, _ = b.request("GET", location+"&client_id=client-a", nil, "")
	if s != 404 || strings.Contains(body, "private-a") {
		t.Fatal("second client obtained another receipt")
	}
	s, body, _ = b.request("GET", "/portal/requests?client_id=client-a", nil, "")
	if s != 200 || strings.Contains(body, "private-a") {
		t.Fatal("list uses caller-selected owner")
	}
	// The same opaque retry key is scoped to each server-owned client.
	g := requestForm(t, b)
	g.Set("submission_key", f.Get("submission_key"))
	s, _, h = b.request("POST", "/portal/requests", g, b.site.URL)
	if s != 303 || h.Get("Location") == location {
		t.Fatal("client retry scope was shared")
	}
	b.client = a
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	operator := *a
	operator.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b.client = &operator
	b.finish(b.start(b.invite("operator", "client-a", "operator")))
	s, _, _ = b.request("GET", location, nil, "")
	if s != 403 {
		t.Fatal("operator impersonates a client")
	}
}

func TestProjectRequestConcurrentRetriesCommitOne(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	f := requestForm(t, b)
	locations := make(chan string, 6)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			s, _, h := b.request("POST", "/portal/requests", f, b.site.URL)
			if s != 303 {
				t.Errorf("concurrent retry=%d", s)
			}
			locations <- h.Get("Location")
		})
	}
	wg.Wait()
	close(locations)
	first := <-locations
	for value := range locations {
		if value != first {
			t.Fatal("concurrent retries split receipts")
		}
	}
	var n int
	if err := b.pool.QueryRow(b.f.ctx, "SELECT count(*) FROM portal_requests").Scan(&n); err != nil || n != 1 {
		t.Fatal("concurrent retries duplicated records", err)
	}
}

var requestKeyInput = regexp.MustCompile(`name="submission_key" value="([^"]+)"`)

func requestForm(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	s, body, _ := b.request("GET", "/portal/requests/new?lang=en", nil, "")
	if s != 200 {
		t.Fatalf("project request form=%d want 200", s)
	}
	key := requestKeyInput.FindStringSubmatch(body)
	token := csrfInput.FindStringSubmatch(body)
	if len(key) != 2 || len(token) != 2 {
		t.Fatal("native form lacks retry key or CSRF token")
	}
	return url.Values{"submission_key": {key[1]}, "csrf_token": {html.UnescapeString(token[1])}, "lang": {"en"}, "product": {"website"}, "title": {"Fictional booking website"}, "brief": {"Let visitors ask for an appointment. No real customer data."}}
}

// Missing durable request storage, reassigning ownership from the browser or
// returning a fresh receipt on retry all break this real TLS/SQL control.
func TestProjectRequestDurableReceiptAndRetry(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	form := requestForm(t, b)
	s, _, h := b.request("POST", "/portal/requests", form, b.site.URL)
	location := h.Get("Location")
	if s != 303 || !strings.HasPrefix(location, "/portal/requests/") {
		t.Fatalf("submission=%d receipt=%q", s, location)
	}
	s, body, h := b.request("GET", location, nil, "")
	if s != 200 || !strings.Contains(body, "Fictional booking website") || !strings.Contains(body, "No real customer data.") || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("private receipt=%d %s", s, body)
	}
	var before string
	if err := b.pool.QueryRow(b.f.ctx, "SELECT submitted_at::text FROM portal_requests").Scan(&before); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), b.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	b.mu.Lock()
	b.handler = browserHandler(t, New(pool, b.f.broker, b.flags.NewClient()), b.site.URL+"/portal/callback")
	b.mu.Unlock()
	s, _, h = b.request("POST", "/portal/requests", form, b.site.URL)
	if s != 303 || h.Get("Location") != location {
		t.Fatal("retry did not return original durable receipt")
	}
	var n int
	var after string
	if err := pool.QueryRow(b.f.ctx, "SELECT count(*),min(submitted_at)::text FROM portal_requests").Scan(&n, &after); err != nil || n != 1 || before != after {
		t.Fatal("retry duplicated request or changed original submission time", err)
	}
	form.Set("brief", "Different scope")
	s, _, _ = b.request("POST", "/portal/requests", form, b.site.URL)
	if s != 409 {
		t.Fatalf("retry-key payload conflict=%d want 409", s)
	}
}

func TestProjectRequestDanishCharacterLimits(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	f := requestForm(t, b)
	f.Set("title", strings.Repeat("æ", 120))
	f.Set("brief", strings.Repeat("ø", 1200))
	f.Set("lang", "da")
	s, _, h := b.request("POST", "/portal/requests", f, b.site.URL)
	if s != 303 || !strings.HasSuffix(h.Get("Location"), "?lang=da") {
		t.Fatalf("valid Danish character bounds=%d", s)
	}
	s, body, _ := b.request("GET", h.Get("Location"), nil, "")
	if s != 200 || !strings.Contains(body, "Forespørgsel modtaget") || !strings.Contains(body, f.Get("brief")) {
		t.Fatal("Danish receipt lost brief")
	}
}

func TestProjectRequestInvalidFormKeepsSafeDraft(t *testing.T) {
	b := newBrowserFixture(t)
	requestFlag(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	f := requestForm(t, b)
	f.Set("product", "wrong")
	s, body, _ := b.request("POST", "/portal/requests", f, b.site.URL)
	if s != 400 || !strings.Contains(body, `value="Fictional booking website"`) || !strings.Contains(body, f.Get("brief")) || !strings.Contains(body, `name="submission_key" value="`+f.Get("submission_key")+`"`) {
		t.Fatal("validation discarded safe request draft")
	}
}

func TestProjectRequestReceiptNamesSelectedProduct(t *testing.T) {
	for _, tc := range []struct{ product, label string }{{"website", "Website"}, {"app", "Web app"}, {"service", "Service or integration"}} {
		t.Run(tc.product, func(t *testing.T) {
			b := newBrowserFixture(t)
			requestFlag(t, b, true)
			b.finish(b.start(b.invite("client-a", "client-a", "client")))
			f := requestForm(t, b)
			f.Set("product", tc.product)
			s, _, h := b.request("POST", "/portal/requests", f, b.site.URL)
			if s != 303 {
				t.Fatalf("create=%d", s)
			}
			_, body, _ := b.request("GET", h.Get("Location"), nil, "")
			if !strings.Contains(body, "<dd>"+tc.label+"</dd>") {
				t.Fatal("receipt loses the selected offering")
			}
		})
	}
}
