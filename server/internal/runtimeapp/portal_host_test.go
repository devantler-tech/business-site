package runtimeapp

import "testing"

// TestFitsPortalHost keeps the router and the portal on one address space: a
// portal with its own host lives at its root, a shared one under /portal.
func TestFitsPortalHost(t *testing.T) {
	for _, tc := range []struct {
		callback, host string
		fits           bool
	}{
		{"https://portal.devantler.tech/callback", "portal.devantler.tech", true},
		{"https://portal.devantler.tech/portal/callback", "portal.devantler.tech", false},
		{"https://other.example.invalid/callback", "portal.devantler.tech", false},
		{"https://devantler.tech/portal/callback", "", true},
		{"https://devantler.tech/callback", "", false},
	} {
		if got := (Config{callback: tc.callback}).FitsPortalHost(tc.host); got != tc.fits {
			t.Errorf("callback %q host %q: fits=%v, want %v", tc.callback, tc.host, got, tc.fits)
		}
	}
	if !(Config{}).FitsPortalHost("portal.devantler.tech") || !(Config{}).FitsPortalHost("") {
		t.Fatal("no configured portal must fit every routing")
	}
}

// TestWithPublicHostKeepsAnUnconfiguredPortalOff must not turn "no portal" into a
// configured one, or startup would try to connect private dependencies.
func TestWithPublicHostKeepsAnUnconfiguredPortalOff(t *testing.T) {
	if (Config{}).WithPublicHost("devantler.tech") != (Config{}) {
		t.Fatal("public host configured an absent portal")
	}
	c := Config{callback: "https://portal.devantler.tech/callback"}
	if got := c.WithPublicHost("devantler.tech").publicSite; got != "https://devantler.tech" {
		t.Fatalf("public site = %q", got)
	}
	if got := c.WithPublicHost("").publicSite; got != "" {
		t.Fatalf("empty host set public site %q", got)
	}
}
