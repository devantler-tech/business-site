// Package customeridentity prepares dormant native customer identity configuration.
// It neither provisions users nor deploys or activates an identity service.
package customeridentity

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Endpoints are the three separately approved public HTTPS identity destinations.
type Endpoints struct {
	DexIssuer      string
	KeycloakIssuer string
	PortalCallback string
}

var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

// Prepare emits the two native JSON documents only with explicit source opt-in.
// Secret references remain unresolved; the result is not a deployment package.
func Prepare(enabled bool, inputs Endpoints) ([]byte, error) {
	if !enabled {
		return nil, nil
	}
	broker, err := endpoint(inputs.DexIssuer, "")
	if err != nil {
		return nil, errors.New("customer Dex requires a canonical HTTPS origin")
	}
	idp, err := endpoint(inputs.KeycloakIssuer, "/realms/customers")
	if err != nil {
		return nil, errors.New("customer Keycloak requires a canonical HTTPS customers realm")
	}
	portal, err := endpoint(inputs.PortalCallback, "/portal/callback")
	if err != nil {
		return nil, errors.New("portal requires a canonical HTTPS callback")
	}
	if broker.Host == idp.Host || broker.Host == portal.Host || idp.Host == portal.Host {
		return nil, errors.New("customer broker, provider and portal require separate origins")
	}
	callback := inputs.DexIssuer + "/callback"
	realm := map[string]any{
		"realm": "customers", "displayName": "Devantler Tech", "enabled": true,
		"sslRequired": "all", "registrationAllowed": false, "registrationEmailAsUsername": false,
		"loginWithEmailAllowed": true, "duplicateEmailsAllowed": false,
		"resetPasswordAllowed": true, "editUsernameAllowed": false,
		"verifyEmail": true, "rememberMe": false, "bruteForceProtected": true,
		"accessTokenLifespan": 300, "ssoSessionIdleTimeout": 900, "ssoSessionMaxLifespan": 28800,
		"actionTokenGeneratedByAdminLifespan": 900, "actionTokenGeneratedByUserLifespan": 300,
		"passwordPolicy":              "length(14) and notUsername and notEmail",
		"internationalizationEnabled": true, "supportedLocales": []string{"en", "da"}, "defaultLocale": "en",
		"users": []any{}, "identityProviders": []any{}, "userFederationProviders": []any{},
		"requiredActions": []any{
			requiredAction("VERIFY_EMAIL", "Verify email", 10),
			requiredAction("UPDATE_PASSWORD", "Set password", 20),
			requiredAction("CONFIGURE_TOTP", "Configure authenticator", 30),
		},
		"browserFlow": "customer-browser",
		"authenticationFlows": []any{map[string]any{
			"alias": "customer-browser", "providerId": "basic-flow", "topLevel": true, "builtIn": false,
			"authenticationExecutions": []any{
				map[string]any{"authenticator": "auth-username-password-form", "authenticatorFlow": false, "requirement": "REQUIRED", "priority": 10, "userSetupAllowed": false},
				map[string]any{"authenticator": "auth-otp-form", "authenticatorFlow": false, "requirement": "REQUIRED", "priority": 20, "userSetupAllowed": true},
			},
		}},
		"clients": []any{map[string]any{
			"clientId": "customer-dex", "name": "Customer Dex", "enabled": true,
			"protocol": "openid-connect", "clientAuthenticatorType": "client-secret",
			"secret": "${CUSTOMER_KEYCLOAK_CLIENT_SECRET}", "publicClient": false,
			"standardFlowEnabled": true, "directAccessGrantsEnabled": false,
			"implicitFlowEnabled": false, "serviceAccountsEnabled": false, "fullScopeAllowed": false,
			"redirectUris": []string{callback}, "webOrigins": []string{},
			"defaultClientScopes": []string{"profile", "email"}, "optionalClientScopes": []string{},
			"attributes": map[string]string{"pkce.code.challenge.method": "S256"},
		}},
	}
	dex := map[string]any{
		"issuer": inputs.DexIssuer, "enablePasswordDB": false,
		"expiry": map[string]string{"idTokens": "5m", "authRequests": "5m", "signingKeys": "6h"},
		"web":    map[string]string{"https": "0.0.0.0:5556", "tlsCert": "/run/customer-dex/tls.crt", "tlsKey": "/run/customer-dex/tls.key"},
		"storage": map[string]any{"type": "postgres", "config": map[string]any{
			"host": "${CUSTOMER_DEX_DB_HOST}", "port": 5432, "database": "customer_dex",
			"user": "${CUSTOMER_DEX_DB_USER}", "password": "${CUSTOMER_DEX_DB_PASSWORD}",
			"ssl": map[string]string{"mode": "verify-full", "caFile": "/run/customer-dex/database-ca.pem"},
		}},
		"oauth2": map[string]any{
			"responseTypes": []string{"code"}, "grantTypes": []string{"authorization_code"}, "skipApprovalScreen": false,
			"pkce": map[string]any{"enforce": true, "codeChallengeMethodsSupported": []string{"S256"}},
		},
		"connectors": []any{map[string]any{
			"type": "oidc", "id": "customer-keycloak", "name": "Devantler Tech account",
			"config": map[string]any{
				"issuer": inputs.KeycloakIssuer, "clientID": "customer-dex",
				"clientSecret": "${CUSTOMER_KEYCLOAK_CLIENT_SECRET}", "redirectURI": callback,
				"scopes": []string{"profile", "email"}, "pkceChallenge": "S256",
				"insecureSkipEmailVerified": false, "insecureEnableGroups": false, "insecureSkipVerify": false,
			},
		}},
		"staticClients": []any{map[string]any{
			"id": "business-portal", "name": "Devantler Tech portal", "public": false,
			"secretEnv": "CUSTOMER_PORTAL_CLIENT_SECRET", "redirectURIs": []string{inputs.PortalCallback},
		}},
		"logger": map[string]any{"level": "warn", "format": "json", "excludeFields": []string{"email", "username", "preferred_username", "groups"}},
	}
	return json.MarshalIndent(map[string]any{"keycloakRealm": realm, "dexConfig": dex}, "", "  ")
}

// endpoint admits only the exact HTTPS spelling expected by one identity hop.
func endpoint(raw, path string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host != u.Hostname() ||
		u.Path != path || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.String() != raw ||
		len(u.Host) > 253 || u.Host != strings.ToLower(u.Host) || !dnsName.MatchString(u.Host) ||
		net.ParseIP(u.Host) != nil || strings.HasSuffix(u.Host, ".localhost") {
		return nil, errors.New("invalid identity endpoint")
	}
	for _, label := range strings.Split(u.Host, ".") {
		if len(label) > 63 {
			return nil, errors.New("invalid identity endpoint")
		}
	}
	return u, nil
}

// requiredAction enables an onboarding action for every newly provisioned account.
func requiredAction(id, name string, priority int) map[string]any {
	return map[string]any{"alias": id, "providerId": id, "name": name, "enabled": true, "defaultAction": true, "priority": priority, "config": map[string]string{}}
}
