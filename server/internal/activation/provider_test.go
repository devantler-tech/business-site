package activation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

const on = `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`

// activeClient installs the maintained provider with a fictional, cleanup-owned lease.
func activeClient(ctx context.Context, t *testing.T, until time.Time) (*openfeature.Client, *openfeature.EvaluationAPI) {
	t.Helper()
	t.Setenv("PORTAL_IDENTITY_FLAG", on)
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", until.UTC().Format(time.RFC3339Nano))
	p, err := New(ctx)
	if err != nil {
		t.Fatal("valid staging lease was rejected")
	}
	api := isolated.NewAPI()
	t.Cleanup(func() {
		if err := api.Shutdown(context.Background()); err != nil {
			t.Error("staging provider shutdown failed")
		}
	})
	if err := api.SetProviderAndWait(ctx, p); err != nil {
		t.Fatal("staging provider initialization failed")
	}
	return api.NewClient(), api
}

// enabled requires a successful false-default evaluation, not merely a true value.
func enabled(t *testing.T, client *openfeature.Client) bool {
	t.Helper()
	value, err := client.BooleanValue(context.Background(), "portal-identity", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && value
}

// The real SDK/provider must not cache an earlier valid true or map other flags.
func TestMaintainedDecoderAndIndependentWorkGates(t *testing.T) {
	client, _ := activeClient(context.Background(), t, time.Now().Add(time.Minute))
	if !enabled(t, client) {
		t.Fatal("explicit boolean activation did not reach the maintained provider")
	}
	for _, value := range []string{"", "true", "{invalid", "null", `{"defaultVariant":"off","variants":[{"name":"off","value":false}]}`, `{"defaultVariant":"on","variants":[{"name":"on","value":"true"}]}`, strings.Repeat("x", 16385)} {
		t.Setenv("PORTAL_IDENTITY_FLAG", value)
		if enabled(t, client) {
			t.Fatal("missing, false, invalid, wrong-type or oversized configuration retained true")
		}
	}
	t.Setenv("PORTAL_IDENTITY_FLAG", on)
	for _, key := range []string{"portal-requests", "portal-triage", "portal-proposals"} {
		t.Setenv(key, on)
		value, err := client.BooleanValue(context.Background(), key, false, openfeature.NewEvaluationContext("", nil))
		if value || err == nil {
			t.Fatal("staging identity granted an independent work capability")
		}
	}
}

// SDK bookkeeping alone does not revoke a retained client; the lease owner must.
func TestCancellationAndSDKShutdownRevokeRetainedClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, _ := activeClient(ctx, t, time.Now().Add(time.Minute))
	if !enabled(t, client) {
		t.Fatal("valid activation was not exercised before cancellation")
	}
	cancel()
	if enabled(t, client) {
		t.Fatal("cancelled process lifetime retained identity authority")
	}
	client, api := activeClient(context.Background(), t, time.Now().Add(time.Minute))
	if !enabled(t, client) {
		t.Fatal("valid activation was not exercised before shutdown")
	}
	if err := api.Shutdown(context.Background()); err != nil {
		t.Fatal("SDK shutdown failed")
	}
	if enabled(t, client) {
		t.Fatal("SDK shutdown retained identity authority")
	}
}

// An externally extended lease cannot extend an already-running staging process.
func TestLeaseExpiresAndCannotBeExtendedInPlace(t *testing.T) {
	deadline := time.Now().Add(150 * time.Millisecond)
	client, _ := activeClient(context.Background(), t, deadline)
	if !enabled(t, client) {
		t.Fatal("valid short lease did not open identity")
	}
	<-time.After(time.Until(deadline) + time.Millisecond)
	if enabled(t, client) {
		t.Fatal("expired lease retained identity authority")
	}
	client, _ = activeClient(context.Background(), t, time.Now().Add(time.Minute))
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", time.Now().Add(2*time.Minute).UTC().Format(time.RFC3339Nano))
	if enabled(t, client) {
		t.Fatal("process lease was extended without a restart")
	}
}

// TestInvalidLeaseCannotInitialize rejects missing, malformed, expired and excessive authority.
func TestInvalidLeaseCannotInitialize(t *testing.T) {
	t.Setenv("PORTAL_IDENTITY_FLAG", on)
	for _, value := range []string{"", "invalid", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)} {
		t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", value)
		if _, err := New(context.Background()); err == nil || err.Error() != "portal activation is invalid" {
			t.Fatal("missing, malformed, expired or unbounded lease accepted")
		}
	}
}
