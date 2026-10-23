package customeridentity

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreparationDisabled(t *testing.T) {
	for _, inputs := range []Endpoints{{}, {DexIssuer: "http://unsafe.invalid"}} {
		got, err := Prepare(false, inputs)
		if err != nil || len(got) != 0 {
			t.Fatalf("disabled preparation must emit nothing, got %q, %v", got, err)
		}
	}
}

func TestPreparationRejectsUnsafeEndpoints(t *testing.T) {
	valid := Endpoints{"https://login.example.invalid", "https://accounts.example.invalid/realms/customers", "https://site.example.invalid/portal/callback"}
	for _, bad := range []string{"", "http://login.example.invalid", "https://user:secret@login.example.invalid", "https://login.example.invalid/?query=x", "https://login.example.invalid/#fragment", "https://login.example.invalid:443", "https://127.0.0.1", "https://localhost", "https://LOGIN.example.invalid", "https://login.example.invalid/other", "https://login.example.invalid/%2e%2e", "https://login.example.invalid?", "https://login.example.invalid."} {
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
		{"https://login.example.invalid#", valid.KeycloakIssuer, valid.PortalCallback},
		{valid.DexIssuer, "https://accounts.example.invalid/realms/customers#", valid.PortalCallback},
		{valid.DexIssuer, valid.KeycloakIssuer, "https://site.example.invalid/portal/callback#"},
		{valid.DexIssuer, "https://accounts.example.invalid/realms/master", valid.PortalCallback},
		{valid.DexIssuer, valid.KeycloakIssuer, "https://site.example.invalid/portal/callback/"},
		{valid.DexIssuer, "https://login.example.invalid/realms/customers", valid.PortalCallback},
		{valid.DexIssuer, valid.KeycloakIssuer, "https://login.example.invalid/portal/callback"},
		{valid.DexIssuer, valid.KeycloakIssuer, "https://accounts.example.invalid/portal/callback"},
	} {
		t.Run(strings.Join([]string{inputs.DexIssuer, inputs.KeycloakIssuer, inputs.PortalCallback}, "|"), func(t *testing.T) {
			if got, err := Prepare(true, inputs); err == nil || len(got) != 0 {
				t.Fatalf("aliased or mismatched endpoints accepted: %+v", inputs)
			}
		})
	}
}

