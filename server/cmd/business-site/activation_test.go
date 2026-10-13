package main

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// Activation must be checked by the actual command before it opens a listener.
// Without the private dependencies, an activation attempt cannot quietly start.
func TestStagingIdentityRequiresCompleteDependencies(t *testing.T) {
	for _, key := range []string{"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL", "PORTAL_CLIENT_SECRET_FILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("PORTAL_IDENTITY_FLAG", `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`)
	t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
	dir := publicArtifactDir(t)
	artifact, err := loadPublic(dir)
	if err != nil {
		t.Fatal(err)
	}
	oldRevision, oldDigest, oldMode := revision, publicDigest, buildMode
	t.Cleanup(func() { revision, publicDigest, buildMode = oldRevision, oldDigest, oldMode })
	revision, publicDigest, buildMode = strings.Repeat("a", 40), artifact.Digest(), "development"
	listened := false
	listen := func(string, string) (net.Listener, error) {
		listened = true
		return nil, errors.New("fixture listener must not be opened")
	}
	err = runWithListen(context.Background(), []string{"--public", dir}, io.Discard, listen)
	if listened || err == nil || err.Error() != "portal activation is invalid" {
		t.Fatal("the actual command ignored an activation attempt without private identity dependencies")
	}
}
