package runtimeapp

import (
	"errors"
	"testing"
)

// settingsEnv serves values as the process environment.
func settingsEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// withPortal adds extra to the fictional portal dependencies.
func withPortal(extra map[string]string) map[string]string {
	m := fictionalDependencies()
	for key, value := range extra {
		m[key] = value
	}
	return m
}

// requireProjected fails unless every key can come from the settings projection.
func requireProjected(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		found := false
		for _, projected := range projectedPortalSettings {
			found = found || projected == key
		}
		if !found {
			t.Errorf("%s cannot come from the settings projection", key)
		}
	}
}

// The upstream logout is optional, both-or-neither, and only with a portal.
func TestUpstreamLogoutConfiguration(t *testing.T) {
	c, err := ReadConfig(settingsEnv(withPortal(nil)))
	if err != nil || c.endSession != "" || c.endSessionClient != "" {
		t.Fatalf("a portal without upstream logout was refused or gained one: %v", err)
	}
	c, err = ReadConfig(settingsEnv(withPortal(map[string]string{"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/oidc/v1/end_session", "PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"})))
	if err != nil || c.endSession != "https://accounts.example.invalid/oidc/v1/end_session" || c.endSessionClient != "broker-upstream" {
		t.Fatalf("a complete upstream logout was not read: %v", err)
	}
	for name, values := range map[string]map[string]string{
		"endpoint without client": withPortal(map[string]string{"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/end"}),
		"client without endpoint": withPortal(map[string]string{"PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"}),
		"logout without a portal": {"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.example.invalid/end", "PORTAL_UPSTREAM_CLIENT_ID": "broker-upstream"},
	} {
		if _, err := ReadConfig(settingsEnv(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	requireProjected(t, "PORTAL_UPSTREAM_END_SESSION_URL", "PORTAL_UPSTREAM_CLIENT_ID")
}

func TestUpstreamLogoutDefaultsRespectPortalAndExistingSettings(t *testing.T) {
	const endpoint, client = "https://accounts.example.invalid/end", "release-client"
	cfg, err := ReadConfig(settingsEnv(withPortal(nil)))
	if err != nil {
		t.Fatal(err)
	}
	configured, err := cfg.WithUpstreamLogoutDefaults(endpoint, client)
	if err != nil || configured.endSession != endpoint || configured.endSessionClient != client {
		t.Fatal("the complete portal did not accept the release logout pair", err)
	}

	const existingEndpoint, existingClient = "https://old-accounts.example.invalid/end", "platform-client"
	existing, err := ReadConfig(settingsEnv(withPortal(map[string]string{
		"PORTAL_UPSTREAM_END_SESSION_URL": existingEndpoint, "PORTAL_UPSTREAM_CLIENT_ID": existingClient,
	})))
	if err != nil {
		t.Fatal(err)
	}
	withDefaults, err := existing.WithUpstreamLogoutDefaults(endpoint, client)
	if err != nil || withDefaults.endSession != existingEndpoint || withDefaults.endSessionClient != existingClient {
		t.Fatal("the existing projection's logout pair was replaced or mixed", err)
	}
	if _, err := existing.WithUpstreamLogoutDefaults("https://accounts.example.invalid/end?next=bad", client); !errors.Is(err, ErrConfig) {
		t.Fatal("malformed defaults were accepted behind the existing projection", err)
	}
	if _, err := cfg.WithUpstreamLogoutDefaults(endpoint, ""); !errors.Is(err, ErrConfig) {
		t.Fatal("an incomplete release logout pair was accepted", err)
	}
	public, err := (Config{}).WithUpstreamLogoutDefaults(endpoint, client)
	if err != nil || public != (Config{}) {
		t.Fatal("logout defaults configured a public-only runtime", err)
	}
}

// The operator list is optional and only with a portal. Portal startup checks
// its entries (identity.TestLinkOperatorsAcceptsOnlyGitHubIdentities).
func TestOperatorConfiguration(t *testing.T) {
	c, err := ReadConfig(settingsEnv(withPortal(nil)))
	if err != nil || c.operators != "" {
		t.Fatalf("a portal without operators was refused or gained some: %v", err)
	}
	listed := "ChhDZ2d5TmpJd016UXlNQklHWjJsMGFIVmkSCHBsYXRmb3Jt"
	if c, err = ReadConfig(settingsEnv(withPortal(map[string]string{"PORTAL_OPERATOR_SUBJECTS": listed}))); err != nil || c.operators != listed {
		t.Fatalf("the operator list was not read: %v", err)
	}
	if _, err = ReadConfig(settingsEnv(map[string]string{"PORTAL_OPERATOR_SUBJECTS": listed})); err == nil {
		t.Error("operators without a portal were accepted")
	}
	requireProjected(t, "PORTAL_OPERATOR_SUBJECTS")
}
