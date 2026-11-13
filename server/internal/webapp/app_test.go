package webapp

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// publicFixture supplies only synthetic bilingual pages and a public asset.
func publicFixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                        {Data: []byte("<h1>English public site</h1>")},
		"da/index.html":                     {Data: []byte("<h1>Dansk public site</h1>")},
		"projects/index.html":               {Data: []byte("<h1>Projects</h1>")},
		"assets/site.css":                   {Data: []byte("body { color: navy; }")},
		".well-known/site-publication.json": {Data: []byte(`{"repository":"devantler-tech/business-site"}`)},
	}
}

// TestPublicRoutesAndClosedPortal preserves visitor routes while reserving private paths.
func TestPublicRoutesAndClosedPortal(t *testing.T) {
	a, err := Load(publicFixture())
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler(nil)
	for _, tc := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/", 200, "English"}, {"GET", "/da/", 200, "Dansk"},
		{"GET", "/projects", 200, "Projects"}, {"HEAD", "/projects/", 200, ""},
		{"GET", "/assets/site.css", 200, "navy"},
		{"GET", "/.well-known/site-publication.json", 200, "business-site"},
		{"GET", "/missing", 404, ""}, {"GET", "/assets/", 404, ""},
		{"POST", "/", 405, ""}, {"GET", "/portal", 404, ""},
		{"GET", "/portal/sign-in", 404, ""}, {"GET", "/portal/operator", 404, ""},
		{"GET", "/portal.js", 404, ""}, {"GET", "/healthz", 200, "ok"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("status=%d body=%q; want %d containing %q", w.Code, w.Body, tc.status, tc.contains)
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD has body")
			}
			if strings.HasPrefix(tc.path, "/portal") && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private miss is cacheable")
			}
		})
	}
}

// TestRejectsUnsafeArtifact rejects private files, links and incomplete bilingual output.
func TestRejectsUnsafeArtifact(t *testing.T) {
	for _, name := range []string{"portal/index.html", "server/key.txt", ".env", ".git/config", "assets/.secret", "identity.sql", "source.go", "go.mod", "private.pem", "key.key"} {
		t.Run(name, func(t *testing.T) {
			files := publicFixture()
			files[name] = &fstest.MapFile{Data: []byte("private")}
			if _, err := Load(files); err == nil {
				t.Fatal("unsafe artifact accepted")
			}
		})
	}
	files := publicFixture()
	files["linked.css"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("assets/site.css")}
	if _, err := Load(files); err == nil {
		t.Fatal("symlink accepted")
	}
	files = publicFixture()
	delete(files, "da/index.html")
	if _, err := Load(files); err == nil {
		t.Fatal("missing Danish accepted")
	}
	files = publicFixture()
	files["asset.json"] = &fstest.MapFile{Data: []byte("portal_login_attempts")}
	if _, err := Load(files); err == nil {
		t.Fatal("identity data accepted")
	}
	files = publicFixture()
	files["asset.json"] = &fstest.MapFile{Data: []byte("portal_proposals")}
	if _, err := Load(files); err == nil {
		t.Fatal("private proposal storage accepted")
	}
}

// TestPublicArtifactRejectsProposalVisibilityStorage guards the static boundary.
func TestPublicArtifactRejectsProposalVisibilityStorage(t *testing.T) {
	files := publicFixture()
	files["asset.json"] = &fstest.MapFile{Data: []byte("portal_proposal_visibility")}
	if _, err := Load(files); err == nil {
		t.Fatal("private proposal sharing storage accepted")
	}
}

func TestPublicArtifactRejectsProposalConsentStorage(t *testing.T) {
	for _, table := range []string{"portal_proposal_reviews", "portal_proposal_acceptances", "portal_invoices", "portal_credit_transfers", "portal_credit_revisions"} {
		files := publicFixture()
		files["asset.json"] = &fstest.MapFile{Data: []byte(table)}
		if _, err := Load(files); err == nil {
			t.Fatal("private consent storage accepted")
		}
	}
}

// TestSnapshotAndDigestBindActualPublicBytes detects changes without serving mutable files.
func TestSnapshotAndDigestBindActualPublicBytes(t *testing.T) {
	files := publicFixture()
	a, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	first := a.Digest()
	files["index.html"].Data = []byte("changed after startup")
	b, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	if first == b.Digest() {
		t.Fatal("changed public bytes retain digest")
	}
	w := httptest.NewRecorder()
	a.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "English") {
		t.Fatal("artifact is not a read-only snapshot")
	}
	if len(first) != 64 {
		t.Fatal("digest is not a full SHA256")
	}
}

