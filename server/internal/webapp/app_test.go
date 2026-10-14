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
	for _, table := range []string{"portal_proposal_reviews", "portal_proposal_acceptances"} {
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
