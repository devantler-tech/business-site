package identity

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/justinas/nosurf"
	"golang.org/x/oauth2"
)

//go:embed web/*
var browserFiles embed.FS

// presentationAssets are the only files served from web/, never template
// source: the stylesheet, the script, and the Devantler Tech tab icon and
// wordmarks, each with its content type.
var presentationAssets = map[string]string{
	"portal.css":     "text/css; charset=utf-8",
	"portal.js":      "text/javascript; charset=utf-8",
	"icon.png":       "image/png",
	"logo-dark.svg":  "image/svg+xml",
	"logo-light.svg": "image/svg+xml",
}

// browserPage renders every private page with portal links under "/portal".
var browserPage = pages(internalRoot, "")

// templateFuncs binds the portal mount and language-preserving public navigation.
// The public origin is empty when both surfaces share a host.
func templateFuncs(root, site string) template.FuncMap {
	return template.FuncMap{"root": func() string { return root }, "public": func(lang string) string {
		if lang == "da" {
			return site + "/da/"
		}
		return site + "/"
	}}
}

// pages parses the private templates. They write portal links as {{root}}: "/portal"
// when the portal shares a host with the public site, "" when it has its own host;
// links to the public website use {{public .Lang}}.
func pages(root, site string) *template.Template {
	return template.Must(template.New("portal").Funcs(templateFuncs(root, site)).ParseFS(browserFiles, "web/portal.html", "web/contact.html", "web/requests.html", "web/triage.html", "web/work-review.html"))
}

// internalRoot is where every private route is registered and gated.
const internalRoot = "/portal"

type browser struct {
	foundation *Foundation
	sessions   *scs.SessionManager
	oauth      oauth2.Config
	origin     string
	// atRoot serves the portal at the root of its own host: visitors see "/sign-in"
	// for the internal "/portal/sign-in". Routes and gates stay on internal paths.
	atRoot bool
	page   *template.Template
}

