package runtimeapp

import (
	"context"
	"errors"
	"testing"
)

// TestAbsentConfigurationIsPublicOnlyWithoutConnections ensures public startup needs no private service.
func TestAbsentConfigurationIsPublicOnlyWithoutConnections(t *testing.T) {
	cfg, err := ReadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	h, shutdown, err := Portal(context.Background(), cfg, nil)
	if err != nil || h != nil || shutdown == nil {
		t.Fatalf("public-only wiring: handler=%v err=%v", h, err)
	}
	shutdown()
}

// TestPartialOrInvalidConfigurationFailsGenerically rejects unsafe or incomplete private wiring.
func TestPartialOrInvalidConfigurationFailsGenerically(t *testing.T) {
	valid := map[string]string{
		"PORTAL_DATABASE_URL": "postgres://runtime:private@db.example.invalid/app?sslmode=verify-full",
		"PORTAL_OIDC_ISSUER":  "https://broker.example.invalid", "PORTAL_OIDC_CLIENT_ID": "portal",
		"PORTAL_CALLBACK_URL": "https://app.example.invalid/portal/callback", "PORTAL_CLIENT_SECRET_FILE": "/private/secret",
	}
	for _, absent := range []string{"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL", "PORTAL_CLIENT_SECRET_FILE"} {
		_, err := ReadConfig(func(key string) string {
			if key == absent {
				return ""
			}
			return valid[key]
		})
		if !errors.Is(err, ErrConfig) || err.Error() != "portal configuration is invalid" {
			t.Fatal("partial configuration did not fail generically")
		}
	}
	for key, value := range map[string]string{"PORTAL_DATABASE_URL": "postgres://runtime:private@db.example.invalid/app?sslmode=disable", "PORTAL_OIDC_ISSUER": "http://broker.example.invalid", "PORTAL_CALLBACK_URL": "https://app.example.invalid/portal/callback?return=private", "PORTAL_CLIENT_SECRET_FILE": "relative-secret"} {
		_, err := ReadConfig(func(k string) string {
			if k == key {
				return value
			}
			return valid[k]
		})
		if !errors.Is(err, ErrConfig) {
			t.Fatal("unsafe configuration accepted")
		}
	}
	if _, err := ReadConfig(func(k string) string { return valid[k] }); err != nil {
		t.Fatal("valid configuration rejected")
	}
}

// TestExplicitIdentityProjectionSelection makes transport choice independent of activation.
func TestExplicitIdentityProjectionSelection(t *testing.T) {
	values := map[string]string{
		"PORTAL_DATABASE_URL": "postgres://runtime:private@db.example.invalid/app?sslmode=verify-full",
		"PORTAL_OIDC_ISSUER":  "https://broker.example.invalid", "PORTAL_OIDC_CLIENT_ID": "portal",
		"PORTAL_CALLBACK_URL":                  "https://app.example.invalid/portal/callback",
		"PORTAL_CLIENT_SECRET_PROJECTION_FILE": "/private/mount/secret",
	}
	if _, err := ReadConfig(func(key string) string { return values[key] }); err != nil {
		t.Fatal("complete explicit identity projection was rejected")
	}
	for _, tc := range []struct{ name, key, value string }{
		{"mixed readers", "PORTAL_CLIENT_SECRET_FILE", "/private/mount/secret"},
		{"relative projection", "PORTAL_CLIENT_SECRET_PROJECTION_FILE", "relative-secret"},
		{"partial dependencies", "PORTAL_DATABASE_URL", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadConfig(func(key string) string {
				if key == tc.key {
					return tc.value
				}
				return values[key]
			})
			if !errors.Is(err, ErrConfig) {
				t.Fatal("unsafe projection selection was accepted")
			}
		})
	}
}

// TestProductionIdentitySelectionRequiresCompleteDependencies uses the actual
// runtime selector without opening any real broker or database in this unit test.
func TestProductionIdentitySelectionRequiresCompleteDependencies(t *testing.T) {
	t.Setenv("PORTAL_IDENTITY_PROFILE", "production")
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", "")
	t.Setenv("PORTAL_IDENTITY_FLAG", `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`)
	if _, err := IdentityProvider(context.Background(), Config{}); err == nil {
		t.Fatal("production selection without private dependencies accepted")
	}
	values := map[string]string{"PORTAL_DATABASE_URL": "postgres://runtime:private@db.example.invalid/app?sslmode=verify-full", "PORTAL_OIDC_ISSUER": "https://broker.example.invalid", "PORTAL_OIDC_CLIENT_ID": "portal", "PORTAL_CALLBACK_URL": "https://app.example.invalid/portal/callback", "PORTAL_CLIENT_SECRET_FILE": "/private/fixture-secret"}
	cfg, err := ReadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	p, err := IdentityProvider(context.Background(), cfg)
	if err != nil || p == nil {
		t.Fatal("explicit production selection fell back to missing staging lease")
	}
	if state, ok := p.(interface{ Shutdown() }); ok {
		t.Cleanup(state.Shutdown)
	}
	for _, profile := range []string{"unknown", "Production", "staging"} {
		t.Setenv("PORTAL_IDENTITY_PROFILE", profile)
		if _, err := IdentityProvider(context.Background(), cfg); err == nil || err.Error() != "portal activation is invalid" {
			t.Fatal("unknown activation profile accepted or diagnostics disclosed input")
		}
	}
}
