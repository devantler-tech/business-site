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
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

// The source-owned packager sets these together from the checked-out source and
// actual audited bytes. An ordinary go build is not a distributable package.
var revision, publicDigest, buildMode string

func loadPublic(directory string) (*webapp.Artifact, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, webapp.ErrArtifact
	}
	defer root.Close()
	return webapp.Load(root.FS())
}

func run(ctx context.Context, args []string, output io.Writer) error {
	options := flag.NewFlagSet("business-site", flag.ContinueOnError)
	options.SetOutput(io.Discard)
	public := options.String("public", "./public", "packaged public artifact directory")
	listen := options.String("listen", "127.0.0.1:8080", "HTTP listener behind an eventual trusted TLS gateway")
	check := options.Bool("check-public", false, "validate public bytes and print their digest without starting services")
	version := options.Bool("version", false, "print package identity without starting services")
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
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(publicDigest) || buildMode != "release" && buildMode != "development" {
		return errors.New("build a verified runtime package before starting")
	}
	if *version {
		return json.NewEncoder(output).Encode(struct{ Repository, SourceRevision, PublicDigest, Mode string }{"devantler-tech/business-site", revision, publicDigest, buildMode})
	}
	artifact, err := loadPublic(*public)
	if err != nil || artifact.Digest() != publicDigest {
		return errors.New("public artifact does not match this runtime package")
	}
	cfg, err := runtimeapp.ReadConfig(os.Getenv)
	if err != nil {
		return err
	}
	api := isolated.NewAPI()
	if err := api.SetProviderAndWait(ctx, openfeature.NoopProvider{}); err != nil {
		return errors.New("identity gate is unavailable")
	}
	defer api.Shutdown(context.Background())
	portal, closePortal, err := runtimeapp.Portal(ctx, cfg, api.NewClient())
	if err != nil {
		return err
	}
	defer closePortal()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return errors.New("runtime listener is unavailable")
	}
	defer listener.Close()
	_, _ = fmt.Fprintln(output, "runtime ready; portal activation is not configured")
	return runtimeapp.Serve(ctx, listener, artifact.Handler(portal))
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
