package customeridentity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// dexFixture creates fictional, separately owned broker credentials.
func dexFixture(t *testing.T) (string, string) {
	t.Helper()
	input, output := materialFixture(t, "serve")
	if err := os.Remove(filepath.Join(input, "master.key")); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"schemaVersion": "customer-dex-private-v1",
		"dexIssuer":     "https://login.example.invalid", "zitadelIssuer": "https://accounts.example.invalid",
		"portalCallback": "https://site.example.invalid/portal/callback",
		"databaseHost":   "database.example.invalid", "databaseName": "customer_dex", "databaseUser": "dex_runtime",
		"zitadelClientID": "1234567890@fictional",
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	materialWrite(t, input, "settings.json", data)
	ca, cert, key := materialCertificate(t, "login.example.invalid", time.Now().Add(time.Hour))
	materialWrite(t, input, "service-ca.pem", ca)
	materialWrite(t, input, "tls.crt", cert)
	materialWrite(t, input, "tls.key", key)
	upstream, _, _ := materialCertificate(t, "accounts.example.invalid", time.Now().Add(time.Hour))
	materialWrite(t, input, "zitadel-ca.pem", upstream)
	for _, name := range []string{"zitadel-client-secret", "portal-client-secret"} {
		materialWrite(t, input, name, append(materialPassword(t), []byte("$LITERAL'\"")...))
	}
	return input, output
}

// TestDexServeMaterializesClosedRuntime verifies the actual files consumed by native Dex.
func TestDexServeMaterializesClosedRuntime(t *testing.T) {
	input, output := dexFixture(t)
	if err := Materialize("dex-serve", input, output); err != nil {
		t.Fatalf("valid dedicated broker input refused: %v", err)
	}
	dexAssertOutput(t, input, output)
}

// dexAssertOutput also checks the artifact emitted by the compiled entrypoint.
func dexAssertOutput(t *testing.T, input, output string) {
	t.Helper()
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("runtime output is not private")
		}
	}
	if !slices.Equal(names, []string{"config.json", "database-ca.pem", "tls.crt", "tls.key", "zitadel-ca.pem"}) {
		t.Fatalf("unexpected broker output members: %v", names)
	}
	read := func(root, name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, name := range names[1:] {
		if !bytes.Equal(read(input, name), read(output, name)) {
			t.Fatalf("incorrect copy of %s", name)
		}
	}
	var config struct {
		Issuer           string
		EnablePasswordDB bool
		Web              struct{ HTTP, HTTPS, TLSCert, TLSKey string }
		Storage          struct {
			Type   string
			Config struct {
				Host, Database, User, Password string
				Port                           int
				SSL                            struct{ Mode, CAFile string }
			}
		}
		OAuth2 struct {
			GrantTypes, ResponseTypes []string
			PKCE                      struct {
				Enforce                       bool
				CodeChallengeMethodsSupported []string
			}
		}
		Connectors []struct {
			Type, ID string
			Config   struct {
				Issuer, ClientID, ClientSecret, RedirectURI, PKCEChallenge string
				RootCAs                                                    []string
				InsecureSkipVerify                                         bool
			}
		}
		StaticClients []struct {
			ID, Secret, SecretEnv string
			Public                bool
			RedirectURIs          []string
		}
	}
	if err := json.Unmarshal(read(output, "config.json"), &config); err != nil {
		t.Fatal("broker configuration is not valid JSON")
	}
	if config.Issuer != "https://login.example.invalid" || config.EnablePasswordDB || config.Web.HTTP != "" ||
		config.Web.HTTPS != "0.0.0.0:5556" || config.Web.TLSCert != "/run/customer-dex/tls.crt" || config.Web.TLSKey != "/run/customer-dex/tls.key" {
		t.Fatal("broker listener or local-account authority differs")
	}
	db := config.Storage.Config
	if config.Storage.Type != "postgres" || db.Host != "database.example.invalid" || db.Port != 5432 ||
		db.Database != "customer_dex" || db.User != "dex_runtime" || db.Password != string(read(input, "database-password")) ||
		db.SSL.Mode != "verify-full" || db.SSL.CAFile != "/run/customer-dex/database-ca.pem" {
		t.Fatal("broker database or TLS policy differs")
	}
	if !slices.Equal(config.OAuth2.GrantTypes, []string{"authorization_code"}) || !slices.Equal(config.OAuth2.ResponseTypes, []string{"code"}) ||
		!config.OAuth2.PKCE.Enforce || !slices.Equal(config.OAuth2.PKCE.CodeChallengeMethodsSupported, []string{"S256"}) {
		t.Fatal("broker allows a different code flow")
	}
	if len(config.Connectors) != 1 || len(config.StaticClients) != 1 {
		t.Fatal("expected exactly one provider and one portal client")
	}
	connector := config.Connectors[0]
	if connector.Type != "oidc" || connector.ID != "customer-zitadel" || connector.Config.Issuer != "https://accounts.example.invalid" ||
		connector.Config.ClientID != "1234567890@fictional" || connector.Config.ClientSecret != string(read(input, "zitadel-client-secret")) ||
		connector.Config.RedirectURI != "https://login.example.invalid/callback" || connector.Config.PKCEChallenge != "S256" ||
		connector.Config.InsecureSkipVerify || !slices.Equal(connector.Config.RootCAs, []string{"/run/customer-dex/zitadel-ca.pem"}) {
		t.Fatal("broker provider, trust, callback or private client differs")
	}
	client := config.StaticClients[0]
	if client.ID != "business-portal" || client.Public || client.SecretEnv != "" || client.Secret != string(read(input, "portal-client-secret")) ||
		!slices.Equal(client.RedirectURIs, []string{"https://site.example.invalid/portal/callback"}) {
		t.Fatal("broker portal client or its independent private credential differs")
	}
}

