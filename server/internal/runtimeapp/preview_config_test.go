package runtimeapp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"maps"
	"math/big"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/open-feature/go-sdk/openfeature"
)

const enabledPreviewTestFlag = `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`

func previewTestSettings() map[string]string {
	return map[string]string{
		"PORTAL_IDENTITY_PROFILE":              "preview",
		"PORTAL_DATABASE_URL":                  "postgres://portal_preview_web@preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=verify-full",
		"PORTAL_OIDC_ISSUER":                   "https://login.devantler.tech",
		"PORTAL_OIDC_CLIENT_ID":                "business-site-preview",
		"PORTAL_CALLBACK_URL":                  "https://dev.devantler.tech/callback",
		"PORTAL_CLIENT_SECRET_PROJECTION_FILE": "/run/webapp-inputs/preview-oidc-client-secret",
		"PORTAL_OPERATOR_SUBJECTS":             customeridentity.DexSubject("10001234567890", customeridentity.ZitadelConnectorID),
	}
}

func TestUnifiedPreviewRequiresIsolatedStorageOnTheProductionPortal(t *testing.T) {
	const previewKey = "PORTAL_CLIENT_PREVIEW_DATABASE_URL"
	const caKey = "PORTAL_CLIENT_PREVIEW_DATABASE_CA"
	const isolatedURL = "postgres://portal_preview_web@preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=verify-full"
	production := map[string]string{
		"PORTAL_IDENTITY_PROFILE":              "production",
		"PORTAL_DATABASE_URL":                  "postgres://portal_web@portal-db.fixture.invalid:5432/portal?sslmode=verify-full",
		"PORTAL_OIDC_ISSUER":                   "https://login.devantler.tech",
		"PORTAL_OIDC_CLIENT_ID":                "business-portal",
		"PORTAL_CALLBACK_URL":                  "https://portal.devantler.tech/callback",
		"PORTAL_CLIENT_SECRET_PROJECTION_FILE": "/run/webapp-inputs/portal-client-secret",
		"PORTAL_OPERATOR_SUBJECTS":             customeridentity.DexSubject("10001234567890", customeridentity.ZitadelConnectorID),
		previewKey:                             isolatedURL,
		caKey:                                  unifiedPreviewFixtureCA(t),
	}
	settings, err := parsePortalSettings([]byte(previewKey + "=" + isolatedURL + "\n" + caKey + "=" + production[caKey] + "\n"))
	if err != nil || settings[previewKey] != isolatedURL || settings[caKey] != production[caKey] {
		t.Error("the bounded private projection did not admit the isolated preview setting")
	}
	cfg, err := ReadConfig(func(key string) string { return production[key] })
	if err != nil || cfg.previewDatabase != isolatedURL || !cfg.FitsPortalHost("portal.devantler.tech") || cfg.FitsPortalHost("dev.devantler.tech") {
		t.Fatal("unified preview did not retain the regular portal callback")
	}
	store, err := cfg.unifiedPreviewStoreConfig()
	if err != nil || store.ConnConfig.TLSConfig.RootCAs == nil {
		t.Fatal("unified preview did not select its independent trust root")
	}
	for _, test := range []struct{ name, key, value string }{
		{"ordinary database reuse", previewKey, production["PORTAL_DATABASE_URL"]},
		{"preview database used as main store", "PORTAL_DATABASE_URL", isolatedURL},
		{"preview database address override", previewKey, isolatedURL + "&host=portal-db.fixture.invalid"},
		{"preview database cannot select the main CA", previewKey, isolatedURL + "&sslrootcert=/run/webapp-inputs/portal-database-ca"},
		{"missing preview trust root", caKey, ""},
		{"missing preview database", previewKey, ""},
		{"invalid preview trust root", caKey, base64.StdEncoding.EncodeToString([]byte("not a certificate"))},
		{"preview database without hostname validation", previewKey, "postgres://portal_preview_web@preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=require"},
		{"preview database carries another login", previewKey, "postgres://portal_web@preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=verify-full"},
		{"wrong callback host", "PORTAL_CALLBACK_URL", "https://other.example.invalid/callback"},
		{"wrong audience", "PORTAL_OIDC_CLIENT_ID", "different-portal"},
		{"missing operator allowlist", "PORTAL_OPERATOR_SUBJECTS", ""},
		{"wrong identity profile", "PORTAL_IDENTITY_PROFILE", "preview"},
		{"expired staging configuration", "PORTAL_IDENTITY_PROFILE", ""},
		{"missing ordinary database", "PORTAL_DATABASE_URL", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := ReadConfig(func(key string) string {
				if key == test.key {
					return test.value
				}
				return production[key]
			})
			if !errors.Is(err, ErrConfig) || cfg != (Config{}) {
				t.Fatal("unsafe unified preview settings were admitted")
			}
		})
	}
}

func unifiedPreviewFixtureCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestPreviewRuntimeBindsThePrivateHost(t *testing.T) {
	settings := previewTestSettings()
	cfg, err := ReadConfig(func(key string) string { return settings[key] })
	if err != nil || !cfg.FitsPortalHost("dev.devantler.tech") {
		t.Fatal("the separate preview runtime was refused")
	}
	if cfg.FitsPortalHost("portal.devantler.tech") || cfg.FitsPortalHost("") {
		t.Fatal("the preview runtime accepted another serving host")
	}
	settings["PORTAL_IDENTITY_PROFILE"] = "production"
	ordinary, err := ReadConfig(func(key string) string { return settings[key] })
	if err == nil && ordinary.FitsPortalHost("dev.devantler.tech") {
		t.Fatal("normal portal configuration accepted the exclusive preview host")
	}
	for _, equivalentHost := range []string{"DEV.devantler.tech", "dev.devantler.tech."} {
		settings["PORTAL_CALLBACK_URL"] = "https://" + equivalentHost + "/portal/callback"
		ordinary, err = ReadConfig(func(key string) string { return settings[key] })
		if err == nil && ordinary.FitsPortalHost("") {
			t.Fatal("normal portal configuration accepted an equivalent preview callback host")
		}
	}
}

func TestPreviewProfileRequiresPrivateDependencies(t *testing.T) {
	if _, err := ReadConfig(func(key string) string {
		if key == "PORTAL_IDENTITY_PROFILE" {
			return "preview"
		}
		return ""
	}); !errors.Is(err, ErrConfig) {
		t.Fatal("preview profile accepted startup without private dependencies")
	}
}