func TestPreparedNativeConfiguration(t *testing.T) {
	t.Setenv("CUSTOMER_KEYCLOAK_CLIENT_SECRET", "FICTIONAL_KEYCLOAK_SECRET_SENTINEL")
	t.Setenv("CUSTOMER_PORTAL_CLIENT_SECRET", "FICTIONAL_PORTAL_SECRET_SENTINEL")
	data, err := Prepare(true, Endpoints{"https://login.example.invalid", "https://accounts.example.invalid/realms/customers", "https://site.example.invalid/portal/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("_SECRET_SENTINEL")) {
		t.Fatal("source preparation expanded a private runtime credential")
	}
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(data, &bundle); err != nil || len(bundle) != 2 {
		t.Fatalf("expected exactly the two native documents: %s %v", data, err)
	}
	var realm map[string]any
	var dex map[string]any
	if err := json.Unmarshal(bundle["keycloakRealm"], &realm); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bundle["dexConfig"], &dex); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"realm": "customers", "enabled": true, "registrationAllowed": false, "resetPasswordAllowed": true, "verifyEmail": true, "duplicateEmailsAllowed": false, "bruteForceProtected": true, "rememberMe": false, "editUsernameAllowed": false, "sslRequired": "all"} {
		if realm[key] != want {
			t.Errorf("realm %s = %v; want %v", key, realm[key], want)
		}
	}
	for _, key := range []string{"users", "identityProviders", "userFederationProviders"} {
		if got, ok := realm[key].([]any); !ok || len(got) != 0 {
			t.Errorf("realm must contain no pre-created %s", key)
		}
	}
	clients := realm["clients"].([]any)
	if len(clients) != 1 {
		t.Fatalf("expected one confidential Dex upstream client: %v", clients)
	}
	client := clients[0].(map[string]any)
	for key, want := range map[string]any{"clientId": "customer-dex", "publicClient": false, "standardFlowEnabled": true, "directAccessGrantsEnabled": false, "implicitFlowEnabled": false, "serviceAccountsEnabled": false, "fullScopeAllowed": false, "secret": "${CUSTOMER_KEYCLOAK_CLIENT_SECRET}"} {
		if client[key] != want {
			t.Errorf("Keycloak client %s = %v; want %v", key, client[key], want)
		}
	}
	assertJSON(t, client["redirectUris"], `["https://login.example.invalid/callback"]`)
	assertJSON(t, client["attributes"], `{"pkce.code.challenge.method":"S256"}`)
	required := map[string]bool{}
	for _, value := range realm["requiredActions"].([]any) {
		action := value.(map[string]any)
		if action["enabled"] != true || action["defaultAction"] != true {
			t.Errorf("required onboarding action disabled: %v", action)
		}
		required[action["providerId"].(string)] = true
	}
	for _, action := range []string{"VERIFY_EMAIL", "UPDATE_PASSWORD", "CONFIGURE_TOTP"} {
		if !required[action] {
			t.Errorf("missing required onboarding action %s", action)
		}
	}
	if realm["browserFlow"] != "customer-browser" {
		t.Error("MFA enrollment alone cannot enforce the second factor on later logins")
	}
	assertJSON(t, realm["authenticationFlows"], `[{"alias":"customer-browser","providerId":"basic-flow","topLevel":true,"builtIn":false,"authenticationExecutions":[{"authenticator":"auth-username-password-form","authenticatorFlow":false,"requirement":"REQUIRED","priority":10,"userSetupAllowed":false},{"authenticator":"auth-otp-form","authenticatorFlow":false,"requirement":"REQUIRED","priority":20,"userSetupAllowed":true}]}]`)
	if dex["issuer"] != "https://login.example.invalid" || dex["enablePasswordDB"] != false {
		t.Errorf("Dex must remain the sole configured customer issuer without a password DB: %v", dex)
	}
	connectors := dex["connectors"].([]any)
	if len(connectors) != 1 {
		t.Fatalf("social connectors must not be created: %v", connectors)
	}
	connector := connectors[0].(map[string]any)
	if connector["id"] != "customer-keycloak" || connector["type"] != "oidc" {
		t.Errorf("unexpected customer connector: %v", connector)
	}
	config := connector["config"].(map[string]any)
	assertJSON(t, config, `{"issuer":"https://accounts.example.invalid/realms/customers","clientID":"customer-dex","clientSecret":"${CUSTOMER_KEYCLOAK_CLIENT_SECRET}","redirectURI":"https://login.example.invalid/callback","scopes":["profile","email"],"pkceChallenge":"S256","insecureSkipEmailVerified":false,"insecureEnableGroups":false,"insecureSkipVerify":false}`)
	assertJSON(t, dex["staticClients"], `[{"id":"business-portal","name":"Devantler Tech portal","public":false,"secretEnv":"CUSTOMER_PORTAL_CLIENT_SECRET","redirectURIs":["https://site.example.invalid/portal/callback"]}]`)
	assertJSON(t, dex["oauth2"], `{"responseTypes":["code"],"grantTypes":["authorization_code"],"skipApprovalScreen":false,"pkce":{"enforce":true,"codeChallengeMethodsSupported":["S256"]}}`)
	assertJSON(t, dex["expiry"], `{"idTokens":"5m","authRequests":"5m","signingKeys":"6h"}`)
	storage := dex["storage"].(map[string]any)
	if storage["type"] != "postgres" || storage["config"].(map[string]any)["ssl"].(map[string]any)["mode"] != "verify-full" {
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