// Browser constructs the application HTTP surface, without starting a listener,
// migrating a database or enabling a flag. Only one fixed HTTPS callback is used.
// Private routes disappear when portal-identity is missing, false or erroneous.
func (s *Foundation) Browser(callbackURL, clientSecret string) (http.Handler, error) {
	u, err := url.Parse(callbackURL)
	if s == nil || s.pool == nil || s.broker == nil || s.broker.verifier == nil || s.broker.client == nil || clientSecret == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != internalRoot+"/callback" && u.Path != "/callback") || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, ErrDenied
	}
	issuer, err := url.Parse(s.broker.issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return nil, ErrDenied
	}
	csp := "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self' https://" + issuer.Host + "; frame-ancestors 'none'; base-uri 'none'"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var allowed bool
	err = s.pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'portal_sessions','SELECT') AND has_table_privilege(current_user,'portal_sessions','INSERT') AND has_table_privilege(current_user,'portal_sessions','UPDATE') AND has_table_privilege(current_user,'portal_sessions','DELETE')`).Scan(&allowed)
	if err != nil || !allowed {
		return nil, ErrDenied
	}
	sm := scs.New()
	// scs.New starts a cleanup worker for its default in-memory store, which is
	// replaced below and never used. Stop it so construction leaves no goroutine.
	// Stopping immediately is race-free only from scs revision c45a9d1 (pinned in
	// go.mod), which creates the stop channel before starting the worker.
	if unused, ok := sm.Store.(interface{ StopCleanup() }); ok {
		unused.StopCleanup()
	}
	// No background logging of raw store errors. Expired rows never authenticate;
	// scheduled retention/cleanup belongs to the eventual production operator.
	sm.Store = pgxstore.NewWithConfig(s.pool, pgxstore.Config{TableName: "portal_sessions"})
	sm.HashTokenInStore = true
	sm.Lifetime = 30 * time.Minute
	// Read-only requests do not rewrite durable authority, preventing an in-flight
	// page render from resurrecting a token deleted by logout/rotation.
	sm.IdleTimeout = 0
	sm.Cookie.Name = "__Host-portal_session"
	sm.Cookie.Path = "/"
	sm.Cookie.Secure = true
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Persist = false
	atRoot := u.Path == "/callback"
	if atRoot && s.site == "" {
		return nil, ErrDenied
	}
	b := &browser{foundation: s, sessions: sm, origin: "https://" + u.Host, oauth: oauth2.Config{ClientID: s.broker.clientID, ClientSecret: clientSecret, Endpoint: s.broker.endpoint, RedirectURL: callbackURL, Scopes: []string{oidcScope}}}
	b.atRoot = atRoot
	site := ""
	if atRoot {
		site = s.site
	}
	b.page = pages(b.base(), site)
	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, _ error) {
		w.Header().Del("Location")
		b.render(w, r, 503, "unavailable", "", "")
	}
	mux, err := b.routes()
	if err != nil {
		return nil, err
	}
	csrf := nosurf.New(mux)
	csrf.SetBaseCookie(http.Cookie{Name: "__Host-portal_csrf", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 1800})
	csrf.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { b.render(w, r, 403, "error", "", "") }))
	secured := sm.LoadAndSave(csrf)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atRoot {
			r, w = b.internal(r), rootRedirects{w}
		}
		w.Header().Set("Cache-Control", "no-store")
		// no-referrer forces native form POST Origin to "null". strict-origin
		// preserves same-origin CSRF checks without sharing callback paths/tokens.
		w.Header().Set("Referrer-Policy", "strict-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", csp)
		if !s.ready(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if (r.URL.Path == "/portal/requests" || strings.HasPrefix(r.URL.Path, "/portal/requests/")) && !s.requestsReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/portal/contact/verify" && !s.contactVerificationReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if (r.URL.Path == "/portal/operator/requests" || strings.HasPrefix(r.URL.Path, "/portal/operator/requests/")) && !s.triageReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/portal/operator/requests/") && strings.HasSuffix(r.URL.Path, "/proposals") && !s.proposalsReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/portal/operator/requests/") && strings.HasSuffix(r.URL.Path, "/proposal-visibility") && !s.sharingReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/portal/operator/requests/") && (strings.HasSuffix(r.URL.Path, "/work-review") || strings.HasSuffix(r.URL.Path, "/work-approvals") || strings.HasSuffix(r.URL.Path, "/work-revocations")) && !s.workApprovalGate(r.Context()) {
			http.NotFound(w, r)
			return
		}
		// Do not trust caller-controlled Host or forwarded-origin headers.
		if r.Host != u.Host {
			http.Error(w, "Portal unavailable", 421)
			return
		}
		// Stripe signs its webhook; it carries no session, CSRF token or Origin.
		if r.URL.Path == "/portal/stripe/webhook" {
			b.stripeWebhook(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			limit := int64(8192)
			// A 1,200-character Unicode brief can expand under native form encoding.
			// Keep the larger bound confined to the new intake endpoint.
			if r.URL.Path == "/portal/requests" {
				limit = 20000
			}
			if strings.HasPrefix(r.URL.Path, "/portal/operator/requests/") && strings.HasSuffix(r.URL.Path, "/proposals") {
				limit = 60000
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if origin := r.Header.Get("Origin"); origin != "" && origin != b.origin {
				b.render(w, r, 403, "error", "", "")
				return
			}
		}
		secured.ServeHTTP(w, r)
	}), nil
}

const oidcScope = "openid"

// internal maps a root-mounted request onto the internal route it names, so every
// registration and gate below sees one path space. "/sign-in" becomes
// "/portal/sign-in"; a visitor path that already starts with "/portal" maps to
// "/portal/portal/..." and matches nothing.
func (b *browser) internal(r *http.Request) *http.Request {
	p := internalRoot + r.URL.Path
	mapped := new(http.Request)
	*mapped = *r
	u := *r.URL
	u.Path, u.RawPath = p, ""
	mapped.URL = &u
	return mapped
}

// templates is this portal's template set; a browser assembled without Browser
// (as some tests do) renders with the shared "/portal" set.
func (b *browser) templates() *template.Template {
	if b.page != nil {
		return b.page
	}
	return browserPage
}

// base is the visible path prefix of every portal page: "" at the root of its own
// host, "/portal" when it shares a host with the public site.
func (b *browser) base() string {
	if b.atRoot {
		return ""
	}
	return internalRoot
}

// visible is the address a visitor uses for an internal route.
func (b *browser) visible(p string) string {
	if !b.atRoot {
		return p
	}
	return rootPath(p)
}

// rootPath drops the internal "/portal" segment from a local path and keeps any query.
func rootPath(p string) string {
	switch {
	case p == internalRoot:
		return "/"
	case strings.HasPrefix(p, internalRoot+"/"):
		return p[len(internalRoot):]
	case strings.HasPrefix(p, internalRoot+"?"):
		return "/" + p[len(internalRoot):]
	}
	return p
}

// rootRedirects rewrites a local redirect to its visible address. Redirects to
// another origin, such as the identity broker, are left alone.
type rootRedirects struct{ http.ResponseWriter }

func (w rootRedirects) WriteHeader(status int) {
	if location := w.Header().Get("Location"); location != "" {
		w.Header().Set("Location", rootPath(location))
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w rootRedirects) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// routes is the single method-aware registration used by the secured Browser
// and the rendered-link contract. Registration is not authentication evidence.
func (b *browser) routes() (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /portal/sign-in", b.signIn)
	mux.HandleFunc("GET /portal/operator/sign-in", b.operatorSignIn)
	mux.HandleFunc("GET /portal/signed-out", b.signedOut)
	mux.HandleFunc("GET /portal/callback", b.callback)
	mux.HandleFunc("GET /portal/{$}", b.home)
	mux.HandleFunc("GET /portal/operator", b.operator)
	mux.HandleFunc("GET /portal/operator/audit", b.operatorAuditPage)
	for _, route := range operatorRoutes {
		mux.HandleFunc(route.method+" "+operatorAPIRoot+route.path, b.operatorAPI(route))
	}
	mux.HandleFunc("GET /portal/operator/requests", b.triageList)
	mux.HandleFunc("GET /portal/operator/requests/{id}", b.triageDetail)
	mux.HandleFunc("POST /portal/operator/requests/{id}/proposals", b.submitProposal)
	mux.HandleFunc("POST /portal/operator/requests/{id}/proposal-visibility", b.submitVisibility)
	mux.HandleFunc("GET /portal/operator/requests/{id}/work-review", b.showWorkReview)
	mux.HandleFunc("POST /portal/operator/requests/{id}/work-approvals", b.approveWork)
	mux.HandleFunc("POST /portal/operator/requests/{id}/work-revocations", b.revokeWork)
	mux.HandleFunc("POST /portal/operator/requests/{id}/order-end", b.endOrder)
	mux.HandleFunc("POST /portal/operator/requests/{id}/order-completion", b.completeOrder)
	mux.HandleFunc("POST /portal/operator/requests/{id}/finished", b.finishRequest)
	mux.HandleFunc("POST /portal/requests/{id}/addon-end", b.endAddon)
	mux.HandleFunc("POST /portal/operator/requests/{id}/addon-removals", b.retryAddonRemovals)
	mux.HandleFunc("POST /portal/operator/requests/{id}/withdrawal-settlement", b.settleWithdrawal)
	mux.HandleFunc("GET /portal/contact", b.contact)
	mux.HandleFunc("POST /portal/contact", b.updateContact)
	mux.HandleFunc("POST /portal/contact/verify", b.beginContactVerification)
	mux.HandleFunc("GET /portal/requests", b.requests)
	mux.HandleFunc("GET /portal/requests/new", b.newRequest)
	mux.HandleFunc("GET /portal/requests/{id}", b.requestReceipt)
	mux.HandleFunc("POST /portal/requests/{id}/proposal-review", b.reviewProposal)
	mux.HandleFunc("POST /portal/requests/{id}/proposal-acceptance", b.confirmProposal)
	mux.HandleFunc("POST /portal/requests/{id}/orders", b.submitOrder)
	mux.HandleFunc("POST /portal/requests/{id}/order-end", b.clientOrderAction(b.foundation.clientEndMonthlyBilling))
	mux.HandleFunc("POST /portal/requests/{id}/withdrawal", b.clientOrderAction(b.foundation.withdrawOrder))
	mux.HandleFunc("POST /portal/requests", b.submitRequest)
	mux.HandleFunc("POST /portal/logout", b.logout)
	for name, contentType := range presentationAssets {
		data, err := browserFiles.ReadFile("web/" + name)
		if err != nil {
			return nil, ErrDenied
		}
		mux.HandleFunc("GET /portal/assets/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(data)
		})
	}
	return mux, nil
}

// language selects presentation only; neither query nor form data grants access.
func language(r *http.Request) string {
	if r.URL.Query().Get("lang") == "da" || (r.URL.Query().Get("lang") == "" && r.PostForm.Get("lang") == "da") {
		return "da"
	}
	return "en"
}

// refusalLimit and refusalWindow bound the refused round trips one browser can
// make: the third refusal within ten minutes shows one static page instead.
const (
	refusalLimit  = 3
	refusalWindow = 10 * time.Minute
)

// refusals returns this browser's current refusal count and when it started,
// starting afresh once the window has passed.
func (b *browser) refusals(ctx context.Context) (int, int64) {
	count, since := b.sessions.GetInt(ctx, "refused"), b.sessions.GetInt64(ctx, "refused_since")
	if since == 0 || time.Since(time.Unix(since, 0)) > refusalWindow {
		return 0, 0
	}
	return count, since
}

// signIn goes straight to the broker: there is no portal page before the login.
// Customers always sign in through ZITADEL, so customer Dex never shows a chooser.
func (b *browser) signIn(w http.ResponseWriter, r *http.Request) {
	b.startSignIn(w, r, customeridentity.ZitadelConnectorID)
}

// operatorSignIn is the operators' own link: GitHub through platform SSO. It
// exists only while the server configuration lists operators, and reaching it
// grants nothing: the identity that returns must be listed.
func (b *browser) operatorSignIn(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.OperatorsLinked() {
		http.NotFound(w, r)
		return
	}
	b.startSignIn(w, r, customeridentity.PlatformConnectorID)
}

// startSignIn sends the browser to one of the broker's connectors. The connector
// choice is not bound to the attempt, so the callback decides by the returned
// subject instead. A current member goes home, so a link here cannot sign anyone
// out. A browser that keeps being refused gets one static page, never a loop.
func (b *browser) startSignIn(w http.ResponseWriter, r *http.Request, connector string) {
	switch _, _, err := b.foundation.membership(r.Context(), b.principal(r.Context())); {
	case err == nil:
		http.Redirect(w, r, "/portal/?lang="+language(r), http.StatusSeeOther)
		return
	case !errors.Is(err, ErrDenied):
		// A failed lookup is not a missing membership: keep the session intact.
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	refused, since := b.refusals(r.Context())
	if refused >= refusalLimit {
		b.render(w, r, 403, "no-access", "", "")
		return
	}
	a, err := b.foundation.Begin(r.Context())
	if err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	if err = b.sessions.RenewToken(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	// Starting a new sign-in intentionally removes any old authenticated identity.
	if err = b.sessions.Clear(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	b.sessions.Put(r.Context(), "browser", a.BrowserBinding)
	b.sessions.Put(r.Context(), "lang", language(r))
	if refused > 0 {
		b.sessions.Put(r.Context(), "refused", refused)
		b.sessions.Put(r.Context(), "refused_since", since)
	}
	b.sessions.SetDeadline(r.Context(), time.Now().Add(refusalWindow))
	address := b.oauth.AuthCodeURL(a.State, oauth2.S256ChallengeOption(a.PKCEVerifier), oauth2.SetAuthURLParam("nonce", a.Nonce), oauth2.SetAuthURLParam("connector_id", connector))
	// A GET navigation may redirect across origins; only a form POST is bound
	// by form-action on every hop, which the handoff page exists for.
	http.Redirect(w, r, address, http.StatusSeeOther)
}

// refuse ends a sign-in that authenticated but has no membership. It shows no
// error page and keeps no identity: the portal and upstream sessions end and the
// browser returns to a fresh login. The reason is never revealed, so every
// refusal looks the same. The refusal count survives to stop a loop.
func (b *browser) refuse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	refused, since := b.refusals(ctx)
	if refused == 0 {
		since = time.Now().Unix()
	}
	refused++
	lang := b.sessions.GetString(ctx, "lang")
	if err := b.sessions.RenewToken(ctx); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	if err := b.sessions.Clear(ctx); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	b.sessions.Put(ctx, "refused", refused)
	b.sessions.Put(ctx, "refused_since", since)
	b.sessions.Put(ctx, "lang", lang)
	b.sessions.SetDeadline(ctx, time.Unix(since, 0).Add(refusalWindow))
	if refused >= refusalLimit {
		b.render(w, r, 403, "no-access", "", "")
		return
	}
	http.Redirect(w, r, b.endSessionAddress(b.origin+b.visible("/portal/signed-out")), http.StatusSeeOther)
}

// endSessionAddress ends the upstream session, then returns to target, which the
// upstream client must list as a post-logout address. Without upstream logout it
// is target itself.
func (b *browser) endSessionAddress(target string) string {
	if b.foundation.endSession == "" {
		return target
	}
	q := url.Values{"client_id": {b.foundation.endSessionClient}, "post_logout_redirect_uri": {target}}
	return b.foundation.endSession + "?" + q.Encode()
}

// signedOut is where the upstream logout returns after a refusal: a fresh
// sign-in, still counting refusals.
func (b *browser) signedOut(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("lang") == "" && b.sessions.GetString(r.Context(), "lang") == "da" {
		r = withLanguage(r, "da")
	}
	b.signIn(w, r)
}

// withLanguage returns a copy of r presenting lang, without granting anything.
func withLanguage(r *http.Request, lang string) *http.Request {
	urlCopy := *r.URL
	q := urlCopy.Query()
	q.Set("lang", lang)
	urlCopy.RawQuery = q.Encode()
	r = r.Clone(r.Context())
	r.URL = &urlCopy
	return r
}

// brokerHandoff starts native navigation for an internally constructed broker URL.
// Both ordinary sign-in and purpose-bound contact proof retain the same strict CSP.
func (b *browser) brokerHandoff(w http.ResponseWriter, r *http.Request, address string) {
	// Finish the form POST in a fresh portal document. Chromium applies the
	// submitting document's form-action policy to every redirect in a broker's
	// upstream chain; a direct POST redirect would require knowing all providers.
	// Ordinary navigation keeps this boundary confined to the configured broker.
	d := b.data(r, "broker-handoff", "", "")
	d.BrokerURL = address
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = b.templates().ExecuteTemplate(w, "portal.html", d)
}

// callback consumes the attempt before exchange, verifies broker proof and
// invitation membership, then rotates into a bounded authenticated session.
func (b *browser) callback(w http.ResponseWriter, r *http.Request) {
	// Recover presentation language from the durable attempt, never authority or
	// a redirect destination. Errors retain the language the client chose.
	if r.URL.Query().Get("lang") == "" && b.sessions.GetString(r.Context(), "lang") == "da" {
		r = withLanguage(r, "da")
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["state"]) != 1 || len(q["code"]) != 1 || len(q["error"]) != 0 || q.Get("state") == "" || q.Get("code") == "" || len(q.Get("code")) > 4096 || len(q.Get("state")) > 128 {
		b.render(w, r, 400, "error", "", "")
		return
	}
	contactFlow := b.sessions.GetString(r.Context(), "purpose") == "contact"
	var binding contactVerification
	var nonce, verifier string
	if contactFlow {
		if !b.foundation.contactVerificationReady(r.Context()) {
			http.NotFound(w, r)
			return
		}
		binding, nonce, verifier, err = b.foundation.consumeContactVerification(r.Context(), q.Get("state"), b.sessions.GetString(r.Context(), "browser"))
	} else {
		nonce, verifier, err = b.foundation.consumeAttempt(r.Context(), q.Get("state"), b.sessions.GetString(r.Context(), "browser"))
	}
	if err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, b.foundation.broker.client)
	tokens, err := b.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok {
		b.render(w, r, 400, "error", "", "")
		return
	}
	var p Principal
	if contactFlow {
		p, err = b.foundation.confirmContact(ctx, b.principal(r.Context()), binding, raw, nonce)
	} else {
		p, err = b.foundation.completeProof(ctx, nonce, raw)
	}
	if err != nil {
		switch {
		case contactFlow:
			b.contactVerificationFailure(w, r)
		case errors.Is(err, ErrDenied):
			b.refuse(w, r)
		default:
			b.render(w, r, 503, "unavailable", "", "")
		}
		return
	}
	lang := b.sessions.GetString(r.Context(), "lang")
	if err = b.sessions.RenewToken(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	if err = b.sessions.Clear(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	b.sessions.Put(r.Context(), "issuer", p.issuer)
	b.sessions.Put(r.Context(), "subject", p.subject)
	b.sessions.Put(r.Context(), "expires", p.expires.Unix())
	b.sessions.Put(r.Context(), "lang", lang)
	deadline := time.Now().Add(30 * time.Minute)
	if p.expires.Before(deadline) {
		deadline = p.expires
	}
	b.sessions.SetDeadline(r.Context(), deadline)
	// Keep only the opaque reference across browser restarts, bounded by the
	// original server/token deadline. Incomplete sign-ins stay session-only.
	b.sessions.RememberMe(r.Context(), true)
	if lang != "da" {
		lang = "en"
	}
	destination := "/portal/?lang="
	if contactFlow {
		destination = "/portal/contact?lang="
	}
	http.Redirect(w, r, destination+lang, http.StatusSeeOther)
}

// principal reconstructs identity only from the server-owned durable session.
func (b *browser) principal(ctx context.Context) Principal {
	return Principal{issuer: b.sessions.GetString(ctx, "issuer"), subject: b.sessions.GetString(ctx, "subject"), audience: b.foundation.broker.clientID, expires: time.Unix(b.sessions.GetInt64(ctx, "expires"), 0)}
}

// home rechecks current membership before rendering the client's workspace.
func (b *browser) home(w http.ResponseWriter, r *http.Request) {
	client, role, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil {
		// Opening the portal without a membership goes straight to the login;
		// signIn shows the unavailable page instead if the lookup itself failed.
		b.signIn(w, r)
		return
	}
	kind := "home"
	if role == "operator" {
		kind = "operator"
	}
	b.render(w, r, 200, kind, client, nosurf.Token(r))
}

// operator requires the current database role, never token groups or form fields.
func (b *browser) operator(w http.ResponseWriter, r *http.Request) {
	client, role, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil || role != "operator" {
		b.render(w, r, 403, "error", "", "")
		return
	}
	b.render(w, r, 200, "operator", client, nosurf.Token(r))
}

// browserMember preserves the common unauthenticated response before role checks.
func (b *browser) browserMember(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	client, role, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil {
		b.render(w, r, 401, "error", "", "")
		return "", "", false
	}
	return client, role, true
}

// logout durably destroys the portal session.
// It then ends the upstream session and lands on the public website. That is a
// cross-origin step after a form POST, so a handoff page navigates instead.
func (b *browser) logout(w http.ResponseWriter, r *http.Request) {
	if err := b.sessions.Destroy(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	site := b.foundation.site
	if site == "" {
		site = b.origin
	}
	d := b.data(r, "sign-out-handoff", "", "")
	d.BrokerURL = b.endSessionAddress(site + "/")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = b.templates().ExecuteTemplate(w, "portal.html", d)
}

type pageData struct {
	BrokerURL                                                                                                                                  string
	LanguageURL                                                                                                                                string
	Lang, Kind, Client, CSRF, Title, Intro, Action, SignedIn, Logout, Error, Unavailable, Back, Theme, System, Light, Dark, Operator, Boundary string
	RequestsEnabled                                                                                                                            bool
	TriageEnabled                                                                                                                              bool
	Audit                                                                                                                                      []auditEntry
}

// render escapes all page data and exposes no broker tokens or raw store errors.
func (b *browser) render(w http.ResponseWriter, r *http.Request, status int, kind, client, token string) {
	d := b.data(r, kind, client, token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = b.templates().ExecuteTemplate(w, "portal.html", d)
}

// data shares the same localized, escaped layout with the private contact form.
func (b *browser) data(r *http.Request, kind, client, token string) pageData {
	d := pageData{Lang: language(r), Kind: kind, Client: client, CSRF: token, Title: "Your client portal", Intro: "A private space for your work with Devantler Tech.", Action: "Sign in", SignedIn: "You are signed in", Logout: "Sign out", Error: "Sign-in could not be completed. The portal is invitation-only: try again with the account from your invitation, or contact Devantler Tech.", Unavailable: "The portal is unavailable. Please try again later.", Back: "Back to the website", Theme: "Appearance", System: "System", Light: "Light", Dark: "Dark", Operator: "Operator", Boundary: "Ordering and payments are not available yet."}
	other, path := "da", r.URL.Path
	if d.Lang == "da" {
		other = "en"
	}
	// Recovery links must not GET a POST-only route or replay an OIDC callback.
	if kind == "error" || kind == "unavailable" || path == "/portal/logout" || path == "/portal/callback" {
		path = "/portal/sign-in"
	}
	if path == "/portal/contact/verify" {
		path = "/portal/contact"
	}
	if kind == "proposal-invalid" || kind == "proposal-conflict" || kind == "sharing-invalid" || kind == "sharing-conflict" {
		path = "/portal/operator/requests"
	}
	d.LanguageURL = b.visible(path) + "?lang=" + other
	d.RequestsEnabled = kind == "home" && b.foundation.requestsReady(r.Context())
	d.TriageEnabled = kind == "operator" && b.foundation.triageReady(r.Context())
	if d.Lang == "da" {
		d.Title = "Din kundeportal"
		d.Intro = "Et privat sted for dit samarbejde med Devantler Tech."
		d.Action = "Log ind"
		d.SignedIn = "Du er logget ind"
		d.Logout = "Log ud"
		d.Error = "Login kunne ikke gennemføres. Portalen kræver en invitation: prøv igen med kontoen fra din invitation, eller kontakt Devantler Tech."
		d.Unavailable = "Portalen er ikke tilgængelig. Prøv igen senere."
		d.Back = "Tilbage til hjemmesiden"
		d.Theme = "Udseende"
		d.System = "System"
		d.Light = "Lys"
		d.Dark = "Mørk"
		d.Operator = "Operatør"
		d.Boundary = "Bestilling og betaling er endnu ikke tilgængelig."
	}
	return d
}

// LinkPublicSite names the public website's origin (https://host, no path), which
// a portal at the root of its own host links back to. Browser refuses such a
// portal without it, since "/" there is the portal itself.
func (s *Foundation) LinkPublicSite(origin string) error {
	u, err := url.Parse(origin)
	if s == nil || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.String() != origin {
		return ErrDenied
	}
	s.site = origin
	return nil
}
