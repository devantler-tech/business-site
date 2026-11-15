package runtimeapp

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
)

// unsetPortalEnvironment removes every inherited portal setting for one test;
// testing restores the caller's original environment afterward.
func unsetPortalEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range inheritedPortalSettings {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal("fixture environment reset failed")
		}
	}
}

// fictionalDependencies builds complete, parseable settings without a literal credential URI.
func fictionalDependencies() map[string]string {
	database := url.URL{Scheme: "postgres", User: url.UserPassword("portal_web", "fictional"), Host: "db.example.invalid:5432", Path: "/portal", RawQuery: "sslmode=verify-full"}
	return map[string]string{
		"PORTAL_DATABASE_URL": database.String(), "PORTAL_OIDC_ISSUER": "https://login.example.invalid",
		"PORTAL_OIDC_CLIENT_ID": "business-portal", "PORTAL_CALLBACK_URL": "https://portal.example.invalid/portal/callback",
		"PORTAL_CLIENT_SECRET_PROJECTION_FILE": "/run/webapp-inputs/portal-client-secret",
	}
}

// settingsText renders settings in a fixed order, as an operator would write them.
func settingsText(settings map[string]string) string {
	var text strings.Builder
	for _, key := range projectedPortalSettings {
		if value, ok := settings[key]; ok {
			text.WriteString(key + "=" + value + "\n")
		}
	}
	return text.String()
}

// TestPortalSettingsFormat accepts only the closed line format and keys.
func TestPortalSettingsFormat(t *testing.T) {
	complete := settingsText(fictionalDependencies())
	for _, tc := range []struct {
		name, input string
		keys        int
	}{
		{"empty file is off", "", 0},
		{"comments and blank lines are off", "# portal sign-in is off\n\n   \n#PORTAL_DATABASE_URL=ignored\n", 0},
		{"complete dependencies", complete, 5},
		{"no final newline", strings.TrimSuffix(complete, "\n"), 5},
		{"interleaved comments", "# dependencies\n" + strings.ReplaceAll(complete, "\n", "\n\n# next\n"), 5},
		{"value keeps equals signs", "PORTAL_IDENTITY_FLAG={\"a\":\"b=c\"}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := parsePortalSettings([]byte(tc.input))
			if err != nil || len(settings) != tc.keys {
				t.Fatalf("valid settings were refused or miscounted: %d", len(settings))
			}
		})
	}
	if settings, _ := parsePortalSettings([]byte("PORTAL_IDENTITY_FLAG={\"a\":\"b=c\"}")); settings["PORTAL_IDENTITY_FLAG"] != `{"a":"b=c"}` {
		t.Fatal("a value was not preserved after its first equals sign")
	}
	for _, tc := range []struct{ name, input string }{
		{"staging lease stays environment-only", "PORTAL_IDENTITY_LEASE_UNTIL=2026-11-15T00:00:00Z\n"},
		{"owner-only secret file stays environment-only", "PORTAL_CLIENT_SECRET_FILE=/private/secret\n"},
		{"unknown key", "PORTAL_MIGRATION_DATABASE_FILE=/private/migration\n"},
		{"lowercase key", "portal_oidc_issuer=https://login.example.invalid\n"},
		{"repeated key", "PORTAL_OIDC_CLIENT_ID=business-portal\nPORTAL_OIDC_CLIENT_ID=other\n"},
		{"empty value", "PORTAL_IDENTITY_PROFILE=\n"},
		{"missing separator", "PORTAL_IDENTITY_PROFILE\n"},
		{"spaced separator", "PORTAL_IDENTITY_PROFILE = production\n"},
		{"surrounding value whitespace", "PORTAL_IDENTITY_PROFILE=production \n"},
		{"indented key", " PORTAL_IDENTITY_PROFILE=production\n"},
		{"indented comment", "  # not a comment\n"},
		{"carriage return", "PORTAL_IDENTITY_PROFILE=production\r\n"},
		{"NUL", "PORTAL_IDENTITY_PROFILE=\x00production\n"},
		{"invalid UTF-8", "PORTAL_IDENTITY_PROFILE=\xff\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if settings, err := parsePortalSettings([]byte(tc.input)); !errors.Is(err, ErrPortalSettings) || settings != nil {
				t.Fatal("unsafe settings were accepted")
			}
		})
	}
}

