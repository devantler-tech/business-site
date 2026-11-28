package identity

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// CustomerAccountDeletion is the separate authority for permanently removing a
// customer identity. A directory credential alone cannot authorize deletion.
type CustomerAccountDeletion interface {
	AuthorizeDeletion(ctx context.Context, userID string) error
	// DeleteCustomerAccount returns nil only once the exact identity is absent.
	// The implementation must enforce provider-side eligibility and call allowed
	// immediately before each irreversible provider operation.
	DeleteCustomerAccount(ctx context.Context, userID string, allowed func() bool) error
}

// LinkCustomerAccountDeletion installs an independently guarded deletion capability.
// It must run before serving requests and leaves browser authorization unchanged.
func (s *Foundation) LinkCustomerAccountDeletion(deletion CustomerAccountDeletion) error {
	if s == nil || deletion == nil || s.customers == nil || s.operatorOnly {
		return ErrDenied
	}
	s.customerDeletion = deletion
	return nil
}

const customerDeletionExists = `EXISTS (
 SELECT 1 FROM portal_customer_deletions WHERE issuer=$1 AND subject=$2)`

func (b *browser) deleteCustomerAccount(ctx context.Context, r *http.Request) (any, error) {
	executor := b.foundation.customerDeletion
	if humanOperatorAuthority(ctx) == nil || executor == nil {
		return nil, ErrDenied
	}
	var input struct {
		Subject string `json:"subject"`
		Confirm string `json:"confirm"`
	}
	if err := decodeOperatorInput(r, &input, map[string]*string{"subject": &input.Subject, "confirm": &input.Confirm}); err != nil {
		return nil, err
	}
	id := customerAccountID(input.Subject)
	if id == "" || input.Confirm != "DELETE" || !b.foundation.isCustomerSignIn(input.Subject) || input.Subject == previewSubject {
		return nil, errOperatorInput
	}
	setOperatorTarget(ctx, input.Subject)
	issuer := b.foundation.broker.issuer
	if err := b.stageCustomerAccountDeletion(ctx, issuer, input.Subject, id); err != nil {
		return nil, err
	}
	if err := b.confirmCustomerAccountDeletion(ctx, issuer, input.Subject, id); err != nil {
		return nil, err
	}
	return map[string]bool{"deleted": true}, nil
}

func (b *browser) stageCustomerAccountDeletion(ctx context.Context, issuer, subject, id string) error {
	tx, err := b.customerIdentityTransaction(ctx, subject)
	if err != nil {
		return err
	}
	// Rollback closes the connection after a refusal or a failed attempt.
	defer func() { _ = tx.Rollback(ctx) }()
	var previouslyRequested, operator bool
	err = tx.QueryRow(ctx, `SELECT `+customerDeletionExists+`,
 EXISTS (SELECT 1 FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='operator')`, issuer, subject).
		Scan(&previouslyRequested, &operator)
	if err != nil {
		return err
	}
	if operator || !b.operatorMutationAllowed(ctx, tx) {
		return ErrDenied
	}
	if previouslyRequested {
		return nil
	}
	account, err := b.foundation.customers.Account(ctx, id)
	if err != nil {
		return err
	}
	if account.ID != id {
		return ErrDenied
	}
	if err := b.foundation.customerDeletion.AuthorizeDeletion(ctx, id); err != nil {
		return err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return ErrDenied
	}
	if err := stageCustomerAccountAccessRemoval(ctx, tx, issuer, subject); err != nil {
		return err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return ErrDenied
	}
	return tx.Commit(ctx)
}

func stageCustomerAccountAccessRemoval(ctx context.Context, tx pgx.Tx, issuer, subject string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO portal_customer_deletions(issuer,subject) VALUES ($1,$2)`, issuer, subject); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client'`, issuer, subject); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE portal_invitations SET revoked=true WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked`, issuer, subject)
	return err
}

func (b *browser) confirmCustomerAccountDeletion(ctx context.Context, issuer, subject, id string) error {
	tx, err := b.customerIdentityTransaction(ctx, subject)
	if err != nil {
		return err
	}
	// A retry takes the same lock and returns the recorded confirmation.
	defer func() { _ = tx.Rollback(ctx) }()
	var confirmed bool
	err = tx.QueryRow(ctx, `SELECT confirmed_at IS NOT NULL FROM portal_customer_deletions WHERE issuer=$1 AND subject=$2`, issuer, subject).Scan(&confirmed)
	if err != nil || confirmed {
		return err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return ErrDenied
	}
	if err := b.foundation.customerDeletion.DeleteCustomerAccount(ctx, id, func() bool {
		return b.operatorMutationAllowed(ctx, tx)
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE portal_customer_deletions SET confirmed_at=clock_timestamp()
 WHERE issuer=$1 AND subject=$2 AND confirmed_at IS NULL`, issuer, subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