// TestPrivateRoutingNeverFallsBackOrRedirects preserves handler authority and rejects ambiguity.
func TestPrivateRoutingNeverFallsBackOrRedirects(t *testing.T) {
	a, err := Load(publicFixture())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	portal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
	})
	h := a.Handler(portal)
	for _, path := range []string{"/portal", "/portal/", "/portal/sign-in"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader("csrf_token=fixture")))
		if w.Code != 403 || w.Header().Get("Location") != "" {
			t.Fatal("private handler replaced or redirected")
		}
	}
	if calls != 3 {
		t.Fatal("portal requests not delegated unchanged")
	}
	for _, path := range []string{"//portal/sign-in", "/%70ortal/sign-in", "/portal%2fsign-in", "/assets/../portal/sign-in", "/%252e%252e/.env", "/assets/%2e%2e/.env", "/.env", "/%2fportal", "/portal\\sign-in"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unsafe path %q: %d headers=%v", path, w.Code, w.Header())
		}
	}
	if calls != 3 {
		t.Fatal("ambiguous path reached private handler")
	}
}

// TestHostHandlerShowsEachPageOnOneHost serves the public site and the portal
// from one runtime, redirecting each host's foreign paths to the other host.
func TestHostHandlerShowsEachPageOnOneHost(t *testing.T) {
	a, err := Load(publicFixture())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	portal := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTeapot)
	})
	h := a.HostHandler(portal, Hosts{Public: "devantler.tech", Portal: "portal.devantler.tech"})
	for _, tc := range []struct {
		method, host, path string
		status             int
		location, contains string
	}{
		// The portal host opens the portal and sends public pages away.
		{"GET", "portal.devantler.tech", "/", 302, "/portal/", ""},
		{"GET", "Portal.Devantler.Tech:443", "/?lang=da", 302, "/portal/?lang=da", ""},
		{"HEAD", "portal.devantler.tech", "/", 302, "/portal/", ""},
		{"GET", "portal.devantler.tech", "/da/", 302, "https://devantler.tech/da/", ""},
		{"GET", "portal.devantler.tech", "/assets/site.css?v=1", 302, "https://devantler.tech/assets/site.css?v=1", ""},
		{"GET", "portal.devantler.tech", "/portal/", 418, "", ""},
		{"POST", "portal.devantler.tech", "/portal/sign-in", 418, "", ""},
		{"POST", "portal.devantler.tech", "/", 405, "", ""},
		{"GET", "portal.devantler.tech", "/healthz", 200, "", "ok"},
		// The public host serves pages and sends portal paths to the portal host.
		{"GET", "devantler.tech", "/", 200, "", "English"},
		{"GET", "devantler.tech.", "/da/", 200, "", "Dansk"},
		{"GET", "devantler.tech", "/portal", 302, "https://portal.devantler.tech/portal", ""},
		{"GET", "devantler.tech", "/portal/sign-in?lang=da", 302, "https://portal.devantler.tech/portal/sign-in?lang=da", ""},
		{"POST", "devantler.tech", "/portal/sign-in", 405, "", ""},
		{"GET", "devantler.tech", "/missing", 404, "", ""},
		// www has one canonical name.
		{"GET", "www.devantler.tech", "/projects", 302, "https://devantler.tech/projects", ""},
		{"GET", "www.devantler.tech", "/portal/", 302, "https://devantler.tech/portal/", ""},
		// A staging name or a probe's address behaves like the public host.
		{"GET", "business-site.platform.devantler.tech", "/", 200, "", "English"},
		{"GET", "10.0.0.7:8080", "/healthz", 200, "", "ok"},
		{"GET", "10.0.0.7:8080", "/portal/", 302, "https://portal.devantler.tech/portal/", ""},
	} {
		t.Run(tc.method+" "+tc.host+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Host = tc.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Location") != tc.location || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("status=%d location=%q body=%q; want %d %q containing %q", w.Code, w.Header().Get("Location"), w.Body, tc.status, tc.location, tc.contains)
			}
			if tc.status == 302 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("host redirect is cacheable")
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD has body")
			}
		})
	}
	if calls != 2 {
		t.Fatalf("portal handler calls = %d, want 2", calls)
	}
	// Unsafe spellings never become redirects to another origin, on any host.
	for _, host := range []string{"portal.devantler.tech", "devantler.tech", "www.devantler.tech"} {
		for _, path := range []string{"//evil.example/", "/%2f%2fevil.example", "/assets/../portal/sign-in", "/portal\\sign-in", "/%70ortal/sign-in"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 404 || w.Header().Get("Location") != "" {
				t.Fatalf("unsafe %s%s: %d location=%q", host, path, w.Code, w.Header().Get("Location"))
			}
		}
	}
	if calls != 2 {
		t.Fatal("ambiguous path reached private handler")
	}
}
