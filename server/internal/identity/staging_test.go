package identity

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/activation"
)

// Preserve actual TLS federation, SQL invitations, session cookies and current
// membership while using the executable's maintained staging provider.
func TestStagingProviderBrowserSessionAndRevocation(t *testing.T) {
	b := newBrowserFixture(t)
	t.Setenv("PORTAL_IDENTITY_FLAG", `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`)
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
	lifetime, cancel := context.WithCancel(b.f.ctx)
	defer cancel()
	provider, err := activation.New(lifetime)
	if err != nil {
		t.Fatal("maintained staging provider rejected the valid fixture")
	}
	if err := b.flags.SetProviderAndWait(lifetime, provider); err != nil {
		t.Fatal("staging provider initialization failed")
	}
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	if status, _, _ := b.request(http.MethodGet, "/portal/", nil, ""); status != http.StatusOK {
		t.Fatal("staging provider did not admit the actual invited browser session")
	}
	for _, route := range []string{"/portal/requests", "/portal/operator/requests"} {
		status, _, headers := b.request(http.MethodGet, route, nil, "")
		if status != http.StatusNotFound || headers.Get("Cache-Control") != "no-store" {
			t.Fatal("staging identity activated an independent work gate")
		}
	}
	if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "client-a"); err != nil {
		t.Fatal("trusted fixture membership revocation failed")
	}
	if status, _, _ := b.request(http.MethodGet, "/portal/", nil, ""); status == http.StatusOK {
		t.Fatal("revoked membership retained a private browser workspace")
	}
	cancel()
	if status, _, headers := b.request(http.MethodGet, "/portal/", nil, ""); status != http.StatusNotFound || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("cancelled staging process retained its browser boundary")
	}
}
