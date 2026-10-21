package identity

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// workMember names a server-derived membership, never a browser role claim.
type workMember struct {
	Issuer, Subject, Client, Role, Incarnation string
}

// workEvidence is a closed typed encoding. Keep all selected invoice facts,
// complete invoice history and every latest transfer, not just matching totals.
type workEvidence struct {
	Format          int
	Request, Client string
	Acceptance      string
	Event           int
	Hash            string
	Proposal        projectProposal
	Invoice         projectInvoice
	Invoices        []projectInvoice
	Transfers       []creditRevision
	Acceptor, Actor workMember
}

// lockWorkMembers uses one global identity ordering after the request lock. It
// also locks revoked rows: restoration cannot race this transaction's verdict.
func lockWorkMembers(ctx context.Context, tx pgx.Tx, wanted []workMember) ([]workMember, []bool, error) {
	order := make([]int, len(wanted))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		if result := cmp.Compare(wanted[a].Issuer, wanted[b].Issuer); result != 0 {
			return result
		}
		return cmp.Compare(wanted[a].Subject, wanted[b].Subject)
	})
	actual, matches := make([]workMember, len(wanted)), make([]bool, len(wanted))
	for _, i := range order {
		member := workMember{Issuer: wanted[i].Issuer, Subject: wanted[i].Subject}
		var revoked, recorded bool
		err := tx.QueryRow(ctx, `SELECT m.client_id,m.role,m.incarnation::text,m.revoked,
 EXISTS(SELECT 1 FROM public.portal_membership_transitions h WHERE h.incarnation=m.incarnation
 AND h.kind IN ('baseline','insert','update') AND h.issuer=m.issuer AND h.subject=m.subject
 AND h.client_id=m.client_id AND h.role=m.role AND h.revoked=m.revoked)
 FROM public.portal_memberships m WHERE m.issuer=$1 AND m.subject=$2 FOR SHARE OF m`, member.Issuer, member.Subject).Scan(&member.Client, &member.Role, &member.Incarnation, &revoked, &recorded)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, errTriageStorage
		}
		actual[i] = member
		matches[i] = !revoked && recorded && member.Client == wanted[i].Client && member.Role == wanted[i].Role && member.Incarnation != "" && (wanted[i].Incarnation == "" || wanted[i].Incarnation == member.Incarnation)
	}
	return actual, matches, nil
}

// readWorkInvoices neither owns nor commits the caller's request transaction.
func readWorkInvoices(ctx context.Context, tx pgx.Tx, id, client string) ([]projectInvoice, error) {
	rows, err := tx.Query(ctx, `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE i.request_id=$1 AND i.client_id=$2 ORDER BY i.sequence DESC LIMIT 51`, id, client)
	if err != nil {
		return nil, errTriageStorage
	}
	var records []projectInvoice
	for rows.Next() {
		d, err := scanInvoice(rows)
		if err != nil {
			rows.Close()
			return nil, errTriageStorage
		}
		d.Recorded = d.Recorded.UTC()
		records = append(records, d)
	}
	rows.Close()
	if rows.Err() != nil || len(records) > 50 {
		return nil, errTriageStorage
	}
	for i, record := range records {
		if record.Sequence != len(records)-i {
			return nil, errTriageStorage
		}
	}
	return records, nil
}

