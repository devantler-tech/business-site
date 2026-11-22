// Package webapp composes the immutable public output and reserved portal routes.
package webapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

// Artifact holds a validated, bounded snapshot. Requests never open mutable files.
type Artifact struct {
	files  map[string][]byte
	digest string
}

// ErrArtifact reports validation failure without exposing local source paths.
var ErrArtifact = errors.New("public artifact is invalid")

const maxArtifactBytes = 128 << 20

// publicPath reserves the private namespaces at any depth and rejects source,
// credentials and hidden files. Only the public discovery directory is exempt.
func publicPath(name string) bool {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\%\x00") {
		return false
	}
	for i, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") && (i != 0 || part != ".well-known") {
			return false
		}
		if part == "portal" || part == "server" || part == "go.mod" || part == "go.sum" {
			return false
		}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".go", ".sql", ".pem", ".key":
		return false
	}
	return true
}

// Load snapshots regular public files, refuses links/special files and binds the
// exact path/length/bytes to a deterministic digest. It fails closed on read errors.
func Load(source fs.FS) (*Artifact, error) {
	if source == nil {
		return nil, ErrArtifact
	}
	a := &Artifact{files: make(map[string][]byte)}
	hash := sha256.New()
	var total int64
	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return ErrArtifact
		}
		if name == "." {
			return nil
		}
		if !publicPath(name) || entry.Type()&fs.ModeSymlink != 0 {
			return ErrArtifact
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return ErrArtifact
		}
		f, err := source.Open(name)
		if err != nil {
			return ErrArtifact
		}
		data, readErr := io.ReadAll(io.LimitReader(f, maxArtifactBytes-total+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			return ErrArtifact
		}
		total += int64(len(data))
		if total > maxArtifactBytes {
			return ErrArtifact
		}
		for _, marker := range []string{"portal_login_attempts", "portal_invitations", "portal_memberships", "portal_proposals", "portal_proposal_visibility", "portal_proposal_reviews", "portal_proposal_acceptances", "portal_invoices", "portal_credit_transfers", "portal_credit_revisions", "PORTAL_TEST_DATABASE_URL", "portal_fixture_owner", "fixture-only"} {
			if bytes.Contains(data, []byte(marker)) {
				return ErrArtifact
			}
		}
		a.files[name] = data
		// hash.Hash.Write always consumes the input and returns a nil error.
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", name, len(data))
		hash.Write(data)
		return nil
	})
	if err != nil || len(a.files["index.html"]) == 0 || len(a.files["da/index.html"]) == 0 {
		return nil, ErrArtifact
	}
	a.digest = hex.EncodeToString(hash.Sum(nil))
	return a, nil
}

// Digest identifies the snapshotted public paths and bytes, not a deployment.
func (a *Artifact) Digest() string { return a.digest }

// miss emits a non-cacheable denial without reflecting the requested path.
func miss(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, "Not found\n")
	}
}

// unsafePath rejects encoded, ambiguous or uncleaned paths before any routing.
// EscapedPath also catches encodings Go leaves out of RawPath, such as %3F,
// which would otherwise decode into a redirect target's query or fragment.
func unsafePath(r *http.Request) bool {
	p := r.URL.Path
	return r.URL.RawPath != "" || strings.Contains(r.URL.EscapedPath(), "%") || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\%\x00") || strings.Contains(p, "//") || path.Clean(p) != strings.TrimSuffix(p, "/") && p != "/"
}

// servePrivate delegates the portal unchanged and reports whether it took the request.
func servePrivate(w http.ResponseWriter, r *http.Request, portal http.Handler) bool {
	p := r.URL.Path
	if p != "/portal" && !strings.HasPrefix(p, "/portal/") {
		return false
	}
	if portal == nil {
		miss(w, r)
	} else {
		portal.ServeHTTP(w, r)
	}
	return true
}

// readOnly answers health and method refusals and reports whether it did.
func readOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "ok\n")
		}
		return true
	}
	return false
}

// Hosts names the public site's host and the portal's own host.
type Hosts struct{ Public, Portal string }