// TestPortalSettingsProjectionFeedsExistingConfiguration proves the projected
// values reach ReadConfig and the activation selector exactly as inherited
// variables do, and that only one source may decide them.
func TestPortalSettingsProjectionFeedsExistingConfiguration(t *testing.T) {
	dependencies := fictionalDependencies()
	inherited, err := ReadConfig(func(key string) string { return dependencies[key] })
	if err != nil || inherited == (Config{}) {
		t.Fatal("fictional dependencies were not a complete configuration")
	}
	t.Run("dependencies without activation stay off", func(t *testing.T) {
		unsetPortalEnvironment(t)
		if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(settingsText(dependencies)), 0440)); err != nil {
			t.Fatal("safe projected settings were refused")
		}
		projected, err := ReadConfig(os.Getenv)
		if err != nil || projected != inherited {
			t.Fatal("projected settings differ from the same inherited environment")
		}
		provider, err := IdentityProvider(context.Background(), projected)
		if err != nil || provider != (openfeature.NoopProvider{}) {
			t.Fatal("complete dependencies without activation did not keep the no-op gate")
		}
	})
	t.Run("comment-only file is ordinary public startup", func(t *testing.T) {
		unsetPortalEnvironment(t)
		if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte("# portal sign-in is off\n"), 0440)); err != nil {
			t.Fatal("comment-only settings were refused")
		}
		for _, key := range inheritedPortalSettings {
			if _, set := os.LookupEnv(key); set {
				t.Fatal("comment-only settings installed a value")
			}
		}
		if cfg, err := ReadConfig(os.Getenv); err != nil || cfg != (Config{}) {
			t.Fatal("comment-only settings configured portal dependencies")
		}
	})
	t.Run("production activation is selected", func(t *testing.T) {
		unsetPortalEnvironment(t)
		settings := maps.Clone(dependencies)
		settings["PORTAL_IDENTITY_PROFILE"] = "production"
		settings["PORTAL_IDENTITY_FLAG"] = `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`
		if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(settingsText(settings)), 0440)); err != nil {
			t.Fatal("projected production activation was refused")
		}
		cfg, err := ReadConfig(os.Getenv)
		if err != nil {
			t.Fatal("projected production dependencies were refused")
		}
		provider, err := IdentityProvider(context.Background(), cfg)
		if err != nil || provider == (openfeature.NoopProvider{}) {
			t.Fatal("projected production activation did not select the production provider")
		}
		if state, ok := provider.(interface{ Shutdown() }); ok {
			t.Cleanup(state.Shutdown)
		}
	})
	t.Run("Numina billing settings are projected", func(t *testing.T) {
		unsetPortalEnvironment(t)
		settings := maps.Clone(dependencies)
		settings["PORTAL_NUMINA_TOKEN_FILE"] = "/run/webapp-inputs/numina-api-key"
		settings["PORTAL_NUMINA_SALES_ACCOUNT"] = "1010"
		if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(settingsText(settings)), 0440)); err != nil {
			t.Fatal("projected billing settings were refused")
		}
		cfg, err := ReadConfig(os.Getenv)
		if err != nil || cfg.billing.tokenFile != "/run/webapp-inputs/numina-api-key" || cfg.billing.salesAccount != 1010 || cfg.billing.vatCode != "U25" {
			t.Fatalf("projected billing settings were not read: %+v %v", cfg.billing, err)
		}
	})
	for _, key := range inheritedPortalSettings {
		t.Run("inherited "+key, func(t *testing.T) {
			unsetPortalEnvironment(t)
			t.Setenv(key, "")
			if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(settingsText(dependencies)), 0440)); !errors.Is(err, ErrPortalSettings) {
				t.Fatal("projected settings were mixed with an inherited setting")
			}
			for _, projected := range projectedPortalSettings {
				if os.Getenv(projected) != "" {
					t.Fatal("a refused projection installed settings")
				}
			}
		})
	}
	for _, tc := range []struct {
		name string
		mode os.FileMode
	}{{"other read", 0444}, {"group write", 0460}, {"execute", 0540}} {
		t.Run(tc.name, func(t *testing.T) {
			unsetPortalEnvironment(t)
			if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(settingsText(dependencies)), tc.mode)); !errors.Is(err, ErrPortalSettings) || os.Getenv("PORTAL_DATABASE_URL") != "" {
				t.Fatal("unsafe projection permissions were accepted")
			}
		})
	}
	t.Run("unconfined paths", func(t *testing.T) {
		unsetPortalEnvironment(t)
		outside := projectedIdentityFixture(t, []byte(settingsText(dependencies)), 0440)
		escape := filepath.Join(t.TempDir(), "escape")
		if err := os.Symlink(outside, escape); err != nil {
			t.Fatal("escape fixture creation failed")
		}
		for _, path := range []string{escape, filepath.Join(t.TempDir(), "missing"), t.TempDir(), "relative-settings"} {
			if err := ApplyPortalSettingsProjection(path); !errors.Is(err, ErrPortalSettings) || os.Getenv("PORTAL_DATABASE_URL") != "" {
				t.Fatal("unconfined or nonregular settings were accepted")
			}
		}
	})
}