// checkWorkTransferClosure proves the bounded reservation census as well as the
// revision read. A matching total cannot turn unobserved or gapped history into
// complete evidence. This is stricter approval admission, not a credit rewrite.
func checkWorkTransferClosure(ctx context.Context, tx pgx.Tx, id, client string, invoice int, history creditHistory) error {
	type identity struct{ source, reference string }
	observed := make(map[identity]bool, len(history.Transfers))
	for _, transfer := range history.Transfers {
		if len(transfer.History) == 0 || len(transfer.History) > 16 || transfer.Latest.Revision != len(transfer.History) {
			return errTriageStorage
		}
		for i, revision := range transfer.History {
			if revision.Source != transfer.Latest.Source || revision.Reference != transfer.Latest.Reference || revision.Revision != len(transfer.History)-i || revision.Expected != revision.Revision-1 {
				return errTriageStorage
			}
		}
		observed[identity{transfer.Latest.Source, transfer.Latest.Reference}] = true
	}
	rows, err := tx.Query(ctx, `SELECT source,reference,ordinal FROM portal_credit_transfers WHERE request_id=$1 AND client_id=$2 AND invoice_sequence=$3 ORDER BY ordinal LIMIT 33`, id, client, invoice)
	if err != nil {
		return errTriageStorage
	}
	count := 0
	for rows.Next() {
		var key identity
		var ordinal int
		if err := rows.Scan(&key.source, &key.reference, &ordinal); err != nil {
			rows.Close()
			return errTriageStorage
		}
		count++
		if count > 32 || ordinal != count || !observed[key] {
			rows.Close()
			return errTriageStorage
		}
	}
	rows.Close()
	if rows.Err() != nil || count != len(observed) {
		return errTriageStorage
	}
	return nil
}

// collectWorkEvidence reads only under the caller's request lock. The current
// viewer and original approving actor can be different authorized operators.
func collectWorkEvidence(ctx context.Context, tx pgx.Tx, id, client string, invoice int, actor, viewer workMember) (workEvidence, []byte, string, error) {
	review, acceptance, err := currentInvoiceAcceptance(ctx, tx, id, client)
	if err != nil {
		return workEvidence{}, nil, "", err
	}
	acceptor := workMember{Client: client, Role: "client"}
	if err := tx.QueryRow(ctx, `SELECT client_issuer,client_subject FROM portal_proposal_acceptances WHERE id=$1 AND request_id=$2 AND client_id=$3`, acceptance, id, client).Scan(&acceptor.Issuer, &acceptor.Subject); err != nil {
		return workEvidence{}, nil, "", errTriageStorage
	}
	members, matches, err := lockWorkMembers(ctx, tx, []workMember{acceptor, actor, viewer})
	if err != nil {
		return workEvidence{}, nil, "", err
	}
	if !matches[2] {
		return workEvidence{}, nil, "", ErrDenied
	}
	if !matches[0] || !matches[1] {
		return workEvidence{}, nil, "", errProposalConflict
	}
	history, err := readCreditHistory(ctx, tx, id, client, invoice)
	if err != nil {
		return workEvidence{}, nil, "", err
	}
	if err := checkWorkTransferClosure(ctx, tx, id, client, invoice, history); err != nil {
		return workEvidence{}, nil, "", err
	}
	d := history.Invoice
	if !d.Current || d.Acceptance != acceptance || !d.NetMinor.Valid || !d.VATMinor.Valid || d.NetMinor.Int64+d.VATMinor.Int64 != d.AmountMinor || d.AmountMinor <= 0 || history.Unsettled || history.Total != d.AmountMinor {
		return workEvidence{}, nil, "", errProposalConflict
	}
	invoices, err := readWorkInvoices(ctx, tx, id, client)
	if err != nil {
		return workEvidence{}, nil, "", err
	}
	d.Recorded = d.Recorded.UTC()
	review.Proposal.Prepared = review.Proposal.Prepared.UTC()
	evidence := workEvidence{Format: 1, Request: id, Client: client, Acceptance: acceptance, Event: review.Event, Hash: review.Hash, Proposal: review.Proposal, Invoice: d, Invoices: invoices, Acceptor: members[0], Actor: members[1]}
	for _, transfer := range history.Transfers {
		latest := transfer.Latest
		latest.Recorded = latest.Recorded.UTC()
		evidence.Transfers = append(evidence.Transfers, latest)
	}
	slices.SortFunc(evidence.Transfers, func(a, b creditRevision) int {
		if result := cmp.Compare(a.Source, b.Source); result != 0 {
			return result
		}
		return cmp.Compare(a.Reference, b.Reference)
	})
	data, err := json.Marshal(evidence)
	if err != nil || len(data) == 0 || len(data) > 2097152 {
		return workEvidence{}, nil, "", errTriageStorage
	}
	digest := sha256.Sum256(data)
	return evidence, data, hex.EncodeToString(digest[:]), nil
}
