package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/justinas/nosurf"
	"github.com/open-feature/go-sdk/openfeature"
)

var creditIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$`)

type creditRevision struct {
	Source, Reference, Key, State, Note string
	Revision, Expected                  int
	AmountMinor                         int64
	Recorded                            time.Time
}

func (d creditRevision) AmountDKK(lang string) string {
	return (projectInvoice{AmountMinor: d.AmountMinor}).AmountDKK(lang)
}

func (d creditRevision) StateLabel(lang string) string {
	if lang == "da" {
		return map[string]string{"confirmed": "Bekræftet", "unverified": "Ubekræftet", "disputed": "Bestridt", "invalidated": "Ugyldiggjort"}[d.State]
	}
	return map[string]string{"confirmed": "Confirmed", "unverified": "Unverified", "disputed": "Disputed", "invalidated": "Invalidated"}[d.State]
}

type creditTransfer struct {
	Latest  creditRevision
	History []creditRevision
	Form    creditFormData
}

type creditHistory struct {
	Invoice   projectInvoice
	Transfers []creditTransfer
	Total     int64
	Unsettled bool
}

func (h creditHistory) TotalDKK(lang string) string {
	return (projectInvoice{AmountMinor: h.Total}).AmountDKK(lang)
}

// Status reports recorded evidence, never permission to start work. Unknown or
// disputed evidence prevents a fully-credited result even when other funds match.
func (h creditHistory) Status(lang string) string {
	state := "none"
	switch {
	case h.Unsettled:
		state = "unsettled"
	case h.Total > h.Invoice.AmountMinor:
		state = "excess"
	case h.Total == h.Invoice.AmountMinor:
		state = "full"
	case h.Total > 0:
		state = "partial"
	}
	if lang == "da" {
		return map[string]string{"none": "Ingen bekræftede krediteringer", "unsettled": "Uafklaret dokumentation", "excess": "Overskydende kreditering", "full": "Fuldt krediteret", "partial": "Delvist krediteret"}[state]
	}
	return map[string]string{"none": "No confirmed credits", "unsettled": "Unresolved evidence", "excess": "Excess credit", "full": "Fully credited", "partial": "Partially credited"}[state]
}

// creditsReady has no production activation mapping and is false by default.
func (s *Foundation) creditsReady(ctx context.Context) bool {
	if !s.invoicesReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-credit-reconciliation", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx) && ctx.Err() == nil
}

func (s *Foundation) creditOperator(ctx context.Context, p Principal) error {
	if !s.creditsReady(ctx) {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

// parseCredit preserves external identity bytes. Normalization could conflate
// distinct bank references, so the namespace and reference are case-sensitive.
func parseCredit(r *http.Request) (creditRevision, bool) {
	if err := r.ParseForm(); err != nil {
		return creditRevision{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return creditRevision{}, false
		}
		switch name {
		case "csrf_token", "lang", "credit_key", "credit_expected", "source", "reference", "state", "amount_dkk", "note":
		default:
			return creditRevision{}, false
		}
	}
	expected, valid := proposalNumber(r.PostForm.Get("credit_expected"), 15)
	d := creditRevision{Expected: expected, Key: r.PostForm.Get("credit_key"), Source: r.PostForm.Get("source"), Reference: r.PostForm.Get("reference"), State: r.PostForm.Get("state"), Note: strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(r.PostForm.Get("note"), "\r\n", "\n"), "\r", "\n"))}
	amountOK := false
	switch d.State {
	case "confirmed", "unverified", "disputed":
		d.AmountMinor, amountOK = invoiceMinorUnits(r.PostForm.Get("amount_dkk"))
	case "invalidated":
		amountOK = r.PostForm.Get("amount_dkk") == ""
	}
	return d, valid && amountOK && (expected < 15 || d.State == "invalidated") && opaqueRequestID.MatchString(d.Key) && creditIdentity.MatchString(d.Source) && creditIdentity.MatchString(d.Reference) && validRequestText(d.Note, 300, true)
}

const creditColumns = `source,reference,submission_key,state,note,revision,expected_revision,amount_minor,recorded_at`

func scanCredit(row pgx.Row) (creditRevision, error) {
	var d creditRevision
	err := row.Scan(&d.Source, &d.Reference, &d.Key, &d.State, &d.Note, &d.Revision, &d.Expected, &d.AmountMinor, &d.Recorded)
	return d, err
}

// boundCreditInvoice derives the client and accepted-version binding from the
// stored invoice. Historical money remains recordable after a re-quote/withdrawal.
func boundCreditInvoice(ctx context.Context, tx pgx.Tx, id, client string, invoice int) (projectInvoice, error) {
	d, err := scanInvoice(tx.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE request_id=$1 AND client_id=$2 AND sequence=$3`, id, client, invoice))
	if errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	if err != nil {
		return d, errTriageStorage
	}
	_, acceptance, err := currentInvoiceAcceptance(ctx, tx, id, client)
	if err != nil && !errors.Is(err, errProposalConflict) {
		return d, err
	}
	d.Current = acceptance != "" && d.Acceptance == acceptance
	return d, nil
}

