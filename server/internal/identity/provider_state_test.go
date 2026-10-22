package identity

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

type unsettledProvider struct {
	openfeature.FeatureProvider
	events chan openfeature.Event
	close  sync.Once
}

func (*unsettledProvider) Init(openfeature.EvaluationContext) error { return nil }
func (p *unsettledProvider) Shutdown()                              { p.close.Do(func() { close(p.events) }) }
func (p *unsettledProvider) EventChannel() <-chan openfeature.Event { return p.events }

// requireFixtureSDKEvent installs a true-valued provider, then observes its real
// SDK state transition before any handler assertion. Both native journeys use
// the same setup without substituting a boolean or bypassing event delivery.
func requireFixtureSDKEvent(t *testing.T, api *openfeature.EvaluationAPI, client *openfeature.Client, flags map[string]memprovider.InMemoryFlag, event openfeature.EventType) {
	t.Helper()
	seen := make(chan struct{})
	callback := func(openfeature.EventDetails) { close(seen) }
	client.AddHandler(event, &callback)
	t.Cleanup(func() { client.RemoveHandler(event, &callback) })
	provider := &unsettledProvider{FeatureProvider: memprovider.NewInMemoryProvider(flags), events: make(chan openfeature.Event, 1)}
	if err := api.SetProviderAndWait(t.Context(), provider); err != nil {
		t.Fatal("provider fixture initialization failed", err)
	}
	provider.events <- openfeature.Event{EventType: event}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("SDK state control did not settle")
	}
}

// The SDK may evaluate ERROR/STALE providers, even when they return true.
// The application's identity boundary must reject those states independently.
func TestReadyRejectsUnhealthySDKProvider(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		t.Run(string(event), func(t *testing.T) {
			p := &unsettledProvider{
				FeatureProvider: memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
					"portal-identity": {State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}},
				}),
				events: make(chan openfeature.Event, 1),
			}
			api := isolated.NewAPI()
			t.Cleanup(func() {
				if err := api.Shutdown(context.Background()); err != nil {
					t.Error("state fixture shutdown failed")
				}
			})
			if err := api.SetProviderAndWait(t.Context(), p); err != nil {
				t.Fatal("state fixture initialization failed")
			}
			client := api.NewClient()
			s := New(&pgxpool.Pool{}, &Broker{verifier: &oidc.IDTokenVerifier{}}, client)
			if !s.ready(t.Context()) {
				t.Fatal("healthy fixture did not exercise true evaluation")
			}
			settled := make(chan struct{})
			callback := func(openfeature.EventDetails) { close(settled) }
			client.AddHandler(event, &callback)
			p.events <- openfeature.Event{EventType: event}
			select {
			case <-settled:
			case <-time.After(time.Second):
				t.Fatal("SDK did not consume the provider state event")
			}
			if s.ready(t.Context()) {
				t.Fatal("unhealthy SDK state admitted identity despite a true evaluation")
			}
		})
	}
}
