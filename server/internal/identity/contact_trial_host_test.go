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
	"net"
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
		"node":        "", "worker": "",
	}
	start := func(mode string, ready bool) func() {
		t.Helper()
		config["mode"] = mode
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "--private-contact-trial")
		command.ExtraFiles = []*os.File{file}
		// A real os.Pipe is the setup transport even for this tiny fixture.
		command.Stdin = bytes.NewReader(data)
		var diagnostics bytes.Buffer
		command.Stderr = &diagnostics
		out, err := command.StdoutPipe()
		if err != nil || command.Start() != nil {
			t.Fatal("start developer host")
		}
		stopped := false
		stop := func() {
			t.Helper()
			if stopped {
				return
			}
			stopped = true
			if err := command.Process.Signal(syscall.SIGTERM); err != nil {
				t.Error("signal owned trial host")
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case err := <-done:
				if err != nil || diagnostics.Len() != 0 {
					t.Error("trial host did not shut down cleanly")
				}
			case <-time.After(15 * time.Second):
				_ = command.Process.Kill() // only the child this test owns
				<-done
				t.Error("trial host did not meet its shutdown bound")
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
				_ = command.Wait() // EOF precedes reading the completed diagnostic copy.
				stopped = true
				t.Fatalf("trial host failed before readiness: %s", diagnostics.String())
			}
			if !ready {
				if got != "" {
					t.Error("unsafe database reached readiness")
				}
				if err := command.Wait(); err == nil || strings.TrimSpace(diagnostics.String()) != "trial database refused" {
					t.Error("unsafe database was not refused with a safe diagnostic")
				}
				stopped = true
			}
		case <-time.After(20 * time.Second):
			t.Fatal("trial host startup exceeded its bound")
		}
		return stop
	}
	stop := start("closed", true)
	if status, _, h := f.request("GET", "/portal/sign-in", nil, ""); status != 404 || h.Get("Cache-Control") != "no-store" {
		t.Fatal("closed mode activated identity")
	}
	stop()
	stop = start("go", true)
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
	stop = start("go", true)
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
	start("go", false)
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
	base, err := pgxpool.ParseConfig(os.Getenv("PORTAL_TEST_DATABASE_URL"))
	if err != nil || base.ConnConfig.User != "portal_runtime" || base.ConnConfig.Database != "portal_identity_test" || base.ConnConfig.Host != "127.0.0.1" && base.ConnConfig.Host != "localhost" || base.ConnConfig.Port != 5432 && base.ConnConfig.Port != 55444 {
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