func TestPreviewRuntimeRejectsSharedDependencies(t *testing.T) {
	for _, test := range []struct {
		name     string
		override map[string]string
	}{
		{"ordinary database", map[string]string{"PORTAL_DATABASE_URL": "postgres://ordinary_runtime@production-fixture.example.invalid:5432/portal?sslmode=verify-full"}},
		{"database host override", map[string]string{"PORTAL_DATABASE_URL": previewTestSettings()["PORTAL_DATABASE_URL"] + "&host=production.example.invalid"}},
		{"ordinary database role", map[string]string{"PORTAL_DATABASE_URL": "postgres://ordinary_runtime@preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=verify-full"}},
		{"ordinary database name", map[string]string{"PORTAL_DATABASE_URL": "postgres://portal_preview_web@preview-fixture.business-site-dev.svc.cluster.local:5432/portal?sslmode=verify-full"}},
		{"hostname outside the preview service form", map[string]string{"PORTAL_DATABASE_URL": "postgres://portal_preview_web@other.preview-fixture.business-site-dev.svc.cluster.local:5432/portal_preview?sslmode=verify-full"}},
		{"ordinary audience", map[string]string{"PORTAL_OIDC_CLIENT_ID": "portal"}},
		{"different issuer", map[string]string{"PORTAL_OIDC_ISSUER": "https://different.example.invalid"}},
		{"ordinary callback", map[string]string{"PORTAL_CALLBACK_URL": "https://portal.devantler.tech/callback"}},
		{"mixed workspace operator list", map[string]string{"PORTAL_OPERATOR_SUBJECTS": previewTestSettings()["PORTAL_OPERATOR_SUBJECTS"] + "," + customeridentity.DexSubject("20001234567890", customeridentity.ZitadelConnectorID)}},
		{"missing verified operator", map[string]string{"PORTAL_OPERATOR_SUBJECTS": ""}},
		{"legacy operator connector", map[string]string{"PORTAL_OPERATOR_SUBJECTS": customeridentity.DexSubject("10001234567890", customeridentity.PlatformConnectorID)}},
		{"real invoicing", map[string]string{"PORTAL_NUMINA_TOKEN_FILE": "/fixture/numina-token"}},
		{"real card collection", map[string]string{"PORTAL_STRIPE_KEY_FILE": "/fixture/stripe-key", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/fixture/stripe-webhook", "PORTAL_STRIPE_TAX_RATE": "txr_fixture"}},
		{"real mail", map[string]string{"PORTAL_SMTP_USERNAME_FILE": "/fixture/smtp-user", "PORTAL_SMTP_PASSWORD_FILE": "/fixture/smtp-password"}},
		{"customer provisioning", map[string]string{"PORTAL_ZITADEL_URL": "https://accounts.devantler.tech", "PORTAL_ZITADEL_ORG_ID": "10001234567890", "PORTAL_ZITADEL_TOKEN_PROJECTION_FILE": "/fixture/customer-token"}},
		{"common upstream logout", map[string]string{"PORTAL_UPSTREAM_END_SESSION_URL": "https://accounts.devantler.tech/oidc/v1/end_session", "PORTAL_UPSTREAM_CLIENT_ID": "production"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := previewTestSettings()
			maps.Copy(settings, test.override)
			if _, err := ReadConfig(func(key string) string { return settings[key] }); !errors.Is(err, ErrConfig) {
				t.Fatal("accepted preview settings that could reach another environment")
			}
		})
	}
}

func TestPreviewProviderKeepsRealBillingAndWorkGatesClosed(t *testing.T) {
	t.Setenv("PORTAL_IDENTITY_PROFILE", "preview")
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", "")
	for _, key := range []string{
		"PORTAL_IDENTITY_FLAG", "PORTAL_PROPOSAL_ACCEPTANCE_FLAG",
		"PORTAL_INVOICE_BILLING_FLAG", "PORTAL_CARD_BILLING_FLAG", "PORTAL_CONTACT_VERIFICATION_FLAG",
	} {
		t.Setenv(key, enabledPreviewTestFlag)
	}
	settings := previewTestSettings()
	cfg, err := ReadConfig(func(key string) string { return settings[key] })
	if err != nil {
		t.Fatal("could not admit the preview configuration")
	}
	provider, err := IdentityProvider(context.Background(), cfg)
	if err != nil {
		t.Fatal("the isolated preview capability provider did not start")
	}
	if state, ok := provider.(interface{ Shutdown() }); ok {
		t.Cleanup(state.Shutdown)
	}
	for _, key := range []string{"portal-identity", "portal-proposal-acceptance"} {
		result := provider.BooleanEvaluation(context.Background(), key, false, openfeature.FlattenedContext{})
		if !result.Value || result.ResolutionDetail().ErrorCode != "" {
			t.Fatalf("preview capability %s was refused", key)
		}
	}
	for _, key := range []string{"portal-invoice-billing", "portal-card-billing", "portal-contact-verification", "portal-work-approvals", "portal-support"} {
		result := provider.BooleanEvaluation(context.Background(), key, true, openfeature.FlattenedContext{})
		if result.Value {
			t.Fatalf("preview capability %s should be explicitly closed", key)
		}
	}
	t.Setenv("PORTAL_IDENTITY_PROFILE", "production")
	if _, err := IdentityProvider(context.Background(), cfg); err == nil {
		t.Fatal("preview dependencies were accepted by the production provider")
	}
}
