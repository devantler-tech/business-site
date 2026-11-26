// Package runtimeapp wires the existing identity boundary without activating it.
package runtimeapp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/devantler-tech/business-site/server/internal/activation"
	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/devantler-tech/business-site/server/internal/webapp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// ErrConfig intentionally omits private dependency details from startup errors.
var ErrConfig = errors.New("portal configuration is invalid")

// Config holds the admitted private dependencies and their runtime boundary.
type Config struct {
	database, issuer, client, callback, secretFile string
	secretProjected                                bool
	preview                                        bool
	previewDatabase, previewDatabaseCA             string
	billing                                        billingConfig
	cards                                          cardConfig
	mail                                           mailConfig
	// endSession and endSessionClient are the optional upstream logout; both or neither.
	endSession, endSessionClient string
	// operators are the optional exact customer Dex operator subjects.
	operators string
	customers customerDirectoryConfig
	// Machine policy contains public identities and action grants, never credentials.
	agentAudience, agentSubjects string
	// publicSite is the public website a portal on its own host links back to.
	publicSite string
	orderTerms [2]string
}

// WithUpstreamLogoutDefaults fills a complete portal's missing logout pair.
// A complete platform projection supplies both values during the transition.
func (c Config) WithUpstreamLogoutDefaults(endpoint, client string) (Config, error) {
	if err := identity.ValidateUpstreamLogout(endpoint, client); err != nil {
		return Config{}, ErrConfig
	}
	if c.issuer != "" && c.endSession == "" {
		c.endSession, c.endSessionClient = endpoint, client
	}
	return c, nil
}

// billingConfig is the optional Numina invoice path (ADR 0030). Its own
// false-default gate still decides whether anything is invoiced.
type billingConfig struct {
	tokenFile, baseURL, vatCode string
	salesAccount                int
}

// IdentityProvider keeps ordinary startup closed. An explicit profile requires
// complete private dependencies admitted for that profile.
func IdentityProvider(ctx context.Context, c Config) (openfeature.FeatureProvider, error) {
	profile := os.Getenv("PORTAL_IDENTITY_PROFILE")
	if c.preview != (profile == "preview") {
		return nil, activation.ErrInvalid
	}
	if profile == "" && os.Getenv("PORTAL_IDENTITY_FLAG") == "" && os.Getenv("PORTAL_IDENTITY_LEASE_UNTIL") == "" {
		return openfeature.NoopProvider{}, nil
	}
	if c == (Config{}) {
		return nil, activation.ErrInvalid
	}
	switch profile {
	case "":
		return activation.New(ctx)
	case "production":
		return activation.NewProduction(ctx)
	case "preview":
		return activation.NewPreview(ctx)
	default:
		return nil, activation.ErrInvalid
	}
}

// readSecret accepts root-confined projected secrets within their mount, but not
// escapes. Metadata and bounded bytes come from the same open descriptor.
func readSecret(filename string) (string, error) {
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return "", ErrConfig
	}
	defer func() {
		// Read-only descriptor cleanup cannot change the validated secret bytes.
		_ = root.Close()
	}()
	// The supported runtime targets are Unix services. Nonblocking open avoids
	// waiting on a FIFO before the same-descriptor regular-file check.
	f, err := root.OpenFile(filepath.Base(filename), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", ErrConfig
	}
	defer func() {
		// No writes need flushing; preserve the read/validation error on failure.
		_ = f.Close()
	}()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", ErrConfig
	}
	secret, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(secret) > 16384 {
		return "", ErrConfig
	}
	value := trimInputLineEnding(string(secret))
	if value == "" {
		return "", ErrConfig
	}
	return value, nil
}

// trimInputLineEnding removes only one LF or CRLF, preserving all other bytes.
func trimInputLineEnding(value string) string {
	if strings.HasSuffix(value, "\r\n") {
		return strings.TrimSuffix(value, "\r\n")
	}
	return strings.TrimSuffix(value, "\n")
}

