package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/runtimeapp"
)

// inheritedPortalKeys are the settings a projection refuses to share with the environment.
var inheritedPortalKeys = []string{"PORTAL_DATABASE_URL", "PORTAL_OIDC_ISSUER", "PORTAL_OIDC_CLIENT_ID", "PORTAL_CALLBACK_URL", "PORTAL_CLIENT_SECRET_FILE", "PORTAL_CLIENT_SECRET_PROJECTION_FILE", "PORTAL_IDENTITY_PROFILE", "PORTAL_IDENTITY_FLAG", "PORTAL_IDENTITY_LEASE_UNTIL"}

// unsetPortalEnvironment removes inherited portal settings for one test only.
func unsetPortalEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range inheritedPortalKeys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal("fixture environment reset failed")
		}
	}
}

// portalSettingsFile writes fictional settings with the projection's group-read shape.
func portalSettingsFile(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "portal-settings")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal("settings fixture write failed")
	}
	if err := os.Chown(file, -1, os.Getegid()); err != nil {
		t.Fatal("settings fixture group failed")
	}
	if err := os.Chmod(file, mode); err != nil {
		t.Fatal("settings fixture mode failed")
	}
	return file
}

// refusingListener fails the test if a refused command opens a service.
func refusingListener(t *testing.T) func(string, string) (net.Listener, error) {
	return func(string, string) (net.Listener, error) {
		t.Error("a refused command opened a listener")
		return nil, errors.New("fixture listener must not open")
	}
}

