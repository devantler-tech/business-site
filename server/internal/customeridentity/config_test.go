package customeridentity

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// TestZitadelPreparation closes native enrollment and defers private bootstrap.
// A realm-path assumption or omitted registration policy must fail this contract.
func TestZitadelPreparation(t *testing.T) {
	data, err := Prepare(true, Endpoints{"https://login.example.invalid", "https://accounts.example.invalid", "https://site.example.invalid/portal/callback"})
	if err != nil {
		t.Fatalf("ZITADEL uses a root issuer, not a Keycloak realm: %v", err)
	}
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(data, &bundle); err != nil || len(bundle) != 4 {
		t.Fatalf("need native config, setup, application request and Dex documents: %v", err)
	}
	var config, setup, application map[string]any
	for key, destination := range map[string]*map[string]any{"zitadelConfig": &config, "zitadelSteps": &setup, "zitadelApplication": &application} {
		if err := json.Unmarshal(bundle[key], destination); err != nil {
			t.Fatal(err)
		}
	}
	assertJSON(t, setup, `{"FirstInstance":{"Skip":true}}`)
	defaults := nativeValue[map[string]any](t, config["DefaultInstance"])
	assertJSON(t, defaults["Restrictions"], `{"DisallowPublicOrgRegistration":true}`)
	assertJSON(t, defaults["PasswordComplexityPolicy"], `{"MinLength":14}`)
	assertJSON(t, defaults["LockoutPolicy"], `{"MaxPasswordAttempts":5,"MaxOTPAttempts":5,"ShouldShowLockoutFailure":false}`)
	policy := nativeValue[map[string]any](t, defaults["LoginPolicy"])
	for key, want := range map[string]any{"AllowRegister": false, "AllowExternalIDP": false, "ForceMFA": true, "ForceMFALocalOnly": false, "AllowDomainDiscovery": false, "IgnoreUnknownUsernames": true} {
		if policy[key] != want {
			t.Errorf("unsafe native login policy %s = %v", key, policy[key])
		}
	}
	assertJSON(t, defaults["Features"], `{"LoginV2":{"Required":false}}`)
	oidc := nativeValue[map[string]any](t, application["oidcConfiguration"])
	assertJSON(t, oidc["loginVersion"], `{"loginV1":{}}`)
	assertJSON(t, oidc["redirectUris"], `["https://login.example.invalid/callback"]`)
	assertJSON(t, oidc["grantTypes"], `["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"]`)
	if application["projectId"] != "${CUSTOMER_ZITADEL_PROJECT_ID}" || oidc["developmentMode"] != false {
		t.Fatal("generated application must require private provisioning, never development defaults")
	}
	if bytes.Contains(data, []byte(`"Human"`)) || bytes.Contains(data, []byte(`"keycloakRealm"`)) {
		t.Fatal("source preparation must not create bootstrap users or the rejected realm")
	}
}

// TestPreparationDisabled proves that unconfigured preparation stays dormant.
func TestPreparationDisabled(t *testing.T) {
	for _, inputs := range []Endpoints{{}, {DexIssuer: "http://unsafe.invalid"}} {
		got, err := Prepare(false, inputs)
		if err != nil || len(got) != 0 {
			t.Fatalf("disabled preparation must emit nothing, got %q, %v", got, err)
		}
	}
}

// TestPreparationRejectsUnsafeEndpoints retains each distinct origin/refusal case.
func TestPreparationRejectsUnsafeEndpoints(t *testing.T) {
	valid := Endpoints{"https://login.example.invalid", "https://accounts.example.invalid", "https://site.example.invalid/portal/callback"}
	// Preserve the fictional user-info input without a credential-bearing literal.
	withUserinfo := (&url.URL{Scheme: "https", User: url.UserPassword("user", "secret"), Host: "login.example.invalid"}).String()
	for _, bad := range []string{"", "http://login.example.invalid", withUserinfo, "https://login.example.invalid/?query=x", "https://login.example.invalid/#fragment", "https://login.example.invalid:443", "https://127.0.0.1", "https://localhost", "https://LOGIN.example.invalid", "https://login.example.invalid/other", "https://login.example.invalid/%2e%2e", "https://login.example.invalid?", "https://login.example.invalid."} {
		t.Run(bad, func(t *testing.T) {
			inputs := valid
			inputs.DexIssuer = bad
			got, err := Prepare(true, inputs)
			if err == nil || len(got) != 0 {
				t.Fatalf("unsafe issuer must emit no configuration: %q %v", got, err)
			}
		})
	}
	for _, inputs := range []Endpoints{
		{"https://login.example.invalid#", valid.ZitadelIssuer, valid.PortalCallback},
		{valid.DexIssuer, "https://accounts.example.invalid#", valid.PortalCallback},
		{valid.DexIssuer, valid.ZitadelIssuer, "https://site.example.invalid/portal/callback#"},
		{valid.DexIssuer, "https://accounts.example.invalid/realms/master", valid.PortalCallback},
		{valid.DexIssuer, "https://accounts.example.invalid/realms/customers", valid.PortalCallback},
		{valid.DexIssuer, valid.ZitadelIssuer, "https://site.example.invalid/portal/callback/"},
		{valid.DexIssuer, "https://login.example.invalid", valid.PortalCallback},
		{valid.DexIssuer, valid.ZitadelIssuer, "https://login.example.invalid/portal/callback"},
		{valid.DexIssuer, valid.ZitadelIssuer, "https://accounts.example.invalid/portal/callback"},
	} {
		t.Run(strings.Join([]string{inputs.DexIssuer, inputs.ZitadelIssuer, inputs.PortalCallback}, "|"), func(t *testing.T) {
			if got, err := Prepare(true, inputs); err == nil || len(got) != 0 {
				t.Fatalf("aliased or mismatched endpoints accepted: %+v", inputs)
			}
		})
	}
}

