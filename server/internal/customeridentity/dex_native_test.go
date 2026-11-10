//go:build integration

// cspell:words hostnossl hostssl PGDATA pgdata isready psql usename
package customeridentity

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	nativeDexImage      = "ghcr.io/devantler-tech/platform-dex@sha256:cffb54b332b8d957ca4c8611814ee1685837370ce5fa7069dacdbc39c0c2afc7"
	nativeDexDBImage    = "postgres:17.11-bookworm@sha256:3645570cccdfa447589da9f57dd740faa29b30938e861289a5574b6ca6b03826"
	nativeDexIssuer     = "https://login.example.invalid"
	nativeDexProvider   = "https://accounts.example.invalid"
	nativeDexDBHost     = "database.example.invalid"
	nativeDexWrongHost  = "wrong.database.example.invalid"
	nativeDexFixtureEnv = "PORTAL_NATIVE_DEX_DISCOVERY_FIXTURE"
)

var nativeDexID = regexp.MustCompile(`^[0-9a-f]{64}$`)

var nativeDexRestriction = []string{
	"--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
	"--user=65534:65534", "--pids-limit=128", "--memory=256m", "--cpus=1",
}

// nativeDexDocker keeps private fixture values out of error messages and bounds each operation.
func nativeDexDocker(input []byte, limit time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func nativeDexRun(t *testing.T, args ...string) []byte {
	t.Helper()
	out, err := nativeDexDocker(nil, 25*time.Second, args...)
	if err != nil {
		t.Fatalf("native Dex Docker %s failed: %v", args[0], err)
	}
	return bytes.TrimSpace(out)
}

// nativeDexContainer registers cleanup for an ID returned by the daemon, never a shared name.
func nativeDexContainer(t *testing.T, args ...string) string {
	t.Helper()
	id := string(nativeDexRun(t, append([]string{"create", "--platform=linux/amd64"}, args...)...))
	if !nativeDexID.MatchString(id) {
		t.Fatal("unexpected native Dex container identity")
	}
	t.Cleanup(func() {
		if _, err := nativeDexDocker(nil, 20*time.Second, "rm", "--force", "--volumes", id); err != nil {
			t.Error("native Dex container cleanup failed")
		}
	})
	return id
}

func nativeDexPoll(t *testing.T, operation string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("native Dex fixture did not converge: %s", operation)
}

// nativeDexWriteArchive gives the broker its private output without a privileged carrier process.
func nativeDexWriteArchive(t *testing.T, directory string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	header := tar.Header{Name: "customer-dex/", Typeflag: tar.TypeDir, Mode: 0700, Uid: 65534, Gid: 65534}
	projectionMust(t, writer.WriteHeader(&header))
	names := []string{"config.json", "database-ca.pem", "tls.crt", "tls.key", "zitadel-ca.pem"}
	entries, err := os.ReadDir(directory)
	projectionMust(t, err)
	if len(entries) != len(names) {
		t.Fatal("unexpected native Dex runtime output")
	}
	for i, entry := range entries {
		info, err := entry.Info()
		projectionMust(t, err)
		if entry.Name() != names[i] || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("unexpected native Dex runtime member")
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		projectionMust(t, err)
		file := tar.Header{Name: "customer-dex/" + entry.Name(), Mode: 0600, Size: int64(len(contents)), Uid: 65534, Gid: 65534}
		projectionMust(t, writer.WriteHeader(&file))
		_, err = writer.Write(contents)
		projectionMust(t, err)
	}
	projectionMust(t, writer.Close())
	return data.Bytes()
}

func nativeDexRuntimeContainer(t *testing.T, network, directory string) string {
	t.Helper()
	volume := "customer-dex-fixture-" + strings.ToLower(rand.Text())
	if string(nativeDexRun(t, "volume", "create", volume)) != volume {
		t.Fatal("unexpected native Dex volume identity")
	}
	t.Cleanup(func() {
		if _, err := nativeDexDocker(nil, 20*time.Second, "volume", "rm", volume); err != nil {
			t.Error("native Dex runtime volume cleanup failed")
		}
	})
	mount := "type=volume,source=" + volume + ",target="
	carrier := nativeDexContainer(t, "--network=none", "--pull=never", "--mount", mount+"/fixture", nativeDexImage)
	_, err := nativeDexDocker(nativeDexWriteArchive(t, directory), 20*time.Second, "cp", "-", carrier+":/fixture")
	projectionMust(t, err)
	args := append([]string{"--network", network, "--pull=never"}, nativeDexRestriction...)
	return nativeDexContainer(t, append(args, "--mount", mount+"/run,readonly",
		"--env", "DEX_EXPAND_ENV=false", "--env", "DEX_CONFIG_DISALLOW_UNKNOWN_FIELDS=true",
		"--env", "DEX_CONTINUE_ON_CONNECTOR_FAILURE=false", nativeDexImage, "dex", "serve", "/run/customer-dex/config.json")...)
}

// nativeDexDiscoveryFixture starts a private OIDC discovery server without creating a user.
func nativeDexDiscoveryFixture(t *testing.T, network string, cert, key []byte) string {
	t.Helper()
	executable, err := os.Executable()
	projectionMust(t, err)
	fixture := t.TempDir()
	for name, data := range map[string][]byte{"tls.crt": cert, "tls.key": key} {
		projectionMust(t, os.WriteFile(filepath.Join(fixture, name), data, 0444))
	}
	args := append([]string{"--network", network, "--network-alias", "accounts.example.invalid", "--pull=never"}, nativeDexRestriction...)
	args = append(args, "--sysctl", "net.ipv4.ip_unprivileged_port_start=0", "--entrypoint", "/fixture",
		"--mount", "type=bind,source="+executable+",target=/fixture,readonly",
		"--mount", "type=bind,source="+filepath.Join(fixture, "tls.crt")+",target=/fixture.crt,readonly",
		"--mount", "type=bind,source="+filepath.Join(fixture, "tls.key")+",target=/fixture.key,readonly",
		"--env", nativeDexFixtureEnv+"=true", nativeDexImage, "-test.run=^TestNativeDexDiscoveryHelper$")
	id := nativeDexContainer(t, args...)
	nativeDexRun(t, "start", id)
	return id
}

// TestNativeDexDiscoveryHelper serves only issuer discovery inside the owned test network.
func TestNativeDexDiscoveryHelper(t *testing.T) {
	if os.Getenv(nativeDexFixtureEnv) != "true" {
		t.Fatal("the native discovery fixture requires its isolated container")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"issuer": nativeDexProvider, "authorization_endpoint": nativeDexProvider + "/authorize",
			"token_endpoint": nativeDexProvider + "/token", "jwks_uri": nativeDexProvider + "/keys",
		}); err != nil {
			t.Error("synthetic discovery response failed")
		}
	})
	server := http.Server{Addr: ":443", Handler: mux, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second}
	t.Fatal(server.ListenAndServeTLS("/fixture.crt", "/fixture.key"))
}

