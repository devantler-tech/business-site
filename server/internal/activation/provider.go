// Package activation owns bounded operator-only staging, not production release.
package activation

import (
	"context"
	"errors"
	"os"
	"time"

	fromenv "github.com/open-feature/go-sdk-contrib/providers/from-env/pkg"
	"github.com/open-feature/go-sdk/openfeature"
)

// ErrInvalid omits private configuration values from every startup diagnostic.
var ErrInvalid = errors.New("portal activation is invalid")

type provider struct {
	openfeature.FeatureProvider
	lifetime context.Context
	cancel   context.CancelFunc
	until    time.Time
	lease    string
}

// New uses the maintained JSON decoder behind a ten-minute maximum operator
// lease. It owns no watcher/service; environment rotations require a restart.
func New(ctx context.Context) (openfeature.FeatureProvider, error) {
	lease := os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL")
	until, err := time.Parse(time.RFC3339Nano, lease)
	now := time.Now()
	remaining := until.Sub(now)
	if ctx == nil || ctx.Err() != nil || err != nil || remaining <= 0 || remaining > 10*time.Minute || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 {
		return nil, ErrInvalid
	}
	// Retain a monotonic deadline even though the operator supplies wall time.
	deadline := now.Add(remaining)
	lifetime, cancel := context.WithDeadline(ctx, deadline)
	mapped := fromenv.NewProvider(fromenv.WithFlagToEnvMapper(func(key string) string {
		if key == "portal-identity" {
			return "PORTAL_IDENTITY_FLAG"
		}
		return ""
	}))
	return &provider{mapped, lifetime, cancel, deadline, lease}, nil
}

func (p *provider) Init(openfeature.EvaluationContext) error {
	if p.lifetime.Err() != nil || !time.Now().Before(p.until) {
		return ErrInvalid
	}
	return nil
}

func (p *provider) Shutdown() { p.cancel() }

func (p *provider) valid(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil && p.lifetime.Err() == nil && time.Now().Before(p.until) &&
		os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == p.lease && len(os.Getenv("PORTAL_IDENTITY_FLAG")) <= 16384
}

// BooleanEvaluation delegates values to the maintained provider, not a second
// flag parser. Check both sides so cancellation during resolution cannot admit.
func (p *provider) BooleanEvaluation(ctx context.Context, key string, fallback bool, data openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	denied := openfeature.BoolResolutionDetail{Value: false, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
		Reason: openfeature.ErrorReason, ResolutionError: openfeature.NewGeneralResolutionError("portal activation is unavailable"),
	}}
	if !p.valid(ctx) {
		return denied
	}
	result := p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, data)
	if !p.valid(ctx) || result.ResolutionDetail().ErrorCode != "" {
		return denied
	}
	return result
}
