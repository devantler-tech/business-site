package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
)

var errPortalInvite = errors.New("portal invitation arguments are invalid")

// ZITADEL user IDs are printable ASCII without whitespace; client IDs are
// lowercase workspace slugs. Lifetimes are whole days, at most Invite's week.
var (
	inviteUser   = regexp.MustCompile(`^[!-~]{1,200}$`)
	inviteClient = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	inviteDays   = regexp.MustCompile(`^[1-7]$`)
)

// portalInviteOptions selects one trusted invitation, never service startup.
type portalInviteOptions struct {
	enabled                  bool
	user, client, role, days string
}

// registerPortalInviteOptions keeps operator provisioning inside the packaged executable.
func registerPortalInviteOptions(options *flag.FlagSet) *portalInviteOptions {
	command := &portalInviteOptions{}
	options.BoolVar(&command.enabled, "portal-invite", false, "create one portal invitation for a ZITADEL user without starting services")
	options.StringVar(&command.user, "invite-zitadel-user", "", "ZITADEL user ID of the already created customer account")
	options.StringVar(&command.client, "invite-client", "", "portal client workspace ID for the new membership")
	options.StringVar(&command.role, "invite-role", "", "client or operator")
	options.StringVar(&command.days, "invite-days", "", "invitation lifetime in whole days, 1 to 7")
	return command
}

// selected recognizes even incomplete or explicitly false requests so they cannot serve HTTP.
func (command *portalInviteOptions) selected(seen map[string]int) bool {
	return seen["portal-invite"] != 0 || seen["invite-zitadel-user"] != 0 || seen["invite-client"] != 0 || seen["invite-role"] != 0 || seen["invite-days"] != 0
}

// run refuses mixed or malformed options before reading any private input, then
// invites the customer Dex subject for that ZITADEL user and prints only the code.
func (command *portalInviteOptions) run(ctx context.Context, options *flag.FlagSet, seen map[string]int, settings string, output io.Writer) error {
	names := []string{"portal-invite", "portal-settings-projection", "invite-zitadel-user", "invite-client", "invite-role", "invite-days"}
	allowed := command.enabled && settings != "" && inviteUser.MatchString(command.user) && inviteClient.MatchString(command.client) &&
		(command.role == "client" || command.role == "operator") && inviteDays.MatchString(command.days) && ctx.Err() == nil
	for _, name := range names {
		if seen[name] != 1 {
			allowed = false
		}
	}
	options.Visit(func(option *flag.Flag) {
		if !slices.Contains(names, option.Name) {
			allowed = false
		}
	})
	if !allowed {
		return errPortalInvite
	}
	if !validPackageIdentity() {
		return errors.New("build a verified runtime package before starting")
	}
	if err := runtimeapp.ApplyPortalSettingsProjection(settings); err != nil {
		return err
	}
	subject := customeridentity.DexSubject(command.user, customeridentity.ZitadelConnectorID)
	lifetime := time.Duration(command.days[0]-'0') * 24 * time.Hour
	if err := runtimeapp.Invite(ctx, subject, command.client, command.role, lifetime); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "portal invitation created: that user accepts it by signing in")
	return err
}
