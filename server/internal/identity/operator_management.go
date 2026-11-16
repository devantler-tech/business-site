package identity

import (
	"context"
	"net/http"
)

var managementRoutes = []operatorRoute{
	{"POST", "/invitations/revoke", "invitations.revoke", (*browser).revokeCustomerInvitation},
	{"POST", "/workspaces", "workspaces.create", (*browser).createCustomerWorkspace},
	{"GET", "/workspaces/members", "workspaces.members.list", (*browser).customerWorkspaceMembers},
	{"POST", "/members/deactivate", "members.deactivate", (*browser).deactivateCustomerMember},
	{"POST", "/members/reactivate", "members.reactivate", (*browser).reactivateCustomerMember},
}

func (*browser) revokeCustomerInvitation(*http.Request, context.Context) (any, error) {
	return nil, ErrDenied
}

func (*browser) createCustomerWorkspace(*http.Request, context.Context) (any, error) {
	return nil, ErrDenied
}

func (*browser) customerWorkspaceMembers(*http.Request, context.Context) (any, error) {
	return nil, ErrDenied
}

func (*browser) deactivateCustomerMember(*http.Request, context.Context) (any, error) {
	return nil, ErrDenied
}

func (*browser) reactivateCustomerMember(*http.Request, context.Context) (any, error) {
	return nil, ErrDenied
}
