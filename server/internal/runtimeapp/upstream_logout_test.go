package runtimeapp

import "testing"

// The upstream logout is optional, both-or-neither, and only with a portal.
func TestUpstreamLogoutConfiguration(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	with := func(extra map[string]string) map[string]string {
		m := fictionalDependencies()
		for key, value := range extra {
			m[key] = value
		}
		return m
	}
	c, err := ReadConfig(env(with(nil)))
	if err != nil || c.endSession != "" || c.endSessionClient != "" {
		t.Fatalf("a portal without upstream logout was refused or gained one: %v", err)
	}
	c, err = ReadConfig(env(with(map[string]string{"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/oidc/v1/end_session", "PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"})))
	if err != nil || c.endSession != "https://accounts.example.invalid/oidc/v1/end_session" || c.endSessionClient != "broker-upstream" {
		t.Fatalf("a complete upstream logout was not read: %v", err)
	}
	for name, values := range map[string]map[string]string{
		"endpoint without client": with(map[string]string{"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/end"}),
		"client without endpoint": with(map[string]string{"PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"}),
		"logout without a portal": {"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/end", "PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"},
	} {
		if _, err := ReadConfig(env(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, key := range []string{"PORTAL_UPSTREAM_END_SESSION_URL", "PORTAL_UPSTREAM_CLIENT_ID"} {
		found := false
		for _, projected := range projectedPortalSettings {
			found = found || projected == key
		}
		if !found {
			t.Errorf("%s cannot come from the settings projection", key)
		}
	}
}
