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