// persistCredit appends one content-bound receipt under the request lock. The
// global transfer PK arbitrates competing invoices even on different requests.
func (s *Foundation) persistCredit(ctx context.Context, p Principal, id string, invoice int, d creditRevision) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.creditOperator(ctx, p); err != nil {
		return err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if _, err = boundCreditInvoice(ctx, tx, id, client, invoice); err != nil {
		return err
	}
	var issuer, subject string
	var savedInvoice int
	err = tx.QueryRow(ctx, `SELECT operator_issuer,operator_subject,invoice_sequence FROM portal_credit_revisions WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key).Scan(&issuer, &subject, &savedInvoice)
	if err == nil {
		saved, readErr := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM portal_credit_revisions WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key))
		if readErr != nil {
			return errTriageStorage
		}
		if issuer != p.issuer || subject != p.subject || savedInvoice != invoice || saved.Source != d.Source || saved.Reference != d.Reference || saved.State != d.State || saved.Note != d.Note || saved.AmountMinor != d.AmountMinor || saved.Expected != d.Expected {
			return errProposalConflict
		}
		return commitProposalWrite(ctx, p, tx, s.creditsReady)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	if err = reserveCreditTransfer(ctx, tx, id, client, invoice, d); err != nil {
		return err
	}
	var revision int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM portal_credit_revisions WHERE source=$1 AND reference=$2`, d.Source, d.Reference).Scan(&revision); err != nil {
		return errTriageStorage
	}
	if revision != d.Expected || revision >= 16 || (revision == 15 && d.State != "invalidated") {
		return errProposalConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO portal_credit_revisions(source,reference,request_id,client_id,invoice_sequence,revision,expected_revision,submission_key,state,amount_minor,note,operator_issuer,operator_subject) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, d.Source, d.Reference, id, client, invoice, revision+1, d.Expected, d.Key, d.State, d.AmountMinor, d.Note, p.issuer, p.subject)
	if err != nil {
		return creditWriteError(err)
	}
	return commitProposalWrite(ctx, p, tx, s.creditsReady)
}

// reserveCreditTransfer never reallocates a previously reserved identity.
func reserveCreditTransfer(ctx context.Context, tx pgx.Tx, id, client string, invoice int, d creditRevision) error {
	var savedID, savedClient string
	var savedInvoice int
	err := tx.QueryRow(ctx, `SELECT request_id,client_id,invoice_sequence FROM portal_credit_transfers WHERE source=$1 AND reference=$2`, d.Source, d.Reference).Scan(&savedID, &savedClient, &savedInvoice)
	if err == nil {
		if savedID != id || savedClient != client || savedInvoice != invoice {
			return errProposalConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	if d.Expected != 0 {
		return errProposalConflict
	}
	var ordinal int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(ordinal),0) FROM portal_credit_transfers WHERE request_id=$1 AND client_id=$2 AND invoice_sequence=$3`, id, client, invoice).Scan(&ordinal); err != nil {
		return errTriageStorage
	}
	if ordinal >= 32 {
		return errProposalConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO portal_credit_transfers(source,reference,request_id,client_id,invoice_sequence,ordinal) VALUES($1,$2,$3,$4,$5,$6)`, d.Source, d.Reference, id, client, invoice, ordinal+1)
	return creditWriteError(err)
}

func creditWriteError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		return errProposalConflict
	}
	return errTriageStorage
}