// ReadCompanyDisclosure uses the same bounded private projection boundary as
// identity configuration, without activating or connecting portal dependencies.
func ReadCompanyDisclosure(filename string) (string, error) {
	if !filepath.IsAbs(filename) {
		return "", errors.New("company disclosure is unavailable")
	}
	value, err := readSecret(filename)
	if err != nil {
		return "", errors.New("company disclosure is unavailable")
	}
	return value, nil
}

// ReadConfig accepts either no identity dependencies or a complete validated
// set. It parses no activation flag; the real OpenFeature SDK remains the gate.
func ReadConfig(getenv func(string) string) (Config, error) {
	c := Config{database: getenv("PORTAL_DATABASE_URL"), issuer: getenv("PORTAL_OIDC_ISSUER"), client: getenv("PORTAL_OIDC_CLIENT_ID"), callback: getenv("PORTAL_CALLBACK_URL"), secretFile: getenv("PORTAL_CLIENT_SECRET_FILE")}
	profile := getenv("PORTAL_IDENTITY_PROFILE")
	c.preview = profile == "preview"
	c.previewDatabase = getenv("PORTAL_CLIENT_PREVIEW_DATABASE_URL")
	c.previewDatabaseCA = getenv("PORTAL_CLIENT_PREVIEW_DATABASE_CA")
	if (c.previewDatabase == "") != (c.previewDatabaseCA == "") {
		return Config{}, ErrConfig
	}
	if projected := getenv("PORTAL_CLIENT_SECRET_PROJECTION_FILE"); projected != "" {
		if c.secretFile != "" {
			return Config{}, ErrConfig
		}
		c.secretFile, c.secretProjected = projected, true
	}
	count := 0
	for _, value := range []string{c.database, c.issuer, c.client, c.callback, c.secretFile} {
		if value != "" {
			count++
		}
	}
	billing, err := readBillingConfig(getenv)
	if err != nil || (count == 0 && billing != (billingConfig{})) {
		return Config{}, ErrConfig
	}
	cards, err := readCardConfig(getenv)
	if err != nil || (count == 0 && cards != (cardConfig{})) {
		return Config{}, ErrConfig
	}
	mailing, err := readMailConfig(getenv)
	if err != nil || (count == 0 && mailing != (mailConfig{})) {
		return Config{}, ErrConfig
	}
	endSession, endSessionClient := getenv("PORTAL_UPSTREAM_END_SESSION_URL"), getenv("PORTAL_UPSTREAM_CLIENT_ID")
	if (endSession == "") != (endSessionClient == "") || (count == 0 && endSession != "") {
		return Config{}, ErrConfig
	}
	operators := getenv("PORTAL_OPERATOR_SUBJECTS")
	if count == 0 && operators != "" {
		return Config{}, ErrConfig
	}
	customers, err := readCustomerDirectoryConfig(getenv)
	if err != nil || (count == 0 && customers != (customerDirectoryConfig{})) {
		return Config{}, ErrConfig
	}
	agentAudience, agentSubjects := getenv("PORTAL_AGENT_AUDIENCE"), getenv("PORTAL_AGENT_SUBJECTS")
	if identity.ValidateAgents(c.issuer, c.client, agentAudience, agentSubjects) != nil {
		return Config{}, ErrConfig
	}
	if count == 0 {
		if c.preview || c.previewDatabase != "" {
			return Config{}, ErrConfig
		}
		return c, nil
	}
	if count != 5 || !filepath.IsAbs(c.secretFile) {
		return Config{}, ErrConfig
	}
	c.billing, c.cards, c.mail = billing, cards, mailing
	c.endSession, c.endSessionClient = endSession, endSessionClient
	c.operators = operators
	c.customers = customers
	c.agentAudience, c.agentSubjects = agentAudience, agentSubjects
	issuer, err := url.Parse(c.issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return Config{}, ErrConfig
	}
	callback, err := url.Parse(c.callback)
	if err != nil || callback.Scheme != "https" || callback.Host == "" || callback.User != nil || (callback.Path != "/portal/callback" && callback.Path != "/callback") || callback.RawPath != "" || callback.RawQuery != "" || callback.Fragment != "" {
		return Config{}, ErrConfig
	}
	db, err := pgxpool.ParseConfig(c.database)
	if err != nil || db.ConnConfig.TLSConfig == nil || db.ConnConfig.TLSConfig.InsecureSkipVerify || len(db.ConnConfig.Fallbacks) != 0 {
		return Config{}, ErrConfig
	}
	if c.preview && !c.isolatedPreview(db.ConnConfig) {
		return Config{}, ErrConfig
	}
	if c.previewDatabase != "" && (profile != "production" || !c.isolatedUnifiedPreview(db.ConnConfig)) {
		return Config{}, ErrConfig
	}
	return c, nil
}

