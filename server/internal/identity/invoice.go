package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The external invoice-record module (#117) is retired by ADR 0030: Numina is
// the record for invoices. Its v10/v12 tables remain historical schema, and
// work approval still reads them as evidence until #268 binds approvals to the
// order's funding record instead. Nothing in the portal writes them any more.

type projectInvoice struct {
	Sequence, Expected, Version, Event        int
	Key, Acceptance, Hash, Reference, Purpose string
	AmountMinor                               int64
	NetMinor, VATMinor                        pgtype.Int8
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

type invoiceTaxView struct {
	Lang, Net, VAT string
	Recorded       bool
}

// TaxBreakdown formats recorded facts only; unknown historical tax stays unknown.
func (d projectInvoice) TaxBreakdown(lang string) invoiceTaxView {
	view := invoiceTaxView{Lang: lang, Recorded: d.NetMinor.Valid && d.VATMinor.Valid}
	if view.Recorded {
		view.Net = (projectInvoice{AmountMinor: d.NetMinor.Int64}).AmountDKK(lang)
		view.VAT = (projectInvoice{AmountMinor: d.VATMinor.Int64}).AmountDKK(lang)
	}
	return view
}

const invoiceColumns = `i.sequence,i.expected_sequence,i.submission_key,i.acceptance_id,i.proposal_version,i.visibility_event,i.content_hash,i.reference,i.purpose,i.amount_minor,i.recorded_at,i.net_minor,i.vat_minor`

// scanInvoice materializes the explicit private invoice columns, preserving storage errors.
func scanInvoice(row pgx.Row) (projectInvoice, error) {
	var d projectInvoice
	err := row.Scan(&d.Sequence, &d.Expected, &d.Key, &d.Acceptance, &d.Version, &d.Event, &d.Hash, &d.Reference, &d.Purpose, &d.AmountMinor, &d.Recorded, &d.NetMinor, &d.VATMinor)
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
