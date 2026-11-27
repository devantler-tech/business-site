package identity

import (
	"context"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// CustomerDirectory is limited to the configured Customers organization. It
// delegates account setup and mail to the provider and never deletes users.
type CustomerDirectory interface {
	Profiles(context.Context, []string) (map[string]customeridentity.Profile, error)
	EnsureUser(context.Context, string, string, func() bool) (string, error)
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
