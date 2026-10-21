//go:build contacttrialintegration

package identity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This correctness control launches the real command. Its retained listener,
// broker, control-plane invitations and HTTP clients stay in the test process;
// none of its timings is comparative service evidence.
func TestContactTrialHostRetainsOriginAndAuthority(t *testing.T) {
	contactTrialHostProcess(t)
}

func contactTrialHostProcess(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, migrator := contactHostDatabase(t)
	f := newBrowserFixtureOnPool(t, pool)
	binary := filepath.Join(t.TempDir(), "contact-trial-host")
	build := exec.Command("go", "build", "-race", "-o", binary, "../../cmd/contact-trial-host")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual developer host: %s", output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error("retained trial listener cleanup failed")
		}
	})
	file, err := listener.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer closeContactHostFile(t, file)
	// Only this separately retained socket is ever used by the compiled host.
	// The kernel keeps its endpoint bound throughout every child restart.
	certificate := f.site.TLS.Certificates[0]
	f.site.Close()
	f.site.URL = "https://" + listener.Addr().String()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal("marshal fictional TLS key")
	}
	config := map[string]string{
		"mode": "go", "database": fmt.Sprintf("postgres://portal_runtime:fixture-only@127.0.0.1:%d/%s?sslmode=disable", pool.Config().ConnConfig.Port, pool.Config().ConnConfig.Database), "issuer": f.broker.URL,
		"callback":    f.site.URL + "/portal/callback",
		"certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})),
		"key":         string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})),
		"trust":       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.broker.TLS.Certificates[0].Certificate[0]})),
	}
	passedFile := file
	var inputOverride *os.File
	start := func(t *testing.T, mode, expectedError string) func() {
		t.Helper()
		ready := expectedError == ""
		config["mode"] = mode
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "--private-contact-trial")
		// Ambient PostgreSQL options must not alter the pipe-defined trial.
		// Readiness SELECTs alone miss this option; the real journey must write.
		command.Env = append(os.Environ(), "PGOPTIONS=-c default_transaction_read_only=on")
		command.ExtraFiles = []*os.File{passedFile}
		// A real os.Pipe is the setup transport even for this tiny fixture.
		command.Stdin = bytes.NewReader(data)
		if inputOverride != nil {
			command.Stdin = inputOverride
		}
		command.WaitDelay = time.Second
		var diagnostics bytes.Buffer
		command.Stderr = &diagnostics
		out, err := command.StdoutPipe()
		if err != nil || command.Start() != nil {
			t.Fatal("start developer host")
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		stopped := false
		wait := func(wantSuccess bool) {
			t.Helper()
			stopped = true
			select {
			case err := <-done:
				if (err == nil) != wantSuccess {
					t.Error("trial host exited with an unexpected status")
				}
			case <-time.After(15 * time.Second):
				_ = command.Process.Kill() // only the child this test owns
				<-done
				t.Error("trial host did not meet its exit bound")
			}
		}
		stop := func() {
			t.Helper()
			if stopped {
				return
			}
			if err := command.Process.Signal(syscall.SIGTERM); err != nil {
				t.Error("signal owned trial host")
			}
			wait(true)
			if diagnostics.Len() != 0 {
				t.Error("trial host did not shut down cleanly")
			}
		}
		t.Cleanup(stop)
		line := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(out)
			if scanner.Scan() {
				line <- scanner.Text()
			} else {
				line <- ""
			}
		}()
		select {
		case got := <-line:
			if ready && got != "contact-trial-host ready" {
				wait(false)
				t.Fatalf("trial host failed before readiness: %s", diagnostics.String())
			}
			if !ready {
				if got != "" {
					t.Error("unsafe setup reached readiness")
					stop()
					return stop
				}
				wait(false)
				if strings.TrimSpace(diagnostics.String()) != expectedError {
					t.Error("unsafe setup was not refused with a safe diagnostic")
				}
			}
		case <-time.After(20 * time.Second):
			t.Fatal("trial host startup exceeded its bound")
		}
		return stop
	}
	invalidSetup := "invalid trial setup; use the documented private pipe and disposable loopback fixture"
	// The actual command must reject the retired mode and even empty legacy
	// executable fields before readiness, rather than silently falling back.
	start(t, "svelte", invalidSetup)
	for _, fields := range []map[string]string{
		{"node": "", "worker": ""},
		{"node": "/fixture/node", "worker": "/fixture/worker.mjs"},
		{"node": "/fixture/node"},
		{"worker": "/fixture/worker.mjs"},
	} {
		for key, value := range fields {
			config[key] = value
		}
		start(t, "go", invalidSetup)
		delete(config, "node")
		delete(config, "worker")
	}
	// Each parseable matching key pair must be refused before readiness. Removing
	// hostname, validity or server-auth validation must fail its own real process.
	now := time.Now()
	wrongHostKey, err := x509.MarshalPKCS8PrivateKey(f.f.key)
	if err != nil {
		t.Fatal("marshal fictional mismatched-host key")
	}
	validCertificate, validKey := config["certificate"], config["key"]
	for _, test := range []struct {
		name                string
		host                net.IP
		dns                 []string
		notBefore, notAfter time.Time
		usage               x509.ExtKeyUsage
	}{
		{"wrong-host", nil, []string{"unrelated.example.invalid"}, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth},
		{"expired", net.ParseIP("127.0.0.1"), nil, now.Add(-2 * time.Hour), now.Add(-time.Hour), x509.ExtKeyUsageServerAuth},
		{"not-yet-valid", net.ParseIP("127.0.0.1"), nil, now.Add(time.Hour), now.Add(2 * time.Hour), x509.ExtKeyUsageServerAuth},
		{"client-auth-only", net.ParseIP("127.0.0.1"), nil, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageClientAuth},
	} {
		t.Run(test.name, func(t *testing.T) {
			leaf := &x509.Certificate{
				SerialNumber: big.NewInt(2), DNSNames: test.dns,
				NotBefore: test.notBefore, NotAfter: test.notAfter,
				KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{test.usage},
			}
			if test.host != nil {
				leaf.IPAddresses = []net.IP{test.host}
			}
			der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &f.f.key.PublicKey, f.f.key)
			if err != nil {
				t.Fatal("create fictional refusal certificate")
			}
			config["certificate"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
			config["key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: wrongHostKey}))
			start(t, "go", invalidSetup)
		})
	}
	config["certificate"], config["key"] = validCertificate, validKey
	// Use the same valid setup as the positive control, varying only its actual
	// transport/listener/trust boundary. No database or broker response is faked.
	regular, err := os.CreateTemp(t.TempDir(), "setup")
	if err != nil {
		t.Fatal(err)
	}
	defer closeContactHostFile(t, regular)
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := regular.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := regular.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	inputOverride = regular
	start(t, "go", invalidSetup)
	inputOverride = nil
	passedFile = regular
	start(t, "go", invalidSetup)
	passedFile = file
	callback := config["callback"]
	config["callback"] = "https://127.0.0.1:1/portal/callback"
	start(t, "go", invalidSetup)
	config["callback"] = callback
	stalled, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer closeContactHostFile(t, stalled)
	defer closeContactHostFile(t, writer)
	inputOverride = stalled
	start(t, "go", invalidSetup) // writer deliberately remains open until the five-second deadline
	inputOverride = nil
	wildcard, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wildcard.Close(); err != nil {
			t.Error("wildcard refusal fixture cleanup failed")
		}
	}()
	wildcardFile, err := wildcard.File()
	if err != nil {
		t.Fatal(err)
	}
	defer closeContactHostFile(t, wildcardFile)
	passedFile = wildcardFile
	config["callback"] = fmt.Sprintf("https://127.0.0.1:%d/portal/callback", wildcard.Addr().(*net.TCPAddr).Port)
	start(t, "go", invalidSetup)
	passedFile, config["callback"] = file, callback
	wrongCA := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, wrongCA, wrongCA, &f.f.key.PublicKey, f.f.key)
	if err != nil {
		t.Fatal(err)
	}
	trust := config["trust"]
	config["trust"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	start(t, "go", "trial broker refused")
	config["trust"] = trust
	stop := start(t, "closed", "")
	if status, _, h := f.request("GET", "/portal/sign-in", nil, ""); status != 404 || h.Get("Cache-Control") != "no-store" {
		t.Fatal("closed mode activated identity")
	}
	stop()
	stop = start(t, "go", "")
	actors := journeyActors(f)
	assertIndependentJourneyActors(f, actors)
	if run := privateContactJourney(f, actors); len(run.Responses) != 21 {
		t.Fatal("compiled host did not complete the maintained three-actor journey")
	}
	// Keep a real authenticated session and saved revision across the restart.
	f.client = actors[0].client
	f.mu.Lock()
	f.subject = "client-a"
	f.mu.Unlock()
	f.finish(f.start(""))
	_, before, _ := f.request("GET", "/portal/contact", nil, "")
	_, revision := journeyForm(f, before)
	stop()
	stop = start(t, "go", "")
	status, after, _ := f.request("GET", "/portal/contact", nil, "")
	if status != 200 || !strings.Contains(after, `value="Alice Fictional"`) {
		t.Fatal("same-origin restart lost the original authenticated record")
	}
	_, current := journeyForm(f, after)
	if current != revision {
		t.Fatal("restart changed the saved contact revision")
	}
	stop()
	// Genuine role-gate negative control: forbidden immutable-history authority.
	if _, err := migrator.Exec(ctx, "GRANT TRUNCATE ON portal_requests TO portal_runtime"); err != nil {
		t.Fatal(err)
	}
	start(t, "go", "trial database refused")
}

