package runtimeapp

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"regexp"
	"strings"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	return fitsPreviewDatabase(db) && c.issuer == "https://login.devantler.tech" &&
		c.client == "business-site-preview" &&
		c.callback == "https://"+previewPortalHost+"/callback" &&
		c.secretProjected &&
		customeridentity.DexConnector(c.operators) == customeridentity.ZitadelConnectorID &&
		c.billing == (billingConfig{}) && c.cards == (cardConfig{}) && c.mail == (mailConfig{}) &&
		c.customers == (customerDirectoryConfig{}) && c.agentAudience == "" && c.endSession == ""
}

func fitsPreviewDatabase(db *pgx.ConnConfig) bool {
	return previewDatabaseHost.MatchString(db.Host) && db.Port == 5432 &&
		db.Database == "portal_preview" && db.User == "portal_preview_web" &&
		db.TLSConfig != nil && !db.TLSConfig.InsecureSkipVerify && db.TLSConfig.ServerName == db.Host &&
		len(db.Fallbacks) == 0
}

// The ordinary portal keeps its existing sign-in and database. Only the
// explicitly configured preview store can contain fictional customer records.
func (c Config) isolatedUnifiedPreview(primary *pgx.ConnConfig) bool {
	if c.callback != "https://portal.devantler.tech/callback" || c.issuer != "https://login.devantler.tech" ||
		c.client != "business-portal" || !c.secretProjected || c.operators == "" ||
		previewDatabaseHost.MatchString(primary.Host) || primary.Database == "portal_preview" || primary.User == "portal_preview_web" {
		return false
	}
	_, err := c.unifiedPreviewStoreConfig()
	return err == nil
}

func (c Config) unifiedPreviewStoreConfig() (*pgxpool.Config, error) {
	dsn, err := url.Parse(c.previewDatabase)
	if err != nil || dsn.Scheme != "postgres" || dsn.Fragment != "" || dsn.RawQuery != "sslmode=verify-full" {
		return nil, ErrConfig
	}
	cfg, err := pgxpool.ParseConfig(c.previewDatabase)
	if err != nil || !fitsPreviewDatabase(cfg.ConnConfig) {
		return nil, ErrConfig
	}
	// The preview database has its own trust root. It must never become a
	// trust anchor for the ordinary customer database.
	encoded, err := base64.StdEncoding.Strict().DecodeString(c.previewDatabaseCA)
	if err != nil || !bytes.HasPrefix(encoded, []byte("-----BEGIN CERTIFICATE-----")) {
		return nil, ErrConfig
	}
	block, remaining := pem.Decode(encoded)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(remaining)) != 0 {
		return nil, ErrConfig
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !ca.IsCA {
		return nil, ErrConfig
	}
	root := x509.NewCertPool()
	root.AddCert(ca)
	cfg.ConnConfig.TLSConfig.RootCAs = root
	return cfg, nil
}
