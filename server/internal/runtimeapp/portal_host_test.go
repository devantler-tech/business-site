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