func closeContactHostFile(t *testing.T, file *os.File) {
	t.Helper()
	if err := file.Close(); err != nil {
		t.Error("trial descriptor cleanup failed")
	}
}

// contactHostDatabase creates only a unique, explicitly disposable database and
// never gives its runtime role the ordinary reset fixture's TRUNCATE authority.
func contactHostDatabase(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	base, err := contactHostFixtureConfig(os.Getenv("PORTAL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("explicit disposable loopback database fixture is required")
	}
	base.ConnConfig.Host = "127.0.0.1"
	base.ConnConfig.Password = "fixture-only"
	adminConfig := base.ConnConfig.Copy()
	adminConfig.User, adminConfig.Database = "portal_fixture_owner", "postgres"
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect disposable fixture administrator")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "portal_trial_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted+" OWNER portal_migrator"); err != nil {
		t.Fatal("create unique trial fixture database")
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error("owned trial database cleanup failed")
		}
		if err := admin.Close(ctx); err != nil {
			t.Error("trial administrator cleanup failed")
		}
	})
	base.ConnConfig.Database = name
	base.ConnConfig.User = "portal_migrator"
	migrator, err := pgxpool.NewWithConfig(ctx, base.Copy())
	if err != nil {
		t.Fatal("connect trial migrator")
	}
	t.Cleanup(migrator.Close)
	if err := Migrate(ctx, migrator); err != nil {
		t.Fatal("migrate unique trial fixture")
	}
	if _, err := migrator.Exec(ctx, "GRANT SELECT ON portal_membership_transitions TO portal_runtime"); err != nil {
		t.Fatal("grant read-only membership evidence")
	}
	if _, err := migrator.Exec(ctx, "GRANT SELECT,INSERT ON portal_work_approvals,portal_work_revocations,portal_work_current TO portal_runtime; GRANT UPDATE(approval_id,generation) ON portal_work_current TO portal_runtime"); err != nil {
		t.Fatal("grant confined work-approval evidence")
	}
	if _, err := migrator.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO portal_runtime; GRANT SELECT,INSERT,UPDATE,DELETE ON portal_login_attempts,portal_invitations,portal_memberships,portal_sessions,portal_contacts TO portal_runtime; GRANT SELECT,INSERT ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions TO portal_runtime; GRANT SELECT,INSERT,UPDATE ON portal_proposal_reviews TO portal_runtime"); err != nil {
		t.Fatal("grant restricted trial runtime")
	}
	base.ConnConfig.User = "portal_runtime"
	pool, err := pgxpool.NewWithConfig(ctx, base)
	if err != nil {
		t.Fatal("connect trial runtime")
	}
	t.Cleanup(pool.Close)
	if err := CheckRuntimeRole(ctx, pool); err != nil {
		t.Fatal(fmt.Errorf("restricted trial runtime: %w", err))
	}
	return pool, migrator
}

