package identity

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
	"golang.org/x/oauth2"
)

//go:embed web/*
var browserFiles embed.FS

var browserPage = template.Must(template.ParseFS(browserFiles, "web/portal.html"))

type browser struct {
	foundation *Foundation
	sessions   *scs.SessionManager
	oauth      oauth2.Config
	origin     string
}

// Browser constructs the application HTTP surface, without starting a listener,
// migrating a database or enabling a flag. Only one fixed HTTPS callback is used.
// Private routes disappear when portal-identity is missing, false or erroneous.
func (s *Foundation) Browser(callbackURL, clientSecret string) (http.Handler, error) {
	u, err := url.Parse(callbackURL)
	if s == nil || s.pool == nil || s.broker == nil || s.broker.verifier == nil || s.broker.client == nil || clientSecret == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "/portal/callback" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, ErrDenied
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var allowed bool
	err = s.pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'portal_sessions','SELECT') AND has_table_privilege(current_user,'portal_sessions','INSERT') AND has_table_privilege(current_user,'portal_sessions','UPDATE') AND has_table_privilege(current_user,'portal_sessions','DELETE')`).Scan(&allowed)
	if err != nil || !allowed {
		return nil, ErrDenied
	}
	sm := scs.New()
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
	b := &browser{foundation: s, sessions: sm, origin: "https://" + u.Host, oauth: oauth2.Config{ClientID: s.broker.clientID, ClientSecret: clientSecret, Endpoint: s.broker.endpoint, RedirectURL: callbackURL, Scopes: []string{oidcScope}}}
	sm.ErrorFunc = func(w http.ResponseWriter, r *http.Request, _ error) {
		w.Header().Del("Location")
		b.render(w, r, 503, "unavailable", "", "")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /portal/sign-in", b.signIn)
	mux.HandleFunc("POST /portal/sign-in", b.begin)
	mux.HandleFunc("GET /portal/callback", b.callback)
	mux.HandleFunc("GET /portal/{$}", b.home)
	mux.HandleFunc("GET /portal/operator", b.operator)
	mux.HandleFunc("POST /portal/logout", b.logout)
	// Only the two public presentation assets are served, never template source.
	for name, contentType := range map[string]string{"portal.css": "text/css; charset=utf-8", "portal.js": "text/javascript; charset=utf-8"} {
		data, err := browserFiles.ReadFile("web/" + name)
		if err != nil {
			return nil, ErrDenied
		}
		mux.HandleFunc("GET /portal/assets/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(data)
		})
	}
	csrf := nosurf.New(mux)
	csrf.SetBaseCookie(http.Cookie{Name: "__Host-portal_csrf", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 1800})
	csrf.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { b.render(w, r, 403, "error", "", "") }))
	secured := sm.LoadAndSave(csrf)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// no-referrer forces native form POST Origin to "null". strict-origin
		// preserves same-origin CSRF checks without sharing callback paths/tokens.
		w.Header().Set("Referrer-Policy", "strict-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		issuer, _ := url.Parse(s.broker.issuer)
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; form-action 'self' https://"+issuer.Host+"; frame-ancestors 'none'; base-uri 'none'")
		if !s.ready(r.Context()) {
			http.NotFound(w, r)
			return
		}
		// Do not trust caller-controlled Host or forwarded-origin headers.
		if r.Host != u.Host {
			http.Error(w, "Portal unavailable", 421)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if origin := r.Header.Get("Origin"); origin != "" && origin != b.origin {
				b.render(w, r, 403, "error", "", "")
				return
			}
		}
		secured.ServeHTTP(w, r)
	}), nil
}

const oidcScope = "openid"

func language(r *http.Request) string {
	if r.URL.Query().Get("lang") == "da" || (r.URL.Query().Get("lang") == "" && r.PostForm.Get("lang") == "da") {
		return "da"
	}
	return "en"
}

func (b *browser) signIn(w http.ResponseWriter, r *http.Request) {
	b.render(w, r, 200, "sign-in", "", nosurf.Token(r))
}

func (b *browser) begin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || len(r.PostForm["invitation"]) > 1 || len(r.PostForm.Get("invitation")) > 256 {
		b.render(w, r, 400, "error", "", "")
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
	b.sessions.Put(r.Context(), "invitation", r.PostForm.Get("invitation"))
	lang := "en"
	if r.PostForm.Get("lang") == "da" {
		lang = "da"
	}
	b.sessions.Put(r.Context(), "lang", lang)
	b.sessions.SetDeadline(r.Context(), time.Now().Add(10*time.Minute))
	address := b.oauth.AuthCodeURL(a.State, oauth2.S256ChallengeOption(a.PKCEVerifier), oauth2.SetAuthURLParam("nonce", a.Nonce))
	http.Redirect(w, r, address, http.StatusSeeOther)
}

func (b *browser) callback(w http.ResponseWriter, r *http.Request) {
	// Recover presentation language from the durable attempt, never authority or
	// a redirect destination. Errors retain the language the client chose.
	if r.URL.Query().Get("lang") == "" && b.sessions.GetString(r.Context(), "lang") == "da" {
		copy := *r.URL
		q := copy.Query()
		q.Set("lang", "da")
		copy.RawQuery = q.Encode()
		r = r.Clone(r.Context())
		r.URL = &copy
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["state"]) != 1 || len(q["code"]) != 1 || len(q["error"]) != 0 || q.Get("state") == "" || q.Get("code") == "" || len(q.Get("code")) > 4096 || len(q.Get("state")) > 128 {
		b.render(w, r, 400, "error", "", "")
		return
	}
	nonce, verifier, err := b.foundation.consumeAttempt(r.Context(), q.Get("state"), b.sessions.GetString(r.Context(), "browser"))
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
	p, err := b.foundation.completeProof(ctx, nonce, raw, b.sessions.GetString(r.Context(), "invitation"))
	if err != nil {
		b.render(w, r, 400, "error", "", "")
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
	if lang != "da" {
		lang = "en"
	}
	http.Redirect(w, r, "/portal/?lang="+lang, http.StatusSeeOther)
}

func (b *browser) principal(ctx context.Context) Principal {
	return Principal{issuer: b.sessions.GetString(ctx, "issuer"), subject: b.sessions.GetString(ctx, "subject"), audience: b.foundation.broker.clientID, expires: time.Unix(b.sessions.GetInt64(ctx, "expires"), 0)}
}

func (b *browser) home(w http.ResponseWriter, r *http.Request) {
	client, _, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil {
		b.render(w, r, 401, "error", "", "")
		return
	}
	b.render(w, r, 200, "home", client, nosurf.Token(r))
}

func (b *browser) operator(w http.ResponseWriter, r *http.Request) {
	client, role, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil || role != "operator" {
		b.render(w, r, 403, "error", "", "")
		return
	}
	b.render(w, r, 200, "operator", client, nosurf.Token(r))
}

func (b *browser) logout(w http.ResponseWriter, r *http.Request) {
	if err := b.sessions.Destroy(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	http.Redirect(w, r, "/portal/sign-in?lang="+language(r), http.StatusSeeOther)
}

type pageData struct{ Lang, Kind, Client, CSRF, Title, Intro, Invitation, Action, SignedIn, Logout, Error, Unavailable, Back, Theme, System, Light, Dark, Operator, Boundary string }

func (b *browser) render(w http.ResponseWriter, r *http.Request, status int, kind, client, token string) {
	d := pageData{Lang: language(r), Kind: kind, Client: client, CSRF: token, Title: "Your client portal", Intro: "A private space for your work with Devantler Tech.", Invitation: "Invitation code (first sign-in only)", Action: "Continue to sign in", SignedIn: "You are signed in", Logout: "Sign out", Error: "Sign-in could not be completed. Try again, or contact Devantler Tech if you need an invitation.", Unavailable: "The portal is unavailable. Please try again later.", Back: "Back to the website", Theme: "Appearance", System: "System", Light: "Light", Dark: "Dark", Operator: "Operator", Boundary: "Ordering and payments are not available yet."}
	if d.Lang == "da" {
		d.Title = "Din kundeportal"
		d.Intro = "Et privat sted for dit samarbejde med Devantler Tech."
		d.Invitation = "Invitationskode (kun ved første login)"
		d.Action = "Fortsæt til login"
		d.SignedIn = "Du er logget ind"
		d.Logout = "Log ud"
		d.Error = "Login kunne ikke gennemføres. Prøv igen, eller kontakt Devantler Tech, hvis du mangler en invitation."
		d.Unavailable = "Portalen er ikke tilgængelig. Prøv igen senere."
		d.Back = "Tilbage til hjemmesiden"
		d.Theme = "Udseende"
		d.System = "System"
		d.Light = "Lys"
		d.Dark = "Mørk"
		d.Operator = "Operatør"
		d.Boundary = "Bestilling og betaling er endnu ikke tilgængelig."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// The static template is parsed at construction, has no caller-selected names,
	// and escapes all private display values. No raw errors or tokens are rendered.
	_ = browserPage.ExecuteTemplate(w, "portal.html", d)
}