// readCredits owns the authenticated request transaction and its final authority check.
func (s *Foundation) readCredits(ctx context.Context, p Principal, id string, invoice int) (creditHistory, error) {
	if err := s.creditOperator(ctx, p); err != nil {
		return creditHistory{}, err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return creditHistory{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	h, err := readCreditHistory(ctx, tx, id, client, invoice)
	if err != nil {
		return creditHistory{}, err
	}
	if err = commitProposalWrite(ctx, p, tx, s.creditsReady); err != nil {
		return creditHistory{}, err
	}
	return h, nil
}

// readCreditHistory reads the exact invoice and complete credit evidence within
// its caller's request-locked transaction. It neither authorizes nor commits;
// the owner must perform its final authority check before releasing the result.
func readCreditHistory(ctx context.Context, tx pgx.Tx, id, client string, invoice int) (creditHistory, error) {
	d, err := boundCreditInvoice(ctx, tx, id, client, invoice)
	if err != nil {
		return creditHistory{}, err
	}
	h := creditHistory{Invoice: d}
	rows, err := tx.Query(ctx, `SELECT `+creditColumns+` FROM portal_credit_revisions WHERE request_id=$1 AND client_id=$2 AND invoice_sequence=$3 ORDER BY source,reference,revision DESC LIMIT 513`, id, client, invoice)
	if err != nil {
		return creditHistory{}, errTriageStorage
	}
	count := 0
	for rows.Next() {
		d, err := scanCredit(rows)
		if err != nil {
			rows.Close()
			return creditHistory{}, errTriageStorage
		}
		count++
		if len(h.Transfers) == 0 || h.Transfers[len(h.Transfers)-1].Latest.Source != d.Source || h.Transfers[len(h.Transfers)-1].Latest.Reference != d.Reference {
			h.Transfers = append(h.Transfers, creditTransfer{Latest: d})
			if d.State == "confirmed" {
				h.Total += d.AmountMinor
			}
			h.Unsettled = h.Unsettled || d.State == "unverified" || d.State == "disputed"
		}
		last := &h.Transfers[len(h.Transfers)-1]
		last.History = append(last.History, d)
	}
	rows.Close()
	if rows.Err() != nil || count > 512 || len(h.Transfers) > 32 {
		return creditHistory{}, errTriageStorage
	}
	return h, nil
}

type creditPageData struct {
	pageData
	History           creditHistory
	Path, LanguageURL string
	Form              creditFormData
}

type creditFormData struct {
	Lang, CSRF, Path, Key, Source, Reference string
	Expected                                 int
}

func creditRoute(r *http.Request) (string, int, bool) {
	id := r.PathValue("id")
	sequence, ok := proposalNumber(r.PathValue("invoice"), 50)
	return id, sequence, ok && sequence > 0 && opaqueRequestID.MatchString(id)
}

func (b *browser) showCredits(w http.ResponseWriter, r *http.Request) {
	id, invoice, valid := creditRoute(r)
	if !valid {
		http.NotFound(w, r)
		return
	}
	history, err := b.foundation.readCredits(r.Context(), b.principal(r.Context()), id, invoice)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	page := creditPageData{pageData: b.data(r, "credits", "", nosurf.Token(r)), History: history, Path: fmt.Sprintf("/portal/operator/requests/%s/invoices/%d/credits", id, invoice)}
	page.Form = creditFormData{Lang: page.Lang, CSRF: page.CSRF, Path: page.Path, Key: secret()}
	for i := range page.History.Transfers {
		d := page.History.Transfers[i].Latest
		page.History.Transfers[i].Form = creditFormData{Lang: page.Lang, CSRF: page.CSRF, Path: page.Path, Key: secret(), Source: d.Source, Reference: d.Reference, Expected: d.Revision}
	}
	page.Title, page.Intro = "Bank-credit evidence", "Record observed transfers and corrections for this invoice."
	other := "da"
	if page.Lang == "da" {
		page.Title, page.Intro, other = "Dokumentation for bankkrediteringer", "Registrer observerede overførsler og rettelser til denne faktura.", "en"
	}
	page.LanguageURL = page.Path + "?lang=" + other
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = browserPage.ExecuteTemplate(w, "credits.html", page)
}

func (b *browser) submitCredit(w http.ResponseWriter, r *http.Request) {
	if err := b.foundation.creditOperator(r.Context(), b.principal(r.Context())); err != nil {
		b.triageError(w, r, err)
		return
	}
	id, invoice, valid := creditRoute(r)
	if !valid {
		http.NotFound(w, r)
		return
	}
	d, valid := parseCredit(r)
	if !valid {
		b.render(w, r, 400, "credit-error", "", "")
		return
	}
	err := b.foundation.persistCredit(r.Context(), b.principal(r.Context()), id, invoice, d)
	switch {
	case err == nil:
		http.Redirect(w, r, r.URL.Path+"?lang="+language(r), http.StatusSeeOther)
	case errors.Is(err, errProposalConflict):
		b.render(w, r, 409, "credit-error", "", "")
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
	default:
		b.triageError(w, r, err)
	}
}