// TestDexServeRefusesIncompleteOrSharedAuthority exercises the private producer boundary.
func TestDexServeRefusesIncompleteOrSharedAuthority(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*testing.T, string)
	}{
		{"missing-client-secret", func(t *testing.T, input string) {
			if err := os.Remove(filepath.Join(input, "portal-client-secret")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unresolved-client-secret", func(t *testing.T, input string) {
			materialWrite(t, input, "portal-client-secret", []byte("${CUSTOMER_PORTAL_CLIENT_SECRET}"))
		}},
		{"shared-client-secret", func(t *testing.T, input string) {
			secret, err := os.ReadFile(filepath.Join(input, "zitadel-client-secret"))
			if err != nil {
				t.Fatal(err)
			}
			materialWrite(t, input, "portal-client-secret", secret)
		}},
		{"wrong-database", func(t *testing.T, input string) {
			settings := bytes.ReplaceAll(materialFixtureSettings(t, input), []byte("customer_dex\""), []byte("customer_zitadel\""))
			materialWrite(t, input, "settings.json", settings)
		}},
		{"wrong-schema", func(t *testing.T, input string) {
			settings := bytes.ReplaceAll(materialFixtureSettings(t, input), []byte("customer-dex-private-v1"), []byte("customer-zitadel-private-v1"))
			materialWrite(t, input, "settings.json", settings)
		}},
		{"unsafe-upstream-trust", func(t *testing.T, input string) {
			materialWrite(t, input, "zitadel-ca.pem", []byte("not a CA"))
		}},
		{"wrong-service-name", func(t *testing.T, input string) {
			ca, cert, key := materialCertificate(t, "other.example.invalid", time.Now().Add(time.Hour))
			materialWrite(t, input, "service-ca.pem", ca)
			materialWrite(t, input, "tls.crt", cert)
			materialWrite(t, input, "tls.key", key)
		}},
		{"bootstrap-input", func(t *testing.T, input string) {
			materialWrite(t, input, "bootstrap-password", materialPassword(t))
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			input, output := dexFixture(t)
			change.apply(t, input)
			if err := Materialize("dex-serve", input, output); err != errMaterial {
				t.Fatal("broker preparation accepted or disclosed private invalid input")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("refused broker preparation created usable output")
			}
		})
	}
}