// The invitation is an exclusive one-shot command: incomplete, malformed or
// mixed options are refused before any private input is read or a listener opens.
func TestPortalInviteRejectsMixedOrMalformedOptions(t *testing.T) {
	public := configuredPublicArtifact(t)
	unsetPortalEnvironment(t)
	valid := []string{"--portal-invite", "--portal-settings-projection", "/missing/portal-settings", "--invite-zitadel-user", "345678901234567890", "--invite-client", "client-a", "--invite-role", "client", "--invite-days", "7"}
	set := func(option, value string) []string {
		args := slices.Clone(valid)
		args[slices.Index(args, option)+1] = value
		return args
	}
	cases := [][]string{
		append(slices.Clone(valid), "--invite-days", "7"),
		append(slices.Clone(valid), "--listen", "127.0.0.1:0"),
		append(slices.Clone(valid), "--public", public),
		append(slices.Clone(valid), "--version"),
		append(slices.Clone(valid), "--migrate"),
		append(slices.Clone(valid), "--runtime-role", "portal_web"),
		append(slices.Clone(valid), "--company-disclosure-projection", "/missing/disclosure"),
		append([]string{"--portal-invite=false"}, valid[1:]...),
		{"--invite-client", "client-a", "--public", public},
	}
	// Omit each option in turn: the switch, then every option and its value.
	for _, span := range [][2]int{{0, 1}, {1, 3}, {3, 5}, {5, 7}, {7, 9}, {9, 11}} {
		cases = append(cases, slices.Delete(slices.Clone(valid), span[0], span[1]))
	}
	for _, value := range []string{"0", "8", "07", "+7", " 7", "1d", ""} {
		cases = append(cases, set("--invite-days", value))
	}
	for _, value := range []string{"admin", "Client", "client ", ""} {
		cases = append(cases, set("--invite-role", value))
	}
	for _, value := range []string{"", "account a", "account\n", strings.Repeat("1", 201)} {
		cases = append(cases, set("--invite-zitadel-user", value))
	}
	for _, value := range []string{"", "Client-A", "-client", "client-", "client_a", strings.Repeat("a", 65)} {
		cases = append(cases, set("--invite-client", value))
	}
	cases = append(cases, set("--portal-settings-projection", ""))
	for _, args := range cases {
		var output strings.Builder
		if err := runWithListen(context.Background(), args, &output, refusingListener(t)); !errors.Is(err, errPortalInvite) || output.Len() != 0 {
			t.Fatalf("%q: malformed invitation options were accepted: %v", args, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWithListen(ctx, valid, io.Discard, refusingListener(t)); !errors.Is(err, errPortalInvite) {
		t.Fatal("a cancelled invitation read private input")
	}
	// Valid options reach the confined settings reader, which refuses a missing file.
	var output strings.Builder
	if err := runWithListen(context.Background(), valid, &output, refusingListener(t)); !errors.Is(err, runtimeapp.ErrPortalSettings) || output.Len() != 0 || strings.Contains(err.Error(), "missing") {
		t.Fatal("valid invitation options did not reach the private settings boundary")
	}
	revision = ""
	if err := runWithListen(context.Background(), valid, io.Discard, refusingListener(t)); err == nil || errors.Is(err, runtimeapp.ErrPortalSettings) {
		t.Fatal("an unpackaged build read invitation settings")
	}
}

// Settings configure serving only. Ambiguous, unsafe or mixed inputs are
// refused before a listener opens; errors never include the path.
func TestPortalSettingsProjectionRefusals(t *testing.T) {
	public := configuredPublicArtifact(t)
	safe := portalSettingsFile(t, "# Portal sign-in is off.\n", 0440)
	for _, tc := range []struct {
		args    []string
		want    error
		inherit bool
	}{
		{[]string{"--portal-settings-projection", safe, "--portal-settings-projection", safe}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection="}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", safe, "--check-public"}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", safe, "--version"}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", safe, "--check-serving-public"}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", portalSettingsFile(t, "PORTAL_IDENTITY_PROFILE=production\n", 0444)}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", portalSettingsFile(t, "PORTAL_IDENTITY_LEASE_UNTIL=2026-11-15T00:00:00Z\n", 0440)}, runtimeapp.ErrPortalSettings, false},
		{[]string{"--portal-settings-projection", safe}, runtimeapp.ErrPortalSettings, true},
		{[]string{"--portal-settings-projection", safe, "--migrate", "--migration-database-file", "/missing/private-connection"}, runtimeapp.ErrMigration, false},
		{[]string{"--portal-settings-projection", safe, "--identity-materialize", "--identity-phase", "serve", "--identity-inputs", "/missing/inputs", "--identity-output", "/missing/output"}, errIdentityPhase, false},
	} {
		unsetPortalEnvironment(t)
		if tc.inherit {
			t.Setenv("PORTAL_OIDC_ISSUER", "")
		}
		var output strings.Builder
		err := runWithListen(context.Background(), append([]string{"--public", public}, tc.args...), &output, refusingListener(t))
		if !errors.Is(err, tc.want) || output.Len() != 0 || strings.Contains(err.Error(), safe) {
			t.Fatalf("%q: unsafe settings combination was accepted or disclosed: %v", tc.args, err)
		}
		if os.Getenv("PORTAL_IDENTITY_PROFILE") != "" {
			t.Fatal("a refused settings projection installed a value")
		}
	}
}

// A projection with no keys is exactly ordinary public startup: public pages
// serve and private routes stay reserved without any portal dependency.
func TestPortalSettingsProjectionWithoutKeysServesPublicOnly(t *testing.T) {
	unsetPortalEnvironment(t)
	dir := configuredPublicArtifact(t)
	settings := portalSettingsFile(t, "# Portal sign-in is off until its settings are added.\n\n", 0440)
	var listen string
	ready := make(chan struct{})
	var once sync.Once
	output := writeFunc(func(data []byte) (int, error) {
		once.Do(func() { close(ready) })
		return len(data), nil
	})
	cancel, done, join := startRuntime(t, []string{"--public", dir, "--listen", "127.0.0.1:0", "--portal-settings-projection", settings}, output, recordedListener(&listen))
	defer join()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("comment-only settings stopped public startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("comment-only settings did not start public serving")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for route, want := range map[string]int{"/": http.StatusOK, "/portal/sign-in": http.StatusNotFound} {
		res, err := client.Get("http://" + listen + route)
		if err != nil {
			t.Fatal("public runtime did not answer")
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s: status %d, want %d", route, res.StatusCode, want)
		}
	}
	for _, key := range inheritedPortalKeys {
		if _, set := os.LookupEnv(key); set {
			t.Fatal("comment-only settings installed a portal value")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("public runtime did not stop cleanly")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("public runtime did not stop")
	}
}