// requestHost is the request's lowercase host name without port or trailing dot.
func requestHost(r *http.Request) string {
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// redirect sends a GET or HEAD to a fixed origin, keeping only the checked path and query.
func redirect(w http.ResponseWriter, r *http.Request, target string, status int) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", target)
	w.WriteHeader(status)
}

// HostHandler serves one runtime under two hosts without showing a page twice.
// The portal lives at the root of its own host: "/", "/sign-in" and so on go to
// the portal, which must be built for that host (its callback is "/callback").
// There, a public page redirects to the public host and an old "/portal…" address
// redirects permanently to the same path without the prefix. On every other host
// (the public host, its www name, a staging name or a health probe's address),
// "/portal…" redirects to the portal host's root-based path and public pages are
// served; www additionally redirects to the public host.
func (a *Artifact) HostHandler(portal http.Handler, hosts Hosts) http.Handler {
	public := a.Handler(nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if unsafePath(r) {
			miss(w, r)
			return
		}
		p := r.URL.Path
		private := p == "/portal" || strings.HasPrefix(p, "/portal/")
		switch requestHost(r) {
		case hosts.Portal:
			switch {
			case p == "/healthz":
				readOnly(w, r)
			case private:
				if !readOnly(w, r) {
					redirect(w, r, rootPath(p), http.StatusMovedPermanently)
				}
			case p != "/" && a.serves(p):
				if !readOnly(w, r) {
					redirect(w, r, "https://"+hosts.Public+p, http.StatusFound)
				}
			default:
				servePortal(w, r, portal)
			}
		case "www." + hosts.Public:
			if !readOnly(w, r) {
				redirect(w, r, "https://"+hosts.Public+p, http.StatusMovedPermanently)
			}
		default:
			if !private {
				public.ServeHTTP(w, r)
			} else if !readOnly(w, r) {
				redirect(w, r, "https://"+hosts.Portal+rootPath(p), http.StatusFound)
			}
		}
	})
}

// rootPath is the portal host's address for a "/portal…" path.
func rootPath(p string) string {
	if p == "/portal" {
		return "/"
	}
	return strings.TrimPrefix(p, "/portal")
}

// servePortal hands a request to the portal unchanged, or denies it while the
// portal is not configured.
func servePortal(w http.ResponseWriter, r *http.Request, portal http.Handler) {
	if portal == nil {
		miss(w, r)
		return
	}
	portal.ServeHTTP(w, r)
}

// serves reports whether the public site has a page or file at p, using the same
// lookup as Handler, so the portal host can send public addresses to the public host.
func (a *Artifact) serves(p string) bool {
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		name = "index.html"
	} else if strings.HasSuffix(name, "/") {
		name += "index.html"
	}
	if !publicPath(name) {
		return false
	}
	if _, found := a.files[name]; found {
		return true
	}
	_, folder := a.files[name+"/index.html"]
	return folder && !strings.HasSuffix(p, "/")
}

// notFound answers a public miss with the built 404 page, as GitHub Pages did.
// Like miss, it never reflects the requested path and is not cacheable.
func (a *Artifact) notFound(w http.ResponseWriter, r *http.Request) {
	page, found := a.files["404.html"]
	if !found {
		miss(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
}

// Handler delegates canonical /portal requests unchanged before public lookup.
// It has no directory listings or SPA fallback. A folder requested without its
// trailing slash redirects permanently to the slash form, as GitHub Pages did,
// so each page has one address.
func (a *Artifact) Handler(portal http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		p := r.URL.Path
		if unsafePath(r) {
			miss(w, r)
			return
		}
		if servePrivate(w, r, portal) || readOnly(w, r) {
			return
		}
		name := strings.TrimPrefix(p, "/")
		if name == "" {
			name = "index.html"
		} else if strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		if !publicPath(name) {
			miss(w, r)
			return
		}
		data, found := a.files[name]
		if !found && !strings.HasSuffix(p, "/") {
			if _, folder := a.files[name+"/index.html"]; folder {
				// p passed unsafePath, so it is one absolute path on this host.
				redirect(w, r, p+"/", http.StatusMovedPermanently)
				return
			}
		}
		if !found {
			a.notFound(w, r)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(data))
	})
}
