package identity

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// identifyDirectoryPeople decorates only the operator's HTML directory. API
// inventories keep their existing records and never include personal labels.
func (b *browser) identifyDirectoryPeople(ctx context.Context, page *managementPage, viewer string) {
	unknown, self := "Name unavailable", "You"
	if page.Lang == "da" {
		unknown, self = "Navn mangler", "Dig"
	}
	for i := range page.Workspaces {
		workspace := &page.Workspaces[i]
		workspace.Members = slices.DeleteFunc(workspace.Members, func(member workspaceMember) bool {
			return member.Role == "operator" && (member.Revoked ||
				(customeridentity.DexConnector(member.Subject) != "" && !b.foundation.operators[member.Subject]))
		})
	}
	profiles := b.directoryProfiles(ctx, page)
	for i := range page.Workspaces {
		members := page.Workspaces[i].Members
		for j := range members {
			member := &members[j]
			switch {
			case member.Role == "operator" && member.Subject == viewer:
				member.Person.Name = self
			case member.Role == "operator":
				member.Person.Name = unknown
			default:
				member.Person = directoryLabel(profiles, member.Subject, unknown)
			}
		}
		slices.SortStableFunc(members, func(left, right workspaceMember) int {
			if left.Role != right.Role {
				return strings.Compare(right.Role, left.Role)
			}
			return strings.Compare(strings.ToLower(left.Person.Name), strings.ToLower(right.Person.Name))
		})
	}
	for i := range page.Invitations {
		invitation := &page.Invitations[i]
		invitation.Person = directoryLabel(profiles, invitation.Subject, unknown)
	}
}

func (b *browser) directoryProfiles(ctx context.Context, page *managementPage) map[string]customeridentity.Profile {
	if b.foundation.customers == nil {
		return nil
	}
	ids := map[string]struct{}{}
	addCustomer := func(role, subject string) {
		if id := customeridentity.DexUser(subject, customeridentity.ZitadelConnectorID); role == "client" && id != "" {
			ids[id] = struct{}{}
		}
	}
	for _, workspace := range page.Workspaces {
		for _, member := range workspace.Members {
			addCustomer(member.Role, member.Subject)
		}
	}
	for _, invitation := range page.Invitations {
		addCustomer(invitation.Role, invitation.Subject)
	}
	if len(ids) == 0 || !b.operatorMutationAllowed(ctx, b.foundation.pool) {
		return nil
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Access management stays visible if names are temporarily unavailable.
	profiles, _ := b.foundation.customers.Profiles(lookup, slices.Sorted(maps.Keys(ids)))
	return profiles
}

func directoryLabel(profiles map[string]customeridentity.Profile, subject, fallback string) customeridentity.Profile {
	profile := profiles[customeridentity.DexUser(subject, customeridentity.ZitadelConnectorID)]
	switch {
	case profile.Name == "" && profile.Email == "":
		return customeridentity.Profile{Name: fallback}
	case profile.Name == "" || strings.EqualFold(profile.Name, profile.Email):
		return customeridentity.Profile{Name: profile.Email}
	default:
		return profile
	}
}
