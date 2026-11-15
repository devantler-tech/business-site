package runtimeapp

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrPortalSettings is the entire diagnostic; setting names, values and paths stay private.
var ErrPortalSettings = errors.New("portal settings projection is unavailable")

// projectedPortalSettings are the only keys a settings projection may supply.
// Each keeps its existing environment meaning. The staging lease and the
// owner-only secret file remain environment-only inputs and are refused here.
var projectedPortalSettings = []string{
	"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL",
	"PORTAL_CLIENT_SECRET_PROJECTION_FILE", "PORTAL_IDENTITY_PROFILE", "PORTAL_IDENTITY_FLAG",
	// The optional Numina invoice path (BILLING.md); the key itself stays in its projected file.
	"PORTAL_NUMINA_TOKEN_FILE", "PORTAL_NUMINA_API_URL", "PORTAL_NUMINA_SALES_ACCOUNT", "PORTAL_NUMINA_VAT_CODE",
	// The optional Stripe card path; its key and webhook secret stay in projected files.
	"PORTAL_STRIPE_KEY_FILE", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE", "PORTAL_STRIPE_API_URL", "PORTAL_STRIPE_TAX_RATE",
	// The optional sender of the portal's own emails; its credential stays in projected files.
	"PORTAL_SMTP_USERNAME_FILE", "PORTAL_SMTP_PASSWORD_FILE", "PORTAL_SMTP_ADDR", "PORTAL_MAIL_FROM",
}

// inheritedPortalSettings are every setting ReadConfig and the activation
// provider read. With a projection none may also come from the environment,
// so a single source decides the portal's dependencies and activation.
var inheritedPortalSettings = append([]string{"PORTAL_CLIENT_SECRET_FILE", "PORTAL_IDENTITY_LEASE_UNTIL"}, projectedPortalSettings...)

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
	for _, key := range projectedPortalSettings {
		if value, ok := settings[key]; ok {
			if err := os.Setenv(key, value); err != nil {
				return ErrPortalSettings
			}
		}
	}
	return nil
}

// parsePortalSettings accepts LF-separated KEY=value lines. Blank lines and
// lines starting with # are ignored. Unknown or repeated keys, empty values,
// surrounding whitespace, carriage returns, NUL and invalid UTF-8 are refused.
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
		if !ok || !slices.Contains(projectedPortalSettings, key) || value == "" || strings.TrimSpace(value) != value {
			return nil, ErrPortalSettings
		}
		if _, repeated := settings[key]; repeated {
			return nil, ErrPortalSettings
		}
		settings[key] = value
	}
	return settings, nil
}
