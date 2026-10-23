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
	ZitadelIssuer  string
	PortalCallback string
}

var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

// Prepare emits native configuration and API request templates with explicit opt-in.
// Secret references remain unresolved; the result is not a deployment package.
func Prepare(enabled bool, inputs Endpoints) ([]byte, error) {
	if !enabled {
		return nil, nil
	}
	broker, err := endpoint(inputs.DexIssuer, "")
	if err != nil {
		return nil, errors.New("customer Dex requires a canonical HTTPS origin")
	}
	idp, err := endpoint(inputs.ZitadelIssuer, "")
	if err != nil {
		return nil, errors.New("customer ZITADEL requires a canonical HTTPS origin")
	}
	portal, err := endpoint(inputs.PortalCallback, "/portal/callback")
	if err != nil {
		return nil, errors.New("portal requires a canonical HTTPS callback")
	}
	if broker.Host == idp.Host || broker.Host == portal.Host || idp.Host == portal.Host {
		return nil, errors.New("customer broker, provider and portal require separate origins")
	}
	callback := inputs.DexIssuer + "/callback"
	provider := map[string]any{
		"ExternalDomain": idp.Host, "ExternalPort": 443, "ExternalSecure": true,
		"TLS": map[string]any{"Enabled": true, "CertPath": "/run/customer-zitadel/tls.crt", "KeyPath": "/run/customer-zitadel/tls.key"},
		"DefaultInstance": map[string]any{
			"LoginPolicy": map[string]any{
				"AllowUsernamePassword": true, "AllowRegister": false, "AllowExternalIDP": false,
				"ForceMFA": true, "ForceMFALocalOnly": false, "HidePasswordReset": false,
				"IgnoreUnknownUsernames": true, "AllowDomainDiscovery": false, "DisableLoginWithPhone": true,
				"PasswordlessType": 0, "PasswordCheckLifetime": "15m", "ExternalLoginCheckLifetime": "15m",
				"MfaInitSkipLifetime": "0s", "SecondFactorCheckLifetime": "15m", "MultiFactorCheckLifetime": "15m",
				"DefaultRedirectURI": "https://" + portal.Host + "/portal",
			},
			"Restrictions":             map[string]bool{"DisallowPublicOrgRegistration": true},
			"PasswordComplexityPolicy": map[string]int{"MinLength": 14},
			"LockoutPolicy":            map[string]any{"MaxPasswordAttempts": 5, "MaxOTPAttempts": 5, "ShouldShowLockoutFailure": false},
			"Features":                 map[string]any{"LoginV2": map[string]bool{"Required": false}},
			"OIDCSettings": map[string]string{
				"AccessTokenLifetime": "5m", "IdTokenLifetime": "5m", "RefreshTokenIdleExpiration": "15m", "RefreshTokenExpiration": "8h",
			},
		},
	}
	// Native setup otherwise merges a well-known default human administrator.
	// Source preparation cannot supply private bootstrap authority or create an instance.
	steps := map[string]any{"FirstInstance": map[string]bool{"Skip": true}}
	application := map[string]any{
		"projectId": "${CUSTOMER_ZITADEL_PROJECT_ID}", "name": "Customer Dex",
		"oidcConfiguration": map[string]any{
			"redirectUris": []string{callback}, "responseTypes": []string{"OIDC_RESPONSE_TYPE_CODE"},
			"grantTypes": []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}, "applicationType": "OIDC_APP_TYPE_WEB",
			"authMethodType": "OIDC_AUTH_METHOD_TYPE_BASIC", "version": "OIDC_VERSION_1_0", "developmentMode": false,
			"accessTokenRoleAssertion": false, "idTokenRoleAssertion": false, "idTokenUserinfoAssertion": false,
			"additionalOrigins": []string{}, "postLogoutRedirectUris": []string{},
			"loginVersion": map[string]any{"loginV1": map[string]any{}},
		},
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
			"type": "oidc", "id": "customer-zitadel", "name": "Devantler Tech account",
			"config": map[string]any{
				"issuer": inputs.ZitadelIssuer, "clientID": "${CUSTOMER_ZITADEL_CLIENT_ID}",
				"clientSecret": "${CUSTOMER_ZITADEL_CLIENT_SECRET}", "redirectURI": callback,
				"scopes": []string{"profile", "email"}, "pkceChallenge": "S256", "getUserInfo": true,
				"insecureSkipEmailVerified": false, "insecureEnableGroups": false, "insecureSkipVerify": false,
			},
		}},
		"staticClients": []any{map[string]any{
			"id": "business-portal", "name": "Devantler Tech portal", "public": false,
			"secretEnv": "CUSTOMER_PORTAL_CLIENT_SECRET", "redirectURIs": []string{inputs.PortalCallback},
		}},
		"logger": map[string]any{"level": "warn", "format": "json", "excludeFields": []string{"email", "username", "preferred_username", "groups"}},
	}
	return json.MarshalIndent(map[string]any{"zitadelConfig": provider, "zitadelSteps": steps, "zitadelApplication": application, "dexConfig": dex}, "", "  ")
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
