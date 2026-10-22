package identity

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
)

type workPageData struct {
	pageData
	Work             workReviewData
	Path, Base, Next string
	RevocationKey    string
	ReviewInvoice    int
}

// workReviewQuery accepts at most one language, bounded invoice selector and
// history cursor; query input supplies no client identity or decision authority.
func workReviewQuery(r *http.Request) (int, triageCursor, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, triageCursor{}, false
	}
	for name, values := range q {
		if len(values) != 1 || (name != "lang" && name != "invoice" && name != "cursor") {
			return 0, triageCursor{}, false
		}
	}
	invoice, cursor := 0, triageCursor{}
	if raw, present := q["invoice"]; present {
		var valid bool
		invoice, valid = proposalNumber(raw[0], 50)
		if !valid || invoice == 0 {
			return 0, cursor, false
		}
	}
	if raw, present := q["cursor"]; present {
		var valid bool
		cursor, valid = parseTriageCursor(raw[0])
		if !valid {
			return 0, cursor, false
		}
	}
	return invoice, cursor, true
}

// workPage localizes already-authorized evidence and preserves its invoice and
// history selectors in native navigation without writing a work decision.
func (b *browser) workPage(r *http.Request, work workReviewData, invoice int) workPageData {
	page := workPageData{pageData: b.data(r, "work-review", "", nosurf.Token(r)), Work: work, Base: "/portal/operator/requests/" + work.Request, RevocationKey: secret(), ReviewInvoice: invoice}
	page.Path = page.Base + "/work-review"
	page.Title, page.Intro = "Work decisions", "Review an exact agreement and its funding before recording an explicit decision."
	other := "da"
	if page.Lang == "da" {
		page.Title, page.Intro, other = "Arbejdsbeslutninger", "Gennemgå den præcise aftale og dokumenteret finansiering, før du registrerer en udtrykkelig beslutning.", "en"
	}
	q := r.URL.Query()
	q.Set("lang", other)
	page.LanguageURL = page.Path + "?" + q.Encode()
	if work.Next.ID != "" {
		q.Set("lang", page.Lang)
		q.Set("cursor", encodeTriageCursor(projectRequest{Submitted: work.Next.Submitted, ID: work.Next.ID}))
		page.Next = page.Path + "?" + q.Encode()
	}
	return page
}

// captureWorkResponse shares the bounded original-session capture before either
// a review or decision acquires transaction locks and its pool connection.
func (b *browser) captureWorkResponse(r *http.Request) (context.Context, context.CancelFunc, durableResponseSnapshot, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	snapshot, ok := b.captureDurableResponse(ctx, b.contactAuthority(r.Context(), ""))
	return ctx, cancel, snapshot, ok
}

// showWorkReview buffers the entire frame. The final session checkpoint runs
// after all SDK waits, under the same request/membership locks as its evidence.
func (b *browser) showWorkReview(w http.ResponseWriter, r *http.Request) {
	invoice, cursor, valid := workReviewQuery(r)
	if !valid {
		b.workError(w, r, 400)
		return
	}
	var buffer bytes.Buffer
	ctx, cancel, snapshot, ok := b.captureWorkResponse(r)
	defer cancel()
	if !ok {
		b.triageError(w, r, ErrDenied)
		return
	}
	_, err := b.foundation.readWorkReviewChecked(ctx, b.principal(r.Context()), r.PathValue("id"), invoice, cursor, func(ctx context.Context, tx pgx.Tx, work workReviewData) error {
		page := b.workPage(r, work, invoice)
		if browserPage.ExecuteTemplate(&buffer, "work-review.html", page) != nil {
			return errTriageStorage
		}
		if !b.durableSnapshotAllowed(ctx, snapshot, work.OperatorClient, "operator", tx) {
			return ErrDenied
		}
		return nil
	})
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}

