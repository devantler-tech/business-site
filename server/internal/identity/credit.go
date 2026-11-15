package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The bank-credit evidence module (#120) is retired by ADR 0030: Numina matches
// bank payments. Its v11 tables remain historical schema, read only as work
// approval evidence until #268 binds approvals to the order's funding record.

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

// creditWriteError maps a unique-key race to a conflict and hides other storage errors.
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