// TestPreparedNativeConfiguration checks both native policies and secret separation.
func TestPreparedNativeConfiguration(t *testing.T) {
	t.Setenv("CUSTOMER_ZITADEL_CLIENT_SECRET", "FICTIONAL_PROVIDER_SECRET_SENTINEL")
	t.Setenv("CUSTOMER_ZITADEL_CLIENT_ID", "FICTIONAL_ID_SECRET_SENTINEL")
	t.Setenv("CUSTOMER_PORTAL_CLIENT_SECRET", "FICTIONAL_PORTAL_SECRET_SENTINEL")
	data, err := Prepare(true, Endpoints{"https://login.example.invalid", "https://accounts.example.invalid", "https://site.example.invalid/portal/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("_SECRET_SENTINEL")) {
		t.Fatal("source preparation expanded a private runtime credential")
	}
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(data, &bundle); err != nil || len(bundle) != 4 {
		t.Fatalf("expected exactly four native documents: %s %v", data, err)
	}
	var provider map[string]any
	var dex map[string]any
	if err := json.Unmarshal(bundle["zitadelConfig"], &provider); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bundle["dexConfig"], &dex); err != nil {
		t.Fatal(err)
	}
	if provider["ExternalDomain"] != "accounts.example.invalid" || provider["ExternalSecure"] != true || provider["ExternalPort"] != float64(443) {
		t.Error("provider must use its exact externally verified HTTPS origin")
	}
	assertJSON(t, provider["TLS"], `{"Enabled":true,"CertPath":"/run/customer-zitadel/tls.crt","KeyPath":"/run/customer-zitadel/tls.key"}`)
	defaults := nativeValue[map[string]any](t, provider["DefaultInstance"])
	assertJSON(t, defaults["OIDCSettings"], `{"AccessTokenLifetime":"5m","IdTokenLifetime":"5m","RefreshTokenIdleExpiration":"15m","RefreshTokenExpiration":"8h"}`)
	if dex["issuer"] != "https://login.example.invalid" || dex["enablePasswordDB"] != false {
		t.Errorf("Dex must remain the sole configured customer issuer without a password DB: %v", dex)
	}
	connectors := nativeValue[[]any](t, dex["connectors"])
	if len(connectors) != 1 {
		t.Fatalf("social connectors must not be created: %v", connectors)
	}
	connector := nativeValue[map[string]any](t, connectors[0])
	if connector["id"] != "customer-zitadel" || connector["type"] != "oidc" {
		t.Errorf("unexpected customer connector: %v", connector)
	}
	config := nativeValue[map[string]any](t, connector["config"])
	assertJSON(t, config, `{"issuer":"https://accounts.example.invalid","clientID":"${CUSTOMER_ZITADEL_CLIENT_ID}","clientSecret":"${CUSTOMER_ZITADEL_CLIENT_SECRET}","redirectURI":"https://login.example.invalid/callback","scopes":["profile","email"],"pkceChallenge":"S256","getUserInfo":true,"insecureSkipEmailVerified":false,"insecureEnableGroups":false,"insecureSkipVerify":false}`)
	assertJSON(t, dex["staticClients"], `[{"id":"business-portal","name":"Devantler Tech portal","public":false,"secretEnv":"CUSTOMER_PORTAL_CLIENT_SECRET","redirectURIs":["https://site.example.invalid/portal/callback"]}]`)
	assertJSON(t, dex["oauth2"], `{"responseTypes":["code"],"grantTypes":["authorization_code"],"skipApprovalScreen":false,"pkce":{"enforce":true,"codeChallengeMethodsSupported":["S256"]}}`)
	assertJSON(t, dex["expiry"], `{"idTokens":"5m","authRequests":"5m","signingKeys":"6h"}`)
	storage := nativeValue[map[string]any](t, dex["storage"])
	storageConfig := nativeValue[map[string]any](t, storage["config"])
	if storage["type"] != "postgres" || nativeValue[map[string]any](t, storageConfig["ssl"])["mode"] != "verify-full" {
		t.Errorf("Dex requires durable, certificate-verified storage: %v", storage)
	}
	for _, unsafe := range []string{"staticPasswords", "trustedPeers", "issuerAlias", "providerDiscoveryOverrides", "insecureSkipIssuerCheck", "offline_access", "client_credentials"} {
		if bytes.Contains(data, []byte(`"`+unsafe+`"`)) {
			t.Errorf("unexpected privilege or compatibility override %s", unsafe)
		}
	}
	if strings.Contains(string(data), "PORTAL_IDENTITY_FLAG") {
		t.Error("preparation must never activate the portal")
	}
}

// nativeValue reports schema-shape failures at the assertion instead of panicking.
func nativeValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected native field type %T", value)
	}
	return result
}

// assertJSON compares native values independent of JSON map-key ordering.
func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	normalized, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, normalized) {
		t.Errorf("native payload mismatch\ngot:  %s\nwant: %s", encoded, normalized)
	}
}
