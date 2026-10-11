// Package runtimeapp wires the existing identity boundary without activating it.
package runtimeapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
)

var ErrConfig = errors.New("portal configuration is invalid")

type Config struct{ database, issuer, client, callback, secretFile string }

// A root-confined open accepts projected secrets within their mount, but not
// escapes. Metadata and bounded bytes come from the same open descriptor.
func readSecret(filename string) (string, error) {
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return "", ErrConfig
	}
	defer root.Close()
	// The supported runtime targets are Unix services. Nonblocking open avoids
	// waiting on a FIFO before the same-descriptor regular-file check.
	f, err := root.OpenFile(filepath.Base(filename), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", ErrConfig
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", ErrConfig
	}
	secret, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(secret) == 0 || len(secret) > 16384 {
		return "", ErrConfig
	}
	return string(secret), nil
}

// ReadConfig accepts either no identity dependencies or a complete validated
// set. It parses no activation flag; the real OpenFeature SDK remains the gate.
func ReadConfig(getenv func(string) string) (Config, error) {
	c := Config{getenv("PORTAL_DATABASE_URL"), getenv("PORTAL_OIDC_ISSUER"), getenv("PORTAL_OIDC_CLIENT_ID"), getenv("PORTAL_CALLBACK_URL"), getenv("PORTAL_CLIENT_SECRET_FILE")}
	count := 0
	for _, value := range []string{c.database, c.issuer, c.client, c.callback, c.secretFile} {
		if value != "" {
			count++
		}
	}
	if count == 0 {
		return c, nil
	}
	if count != 5 || !filepath.IsAbs(c.secretFile) {
		return Config{}, ErrConfig
	}
	issuer, err := url.Parse(c.issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return Config{}, ErrConfig
	}
	callback, err := url.Parse(c.callback)
	if err != nil || callback.Scheme != "https" || callback.Host == "" || callback.User != nil || callback.Path != "/portal/callback" || callback.RawPath != "" || callback.RawQuery != "" || callback.Fragment != "" {
		return Config{}, ErrConfig
	}
	db, err := pgxpool.ParseConfig(c.database)
	if err != nil || db.ConnConfig.TLSConfig == nil || db.ConnConfig.TLSConfig.InsecureSkipVerify || len(db.ConnConfig.Fallbacks) != 0 {
		return Config{}, ErrConfig
	}
	return c, nil
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
	secret, err := readSecret(c.secretFile)
	if err != nil {
		return nil, closed, ErrConfig
	}
	cfg, err := pgxpool.ParseConfig(c.database)
	if err != nil {
		return nil, closed, ErrConfig
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// Application SQL is intentionally scoped to the migrated public schema.
	cfg.ConnConfig.RuntimeParams["search_path"] = "public"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "1000"
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startup, cfg)
	if err != nil {
		return nil, closed, ErrConfig
	}
	fail := func() (http.Handler, func(), error) { pool.Close(); return nil, closed, ErrConfig }
	if err := identity.CheckRuntimeRole(startup, pool); err != nil {
		return fail()
	}
	broker, err := identity.NewBroker(startup, c.issuer, c.client)
	if err != nil {
		return fail()
	}
	handler, err := identity.New(pool, broker, flags).Browser(c.callback, secret)
	if err != nil {
		return fail()
	}
	return handler, pool.Close, nil
}