// Validate the complete fixture before deriving any privileged connection. In
// particular, pgx's first Host is not proof that every fallback stays local.
func contactHostFixtureConfig(raw string) (*pgxpool.Config, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "portal_runtime" || (u.Host != "localhost:5432" && u.Host != "127.0.0.1:5432" && u.Host != "127.0.0.1:55444") || u.Path != "/portal_identity_test" || u.RawPath != "" || u.RawQuery != "sslmode=disable" || u.Fragment != "" {
		return nil, ErrDenied
	}
	if password, ok := u.User.Password(); !ok || password != "fixture-only" {
		return nil, ErrDenied
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || len(cfg.ConnConfig.Fallbacks) != 0 || len(cfg.ConnConfig.RuntimeParams) != 0 || cfg.ConnConfig.TLSConfig != nil {
		return nil, ErrDenied
	}
	return cfg, nil
}

func TestContactTrialHostRefusesAlternateFixtureDestinations(t *testing.T) {
	for _, raw := range []string{
		"postgres://portal_runtime:fixture-only@127.0.0.1:55444,remote.example.invalid:5432/portal_identity_test?sslmode=disable",
		"host=127.0.0.1,remote.example.invalid port=55444,5432 user=portal_runtime password=fixture-only dbname=portal_identity_test sslmode=disable",
		"postgres://portal_runtime:fixture-only@127.0.0.1:55444/portal_identity_test?sslmode=disable&options=-c+role=portal_runtime",
	} {
		if _, err := contactHostFixtureConfig(raw); err == nil {
			t.Error("alternate fixture destination or authority accepted")
		}
	}
}
