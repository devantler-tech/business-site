package identity

import (
	"context"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// CustomerDirectory is limited to the configured Customers organization. It
// delegates account setup to the provider and never deletes users.
type CustomerDirectory interface {
	Profiles(context.Context, []string) (map[string]customeridentity.Profile, error)
	SearchAccounts(context.Context, string, uint64) ([]customeridentity.Account, bool, error)
	Account(context.Context, string) (customeridentity.Account, error)
	EnsureUser(ctx context.Context, name, email string, allowed func() bool) (id string, created bool, err error)
	VerifyUser(context.Context, string) error
	DeactivateUser(context.Context, string, func() bool) error
	ReactivateUser(context.Context, string, func() bool) error
}

// LinkCustomerDirectory supplies the optional provider used by operator actions.
// Configuration is completed before serving; it grants no caller authority.
func (s *Foundation) LinkCustomerDirectory(directory CustomerDirectory) error {
	if s == nil || directory == nil {
		return ErrDenied
	}
	s.customers = directory
	return nil
}
