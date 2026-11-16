package identity

import "context"

// CustomerDirectory is limited to the configured Customers organization. It
// delegates account setup and mail to the provider and never deletes users.
type CustomerDirectory interface {
	EnsureUser(context.Context, string, string) (string, error)
	DeactivateUser(context.Context, string) error
	ReactivateUser(context.Context, string) error
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