// readIdentitySecret selects only the explicitly configured transport and hides input diagnostics.
func readIdentitySecret(c Config) (string, error) {
	if !c.secretProjected {
		return readSecret(c.secretFile)
	}
	if !filepath.IsAbs(c.secretFile) || os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return "", ErrConfig
	}
	data, err := projectionBytes(c.secretFile)
	if err != nil {
		return "", ErrConfig
	}
	value := trimInputLineEnding(string(data))
	if value == "" {
		return "", ErrConfig
	}
	return value, nil
}

// Portal connects only when all dependencies were explicitly supplied. It never
// migrates, enrolls customers, or enables the injected provider. Errors are generic.
func Portal(ctx context.Context, c Config, flags *openfeature.Client) (http.Handler, func(), error) {
	closed := func() {}
	if c == (Config{}) {
		return nil, closed, nil
	}
	if flags == nil {
		return nil, closed, ErrConfig
	}
	secret, err := readIdentitySecret(c)
	if err != nil {
		return nil, closed, ErrConfig
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, broker, err := connectPortal(startup, c)
	if err != nil {
		return nil, closed, ErrConfig
	}
	foundation := identity.New(pool, broker, flags)
	if c.orderTerms != [2]string{} {
		if err := foundation.LinkOrderTerms(c.orderTerms[0], c.orderTerms[1]); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	}
	if c.publicSite != "" {
		if err := foundation.LinkPublicSite(c.publicSite); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	}
	if c.endSession != "" {
		if err := foundation.LinkUpstreamLogout(c.endSession, c.endSessionClient); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	}
	if c.preview {
		if err := foundation.LinkPreviewOperator(c.operators); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	} else if c.operators != "" {
		if err := foundation.LinkOperators(strings.Split(c.operators, ",")); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	}
	if err := linkCustomerDirectory(foundation, c.customers); err != nil {
		pool.Close()
		return nil, closed, ErrConfig
	}
	if c.agentAudience != "" {
		if err := foundation.LinkAgents(startup, c.agentAudience, c.agentSubjects); err != nil {
			pool.Close()
			return nil, closed, ErrConfig
		}
	}
	if err := enableCardBilling(foundation, c.cards); err != nil {
		pool.Close()
		return nil, closed, ErrConfig
	}
	stopPreview := func() {}
	if c.previewDatabase != "" {
		foundation.EnableClientPreviewSessions()
		stopPreview = linkUnifiedPreview(ctx, startup, foundation, c)
	}
	serving := false
	defer func() {
		if !serving {
			stopPreview()
		}
	}()
	handler, err := foundation.Browser(c.callback, secret)
	if err != nil {
		pool.Close()
		return nil, closed, ErrConfig
	}
	stopBilling, err := startInvoiceBilling(foundation, c.billing)
	if err != nil {
		pool.Close()
		return nil, closed, ErrConfig
	}
	stopMail, err := startMail(foundation, c.mail)
	if err != nil {
		stopBilling()
		pool.Close()
		return nil, closed, ErrConfig
	}
	serving = true
	return handler, func() { stopMail(); stopBilling(); stopPreview(); pool.Close() }, nil
}

func linkUnifiedPreview(ctx, startup context.Context, foundation *identity.Foundation, c Config) func() {
	closed := func() {}
	cfg, err := c.unifiedPreviewStoreConfig()
	if err != nil {
		slog.Warn("client preview storage configuration is unavailable")
		return closed
	}
	pool, err := connectPortalStore(startup, cfg, true)
	if err != nil {
		slog.Warn("client preview storage is unavailable")
		return closed
	}
	preview := isolated.NewAPI()
	settings := make(map[string]memprovider.InMemoryFlag)
	for _, key := range []string{
		"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance",
	} {
		settings[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "enabled", Variants: map[string]any{"enabled": true}}
	}
	shutdown := func() {
		_ = preview.Shutdown(context.WithoutCancel(ctx))
		pool.Close()
	}
	if err := preview.SetProviderAndWait(startup, memprovider.NewInMemoryProvider(settings)); err != nil {
		shutdown()
		slog.Warn("client preview capabilities are unavailable")
		return closed
	}
	if err := foundation.LinkClientPreview(pool, preview.NewClient()); err != nil {
		shutdown()
		slog.Warn("client preview could not be linked")
		return closed
	}
	return shutdown
}

// connectPortal opens the bounded runtime pool, then checks its restricted login
// and grants before discovering the broker. Errors are generic; the caller owns
// the returned pool.
func connectPortal(startup context.Context, c Config) (*pgxpool.Pool, *identity.Broker, error) {
	cfg, err := pgxpool.ParseConfig(c.database)
	if err != nil {
		return nil, nil, ErrConfig
	}
	pool, err := connectPortalStore(startup, cfg, c.preview)
	if err != nil {
		return nil, nil, ErrConfig
	}
	broker, err := identity.NewBroker(startup, c.issuer, c.client)
	if err != nil {
		pool.Close()
		return nil, nil, ErrConfig
	}
	return pool, broker, nil
}

func connectPortalStore(startup context.Context, cfg *pgxpool.Config, preview bool) (*pgxpool.Pool, error) {
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// Application SQL is intentionally scoped to the migrated public schema.
	cfg.ConnConfig.RuntimeParams["search_path"] = "public"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "1000"
	pool, err := pgxpool.NewWithConfig(startup, cfg)
	if err != nil {
		return nil, ErrConfig
	}
	fail := func() (*pgxpool.Pool, error) { pool.Close(); return nil, ErrConfig }
	// Assess the actual configured login, not a restricted role adopted by a
	// broader credential. Both identities must agree before checking its grants.
	var loginMatches bool
	if err := pool.QueryRow(startup, "SELECT current_user::text = $1 AND session_user::text = $1", cfg.ConnConfig.User).Scan(&loginMatches); err != nil || !loginMatches {
		return fail()
	}
	if err := identity.CheckRuntimeRole(startup, pool); err != nil {
		return fail()
	}
	// Serve only once this release's migration job has migrated the store.
	if err := identity.CheckSchemaCurrent(startup, pool); err != nil {
		return fail()
	}
	if err := identity.CheckPreviewReceiptRole(startup, pool, preview); err != nil {
		return fail()
	}
	return pool, nil
}

// FitsPortalHost reports whether the configured portal matches the runtime's host
// routing. With its own host the portal lives at that host's root, so its callback
// must be exactly https://<host>/callback; sharing the public host, it lives under
// /portal and its callback path is /portal/callback. No configured portal fits both.
func (c Config) FitsPortalHost(host string) bool {
	if c == (Config{}) {
		return true
	}
	callback, err := url.Parse(c.callback)
	if err != nil {
		return false
	}
	if c.preview {
		return host == previewPortalHost && callback.Host == previewPortalHost && callback.Path == "/callback"
	}
	if isPreviewHost(host) || isPreviewHost(callback.Hostname()) {
		return false
	}
	if host == "" {
		return callback.Path == "/portal/callback"
	}
	return callback.Host == host && callback.Path == "/callback"
}

// WithPublicHost records the public website a portal on its own host links back
// to. It leaves an unconfigured portal unconfigured, and has no effect without a host.
func (c Config) WithPublicHost(host string) Config {
	if c == (Config{}) || host == "" {
		return c
	}
	c.publicSite = "https://" + host
	return c
}

// WithOrderTerms binds orders to the admitted serving artifact, after its private
// company projection. Missing documents close ordering, independently of sign-in.
func (c Config) WithOrderTerms(artifact *webapp.Artifact) Config {
	if c == (Config{}) {
		return c
	}
	c.orderTerms = [2]string{}
	if terms, err := artifact.OrderTerms(); err == nil {
		c.orderTerms = terms
	}
	return c
}
