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
	checkServing := options.Bool("check-serving-public", false, "validate the served profile and print its sanitized receipt")
	if err := options.Parse(args); err != nil || options.NArg() != 0 {
		return errors.New("runtime arguments are invalid")
	}
	if *check {
		artifact, err := loadPublic(*public)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, artifact.Digest())
		return err
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(publicDigest) || buildMode != "release" && buildMode != "development" || publicProfile != "default" && publicProfile != "production-template" {
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
		input, err := runtimeapp.ReadCompanyDisclosure(*disclosure)
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
	cfg, err := runtimeapp.ReadConfig(os.Getenv)
	if err != nil {
		return err
	}
	provider, err := runtimeapp.IdentityProvider(ctx, cfg)
	if err != nil {
		return err
	}
	api := isolated.NewAPI()
	defer func() {
		// Both supported providers own no external resource. Staging shutdown
		// cancels its lease before the SDK releases evaluation bookkeeping.
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
	_, _ = fmt.Fprintln(output, "runtime ready; private routes remain independently gated")
	return runtimeapp.Serve(ctx, listener, artifact.Handler(portal))
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
