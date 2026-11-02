// Package activation owns operator-only staging and explicitly selected
// production identity gates. Ordinary application startup remains disabled.
package activation

import (
	"context"
	"errors"
	"os"
	"time"

	environment "github.com/open-feature/go-sdk-contrib/providers/from-env/pkg"
	"github.com/open-feature/go-sdk/openfeature"
)

// ErrInvalid omits private configuration values from every startup diagnostic.
var ErrInvalid = errors.New("portal activation is invalid")

type provider struct {
	openfeature.FeatureProvider
	lifetime   context.Context
	cancel     context.CancelFunc
	until      time.Time
	lease      string
	production bool
}

// New uses the maintained JSON decoder behind a ten-minute maximum operator
// lease. It owns no watcher/service; environment rotations require a restart.
func New(ctx context.Context) (openfeature.FeatureProvider, error) {
	lease := os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL")
	until, err := time.Parse(time.RFC3339Nano, lease)
	now := time.Now()
	remaining := until.Sub(now)
	if ctx == nil || ctx.Err() != nil || err != nil || remaining <= 0 || remaining > 10*time.Minute || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 || os.Getenv("PORTAL_IDENTITY_PROFILE") != "" {
		return nil, ErrInvalid
	}
	// Retain a monotonic deadline even though the operator supplies wall time.
	deadline := now.Add(remaining)
	lifetime, cancel := context.WithDeadline(ctx, deadline)
	return &provider{FeatureProvider: identityEnvironment(), lifetime: lifetime, cancel: cancel, until: deadline, lease: lease}, nil
}

// identityEnvironment uses the existing maintained decoder for one capability;
// no provider-specific authentication or second flag parser is introduced.
func identityEnvironment() openfeature.FeatureProvider {
	return environment.NewProvider(environment.WithFlagToEnvMapper(func(key string) string {
		if key == "portal-identity" {
			return "PORTAL_IDENTITY_FLAG"
		}
		return ""
	}))
}

// NewProduction requires an explicit operator profile and valid typed flag data.
// It inherits process cancellation, has no staging timer, and enables no other key.
func NewProduction(ctx context.Context) (openfeature.FeatureProvider, error) {
	if ctx == nil || ctx.Err() != nil || os.Getenv("PORTAL_IDENTITY_PROFILE") != "production" || os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") != "" || len(os.Getenv("PORTAL_IDENTITY_FLAG")) == 0 || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 {
		return nil, ErrInvalid
	}
	lifetime, cancel := context.WithCancel(ctx)
	p := &provider{FeatureProvider: identityEnvironment(), lifetime: lifetime, cancel: cancel, production: true}
	if err := p.Init(openfeature.NewEvaluationContext("", nil)); err != nil {
		cancel()
		return nil, ErrInvalid
	}
	return p, nil
}

// Init rechecks the selected process lifetime and, for production, the maintained
// decoder's typed result. A false flag is valid closed configuration.
func (p *provider) Init(openfeature.EvaluationContext) error {
	if !p.valid(p.lifetime) {
		return ErrInvalid
	}
	if p.production {
		result := p.FeatureProvider.BooleanEvaluation(p.lifetime, "portal-identity", false, openfeature.FlattenedContext{})
		if result.ResolutionDetail().ErrorCode != "" || !p.valid(p.lifetime) {
			return ErrInvalid
		}
	}
	return nil
}

// Shutdown revokes the process lease, including clients retained outside the SDK.
func (p *provider) Shutdown() { p.cancel() }

// valid checks request/process lifetime and the unchanged profile boundary. Only
// staging uses its original bounded lease; production refuses any such lease.
func (p *provider) valid(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil || p.lifetime.Err() != nil || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 {
		return false
	}
	if p.production {
		return os.Getenv("PORTAL_IDENTITY_PROFILE") == "production" && os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == "" && len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 0
	}
	return os.Getenv("PORTAL_IDENTITY_PROFILE") == "" && time.Now().Before(p.until) && os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == p.lease
}

// BooleanEvaluation delegates values to the maintained provider, not a second
// flag parser. Check both sides so cancellation during resolution cannot admit.
func (p *provider) BooleanEvaluation(ctx context.Context, key string, fallback bool, data openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	// A successful explicit false keeps unrelated capabilities closed even if
	// a future caller supplies true as its SDK fallback. Evaluation errors are
	// still denied by the application's false-default/no-error gate.
	if p.production && key != "portal-identity" {
		return openfeature.BoolResolutionDetail{Value: false, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DisabledReason}}
	}
	denied := openfeature.BoolResolutionDetail{Value: false, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
		Reason: openfeature.ErrorReason, ResolutionError: openfeature.NewGeneralResolutionError("portal activation is unavailable"),
	}}
	if key != "portal-identity" || !p.valid(ctx) {
		return denied
	}
	result := p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, data)
	if !p.valid(ctx) || result.ResolutionDetail().ErrorCode != "" {
		return denied
	}
	return result
}
