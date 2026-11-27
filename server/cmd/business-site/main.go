// business-site serves a validated public artifact and the default-off portal.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
	"github.com/devantler-tech/business-site/server/internal/webapp"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

// The source-owned packager sets these together from the checked-out source and
// actual audited bytes. An ordinary go build is not a distributable package.
var revision, publicDigest, buildMode string

// publicProfile is a permanent package variant, not a portal activation gate.
var publicProfile = "default"

// loadPublic confines artifact reads to the configured root and snapshots bytes.
func loadPublic(directory string) (*webapp.Artifact, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, webapp.ErrArtifact
	}
	defer func() {
		// The snapshot owns copied bytes, not this read-only directory handle.
		// Closing that handle is best-effort cleanup after the load verdict.
		_ = root.Close()
	}()
	return webapp.Load(root.FS())
}

// run validates package identity and private dependencies before opening a listener.
func run(ctx context.Context, args []string, output io.Writer) error {
	return runWithListen(ctx, args, output, net.Listen)
}

// runWithListen keeps listener creation injectable without releasing a test port.
// The ordinary command always uses net.Listen; validation and ownership are shared.
func runWithListen(ctx context.Context, args []string, output io.Writer, listenTCP func(string, string) (net.Listener, error)) error {
	options := flag.NewFlagSet("business-site", flag.ContinueOnError)
	options.SetOutput(io.Discard)
	public := options.String("public", "./public", "packaged public artifact directory")
	listen := options.String("listen", "127.0.0.1:8080", "HTTP listener behind an eventual trusted TLS gateway")
	check := options.Bool("check-public", false, "validate public bytes and print their digest without starting services")
	version := options.Bool("version", false, "print package identity without starting services")
	disclosure := options.String("company-disclosure-file", "", "private projected two-line company disclosure")
	projection := options.String("company-disclosure-projection", "", "explicit confined company projection for the non-root application group")
	checkServing := options.Bool("check-serving-public", false, "validate the served profile and print its sanitized receipt")
	migrate := options.Bool("migrate", false, "run one bounded schema migration job without serving HTTP")
	migrationFile := options.String("migration-database-file", "", "private verified-TLS migration connection file")
	migrationProjection := options.String("migration-database-projection", "", "migration connection file in a Kubernetes Secret projection for the non-root job group")
	retention := options.Bool("retention-purge", false, "delete former clients' portal records after their retention period, without serving HTTP")
	publicHost := options.String("public-host", "", "public site host; with --portal-host, each host redirects the other's pages")
	portalHost := options.String("portal-host", "", "portal host; / opens the portal there")
	portalSettings := options.String("portal-settings-projection", "", "confined projected portal settings file for serving or one invitation")
	logoutURL := options.String("portal-upstream-logout-url", "", "release default for the upstream logout endpoint")
	logoutClientID := options.String("portal-upstream-logout-client-id", "", "release default for the upstream logout client ID")
	runtimeRole := options.String("runtime-role", "", "existing web login that the migration grants its runtime privileges")
	identityPhase := registerIdentityPhaseOptions(options)
	portalInvite := registerPortalInviteOptions(options)
	if err := options.Parse(args); err != nil || options.NArg() != 0 {
		return errors.New("runtime arguments are invalid")
	}
	seen := map[string]int{}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			seen[name]++
		}
	}
	if identityPhase.selected(seen) {
		return identityPhase.run(ctx, options, seen, output)
	}
	if portalInvite.selected(seen) {
		return portalInvite.run(ctx, options, seen, *portalSettings, output)
	}
	if seen["retention-purge"] != 0 {
		return purgeRetention(ctx, options, seen, *retention, *migrationFile, *migrationProjection, output)
	}
	inputs := seen["migration-database-file"] + seen["migration-database-projection"]
	if seen["migrate"] > 1 || inputs > 1 || seen["runtime-role"] > 1 {
		return runtimeapp.ErrMigration
	}
	if *migrate || inputs != 0 || seen["runtime-role"] != 0 {
		projected := seen["migration-database-projection"] == 1
		input := *migrationFile
		if projected {
			input = *migrationProjection
		}
		allowed := *migrate && input != "" && (seen["runtime-role"] == 0 || *runtimeRole != "")
		options.Visit(func(option *flag.Flag) {
			if option.Name != "migrate" && option.Name != "migration-database-file" && option.Name != "migration-database-projection" && option.Name != "runtime-role" {
				allowed = false
			}
		})
		if !allowed || seen["migrate"] != 1 || inputs != 1 {
			return runtimeapp.ErrMigration
		}
		if !validPackageIdentity() {
			return errors.New("build a verified runtime package before starting")
		}
		if !clearPostgresEnvironment() {
			return runtimeapp.ErrMigration
		}
		if err := runtimeapp.MigrateDatabase(ctx, input, *runtimeRole, projected); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "portal schema migration completed")
		return err
	}
	if !validHosts(*publicHost, *portalHost, seen) {
		return errors.New("host arguments are invalid")
	}
	projected := seen["company-disclosure-projection"] != 0
	if projected && (seen["company-disclosure-projection"] != 1 || seen["company-disclosure-file"] != 0 || *projection == "" || *check || *version || publicProfile != "production-template") {
		return runtimeapp.ErrCompanyProjection
	}
	// Portal settings configure serving only; public checks and receipts never read them.
	settingsProjected := seen["portal-settings-projection"] != 0
	if settingsProjected && (seen["portal-settings-projection"] != 1 || *portalSettings == "" || *check || *version || *checkServing) {
		return runtimeapp.ErrPortalSettings
	}
	logoutDefaults := seen["portal-upstream-logout-url"] != 0 || seen["portal-upstream-logout-client-id"] != 0
	if logoutDefaults && (!settingsProjected || *portalHost == "" || seen["portal-upstream-logout-url"] != 1 ||
		seen["portal-upstream-logout-client-id"] != 1 || *logoutURL == "" || *logoutClientID == "") {
		return runtimeapp.ErrPortalSettings
	}
	if *check {
		artifact, err := loadPublic(*public)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, artifact.Digest())
		return err
	}
	if !validPackageIdentity() {
		return errors.New("build a verified runtime package before starting")
	}
	if *version {
		return json.NewEncoder(output).Encode(struct{ Repository, SourceRevision, PublicDigest, Mode, PublicProfile string }{"devantler-tech/business-site", revision, publicDigest, buildMode, publicProfile})
	}
	artifact, err := loadPublic(*public)
	if err != nil || artifact.Digest() != publicDigest {
		return errors.New("public artifact does not match this runtime package")
	}
	if publicProfile == "production-template" {
		var input string
		if projected {
			input, err = runtimeapp.ReadProjectedCompanyDisclosure(*projection)
		} else {
			input, err = runtimeapp.ReadCompanyDisclosure(*disclosure)
		}
		if err != nil {
			return err
		}
		artifact, err = artifact.WithCompanyDisclosure(input)
		if err != nil {
			return errors.New("company disclosure is invalid")
		}
	} else if *disclosure != "" || artifact.HasCompanySlots() {
		return errors.New("public profile does not accept company disclosure")
	}
	if *checkServing {
		return json.NewEncoder(output).Encode(struct{ Repository, SourceRevision, PublicDigest, EffectivePublicDigest, PublicProfile string }{"devantler-tech/business-site", revision, publicDigest, artifact.Digest(), publicProfile})
	}
	if settingsProjected {
		if err := runtimeapp.ApplyPortalSettingsProjection(*portalSettings); err != nil {
			return err
		}
	}
	cfg, err := runtimeapp.ReadConfig(os.Getenv)
	if err != nil {
		return err
	}
	if logoutDefaults {
		cfg, err = cfg.WithUpstreamLogoutDefaults(*logoutURL, *logoutClientID)
		if err != nil {
			return err
		}
	}
	if !cfg.FitsPortalHost(*portalHost) {
		return errors.New("portal callback does not match the portal host")
	}
	if *portalHost != "" {
		cfg = cfg.WithPublicHost(*publicHost)
	}
	cfg = cfg.WithOrderTerms(artifact)
	provider, err := runtimeapp.IdentityProvider(ctx, cfg)
	if err != nil {
		return err
	}
	api := isolated.NewAPI()
	defer func() {
		// These providers own no external resource. Shutdown revokes the
		// selected process gate before the SDK releases its bookkeeping.
		_ = api.Shutdown(context.Background())
	}()
	if err := api.SetProviderAndWait(ctx, provider); err != nil {
		return errors.New("identity gate is unavailable")
	}
	portal, closePortal, err := runtimeapp.Portal(ctx, cfg, api.NewClient())
	if err != nil {
		return err
	}
	defer closePortal()
	listener, err := listenTCP("tcp", *listen)
	if err != nil {
		return errors.New("runtime listener is unavailable")
	}
	defer func() {
		// Serve owns and closes the listener; this fallback may find it closed.
		_ = listener.Close()
	}()
	handler := artifact.Handler(portal)
	if *publicHost != "" || *portalHost != "" {
		handler = artifact.HostHandler(portal, webapp.Hosts{Public: *publicHost, Portal: *portalHost})
	}
	return runtimeapp.ServeReady(ctx, listener, handler, func() {
		_, _ = fmt.Fprintf(output, "runtime ready; listen=%s; private routes remain independently gated\n", listener.Addr())
	})
}

