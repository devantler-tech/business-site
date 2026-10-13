package main

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
)

// Migration is an explicit, separate job: no server option, duplicate switch or
// cancelled invocation can silently open a listener or start another operation.
func TestMigrationModeRejectsMixedOptionsWithoutListening(t *testing.T) {
	public := configuredPublicArtifact(t)
	for _, args := range [][]string{
		{"--migrate"},
		{"--migration-database-file", "/missing/private-connection"},
		{"--migrate=false", "--migration-database-file", "/missing/private-connection"},
		{"--migrate", "--migration-database-file", "/missing/private-connection", "--version"},
		{"--migrate", "--migration-database-file", "/missing/private-connection", "--listen", "127.0.0.1:0"},
		{"--migrate", "--migration-database-file", "/missing/private-connection", "--check-public"},
		{"--migrate", "--migration-database-file", "/missing/private-connection", "--public", "/missing/public"},
		{"--migrate", "--migrate", "--migration-database-file", "/missing/private-connection"},
		{"--migrate", "--migrate=false", "--public", public},
		{"--migration-database-file=", "--public", public},
		{"--migrate", "--migration-database-file", "/one", "--migration-database-file", "/two"},
	} {
		listened := false
		listen := func(string, string) (net.Listener, error) {
			listened = true
			return nil, errors.New("fixture listener must not open")
		}
		if err := runWithListen(context.Background(), args, io.Discard, listen); !errors.Is(err, runtimeapp.ErrMigration) || listened {
			t.Fatal("mixed/incomplete migration options changed operation or opened a listener")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"--migrate", "--migration-database-file", "/missing/private-connection"}, io.Discard); !errors.Is(err, runtimeapp.ErrMigration) {
		t.Fatal("cancelled migration did not fail privately before opening a dependency")
	}
}
