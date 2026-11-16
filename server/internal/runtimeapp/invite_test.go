package runtimeapp

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/devantler-tech/business-site/server/internal/identity"
)

// runInviteCommand runs one compiled invitation with only the supplied
// environment and returns its separate standard output and error streams.
func runInviteCommand(t *testing.T, binary, settings, user string, environment ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--portal-invite", "--portal-settings-projection", settings,
		"--invite-zitadel-user", user, "--invite-client", "client-a", "--invite-role", "client", "--invite-days", "7")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTAL_") && !strings.HasPrefix(value, "portal-") && !strings.HasPrefix(value, "SSL_CERT_FILE=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, environment...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("invitation command exceeded its fixture deadline")
	}
	return stdout.String(), stderr.String(), err
}

// The platform's sequence on one disposable store: the migration grants the web
// role, the web process takes its settings from one projected file, and the
// operator's one-shot invitation reuses that file and role inside the pod.
func TestOperatorPortalInviteUsesProjectedSettings(t *testing.T) {
	binary, public := buildActualCommand(t)
	broker, discoveries, _ := syntheticBroker(t)
	issuer := broker.URL
	database := runtimeFixtureDatabase(t)
	migrator := createFixtureDatabase(t, database)
	if err := identity.MigrateWithRuntimeRole(t.Context(), migrator, "portal_runtime"); err != nil {
		t.Fatal("disposable migration with runtime grants failed")
	}
	database.Host = fixtureTLSDatabase(t, database.Host, broker.TLS.Certificates[0])
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: broker.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal("synthetic certificate write failed")
	}
	query := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {ca}}
	database.RawQuery = query.Encode()
	secret := projectedIdentityFixture(t, []byte("synthetic-constructor-secret"), 0440)
	dependencies := "# Fictional portal dependencies.\nPORTAL_DATABASE_URL=" + database.String() + "\nPORTAL_OIDC_ISSUER=" + issuer +
		"\nPORTAL_OIDC_CLIENT_ID=portal-fixture\nPORTAL_CALLBACK_URL=https://app.example.invalid/portal/callback\nPORTAL_CLIENT_SECRET_PROJECTION_FILE=" + secret + "\n"
	activation := func(value bool) string {
		flag := map[bool]string{true: `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`, false: `{"defaultVariant":"off","variants":[{"name":"off","value":false}]}`}[value]
		return "\nPORTAL_IDENTITY_PROFILE=production\nPORTAL_IDENTITY_FLAG=" + flag + "\n"
	}
	off := projectedIdentityFixture(t, []byte("# Portal sign-in is off.\n"), 0440)
	connected := projectedIdentityFixture(t, []byte(dependencies), 0440)
	enabled := projectedIdentityFixture(t, []byte(dependencies+activation(true)), 0440)
	disabled := projectedIdentityFixture(t, []byte(dependencies+activation(false)), 0440)
	environment := []string{"SSL_CERT_FILE=" + ca}

	// Serving: no keys is public-only startup; complete dependencies connect and
	// discover but keep sign-in closed; production activation opens it.
	for _, tc := range []struct {
		name, settings string
		discovers      bool
		signIn         int
	}{{"off", off, false, 404}, {"connected", connected, true, 404}, {"enabled", enabled, true, 200}} {
		t.Run("serve "+tc.name, func(t *testing.T) {
			before := discoveries.Load()
			address, stop := startActualCommand(t, binary, public, environment, "--portal-settings-projection", tc.settings)
			defer stop()
			commandStatus(t, address, "/", 200)
			commandStatus(t, address, "/portal/sign-in", tc.signIn)
			if (discoveries.Load() != before) != tc.discovers {
				t.Fatal("projected settings did not decide the portal's dependencies")
			}
		})
	}

	user := "345678901234567890"
	for _, tc := range []struct {
		name, settings, refusal string
		environment             []string
	}{
		{"no dependencies", off, "portal invitation requires enabled production identity\n", nil},
		{"dependencies without activation", connected, "portal invitation requires enabled production identity\n", nil},
		{"production flag off", disabled, "portal invitation requires enabled production identity\n", nil},
		{"inherited setting", enabled, "portal settings projection is unavailable\n", []string{"PORTAL_OIDC_ISSUER=" + issuer}},
	} {
		t.Run("refuse "+tc.name, func(t *testing.T) {
			stdout, stderr, err := runInviteCommand(t, binary, tc.settings, user, append(tc.environment, environment...)...)
			if err == nil || stdout != "" || stderr != tc.refusal {
				t.Fatal("invitation without enabled production settings was not refused generically")
			}
		})
	}
	stdout, stderr, err := runInviteCommand(t, binary, enabled, user, environment...)
	if err != nil || stderr != "" || stdout != "portal invitation created: that user accepts it by signing in\n" {
		t.Fatal("enabled production settings did not confirm exactly one invitation")
	}
	var stored bool
	if err := migrator.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_invitations)=1 AND EXISTS (SELECT 1 FROM portal_invitations
 WHERE issuer=$1 AND subject=$2 AND client_id='client-a' AND role='client' AND NOT revoked
 AND expires_at > now()+interval '7 days'-interval '1 minute' AND expires_at <= now()+interval '7 days')`,
		issuer, customeridentity.DexSubject(user, customeridentity.ZitadelConnectorID)).Scan(&stored); err != nil || !stored {
		t.Fatal("invitation was not bound to the broker issuer, Dex subject, client, role and lifetime")
	}
	if strings.Contains(stdout+stderr, user) || strings.Contains(stdout+stderr, issuer) {
		t.Fatal("invitation output disclosed its inputs")
	}
}

// Invite itself refuses an empty configuration before any provider or store work.
func TestOperatorInviteRequiresConfiguredProduction(t *testing.T) {
	unsetPortalEnvironment(t)
	if err := Invite(context.Background(), "subject", "client-a", "client", time.Hour); err != ErrInviteActivation {
		t.Fatal("an unconfigured invitation was not refused")
	}
}