// validHosts accepts neither host or two distinct lowercase DNS names, each given once.
func validHosts(public, portal string, seen map[string]int) bool {
	if seen["public-host"] > 1 || seen["portal-host"] > 1 {
		return false
	}
	if public == "" && portal == "" {
		return true
	}
	name := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	return name.MatchString(public) && name.MatchString(portal) && public != portal && portal != "www."+public
}

// validPackageIdentity applies the same source/profile receipt gate to both
// serving and one-shot jobs; neither an ordinary build nor an unknown variant runs.
func validPackageIdentity() bool {
	return regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(publicDigest) && (buildMode == "release" || buildMode == "development") && (publicProfile == "default" || publicProfile == "production-template")
}

// main maps process termination to bounded HTTP shutdown and generic diagnostics.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	// Release signal registration on both outcomes before os.Exit, which skips defers.
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// clearPostgresEnvironment discards the PostgreSQL-prefixed ambient settings of
// an exclusive one-shot database process, never web startup, before the native
// parser can read an unrelated service/credential file. The parent host is
// unchanged.
func clearPostgresEnvironment() bool {
	for _, setting := range os.Environ() {
		name, _, _ := strings.Cut(setting, "=")
		if strings.HasPrefix(name, "PG") {
			if err := os.Unsetenv(name); err != nil {
				return false
			}
		}
	}
	return true
}

// purgeRetention runs the exclusive retention purge: exactly --retention-purge
// and one --migration-database-file, as the migration role.
func purgeRetention(ctx context.Context, options *flag.FlagSet, seen map[string]int, retention bool, migrationFile, migrationProjection string, output io.Writer) error {
	// Exactly one connection input: a file, or a Kubernetes Secret projection.
	projected := seen["migration-database-projection"] == 1
	input := migrationFile
	if projected {
		input = migrationProjection
	}
	allowed := retention && seen["retention-purge"] == 1 && seen["migration-database-file"]+seen["migration-database-projection"] == 1 && input != ""
	options.Visit(func(option *flag.Flag) {
		if option.Name != "retention-purge" && option.Name != "migration-database-file" && option.Name != "migration-database-projection" {
			allowed = false
		}
	})
	if !allowed {
		return runtimeapp.ErrRetention
	}
	if !validPackageIdentity() {
		return errors.New("build a verified runtime package before starting")
	}
	if !clearPostgresEnvironment() {
		return runtimeapp.ErrRetention
	}
	purged, err := runtimeapp.PurgeRetention(ctx, input, projected)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "portal retention purge completed: %d former clients\n", purged)
	return err
}
