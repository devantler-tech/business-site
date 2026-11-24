package runtimeapp

import (
	"regexp"
	"strings"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

const previewPortalHost = "dev.devantler.tech"

// Preview PostgreSQL must resolve to a service in the isolated dev namespace.
var previewDatabaseHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.business-site-dev\.svc\.cluster\.local$`)

func isPreviewHost(host string) bool {
	return strings.EqualFold(strings.TrimSuffix(host, "."), previewPortalHost)
}

// isolatedPreview admits the dedicated runtime projected settings. It checks the
// effective PostgreSQL destination after URI parameters have been parsed.
func (c Config) isolatedPreview(db *pgx.ConnConfig) bool {
	if !previewDatabaseHost.MatchString(db.Host) || db.Port != 5432 ||
		db.Database != "portal_preview" || db.User != "portal_preview_web" ||
		db.TLSConfig.ServerName != db.Host {
		return false
	}
	return c.issuer == "https://login.devantler.tech" &&
		c.client == "business-site-preview" &&
		c.callback == "https://"+previewPortalHost+"/callback" &&
		c.secretProjected &&
		customeridentity.DexConnector(c.operators) == customeridentity.ZitadelConnectorID &&
		c.billing == (billingConfig{}) && c.cards == (cardConfig{}) && c.mail == (mailConfig{}) &&
		c.customers == (customerDirectoryConfig{}) && c.agentAudience == "" && c.endSession == ""
}