// nativeDexDatabase runs the reviewed PostgreSQL image with encrypted host connections only.
func nativeDexDatabase(t *testing.T, network string, cert, key []byte) string {
	t.Helper()
	fixture := t.TempDir()
	materialWrite(t, fixture, "postgres.env", append([]byte("POSTGRES_PASSWORD="), append(materialPassword(t), '\n')...))
	projectionMust(t, os.WriteFile(filepath.Join(fixture, "tls.crt"), cert, 0444))
	projectionMust(t, os.WriteFile(filepath.Join(fixture, "tls.key"), key, 0400))
	const hba = `local all all trust
hostnossl all all 0.0.0.0/0 reject
hostnossl all all ::/0 reject
hostssl all all 0.0.0.0/0 scram-sha-256
hostssl all all ::/0 scram-sha-256
`
	projectionMust(t, os.WriteFile(filepath.Join(fixture, "pg_hba.conf"), []byte(hba), 0444))
	const start = `install -o postgres -m 0600 /fixture.key /var/lib/postgresql/data/tls.key
install -o postgres -m 0644 /fixture.crt /var/lib/postgresql/data/tls.crt
exec /usr/local/bin/docker-entrypoint.sh postgres -c ssl=on -c ssl_min_protocol_version=TLSv1.3 -c ssl_cert_file=/var/lib/postgresql/data/tls.crt -c ssl_key_file=/var/lib/postgresql/data/tls.key -c hba_file=/fixture.hba`
	id := nativeDexContainer(t, "--network", network, "--network-alias", nativeDexDBHost, "--network-alias", nativeDexWrongHost,
		"--pull=never", "--env-file", filepath.Join(fixture, "postgres.env"), "--env", "PGDATA=/var/lib/postgresql/data/pgdata",
		"--mount", "type=bind,source="+filepath.Join(fixture, "tls.crt")+",target=/fixture.crt,readonly",
		"--mount", "type=bind,source="+filepath.Join(fixture, "tls.key")+",target=/fixture.key,readonly",
		"--mount", "type=bind,source="+filepath.Join(fixture, "pg_hba.conf")+",target=/fixture.hba,readonly",
		"--entrypoint", "/bin/sh", nativeDexDBImage, "-ec", start)
	nativeDexRun(t, "start", id)
	return id
}

