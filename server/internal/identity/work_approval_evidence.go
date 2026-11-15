package identity

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// workMember names a server-derived membership, never a browser role claim.
type workMember struct {
	Issuer, Subject, Client, Role, Incarnation string
}

// workEvidence is a closed typed encoding of the exact accepted agreement, the
// order's one collection path and its funding charge (ADR 0030, decision 6).
// Format 2 replaces the retired invoice-record and bank-credit evidence.
type workEvidence struct {
	Format          int
	Request, Client string
	Acceptance      string
	Event           int
	Hash            string
	Proposal        projectProposal
	Order           workOrder
	Funding         workFunding
	Acceptor, Actor workMember
}

// workOrder is the order an approval's funding belongs to.
type workOrder struct {
	ID, Collection             string
	OneTimeMinor, MonthlyMinor int64
	MonthlyFrom                time.Time
}

// workFunding is the order's first charge, settled and never reversed: the
// accepted one-time amount, or the first month for a monthly-only order.
type workFunding struct {
	Period, Reference              string
	GrossMinor, NetMinor, VATMinor int64
	Settled                        time.Time
}

// GrossDKK is the funded amount in whole kroner.
func (f workFunding) GrossDKK() int64 { return f.GrossMinor / 100 }

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

// readWorkFunding reads the request's order for the acceptance and its first
// charge under the caller's request lock. Anything but a settled charge for
// exactly the agreed amount, never refunded or disputed, is not funding.
func readWorkFunding(ctx context.Context, tx pgx.Tx, id, client, acceptance string) (workOrder, workFunding, error) {
	var o workOrder
	err := tx.QueryRow(ctx, `SELECT id,collection,one_time_minor,monthly_minor,monthly_from FROM portal_orders WHERE request_id=$1 AND client_id=$2 AND acceptance_id=$3`, id, client, acceptance).Scan(&o.ID, &o.Collection, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom)
	if errors.Is(err, pgx.ErrNoRows) {
		return workOrder{}, workFunding{}, errProposalConflict
	}
	if err != nil {
		return workOrder{}, workFunding{}, errTriageStorage
	}
	f := workFunding{Period: "acceptance"}
	want := o.OneTimeMinor
	if want == 0 {
		f.Period, want = monthPeriod(o.MonthlyFrom), o.MonthlyMinor
	}
	// Lock the charge first and read it in a new statement: a settlement change
	// or a reversal (applyReversal locks the charge for update) committed while
	// this waited is then seen, and none can commit until this decision does.
	var locked bool
	err = tx.QueryRow(ctx, `SELECT true FROM portal_order_charges WHERE order_id=$1 AND period=$2 FOR SHARE`, o.ID, f.Period).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return workOrder{}, workFunding{}, errProposalConflict
	}
	if err != nil {
		return workOrder{}, workFunding{}, errTriageStorage
	}
	var settled *time.Time
	err = tx.QueryRow(ctx, `SELECT c.gross_minor,c.net_minor,c.vat_minor,coalesce(c.provider_ref,''),c.settled_at FROM portal_order_charges c
 WHERE c.order_id=$1 AND c.period=$2 AND c.state='settled' AND NOT EXISTS (SELECT 1 FROM portal_charge_reversals r WHERE r.order_id=c.order_id AND r.period=c.period)`, o.ID, f.Period).Scan(&f.GrossMinor, &f.NetMinor, &f.VATMinor, &f.Reference, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return workOrder{}, workFunding{}, errProposalConflict
	}
	if err != nil {
		return workOrder{}, workFunding{}, errTriageStorage
	}
	if settled == nil || f.GrossMinor != want || f.NetMinor+f.VATMinor != f.GrossMinor || f.Reference == "" {
		return workOrder{}, workFunding{}, errProposalConflict
	}
	o.MonthlyFrom, f.Settled = o.MonthlyFrom.UTC(), settled.UTC()
	return o, f, nil
}

// collectWorkEvidence reads only under the caller's request lock. The current
// viewer and original approving actor can be different authorized operators.
func collectWorkEvidence(ctx context.Context, tx pgx.Tx, id, client string, actor, viewer workMember) (workEvidence, []byte, string, error) {
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
	order, funding, err := readWorkFunding(ctx, tx, id, client, acceptance)
	if err != nil {
		return workEvidence{}, nil, "", err
	}
	review.Proposal.Prepared = review.Proposal.Prepared.UTC()
	evidence := workEvidence{Format: 2, Request: id, Client: client, Acceptance: acceptance, Event: review.Event, Hash: review.Hash, Proposal: review.Proposal, Order: order, Funding: funding, Acceptor: members[0], Actor: members[1]}
	data, err := json.Marshal(evidence)
	if err != nil || len(data) == 0 || len(data) > 2097152 {
		return workEvidence{}, nil, "", errTriageStorage
	}
	digest := sha256.Sum256(data)
	return evidence, data, hex.EncodeToString(digest[:]), nil
}
