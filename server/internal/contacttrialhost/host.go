package contacttrialhost

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/devantler-tech/business-site/server/internal/contactrenderer"
	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// Execute is an opt-in developer command. Setup arrives only over a private pipe;
// descriptor 3 is a duplicate of the controller's retained TCP listener. The
// ten-minute lease is a process bound, not a production activation mechanism.
func Execute(ctx context.Context, args []string, input *os.File, output, diagnostics io.Writer) int {
	f := flag.NewFlagSet("contact-trial-host", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	enabled := f.Bool("private-contact-trial", false, "explicit disposable developer experiment")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(output, "contact-trial-host --private-contact-trial (private setup on stdin; retained loopback listener on fd 3)")
			return 0
		}
		_, _ = fmt.Fprintln(diagnostics, "invalid arguments; use --help")
		return 2
	}
	if !*enabled || f.NArg() != 0 {
		_, _ = fmt.Fprintln(diagnostics, "explicit --private-contact-trial is required; use --help")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := run(ctx, input, output); err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 1
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_, _ = fmt.Fprintln(diagnostics, "trial lease expired")
		return 1
	}
	return 0
}

func run(ctx context.Context, input *os.File, output io.Writer) error {
	if input == nil {
		return errSetup
	}
	info, err := input.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return errSetup
	}
	// Exec inherits blocking descriptors. Duplicate this owned input before
	// making it pollable, so a stalled controller still has a fixed read bound.
	fd, err := syscall.Dup(int(input.Fd()))
	if err != nil {
		return errSetup
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return errSetup
	}
	pipe := os.NewFile(uintptr(fd), "trial-setup")
	defer func() { _ = pipe.Close() }() // process-owned duplicate, never the controller's writer
	if err := pipe.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return errSetup
	}
	c, err := readSetup(pipe)
	if err != nil {
		return err
	}
	certificate, err := tls.X509KeyPair([]byte(c.certificate), []byte(c.key))
	if err != nil {
		return errSetup
	}
	callback, _ := url.Parse(c.callback) // validated by readSetup
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return errSetup
	}
	// The controller pins this local application certificate. Verify its current
	// validity, server-auth purpose and hostname without consulting system roots
	// or mistaking the separate broker trust roots for application trust.
	applicationRoots := x509.NewCertPool()
	applicationRoots.AddCert(leaf)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: applicationRoots, DNSName: callback.Hostname(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return errSetup
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(c.trust)) {
		return errSetup
	}
	listenerFile := os.NewFile(3, "trial-listener")
	if listenerFile == nil {
		return errSetup
	}
	listener, err := net.FileListener(listenerFile)
	closeErr := listenerFile.Close()
	if err != nil {
		return errSetup
	}
	defer func() { _ = listener.Close() }() // Serve owns its duplicate; the controller retains its own.
	address, ok := listener.Addr().(*net.TCPAddr)
	if closeErr != nil || !ok || !address.IP.Equal(net.ParseIP("127.0.0.1")) || listener.Addr().String() != callback.Host {
		return errSetup
	}
	api := isolated.NewAPI()
	defer func() { _ = api.Shutdown(context.Background()) }() // no external provider is retained
	values := map[string]memprovider.InMemoryFlag{}
	for name, enabled := range map[string]bool{"portal-identity": c.mode != "closed", "portal-contact-renderer": c.mode == "svelte"} {
		values[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": enabled}}
	}
	if err := api.SetProviderAndWait(ctx, memprovider.NewInMemoryProvider(values)); err != nil {
		return errors.New("trial flag setup failed")
	}
	poolConfig, err := pgxpool.ParseConfig(c.database)
	if err != nil {
		return errSetup
	}
	poolConfig.MaxConns, poolConfig.MinConns = 4, 0
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	// ParseConfig also reads ambient PostgreSQL startup settings. The private
	// setup, not PGOPTIONS or a service file, defines this confined connection.
	poolConfig.ConnConfig.RuntimeParams = map[string]string{
		"search_path": "public", "statement_timeout": "5000", "lock_timeout": "1000",
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startup, poolConfig)
	if err != nil {
		return errors.New("trial database refused")
	}
	defer pool.Close()
	var matches bool
	if err := pool.QueryRow(startup, "SELECT current_user::text = $1 AND session_user::text = $1", poolConfig.ConnConfig.User).Scan(&matches); err != nil || !matches {
		return errors.New("trial database refused")
	}
	if err := identity.CheckRuntimeRole(startup, pool); err != nil {
		return errors.New("trial database refused")
	}
	issuer, _ := url.Parse(c.issuer) // validated numeric loopback endpoint
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != issuer.Host {
				return nil, errors.New("trial broker destination refused")
			}
			return dialer.DialContext(ctx, network, address)
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	broker, err := identity.NewBroker(oidc.ClientContext(startup, client), c.issuer, "portal-fixture")
	if err != nil {
		return errors.New("trial broker refused")
	}
	foundation := identity.New(pool, broker, api.NewClient())
	var handler http.Handler
	if c.mode == "svelte" {
		renderer, err := contactrenderer.New(c.node, c.worker, api.NewClient())
		if err != nil {
			return errors.New("trial renderer refused")
		}
		defer renderer.Close()
		handler, err = foundation.DeveloperContactBrowser(c.callback, "synthetic-client-secret", renderer)
		if err != nil {
			return errors.New("trial browser refused")
		}
	} else {
		handler, err = foundation.Browser(c.callback, "synthetic-client-secret")
		if err != nil {
			return errors.New("trial browser refused")
		}
	}
	if _, err := fmt.Fprintln(output, "contact-trial-host ready"); err != nil {
		return errors.New("trial readiness output failed")
	}
	if err := runtimeapp.Serve(ctx, tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}), handler); err != nil {
		return errors.New("trial serving failed")
	}
	return nil
}
