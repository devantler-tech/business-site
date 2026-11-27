package runtimeapp

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrPortalSettings is the entire diagnostic; setting names, values and paths stay private.
var ErrPortalSettings = errors.New("portal settings projection is unavailable")

var portalSettingName = regexp.MustCompile(`^PORTAL_[A-Z][A-Z0-9_]*$`)

var environmentOnlyPortalSettings = []string{"PORTAL_CLIENT_SECRET_FILE", "PORTAL_IDENTITY_LEASE_UNTIL"}

// projectedPortalSettings lists the supported keys installed from a projection.
// Each keeps its existing environment meaning.
var projectedPortalSettings = []string{
	"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL",
	"PORTAL_CLIENT_SECRET_PROJECTION_FILE", "PORTAL_IDENTITY_PROFILE", "PORTAL_IDENTITY_FLAG",
	"PORTAL_CLIENT_PREVIEW_DATABASE_URL", "PORTAL_CLIENT_PREVIEW_DATABASE_CA",
	// Independently default-off production capabilities; never staging or work approval.
	"PORTAL_CONTACT_VERIFICATION_FLAG", "PORTAL_REQUESTS_FLAG", "PORTAL_TRIAGE_FLAG", "PORTAL_PROPOSALS_FLAG",
	"PORTAL_SUPPORT_FLAG",
	"PORTAL_PROPOSAL_SHARING_FLAG", "PORTAL_PROPOSAL_ACCEPTANCE_FLAG", "PORTAL_INVOICE_BILLING_FLAG", "PORTAL_CARD_BILLING_FLAG",
	// The optional Numina invoice path (BILLING.md); the key itself stays in its projected file.
	"PORTAL_NUMINA_TOKEN_FILE", "PORTAL_NUMINA_API_URL", "PORTAL_NUMINA_SALES_ACCOUNT", "PORTAL_NUMINA_VAT_CODE",
	// The optional Stripe card path; its key and webhook secret stay in projected files.
	"PORTAL_STRIPE_KEY_FILE", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE", "PORTAL_STRIPE_API_URL", "PORTAL_STRIPE_TAX_RATE",
	// The optional sender of the portal's own emails; its credential stays in projected files.
	"PORTAL_SMTP_USERNAME_FILE", "PORTAL_SMTP_PASSWORD_FILE", "PORTAL_SMTP_ADDR", "PORTAL_MAIL_FROM",
	// The optional upstream logout, which ends the identity provider's session too.
	"PORTAL_UPSTREAM_END_SESSION_URL", "PORTAL_UPSTREAM_CLIENT_ID",
	// The optional operators: exact customer Dex subjects.
	"PORTAL_OPERATOR_SUBJECTS",
	// The customer directory's token stays in its separate rotating projection.
	"PORTAL_ZITADEL_URL", "PORTAL_ZITADEL_ORG_ID", "PORTAL_ZITADEL_TOKEN_PROJECTION_FILE",
	// Public machine identities and exact grants; no client secret is read here.
	"PORTAL_AGENT_AUDIENCE", "PORTAL_AGENT_SUBJECTS",
}

// inheritedPortalSettings are every setting ReadConfig and the activation
// provider read. With a projection none may also come from the environment,
// so a single source decides the portal's dependencies and activation.
var inheritedPortalSettings = slices.Concat(environmentOnlyPortalSettings, projectedPortalSettings)

// ApplyPortalSettingsProjection reads one confined projected file, using the
// company-disclosure projection's ownership, mode and size checks, and installs
// its values in this process's environment. ReadConfig and the activation
// provider then read them exactly as they read inherited variables; the latter
// re-reads them on every evaluation, so the file is the process's only source.
// A file with no keys keeps the portal's dependencies absent and sign-in off.
// Restart the process to rotate the projection.
func ApplyPortalSettingsProjection(filename string) error {
	if !filepath.IsAbs(filename) || os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return ErrPortalSettings
	}
	for _, key := range inheritedPortalSettings {
		if _, inherited := os.LookupEnv(key); inherited {
			return ErrPortalSettings
		}
	}
	data, err := projectionSnapshot(filename)
	if err != nil {
		return ErrPortalSettings
	}
	settings, err := parsePortalSettings(data)
	if err != nil {
		return ErrPortalSettings
	}
	unsupported := len(settings)
	for _, key := range projectedPortalSettings {
		if value, ok := settings[key]; ok {
			if err := os.Setenv(key, value); err != nil {
				return ErrPortalSettings
			}
			unsupported--
		}
	}
	if unsupported > 0 {
		slog.Warn("ignored unsupported portal settings", "count", unsupported)
	}
	return nil
}

// parsePortalSettings accepts LF-separated KEY=value lines. Blank lines and
// lines starting with # are ignored. Only the portal namespace is accepted.
// Repeat keys, environment-only keys and malformed lines are refused.
func parsePortalSettings(data []byte) (map[string]string, error) {
	text := string(data)
	if !utf8.ValidString(text) || strings.ContainsAny(text, "\r\x00") {
		return nil, ErrPortalSettings
	}
	settings := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !portalSettingName.MatchString(key) || slices.Contains(environmentOnlyPortalSettings, key) ||
			value == "" || strings.TrimSpace(value) != value {
			return nil, ErrPortalSettings
		}
		if _, repeated := settings[key]; repeated {
			return nil, ErrPortalSettings
		}
		settings[key] = value
	}
	return settings, nil
}
