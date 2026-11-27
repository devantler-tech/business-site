// Package activation owns operator-only staging and explicitly selected
// production capability gates. Ordinary application startup remains disabled.
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

// productionFlags names independent release gates, never work or actor authority.
// Missing capability settings stay off; staging continues to map identity only.
var productionFlags = map[string]string{
	"portal-identity":             "PORTAL_IDENTITY_FLAG",
	"portal-contact-verification": "PORTAL_CONTACT_VERIFICATION_FLAG",
	"portal-requests":             "PORTAL_REQUESTS_FLAG",
	"portal-triage":               "PORTAL_TRIAGE_FLAG",
	"portal-support":              "PORTAL_SUPPORT_FLAG",
	"portal-proposals":            "PORTAL_PROPOSALS_FLAG",
	"portal-proposal-sharing":     "PORTAL_PROPOSAL_SHARING_FLAG",
	"portal-proposal-acceptance":  "PORTAL_PROPOSAL_ACCEPTANCE_FLAG",
	"portal-invoice-billing":      "PORTAL_INVOICE_BILLING_FLAG",
	"portal-card-billing":         "PORTAL_CARD_BILLING_FLAG",
}

// Preview maps only the capabilities needed to exercise a fictional workspace.
var previewFlags = map[string]string{
	"portal-identity":            "PORTAL_IDENTITY_FLAG",
	"portal-requests":            "PORTAL_REQUESTS_FLAG",
	"portal-triage":              "PORTAL_TRIAGE_FLAG",
	"portal-proposals":           "PORTAL_PROPOSALS_FLAG",
	"portal-proposal-sharing":    "PORTAL_PROPOSAL_SHARING_FLAG",
	"portal-proposal-acceptance": "PORTAL_PROPOSAL_ACCEPTANCE_FLAG",
}

type provider struct {
	openfeature.FeatureProvider
	lifetime context.Context
	cancel   context.CancelFunc
	until    time.Time
	lease    string
	profile  string
	settings map[string]string
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
// It inherits process cancellation and has no staging timer. Optional release
// gates require their own settings; identity alone enables none of them.
func NewProduction(ctx context.Context) (openfeature.FeatureProvider, error) {
	return newNamedProfile(ctx, "production", productionFlags)
}

// NewPreview enables the fictional workspace's identity and project workflow.
func NewPreview(ctx context.Context) (openfeature.FeatureProvider, error) {
	return newNamedProfile(ctx, "preview", previewFlags)
}

func newNamedProfile(ctx context.Context, profile string, settings map[string]string) (openfeature.FeatureProvider, error) {
	if ctx == nil || ctx.Err() != nil || os.Getenv("PORTAL_IDENTITY_PROFILE") != profile || os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") != "" || len(os.Getenv("PORTAL_IDENTITY_FLAG")) == 0 || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 {
		return nil, ErrInvalid
	}
	lifetime, cancel := context.WithCancel(ctx)
	p := &provider{FeatureProvider: environment.NewProvider(environment.WithFlagToEnvMapper(func(key string) string {
		return settings[key]
	})), lifetime: lifetime, cancel: cancel, profile: profile, settings: settings}
	if err := p.Init(openfeature.NewEvaluationContext("", nil)); err != nil {
		cancel()
		return nil, ErrInvalid
	}
	return p, nil
}

// Init rechecks the selected process lifetime and, for a named profile, the maintained
// decoder's typed result. A false flag is valid closed configuration.
func (p *provider) Init(openfeature.EvaluationContext) error {
	if !p.valid(p.lifetime) {
		return ErrInvalid
	}
	if p.profile != "" {
		for key, setting := range p.settings {
			if os.Getenv(setting) == "" {
				continue
			}
			result := p.FeatureProvider.BooleanEvaluation(p.lifetime, key, false, openfeature.FlattenedContext{})
			if result.ResolutionDetail().ErrorCode != "" || !p.valid(p.lifetime) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// Shutdown revokes the process lease, including clients retained outside the SDK.
func (p *provider) Shutdown() { p.cancel() }

// valid checks request/process lifetime and the unchanged profile boundary. Only
// staging uses a bounded lease; named profiles use the process lifetime.
func (p *provider) valid(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil || p.lifetime.Err() != nil || len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 16384 {
		return false
	}
	if p.profile != "" {
		for _, setting := range p.settings {
			if len(os.Getenv(setting)) > 16384 {
				return false
			}
		}
		return os.Getenv("PORTAL_IDENTITY_PROFILE") == p.profile && os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == "" && len(os.Getenv("PORTAL_IDENTITY_FLAG")) > 0
	}
	return os.Getenv("PORTAL_IDENTITY_PROFILE") == "" && time.Now().Before(p.until) && os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == p.lease
}

// BooleanEvaluation delegates values to the maintained provider, not a second
// flag parser. Check both sides so cancellation during resolution cannot admit.
func (p *provider) BooleanEvaluation(ctx context.Context, key string, fallback bool, data openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	// A successful explicit false keeps unrelated capabilities closed even if
	// a future caller supplies true as its SDK fallback. Evaluation errors are
	// still denied by the application's false-default/no-error gate.
	if p.profile != "" {
		setting, supported := p.settings[key]
		if !supported || (key != "portal-identity" && os.Getenv(setting) == "") {
			return openfeature.BoolResolutionDetail{Value: false, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DisabledReason}}
		}
	}
	denied := openfeature.BoolResolutionDetail{Value: false, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
		Reason: openfeature.ErrorReason, ResolutionError: openfeature.NewGeneralResolutionError("portal activation is unavailable"),
	}}
	if (p.profile == "" && key != "portal-identity") || !p.valid(ctx) {
		return denied
	}
	result := p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, data)
	if !p.valid(ctx) || result.ResolutionDetail().ErrorCode != "" {
		return denied
	}
	return result
}
