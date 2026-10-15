package identity

import "context"

// readOperatorProposalAcceptances derives scope from the immutable request after
// locking current operator membership. It does not widen the client reader.
func (s *Foundation) readOperatorProposalAcceptances(ctx context.Context, p Principal, id string) ([]proposalAcceptance, error) {
	if !s.acceptanceReady(ctx) {
		return nil, ErrDenied
	}
	if err := s.triageOperator(ctx, p); err != nil {
		return nil, err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return nil, err
	}
	defer rollbackProposalWrite(ctx, tx)
	receipts, _, err := proposalAcceptanceHistory(ctx, tx, id, client)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ErrDenied
	}
	if err = commitProposalWrite(ctx, p, tx, s.acceptanceReady); err != nil {
		return nil, err
	}
	return receipts, nil
}
