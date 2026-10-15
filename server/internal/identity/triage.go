package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

const triagePageSize = 20

var errTriageStorage = errors.New("request inbox unavailable")

type triageCursor struct {
	Submitted time.Time
	ID        string
}

func (s *Foundation) triageReady(ctx context.Context) bool {
	if !s.requestsReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-triage", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

func (s *Foundation) triageOperator(ctx context.Context, p Principal) error {
	if !s.triageReady(ctx) {
		return ErrDenied
	}
	_, role, err := s.membership(ctx, p)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return ErrDenied
		}
		return errTriageStorage
	}
	if role != "operator" {
		return ErrDenied
	}
	return nil
}

// Cursors choose a bounded position only; they carry no identity or authority.
func parseTriageCursor(raw string) (triageCursor, bool) {
	if raw == "" || len(raw) > 160 {
		return triageCursor{}, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return triageCursor{}, false
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[0] != "1" || !opaqueRequestID.MatchString(parts[2]) {
		return triageCursor{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil || stamp.Year() < 1970 || stamp.Nanosecond()%1000 != 0 || stamp.UTC().Format(time.RFC3339Nano) != parts[1] {
		return triageCursor{}, false
	}
	return triageCursor{stamp, parts[2]}, true
}

func encodeTriageCursor(d projectRequest) string {
	return base64.RawURLEncoding.EncodeToString([]byte("1|" + d.Submitted.UTC().Format(time.RFC3339Nano) + "|" + d.ID))
}

// Every data query independently requires the current operator role. The final
// membership/flag read rejects revocation, expiry or gate changes during a read.
func (s *Foundation) readTriage(ctx context.Context, p Principal, id string, cursor triageCursor) ([]projectRequest, error) {
	if err := s.triageOperator(ctx, p); err != nil {
		return nil, err
	}
	if id != "" && !opaqueRequestID.MatchString(id) {
		return nil, pgx.ErrNoRows
	}
	limit := triagePageSize + 1
	if id != "" {
		limit = 1
	}
	rows, err := s.pool.Query(ctx, `SELECT q.id,q.product,q.title,CASE WHEN $3='' THEN '' ELSE q.brief END,q.submitted_at FROM portal_requests q WHERE EXISTS(SELECT 1 FROM portal_memberships m WHERE m.issuer=$1 AND m.subject=$2 AND m.role='operator' AND NOT m.revoked) AND ($3='' OR q.id=$3) AND ($4='' OR (q.submitted_at,q.id COLLATE "C") > ($5::timestamptz,$4 COLLATE "C")) ORDER BY q.submitted_at,q.id COLLATE "C" LIMIT $6`, p.issuer, p.subject, id, cursor.ID, cursor.Submitted, limit)
	if err != nil {
		return nil, errTriageStorage
	}
	defer rows.Close()
	result, err := scanProjectRequests(rows)
	if err != nil {
		return nil, errTriageStorage
	}
	if err = s.triageOperator(ctx, p); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *browser) triageList(w http.ResponseWriter, r *http.Request) {
	b.showTriage(w, r, "")
}

func (b *browser) triageDetail(w http.ResponseWriter, r *http.Request) {
	b.showTriage(w, r, r.PathValue("id"))
}

func (b *browser) showTriage(w http.ResponseWriter, r *http.Request, id string) {
	p := b.principal(r.Context())
	if err := b.foundation.triageOperator(r.Context(), p); err != nil {
		b.triageError(w, r, err)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	var cursor triageCursor
	valid := err == nil
	if values, present := q["cursor"]; present {
		valid = valid && id == "" && len(values) == 1
		if valid {
			cursor, valid = parseTriageCursor(values[0])
		}
	}
	if !valid {
		b.render(w, r, 400, "triage-invalid", "", "")
		return
	}
	// Optional display evaluations may wait even when they return false/error.
	// Complete them before private reads retain their own current-access checks.
	proposalsEnabled := id != "" && b.foundation.proposalsReady(r.Context())
	sharingEnabled := proposalsEnabled && b.foundation.sharingReady(r.Context())
	rows, err := b.foundation.readTriage(r.Context(), p, id, cursor)
	if missingProjectRequest(id, rows, err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	page := b.triagePage(r, rows, id, q.Get("cursor"))
	if proposalsEnabled {
		proposals, err := b.foundation.readProposals(r.Context(), p, id)
		if err != nil {
			b.triageError(w, r, err)
			return
		}
		b.proposalPresentation(r, &page, proposals)
		if sharingEnabled {
			page.Visibility, err = b.foundation.readVisibility(r.Context(), p, id)
			if err != nil {
				b.triageError(w, r, err)
				return
			}
			page.SharingEnabled = true
			page.SharingKey = secret()
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = browserPage.ExecuteTemplate(w, "triage.html", page)
}

// triagePage builds presentation only, after showTriage's current authority and
// bounded data reads. The link contract uses the same cursor/language builder.
func (b *browser) triagePage(r *http.Request, rows []projectRequest, id, cursor string) triagePageData {
	page := triagePageData{pageData: b.data(r, "triage", "", ""), Requests: rows, Detail: id != ""}
	page.Title = "Project request inbox"
	page.Intro = "Review client briefs, oldest first. Reading a request does not approve work."
	page.Boundary = "Not approved for work"
	if page.Lang == "da" {
		page.Title = "Projektforespørgsler"
		page.Intro = "Læs kundernes forespørgsler, ældste først. At læse en forespørgsel godkender ikke arbejde."
		page.Boundary = "Ikke godkendt til arbejde"
	}
	if len(rows) > triagePageSize {
		page.Requests = rows[:triagePageSize]
		page.Next = triageURL(encodeTriageCursor(rows[triagePageSize-1]), page.Lang)
	}
	other := "da"
	if page.Lang == "da" {
		other = "en"
	}
	page.LanguageURL = triageURL(cursor, other)
	if id != "" {
		page.LanguageURL = "/portal/operator/requests/" + id + "?lang=" + other
	}
	return page
}

func (b *browser) triageError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrDenied) {
		b.render(w, r, 403, "error", "", "")
		return
	}
	b.render(w, r, 503, "unavailable", "", "")
}

func triageURL(cursor, lang string) string {
	q := url.Values{"lang": {lang}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return "/portal/operator/requests?" + q.Encode()
}

type triagePageData struct {
	pageData
	Requests          []projectRequest
	Detail            bool
	Next, LanguageURL string
	ProposalsEnabled  bool
	Proposals         []projectProposal
	ExpectedVersion   int
	ProposalKey       string
	SharingEnabled    bool
	SharingKey        string
	Visibility        proposalVisibility
}
