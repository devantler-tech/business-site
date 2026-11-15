package runtimeapp

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

// ErrInvite hides which database, broker or invitation check refused the operation.
var ErrInvite = errors.New("portal invitation failed")

// ErrInviteActivation names the missing precondition without echoing any setting.
var ErrInviteActivation = errors.New("portal invitation requires enabled production identity")

// Invite is the trusted operator's one-shot control-plane provisioning call,
// run inside the web pod with the web process's own configuration and
// restricted login. It requires complete dependencies and the enabled
// production identity profile, binds the invitation to the configured broker
// issuer and returns only the one-time code. It serves and logs nothing.
func Invite(ctx context.Context, subject, client, role string, lifetime time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := ReadConfig(os.Getenv)
	if err != nil {
		return "", err
	}
	if cfg == (Config{}) || os.Getenv("PORTAL_IDENTITY_PROFILE") != "production" {
		return "", ErrInviteActivation
	}
	provider, err := IdentityProvider(ctx, cfg)
	if err != nil {
		return "", err
	}
	api := isolated.NewAPI()
	defer func() {
		// The provider owns no external resource; shutdown revokes its lifetime.
		_ = api.Shutdown(context.Background())
	}()
	if err := api.SetProviderAndWait(ctx, provider); err != nil {
		return "", ErrInviteActivation
	}
	flags := api.NewClient()
	// Foundation.Invite rechecks this gate; checking first avoids connecting
	// to the store or broker while sign-in is switched off.
	if enabled, err := flags.BooleanValue(ctx, "portal-identity", false, openfeature.NewEvaluationContext("", nil)); err != nil || !enabled {
		return "", ErrInviteActivation
	}
	pool, broker, err := connectPortal(ctx, cfg)
	if err != nil {
		return "", ErrConfig
	}
	defer pool.Close()
	token, err := identity.New(pool, broker, flags).Invite(ctx, identity.Invitation{
		Issuer: cfg.issuer, Subject: subject, ClientID: client, Role: role, Expires: time.Now().Add(lifetime),
	})
	if err != nil {
		return "", ErrInvite
	}
	return token, nil
}