// parseWorkForm requires the exact native form fields and canonical generation;
// the domain transaction independently validates every submitted comparison.
func parseWorkForm(r *http.Request, approval bool) (workApprovalInput, workRevocationInput, bool) {
	if r.ParseForm() != nil {
		return workApprovalInput{}, workRevocationInput{}, false
	}
	allowed := map[string]bool{"csrf_token": true, "lang": true, "work_key": true, "work_expected": true}
	if approval {
		for _, field := range []string{"acceptance", "invoice", "evidence_hash"} {
			allowed[field] = true
		}
	} else {
		allowed["approval"] = true
	}
	if len(r.PostForm) != len(allowed) {
		return workApprovalInput{}, workRevocationInput{}, false
	}
	for field, values := range r.PostForm {
		if !allowed[field] || len(values) != 1 {
			return workApprovalInput{}, workRevocationInput{}, false
		}
	}
	raw := r.PostForm.Get("work_expected")
	expected, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || expected < 0 || expected == 1<<63-1 || strconv.FormatInt(expected, 10) != raw || !opaqueRequestID.MatchString(r.PostForm.Get("work_key")) || (r.PostForm.Get("lang") != "en" && r.PostForm.Get("lang") != "da") {
		return workApprovalInput{}, workRevocationInput{}, false
	}
	if !approval {
		d := workRevocationInput{Key: r.PostForm.Get("work_key"), Approval: r.PostForm.Get("approval"), Expected: expected}
		return workApprovalInput{}, d, opaqueRequestID.MatchString(d.Approval)
	}
	invoice, valid := proposalNumber(r.PostForm.Get("invoice"), 50)
	d := workApprovalInput{Key: r.PostForm.Get("work_key"), Expected: expected, Invoice: invoice, Acceptance: r.PostForm.Get("acceptance"), EvidenceHash: r.PostForm.Get("evidence_hash")}
	return d, workRevocationInput{}, valid && invoice > 0 && opaqueRequestID.MatchString(d.Acceptance) && len(d.EvidenceHash) == 64
}

// approveWork selects explicit approval under the shared secured POST boundary.
func (b *browser) approveWork(w http.ResponseWriter, r *http.Request) {
	b.submitWork(w, r, true)
}

// revokeWork selects withdrawal without requiring valid historical funding.
func (b *browser) revokeWork(w http.ResponseWriter, r *http.Request) {
	b.submitWork(w, r, false)
}

// submitWork binds the original durable session after the domain's final SDK
// wait and held locks, then redirects only after the explicit decision commits.
func (b *browser) submitWork(w http.ResponseWriter, r *http.Request, approval bool) {
	a, revocation, valid := parseWorkForm(r, approval)
	if !valid {
		b.workError(w, r, 400)
		return
	}
	var err error
	ctx, cancel, snapshot, ok := b.captureWorkResponse(r)
	defer cancel()
	if !ok {
		b.triageError(w, r, ErrDenied)
		return
	}
	check := func(ctx context.Context, tx pgx.Tx, client string) error {
		if !b.durableSnapshotAllowed(ctx, snapshot, client, "operator", tx) {
			return ErrDenied
		}
		return nil
	}
	if approval {
		_, err = b.foundation.persistWorkApprovalChecked(ctx, b.principal(r.Context()), r.PathValue("id"), a, check)
	} else {
		err = b.foundation.revokeWorkApprovalChecked(ctx, b.principal(r.Context()), r.PathValue("id"), revocation, check)
	}
	switch {
	case err == nil:
		http.Redirect(w, r, "/portal/operator/requests/"+r.PathValue("id")+"/work-review?lang="+language(r), http.StatusSeeOther)
	case errors.Is(err, errProposalConflict):
		b.workError(w, r, 409)
	default:
		b.triageError(w, r, err)
	}
}

// workError returns a localized recovery instruction without reflecting private
// evidence or raw storage errors in the failed decision response.
func (b *browser) workError(w http.ResponseWriter, r *http.Request, status int) {
	page := b.data(r, "unavailable", "", "")
	page.Unavailable = "The work decision was not recorded. Reload the review and check its current agreement, funding and selection before trying again."
	if page.Lang == "da" {
		page.Unavailable = "Arbejdsbeslutningen blev ikke registreret. Genindlæs gennemgangen, og kontrollér den aktuelle aftale, finansiering og valgte beslutning, før du prøver igen."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = browserPage.ExecuteTemplate(w, "portal.html", page)
}
