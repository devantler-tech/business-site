package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/open-feature/go-sdk/openfeature"
)

var invoiceReference = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._/-]{0,63}$`)
var invoiceAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})([.,][0-9]{1,2})?$`)

type projectInvoice struct {
	Sequence, Expected, Version, Event        int
	Key, Acceptance, Hash, Reference, Purpose string
	AmountMinor                               int64
	Recorded                                  time.Time
	Current                                   bool
}

// AmountDKK displays exact minor units with the locale's decimal separator and no grouping.
func (d projectInvoice) AmountDKK(lang string) string {
	separator := "."
	if lang == "da" {
		separator = ","
	}
	return fmt.Sprintf("%d%s%02d", d.AmountMinor/100, separator, d.AmountMinor%100)
}

type invoiceHistory struct {
	Records    []projectInvoice
	Acceptance string
	Expected   int
}

// invoicesReady requires every prerequisite and a fresh independent invoice decision.
func (s *Foundation) invoicesReady(ctx context.Context) bool {
	if !s.acceptanceReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-invoices", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx) && ctx.Err() == nil
}

// invoiceOperator combines the invoice release decision with current operator authority.
func (s *Foundation) invoiceOperator(ctx context.Context, p Principal) error {
	if !s.invoicesReady(ctx) {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

// invoiceMinorUnits parses the operator's decimal DKK without floating point or
// inferring invoice timing, tax treatment or an amount from proposal prices.
func invoiceMinorUnits(raw string) (int64, bool) {
	if !invoiceAmount.MatchString(raw) {
		return 0, false
	}
	parts := strings.Split(strings.ReplaceAll(raw, ",", "."), ".")
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	minor := whole * 100
	if len(parts) == 2 {
		fraction, err := strconv.ParseInt((parts[1] + "0")[:2], 10, 64)
		if err != nil {
			return 0, false
		}
		minor += fraction
	}
	return minor, minor > 0 && minor <= 1000000000
}

// parseInvoice accepts one bounded native form without caller-supplied client or payment state.
func parseInvoice(r *http.Request) (projectInvoice, bool) {
	if err := r.ParseForm(); err != nil {
		return projectInvoice{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return projectInvoice{}, false
		}
		switch name {
		case "csrf_token", "lang", "invoice_key", "invoice_acceptance", "invoice_expected", "reference", "purpose", "amount_dkk":
		default:
			return projectInvoice{}, false
		}
	}
	expected, valid := proposalNumber(r.PostForm.Get("invoice_expected"), 49)
	minor, amountOK := invoiceMinorUnits(r.PostForm.Get("amount_dkk"))
	d := projectInvoice{Expected: expected, AmountMinor: minor, Key: r.PostForm.Get("invoice_key"), Acceptance: r.PostForm.Get("invoice_acceptance"), Reference: strings.ToUpper(strings.TrimSpace(r.PostForm.Get("reference"))), Purpose: strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(r.PostForm.Get("purpose"), "\r\n", "\n"), "\r", "\n"))}
	return d, valid && amountOK && opaqueRequestID.MatchString(d.Key) && opaqueRequestID.MatchString(d.Acceptance) && invoiceReference.MatchString(d.Reference) && validRequestText(d.Purpose, 300, true)
}

const invoiceColumns = `i.sequence,i.expected_sequence,i.submission_key,i.acceptance_id,i.proposal_version,i.visibility_event,i.content_hash,i.reference,i.purpose,i.amount_minor,i.recorded_at`

// scanInvoice materializes the explicit private invoice columns, preserving storage errors.
func scanInvoice(row pgx.Row) (projectInvoice, error) {
	var d projectInvoice
	err := row.Scan(&d.Sequence, &d.Expected, &d.Key, &d.Acceptance, &d.Version, &d.Event, &d.Hash, &d.Reference, &d.Purpose, &d.AmountMinor, &d.Recorded)
	return d, err
}

// currentInvoiceAcceptance requires actual current consent under the shared
// request lock, not an acceptance identifier asserted by the browser.
func currentInvoiceAcceptance(ctx context.Context, tx pgx.Tx, id, client string) (proposalReview, string, error) {
	review, err := currentProposalReview(ctx, tx, id, client)
	if err != nil {
		return proposalReview{}, "", err
	}
	var acceptance string
	err = tx.QueryRow(ctx, `SELECT id FROM portal_proposal_acceptances WHERE request_id=$1 AND client_id=$2 AND proposal_version=$3 AND visibility_event=$4 AND content_hash=$5`, id, client, review.Proposal.Version, review.Event, review.Hash).Scan(&acceptance)
	if errors.Is(err, pgx.ErrNoRows) {
		return proposalReview{}, "", errProposalConflict
	}
	if err != nil {
		return proposalReview{}, "", errTriageStorage
	}
	return review, acceptance, nil
}