func nativeDexDatabaseReady(t *testing.T, id string) {
	t.Helper()
	nativeDexPoll(t, "PostgreSQL TCP listener", func() bool {
		_, err := nativeDexDocker(nil, 3*time.Second, "exec", id, "pg_isready", "-h", "127.0.0.1", "-U", "postgres")
		return err == nil
	})
}

// nativeDexSQL allows only the test's fixed provisioning and readback statements.
func nativeDexSQL(t *testing.T, id, statement string) string {
	t.Helper()
	output, err := nativeDexDocker([]byte(statement), 10*time.Second, "exec", "-i", id, "psql", "-X", "-U", "postgres", "-d", "postgres", "-At", "-v", "ON_ERROR_STOP=1")
	if err != nil {
		t.Fatalf("native Dex database fixture query failed: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func nativeDexClient(t *testing.T, id, hostname, port string, ca []byte) *http.Client {
	t.Helper()
	address := string(nativeDexRun(t, "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", id))
	if ip := net.ParseIP(address); ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		t.Fatal("native Dex fixture is outside its private network")
	}
	roots, err := materialRoots(ca)
	projectionMust(t, err)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: hostname},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address, port))
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("fixture redirect refused")
	}}
}

func nativeDexGet(client *http.Client, url string, value any) (err error) {
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, response.Body.Close())
	}()
	if response.StatusCode != http.StatusOK {
		return errors.New("fixture response was not successful")
	}
	return json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(value)
}

type nativeDexCloseFailure struct {
	io.Reader
	err error
}

func (failure nativeDexCloseFailure) Close() error {
	return failure.err
}

func (failure nativeDexCloseFailure) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: failure}, nil
}

func TestNativeDexGetReportsCloseFailure(t *testing.T) {
	closeErr := errors.New("fixture response close failed")
	client := &http.Client{Transport: nativeDexCloseFailure{strings.NewReader(`{"ready":true}`), closeErr}}
	var response map[string]bool
	if err := nativeDexGet(client, nativeDexIssuer, &response); !errors.Is(err, closeErr) || !response["ready"] {
		t.Fatalf("expected decoded response and its close failure, got %v", err)
	}
}

