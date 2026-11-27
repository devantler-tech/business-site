package identity

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// wholeDKK is an amount owed in whole DKK; the order's own amounts bound it
// further (SettlementLimitDKK).
var wholeDKK = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})$`)

// parseOperatorOrderForm admits exactly an operator's order form: the order
// and, for a withdrawal settlement, the amount owed in whole DKK.
func parseOperatorOrderForm(r *http.Request, amount bool) (string, int64, bool) {
	if err := r.ParseForm(); err != nil {
		return "", 0, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return "", 0, false
		}
		switch name {
		case "csrf_token", "lang", "order":
		case "owed_dkk":
			if !amount {
				return "", 0, false
			}
		default:
			return "", 0, false
		}
	}
	order := r.PostForm.Get("order")
	if !amount {
		return order, 0, opaqueRequestID.MatchString(order)
	}
	owed := r.PostForm.Get("owed_dkk")
	dkk, err := strconv.ParseInt(owed, 10, 64)
	if !wholeDKK.MatchString(owed) || err != nil {
		return "", 0, false
	}
	return order, dkk * 100, opaqueRequestID.MatchString(order)
}

// completeOrder records that early-started work is completely finished.
func (b *browser) completeOrder(w http.ResponseWriter, r *http.Request) {
	order, _, valid := parseOperatorOrderForm(r, false)
	if !valid {
		b.render(w, r, 400, "error", "", "")
		return
	}
	b.proposalMutationResult(w, r, b.foundation.completeOrderWork(r.Context(), b.principal(r.Context()), r.PathValue("id"), order), "error")
}

// settleWithdrawal records the amount owed for work done before a withdrawal.
func (b *browser) settleWithdrawal(w http.ResponseWriter, r *http.Request) {
	order, owed, valid := parseOperatorOrderForm(r, true)
	if !valid {
		b.render(w, r, 400, "error", "", "")
		return
	}
	b.proposalMutationResult(w, r, b.foundation.recordWithdrawalSettlement(r.Context(), b.principal(r.Context()), r.PathValue("id"), order, owed), "error")
}

// completeOrderWork records once that the work the client asked to start early
// is completely finished, which ends their right of withdrawal. Without that
// request, or after a withdrawal, there is nothing to end.
func (s *Foundation) completeOrderWork(ctx context.Context, p Principal, id, orderID string) error {
	return s.recordOperatorOrderFact(ctx, p, id, orderID,
		func(o projectOrder) bool { return o.EarlyStart != nil && o.Withdrawn == nil },
		`INSERT INTO portal_order_completions(order_id,operator_issuer,operator_subject) VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING`)
}

// recordWithdrawalSettlement records once what the client owes for the work
// done before withdrawing from an early-started order: an amount in proportion
// to the whole agreement, at most its first one-time and monthly amounts
// (section 25). The same amount again is a retry and another a conflict.
// Without an early start the client owes nothing, so nothing is recorded.
func (s *Foundation) recordWithdrawalSettlement(ctx context.Context, p Principal, id, orderID string, owed int64) error {
	return s.recordOperatorOrderFact(ctx, p, id, orderID,
		func(o projectOrder) bool {
			return o.Withdrawn != nil && o.EarlyStart != nil && owed <= o.OneTimeMinor+o.MonthlyMinor && (o.Owed == nil || *o.Owed == owed)
		},
		`INSERT INTO portal_withdrawal_settlements(order_id,operator_issuer,operator_subject,owed_minor) VALUES($1,$2,$3,$4) ON CONFLICT(order_id) DO NOTHING`, owed)
}

// recordOperatorOrderFact records one fact about the request's order once,
// under the operator's request lock, when admit accepts the order as read.
// The insert receives the order, the operator's issuer and subject, then args.
func (s *Foundation) recordOperatorOrderFact(ctx context.Context, p Principal, id, orderID string, admit func(projectOrder) bool, insert string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginOperatorWrite, false)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if !admit(o) {
		return errProposalConflict
	}
	if _, err = tx.Exec(ctx, insert, append([]any{o.ID, p.issuer, p.subject}, args...)...); err != nil {
		return errTriageStorage
	}
	return commitProposalWrite(ctx, p, tx, s.orderReady)
}

// SettlementLimitDKK is the most a withdrawal's amount owed may be, in DKK.
func (o projectOrder) SettlementLimitDKK() int64 { return (o.OneTimeMinor + o.MonthlyMinor) / 100 }