// persistInvoice binds one externally issued invoice; it neither confirms money
// nor grants execution. Identical retries retain their original historical row.
func (s *Foundation) persistInvoice(ctx context.Context, p Principal, id string, d projectInvoice) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.invoiceOperator(ctx, p); err != nil {
		return err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	var issuer, subject string
	err = tx.QueryRow(ctx, `SELECT operator_issuer,operator_subject FROM portal_invoices WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key).Scan(&issuer, &subject)
	if err == nil {
		saved, readErr := scanInvoice(tx.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE i.request_id=$1 AND i.client_id=$2 AND i.submission_key=$3`, id, client, d.Key))
		if readErr != nil {
			return errTriageStorage
		}
		if issuer != p.issuer || subject != p.subject || saved.Expected != d.Expected || saved.Acceptance != d.Acceptance || saved.Reference != d.Reference || saved.Purpose != d.Purpose || saved.AmountMinor != d.AmountMinor {
			return errProposalConflict
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return errTriageStorage
		}
		var sequence int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0) FROM portal_invoices WHERE request_id=$1 AND client_id=$2`, id, client).Scan(&sequence); err != nil {
			return errTriageStorage
		}
		if sequence != d.Expected || sequence >= 50 {
			return errProposalConflict
		}
		review, acceptance, err := currentInvoiceAcceptance(ctx, tx, id, client)
		if err != nil {
			return err
		}
		if acceptance != d.Acceptance {
			return errProposalConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO portal_invoices(request_id,client_id,sequence,expected_sequence,submission_key,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,reference,purpose,currency,amount_minor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'DKK',$14)`, id, client, sequence+1, d.Expected, d.Key, acceptance, review.Proposal.Version, review.Event, review.Hash, p.issuer, p.subject, d.Reference, d.Purpose, d.AmountMinor)
		if err != nil {
			var conflict *pgconn.PgError
			if errors.As(err, &conflict) && conflict.Code == "23505" {
				return errProposalConflict
			}
			return errTriageStorage
		}
	}
	return commitProposalWrite(ctx, p, tx, s.invoicesReady)
}

// readInvoices materializes a complete bounded history under current operator
// membership; stale acceptance remains historical, never newly authorized scope.
func (s *Foundation) readInvoices(ctx context.Context, p Principal, id string) (invoiceHistory, error) {
	if err := s.invoiceOperator(ctx, p); err != nil {
		return invoiceHistory{}, err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return invoiceHistory{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	_, acceptance, err := currentInvoiceAcceptance(ctx, tx, id, client)
	if err != nil && !errors.Is(err, errProposalConflict) {
		return invoiceHistory{}, err
	}
	result := invoiceHistory{Acceptance: acceptance}
	rows, err := tx.Query(ctx, `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE i.request_id=$1 AND i.client_id=$2 ORDER BY i.sequence DESC LIMIT 50`, id, client)
	if err != nil {
		return invoiceHistory{}, errTriageStorage
	}
	for rows.Next() {
		d, err := scanInvoice(rows)
		if err != nil {
			rows.Close()
			return invoiceHistory{}, errTriageStorage
		}
		d.Current = acceptance != "" && d.Acceptance == acceptance
		result.Records = append(result.Records, d)
	}
	rows.Close()
	if rows.Err() != nil {
		return invoiceHistory{}, errTriageStorage
	}
	if len(result.Records) > 0 {
		result.Expected = result.Records[0].Sequence
	}
	if err = commitProposalWrite(ctx, p, tx, s.invoicesReady); err != nil {
		return invoiceHistory{}, err
	}
	return result, nil
}

// submitInvoice records an authorized native POST and returns the ordinary request-detail result.
func (b *browser) submitInvoice(w http.ResponseWriter, r *http.Request) {
	if err := b.foundation.invoiceOperator(r.Context(), b.principal(r.Context())); err != nil {
		b.triageError(w, r, err)
		return
	}
	d, valid := parseInvoice(r)
	if !valid {
		b.render(w, r, http.StatusBadRequest, "invoice-error", "", "")
		return
	}
	err := b.foundation.persistInvoice(r.Context(), b.principal(r.Context()), r.PathValue("id"), d)
	b.proposalMutationResult(w, r, err, "invoice-error")
}