func nativeDexKeys(t *testing.T, client *http.Client) []string {
	t.Helper()
	var discovery struct {
		Issuer string   `json:"issuer"`
		JWKS   string   `json:"jwks_uri"`
		PKCE   []string `json:"code_challenge_methods_supported"`
	}
	var keys struct {
		Keys []struct {
			ID string `json:"kid"`
		} `json:"keys"`
	}
	nativeDexPoll(t, "native discovery and persisted signing keys", func() bool {
		if nativeDexGet(client, nativeDexIssuer+"/.well-known/openid-configuration", &discovery) != nil {
			return false
		}
		return discovery.Issuer == nativeDexIssuer && discovery.JWKS == nativeDexIssuer+"/keys" &&
			slices.Equal(discovery.PKCE, []string{"S256"}) && nativeDexGet(client, discovery.JWKS, &keys) == nil && len(keys.Keys) > 0
	})
	var result []string
	for _, key := range keys.Keys {
		if key.ID == "" {
			t.Fatal("native Dex signing key has no identifier")
		}
		result = append(result, key.ID)
	}
	slices.Sort(result)
	return result
}

func nativeDexRequireFailure(t *testing.T, id, cause string) {
	t.Helper()
	out, err := nativeDexDocker(nil, 20*time.Second, "start", "--attach", id)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !strings.Contains(strings.ToLower(string(out)+string(exit.Stderr)), cause) {
		t.Fatal("native Dex did not refuse the expected TLS boundary")
	}
}

func nativeDexPrepare(t *testing.T, input, output string) {
	t.Helper()
	projectionMust(t, Materialize("dex-serve", input, output))
}

