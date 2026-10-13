package identity

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
	"github.com/open-feature/go-sdk/openfeature"
)

// A project request records interest only, never order or execution authority.
type projectRequest struct {
	ID, Key, Product, Title, Brief string
	Submitted                      time.Time
}

var opaqueRequestID = regexp.MustCompile(`^[A-Z2-7]{26}$`)
var errRequestConflict = errors.New("request retry content differs")

func (s *Foundation) requestsReady(ctx context.Context) bool {
	if !s.ready(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-requests", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

func (s *Foundation) requestClient(ctx context.Context, p Principal) (string, error) {
	if !s.requestsReady(ctx) {
		return "", ErrDenied
	}
	client, role, err := s.membership(ctx, p)
	if err != nil || role != "client" {
		return "", ErrDenied
	}
	return client, nil
}

func validRequestText(text string, limit int, multiline bool) bool {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit {
		return false
	}
	for _, r := range text {
		if !unicode.IsControl(r) || (multiline && strings.ContainsRune("\n\r\t", r)) {
			continue
		}
		return false
	}
	return true
}

func parseProjectRequest(r *http.Request) (projectRequest, bool) {
	if err := r.ParseForm(); err != nil {
		return projectRequest{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return projectRequest{}, false
		}
		switch name {
		case "submission_key", "product", "title", "brief", "csrf_token", "lang":
		default:
			return projectRequest{}, false
		}
	}
	// Native textarea values expose LF, but native form submission encodes CRLF.
	// Canonicalize line breaks before counting and retry comparison, not after.
	brief := strings.ReplaceAll(strings.ReplaceAll(r.PostForm.Get("brief"), "\r\n", "\n"), "\r", "\n")
	d := projectRequest{Key: r.PostForm.Get("submission_key"), Product: r.PostForm.Get("product"), Title: strings.TrimSpace(r.PostForm.Get("title")), Brief: strings.TrimSpace(brief)}
	return d, opaqueRequestID.MatchString(d.Key) && (d.Product == "website" || d.Product == "app" || d.Product == "service") && validRequestText(r.PostForm.Get("title"), 120, false) && validRequestText(brief, 1200, true)
}

// persistRequest locks current membership through commit; the unique per-client
// retry key arbitrates concurrent processes without an in-memory authority.
func (s *Foundation) persistRequest(ctx context.Context, p Principal, d projectRequest) (projectRequest, error) {
	if _, err := s.requestClient(ctx, p); err != nil {
		return projectRequest{}, ErrDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return projectRequest{}, ErrDenied
	}
	defer func() {
		// Commit decides success. Rollback is cleanup; pgx discards a connection
		// when rollback fails, and a committed transaction is already closed.
		_ = tx.Rollback(ctx)
	}()
	var client string
	if err = tx.QueryRow(ctx, `SELECT client_id FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked FOR SHARE`, p.issuer, p.subject).Scan(&client); err != nil {
		return projectRequest{}, ErrDenied
	}
	d.ID = secret()
	if _, err = tx.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(client_id,submission_key) DO NOTHING`, d.ID, client, d.Key, d.Product, d.Title, d.Brief); err != nil {
		return projectRequest{}, ErrDenied
	}
	var saved projectRequest
	if err = tx.QueryRow(ctx, `SELECT id,product,title,brief,submitted_at FROM portal_requests WHERE client_id=$1 AND submission_key=$2`, client, d.Key).Scan(&saved.ID, &saved.Product, &saved.Title, &saved.Brief, &saved.Submitted); err != nil {
		return projectRequest{}, ErrDenied
	}
	if saved.Product != d.Product || saved.Title != d.Title || saved.Brief != d.Brief {
		return projectRequest{}, errRequestConflict
	}
	if !s.requestsReady(ctx) || !time.Now().Before(p.expires) {
		return projectRequest{}, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return projectRequest{}, ErrDenied
	}
	return saved, nil
}

func (s *Foundation) readRequests(ctx context.Context, p Principal, id string) ([]projectRequest, error) {
	client, err := s.requestClient(ctx, p)
	if err != nil {
		return nil, ErrDenied
	}
	if id != "" && !opaqueRequestID.MatchString(id) {
		return nil, pgx.ErrNoRows
	}
	rows, err := s.pool.Query(ctx, `SELECT q.id,q.product,q.title,q.brief,q.submitted_at FROM portal_requests q JOIN portal_memberships m ON m.client_id=q.client_id WHERE q.client_id=$1 AND m.issuer=$2 AND m.subject=$3 AND m.role='client' AND NOT m.revoked AND ($4='' OR q.id=$4) ORDER BY q.submitted_at DESC,q.id LIMIT 50`, client, p.issuer, p.subject, id)
	if err != nil {
		return nil, ErrDenied
	}
	defer rows.Close()
	result, err := scanProjectRequests(rows)
	if err != nil || !s.requestsReady(ctx) || !time.Now().Before(p.expires) {
		return nil, ErrDenied
	}
	return result, nil
}

func (b *browser) newRequest(w http.ResponseWriter, r *http.Request) {
	if _, err := b.foundation.requestClient(r.Context(), b.principal(r.Context())); err != nil {
		b.render(w, r, 403, "error", "", "")
		return
	}
	b.renderRequests(w, r, 200, "new", projectRequest{Key: secret()}, nil, "")
}

func (b *browser) requests(w http.ResponseWriter, r *http.Request) { b.showRequests(w, r, "") }
func (b *browser) requestReceipt(w http.ResponseWriter, r *http.Request) {
	b.showRequests(w, r, r.PathValue("id"))
}

func (b *browser) showRequests(w http.ResponseWriter, r *http.Request, id string) {
	rows, err := b.foundation.readRequests(r.Context(), b.principal(r.Context()), id)
	if missingProjectRequest(id, rows, err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		b.render(w, r, 403, "error", "", "")
		return
	}
	kind := "list"
	var receipt projectRequest
	if id != "" {
		kind = "receipt"
		receipt = rows[0]
	}
	b.renderRequests(w, r, 200, kind, receipt, rows, "")
}

func (b *browser) submitRequest(w http.ResponseWriter, r *http.Request) {
	p := b.principal(r.Context())
	if _, err := b.foundation.requestClient(r.Context(), p); err != nil {
		b.render(w, r, 403, "error", "", "")
		return
	}
	d, valid := parseProjectRequest(r)
	if !valid {
		if !opaqueRequestID.MatchString(d.Key) {
			d.Key = secret()
		}
		if !validRequestText(d.Title, 120, false) {
			d.Title = ""
		}
		if !validRequestText(d.Brief, 1200, true) {
			d.Brief = ""
		}
		b.renderRequests(w, r, 400, "new", d, nil, "invalid")
		return
	}
	saved, err := b.foundation.persistRequest(r.Context(), p, d)
	if errors.Is(err, errRequestConflict) {
		b.renderRequests(w, r, 409, "conflict", projectRequest{}, nil, "conflict")
		return
	}
	if err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	http.Redirect(w, r, "/portal/requests/"+saved.ID+"?lang="+language(r), http.StatusSeeOther)
}

type requestPageData struct {
	pageData
	View, FormError string
	Request         projectRequest
	Requests        []projectRequest
}

func (b *browser) renderRequests(w http.ResponseWriter, r *http.Request, status int, view string, d projectRequest, rows []projectRequest, problem string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = browserPage.ExecuteTemplate(w, "requests.html", b.requestPage(r, view, d, rows, problem))
}

func (b *browser) requestPage(r *http.Request, view string, d projectRequest, rows []projectRequest, problem string) requestPageData {
	page := requestPageData{pageData: b.data(r, "requests", "", nosurf.Token(r)), View: view, FormError: problem, Request: d, Requests: rows}
	page.Title = "Your project requests"
	page.Intro = "Tell us what you would like to build. Sending a request is free and does not start paid work."
	if page.Lang == "da" {
		page.Title = "Dine projektforespørgsler"
		page.Intro = "Fortæl os, hvad du gerne vil bygge. Det er gratis at sende en forespørgsel og starter ikke betalt arbejde."
	}
	return page
}