// TestNativeDexTLSAndRestart exercises the signed broker with real TLS and durable PostgreSQL state.
func TestNativeDexTLSAndRestart(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || os.Getenv("PORTAL_NATIVE_DEX_FIXTURE") != "true" {
		t.Fatal("native Dex requires an explicitly selected Linux amd64 fixture")
	}
	for _, image := range []string{nativeDexImage, nativeDexDBImage} {
		if _, err := nativeDexDocker(nil, 90*time.Second, "pull", "--platform=linux/amd64", image); err != nil {
			t.Fatal("pinned native Dex fixture image unavailable")
		}
	}
	network := string(nativeDexRun(t, "network", "create", "--internal", "customer-dex-fixture-"+strings.ToLower(rand.Text())))
	if !nativeDexID.MatchString(network) {
		t.Fatal("unexpected native Dex network identity")
	}
	t.Cleanup(func() {
		if _, err := nativeDexDocker(nil, 20*time.Second, "network", "rm", network); err != nil {
			t.Error("native Dex network cleanup failed")
		}
	})
	expires := time.Now().Add(time.Hour)
	databaseCA, databaseCert, databaseKey := materialCertificate(t, nativeDexDBHost, expires)
	providerCA, providerCert, providerKey := materialCertificate(t, "accounts.example.invalid", expires)
	wrongCA, _, _ := materialCertificate(t, "wrong.example.invalid", expires)
	provider := nativeDexDiscoveryFixture(t, network, providerCert, providerKey)
	providerClient := nativeDexClient(t, provider, "accounts.example.invalid", "443", providerCA)
	nativeDexPoll(t, "synthetic HTTPS provider", func() bool {
		var discovery map[string]string
		return nativeDexGet(providerClient, nativeDexProvider+"/.well-known/openid-configuration", &discovery) == nil &&
			discovery["issuer"] == nativeDexProvider
	})
	db := nativeDexDatabase(t, network, databaseCert, databaseKey)
	nativeDexDatabaseReady(t, db)
	input, output := dexFixture(t)
	dbPassword, err := os.ReadFile(filepath.Join(input, "database-password"))
	projectionMust(t, err)
	if !regexp.MustCompile(`^[a-f0-9]{48}Aa1!$`).Match(dbPassword) {
		t.Fatal("unexpected generated database fixture password")
	}
	nativeDexSQL(t, db, "CREATE ROLE dex_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '"+string(dbPassword)+"';\nCREATE DATABASE customer_dex OWNER dex_runtime;\n")
	materialWrite(t, input, "database-ca.pem", databaseCA)
	materialWrite(t, input, "zitadel-ca.pem", providerCA)
	for _, test := range []struct {
		name, cause string
		change      func()
		restore     func()
	}{
		{"wrong-database-ca", "certificate signed by unknown authority", func() { materialWrite(t, input, "database-ca.pem", wrongCA) }, func() { materialWrite(t, input, "database-ca.pem", databaseCA) }},
		{"wrong-database-name", "certificate is valid for", func() {
			data := bytes.ReplaceAll(materialFixtureSettings(t, input), []byte(nativeDexDBHost), []byte(nativeDexWrongHost))
			materialWrite(t, input, "settings.json", data)
		}, func() {
			data := bytes.ReplaceAll(materialFixtureSettings(t, input), []byte(nativeDexWrongHost), []byte(nativeDexDBHost))
			materialWrite(t, input, "settings.json", data)
		}},
		{"wrong-provider-ca", "certificate signed by unknown authority", func() { materialWrite(t, input, "zitadel-ca.pem", wrongCA) }, func() { materialWrite(t, input, "zitadel-ca.pem", providerCA) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.change()
			defer test.restore()
			negative := filepath.Join(t.TempDir(), "output")
			nativeDexPrepare(t, input, negative)
			id := nativeDexRuntimeContainer(t, network, negative)
			nativeDexRequireFailure(t, id, test.cause)
		})
	}
	nativeDexPrepare(t, input, output)
	dex := nativeDexRuntimeContainer(t, network, output)
	nativeDexRun(t, "start", dex)
	ca, err := os.ReadFile(filepath.Join(input, "service-ca.pem"))
	projectionMust(t, err)
	client := nativeDexClient(t, dex, "login.example.invalid", "5556", ca)
	original := nativeDexKeys(t, client)
	const encryptedSessions = `SELECT count(*) > 0 AND coalesce(bool_and(s.ssl AND s.version='TLSv1.3'),false) FROM pg_stat_ssl s JOIN pg_stat_activity a USING (pid) WHERE a.usename='dex_runtime' AND a.datname='customer_dex';`
	if nativeDexSQL(t, db, encryptedSessions) != "t" {
		t.Fatal("the native broker did not open verified TLS database sessions")
	}
	const wrongServingName = "wrong.example.invalid"
	wrongName := nativeDexClient(t, dex, wrongServingName, "5556", ca)
	var rejected map[string]any
	var hostnameError x509.HostnameError
	if err := nativeDexGet(wrongName, nativeDexIssuer+"/.well-known/openid-configuration", &rejected); !errors.As(err, &hostnameError) || hostnameError.Host != wrongServingName {
		t.Fatal("the native broker serving certificate did not refuse a wrong name")
	}
	wrongServingCA := nativeDexClient(t, dex, "login.example.invalid", "5556", wrongCA)
	var authorityError x509.UnknownAuthorityError
	if err := nativeDexGet(wrongServingCA, nativeDexIssuer+"/.well-known/openid-configuration", &rejected); !errors.As(err, &authorityError) {
		t.Fatal("the native broker serving certificate did not refuse an untrusted CA")
	}
	nativeDexRun(t, "stop", "--time", "5", dex)
	nativeDexRun(t, "stop", "--time", "5", db)
	nativeDexRun(t, "start", db)
	nativeDexDatabaseReady(t, db)
	nativeDexRun(t, "start", dex)
	restarted := nativeDexClient(t, dex, "login.example.invalid", "5556", ca)
	if !slices.Equal(original, nativeDexKeys(t, restarted)) || nativeDexSQL(t, db, encryptedSessions) != "t" {
		t.Fatal("the native broker did not retain its signing state over restart")
	}
}
