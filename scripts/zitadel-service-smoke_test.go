package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func lifecycleTestCertificate(t *testing.T, name string) (tls.Certificate, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fictional lifecycle fixture CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, key.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

func TestLifecycleTLSVerifiesTheOwnedService(t *testing.T) {
	certificate, roots := lifecycleTestCertificate(t, "identity.fixture.localhost")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "identity.fixture.localhost:8080" || r.TLS == nil {
			t.Error("fictional authority or TLS was lost")
		}
		_, _ = io.WriteString(w, `{"ready":true}`)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	client, err := lifecycleTLSClient(server.Listener.Addr().String(), roots)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	data, err := serviceGet(client, "https://identity.fixture.localhost:8080", "/debug/ready")
	if err != nil || string(data) != `{"ready":true}` {
		t.Fatalf("verified fictional TLS service refused: %q %v", data, err)
	}
}

func TestLifecycleTLSRefusesWrongCAHostnameAndPlaintext(t *testing.T) {
	for _, kind := range []string{"wrong-ca", "wrong-hostname", "plaintext"} {
		t.Run(kind, func(t *testing.T) {
			name := "identity.fixture.localhost"
			if kind == "wrong-hostname" {
				name = "different.fixture.localhost"
			}
			certificate, roots := lifecycleTestCertificate(t, name)
			if kind == "wrong-ca" {
				_, roots = lifecycleTestCertificate(t, name)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "private fictional response") }))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			if kind == "plaintext" {
				server.Start()
			} else {
				server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
				server.StartTLS()
			}
			defer server.Close()
			client, err := lifecycleTLSClient(server.Listener.Addr().String(), roots)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			data, err := serviceGet(client, "https://identity.fixture.localhost:8080", "/debug/ready")
			if err == nil || data != nil || strings.Contains(err.Error(), "private fictional") || strings.Contains(err.Error(), server.Listener.Addr().String()) {
				t.Fatalf("unverified transport accepted or private diagnostic exposed: %q %v", data, err)
			}
		})
	}
}

func TestLifecycleTLSRefusesMissingTrustAndEscapedRequests(t *testing.T) {
	_, roots := lifecycleTestCertificate(t, "identity.fixture.localhost")
	for _, invalid := range [][]byte{nil, []byte("not a certificate"), append([]byte("ignored leading data\n"), roots...), append([]byte("-----BEGIN CERTIFICATE-----\nnot-base64\n-----END CERTIFICATE-----\n"), roots...), append(append([]byte{}, roots...), []byte("trailing data")...), append(append([]byte{}, roots...), roots...)} {
		if client, err := lifecycleTLSClient("127.0.0.1:8080", invalid); err == nil || client != nil {
			t.Fatal("missing or ambiguous trust accepted")
		}
	}
	certificate, roots := lifecycleTestCertificate(t, "identity.fixture.localhost")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "must not reach an escaped authority")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	client, err := lifecycleTLSClient(server.Listener.Addr().String(), roots)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	for _, destination := range []string{"https://other.fixture.localhost:8080/debug/ready", "https://identity.fixture.localhost:8443/debug/ready"} {
		response, err := client.Get(destination)
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil {
			t.Fatal("escaped fixture request accepted")
		}
	}
	plaintext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "plaintext must not be accepted")
	}))
	defer plaintext.Close()
	plainClient, err := lifecycleTLSClient(plaintext.Listener.Addr().String(), roots)
	if err != nil {
		t.Fatal(err)
	}
	defer plainClient.CloseIdleConnections()
	response, err := plainClient.Get("http://identity.fixture.localhost:8080/debug/ready")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("plaintext request reached the listening HTTP fixture")
	}
}

func TestLifecycleTLSRequiresCanonicalPrivateNumericEndpoint(t *testing.T) {
	_, roots := lifecycleTestCertificate(t, "identity.fixture.localhost")
	for _, target := range []string{"database:8080", "8.8.8.8:8080", "169.254.169.254:8080", "[::1]:8080", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:+8080", "127.0.0.1:08080"} {
		client, err := lifecycleTLSClient(target, roots)
		if client != nil {
			client.CloseIdleConnections()
		}
		if err == nil || client != nil {
			t.Fatalf("invalid fixture endpoint accepted: %q", target)
		}
	}
}

func TestLifecyclePrivateInputAdmission(t *testing.T) {
	for _, mutation := range []string{"unchanged", "missing", "changed", "extra", "symlink", "hard-link", "mode", "root-mode"} {
		t.Run(mutation, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(root, "serve.json")
			original := []byte(`{"private":"fictional-input-must-not-appear"}`)
			if err = os.WriteFile(name, original, 0444); err != nil {
				t.Fatal(err)
			}
			expected := map[string]lifecycleInput{"serve.json": {contents: bytes.Clone(original), Mode: 0444}}
			switch mutation {
			case "missing":
				err = os.Remove(name)
			case "changed":
				if err = os.Remove(name); err == nil {
					err = os.WriteFile(name, []byte(`{"private":"substituted"}`), 0444)
				}
			case "extra":
				err = os.WriteFile(filepath.Join(root, "other.json"), original, 0444)
			case "symlink", "hard-link":
				outside := filepath.Join(t.TempDir(), "outside.json")
				if err = os.WriteFile(outside, original, 0444); err == nil {
					if err = os.Remove(name); err == nil {
						if mutation == "symlink" {
							err = os.Symlink(outside, name)
						} else {
							err = os.Link(outside, name)
						}
					}
				}
			case "mode":
				err = os.Chmod(name, 0644)
			case "root-mode":
				err = os.Chmod(root, 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = lifecycleValidateInputs(root, expected)
			if mutation == "unchanged" {
				if err != nil {
					t.Fatalf("complete private inputs refused: %v", err)
				}
			} else if err == nil || strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "fictional-input") {
				t.Fatalf("invalid inputs accepted or diagnostic exposed private data: %v", err)
			}
		})
	}
}

func TestLifecycleMaterialSeparatesBootstrapFromServing(t *testing.T) {
	material, err := lifecycleMaterials(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if len(material.files) != 18 || len(material.files["master.key"]) != 32 || material.human != "operator_aaaaaaaaaaaaaaaa@example.invalid" {
		t.Fatal("incomplete fictional private material")
	}
	var documents [3]map[string]any
	for index, name := range []string{"init.json", "setup.json", "serve.json"} {
		if err = json.Unmarshal(material.files[name], &documents[index]); err != nil {
			t.Fatal(err)
		}
		db := documents[index]["Database"].(map[string]any)["postgres"].(map[string]any)
		if db["DSN"] != "" || db["Host"] != "database.fixture.localhost" || db["Database"] != material.database || db["User"].(map[string]any)["Username"] != material.app {
			t.Fatal("database identity or explicit-field selection differs")
		}
		for _, role := range []string{"Admin", "User"} {
			ssl := db[role].(map[string]any)["SSL"].(map[string]any)
			if ssl["Mode"] != "verify-full" || ssl["RootCert"] != "/fixture/ca.pem" {
				t.Fatal("native database certificate/hostname verification missing")
			}
		}
		admin := db["Admin"].(map[string]any)
		if index == 0 {
			if admin["Username"] != material.admin || len(admin["Password"].(string)) < 32 {
				t.Fatal("initialization lacks explicit private authority")
			}
		} else if admin["Username"] != "" || admin["Password"] != "" {
			t.Fatal("database bootstrap authority escaped into setup/serve")
		}
	}
	adminPassword := documents[0]["Database"].(map[string]any)["postgres"].(map[string]any)["Admin"].(map[string]any)["Password"].(string)
	var steps map[string]any
	if err = json.Unmarshal(material.files["steps.json"], &steps); err != nil {
		t.Fatal(err)
	}
	instance := steps["FirstInstance"].(map[string]any)
	human := instance["Org"].(map[string]any)["Human"].(map[string]any)
	if instance["Skip"] != false || human["UserName"] != material.human || human["Email"].(map[string]any)["Verified"] != false || human["Email"].(map[string]any)["Address"] != material.human || human["PasswordChangeRequired"] != true {
		t.Fatal("explicit unverified fictional bootstrap differs")
	}
	humanPassword := human["Password"].(string)
	for index, name := range []string{"setup.json", "serve.json"} {
		if strings.Contains(string(material.files[name]), adminPassword) || strings.Contains(string(material.files[name]), humanPassword) || strings.Contains(string(material.files[name]), material.human) {
			t.Fatal("bootstrap secret/identity escaped the isolated setup input")
		}
		document := documents[index+1]
		if document["ExternalSecure"] != true || document["TLS"].(map[string]any)["Enabled"] != true || document["DefaultInstance"].(map[string]any)["LoginPolicy"].(map[string]any)["AllowRegister"] != false || document["DefaultInstance"].(map[string]any)["Restrictions"].(map[string]any)["DisallowPublicOrgRegistration"] != true {
			t.Fatal("TLS or closed registration configuration missing")
		}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(material.files["ca.pem"]) {
		t.Fatal("fixture root missing")
	}
	for _, pair := range []struct{ prefix, hostname string }{{"service", "identity.fixture.localhost"}, {"database", "database.fixture.localhost"}} {
		certificate, e := tls.X509KeyPair(material.files[pair.prefix+".crt"], material.files[pair.prefix+".key"])
		if e != nil {
			t.Fatal(e)
		}
		leaf, e := x509.ParseCertificate(certificate.Certificate[0])
		if e != nil {
			t.Fatal(e)
		}
		if _, e = leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: pair.hostname}); e != nil {
			t.Fatalf("native fixture certificate/name invalid: %v", e)
		}
		if _, e = leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "other.fixture.localhost"}); e == nil {
			t.Fatal("certificate accepts another endpoint")
		}
	}
}

func TestLifecycleWritesOnlyCompleteFreshPrivateInputs(t *testing.T) {
	for _, mutation := range []string{"unchanged", "missing-steps", "renamed-steps", "extra-input", "occupied-directory"} {
		t.Run(mutation, func(t *testing.T) {
			material, err := lifecycleMaterials(strings.Repeat("b", 32))
			if err != nil {
				t.Fatal(err)
			}
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "missing-steps":
				delete(material.files, "steps.json")
			case "renamed-steps":
				material.files["defaults.json"] = material.files["steps.json"]
				delete(material.files, "steps.json")
			case "extra-input":
				material.files["other.json"] = []byte(`{}`)
			case "occupied-directory":
				if err = os.WriteFile(filepath.Join(root, "do-not-remove"), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			bindings, err := lifecycleWriteInputs(root, material)
			if mutation == "unchanged" {
				if err != nil || len(bindings) != 18 {
					t.Fatalf("complete fresh input write refused: %v", err)
				}
				if err = lifecycleValidateInputs(root, bindings); err != nil {
					t.Fatal(err)
				}
				if bindings["admin-client.env"].Mode != 0600 || bindings["serve.json"].Mode != 0444 {
					t.Fatal("private host/readonly mounted input modes differ")
				}
			} else {
				if err == nil || bindings != nil {
					t.Fatal("missing, renamed, extra or occupied input accepted")
				}
				entries, e := os.ReadDir(root)
				if e != nil || (mutation != "occupied-directory" && len(entries) != 0) || (mutation == "occupied-directory" && (len(entries) != 1 || entries[0].Name() != "do-not-remove")) {
					t.Fatal("refused input changed the directory")
				}
			}
		})
	}
}

// TestLifecycleByteSnapshotsRejectCallerAndFileMutation preserves the original
// binding when caller-owned input slices change, including same-size rewrites.
func TestLifecycleByteSnapshotsRejectCallerAndFileMutation(t *testing.T) {
	for _, rewrite := range []bool{false, true} {
		t.Run(strconv.FormatBool(rewrite), func(t *testing.T) {
			material, err := lifecycleMaterials(strings.Repeat("b", 32))
			if err != nil {
				t.Fatal(err)
			}
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			bindings, err := lifecycleWriteInputs(root, material)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(root, "master.key")
			before, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			material.files["master.key"][0] ^= 1
			if rewrite {
				if err = os.Chmod(name, 0600); err == nil {
					err = os.WriteFile(name, material.files["master.key"], 0600)
				}
				if err == nil {
					err = os.Chmod(name, 0444)
				}
				if err == nil {
					err = os.Chtimes(name, before.ModTime(), before.ModTime())
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			err = lifecycleValidateInputs(root, bindings)
			if (rewrite && err == nil) || (!rewrite && err != nil) {
				t.Fatal("caller mutation changed the captured private byte binding")
			}
			if err != nil && (strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "master.key")) {
				t.Fatal("private input diagnostic disclosed its location")
			}
		})
	}
}

func TestServiceHTTPDiagnosticDoesNotExposeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = io.WriteString(w, "fictional private response must not appear in diagnostic")
	}))
	defer server.Close()
	_, err := serviceGet(server.Client(), server.URL, "/debug/ready")
	if err == nil || !strings.Contains(err.Error(), "status=412") || strings.Contains(err.Error(), "fictional") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("response diagnostic was missing status or exposed body/URL: %v", err)
	}
	server.Close()
	_, err = serviceGet(server.Client(), server.URL, "/debug/ready")
	if err == nil || !strings.Contains(err.Error(), "connectionRefused=true") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("transport diagnostic was missing category or exposed URL: %v", err)
	}
}

func TestServiceTargetUsesOnlyOwnedPrivateEndpoint(t *testing.T) {
	check := func(raw string) (string, error) {
		var container imageContainer
		if err := json.Unmarshal([]byte(raw), &container); err != nil {
			t.Fatal(err)
		}
		return serviceTarget(container, "owned-network")
	}
	const good = `{"NetworkSettings":{"Networks":{"fixture":{"NetworkID":"owned-network","IPAddress":"172.18.0.2"}}}}`
	if target, err := check(good); err != nil || target != "172.18.0.2:8080" {
		t.Fatalf("owned internal address refused: %q %v", target, err)
	}
	for _, raw := range []string{
		`{}`, strings.Replace(good, "owned-network", "foreign-network", 1),
		strings.Replace(good, "172.18.0.2", "", 1),
		strings.Replace(good, "172.18.0.2", "127.0.0.1", 1),
		strings.Replace(good, "172.18.0.2", "169.254.169.254", 1),
		strings.Replace(good, "172.18.0.2", "8.8.8.8", 1),
		strings.Replace(good, "172.18.0.2", "172.18.0.2:80", 1),
		strings.Replace(good, "172.18.0.2", "::1", 1),
		strings.Replace(good, "172.18.0.2", "service.example.invalid", 1),
		`{"NetworkSettings":{"Networks":{"a":{"NetworkID":"owned-network","IPAddress":"172.18.0.2"},"b":{"NetworkID":"other","IPAddress":"172.19.0.2"}}}}`,
	} {
		if target, err := check(raw); err == nil || target != "" {
			t.Fatalf("unsafe fixture endpoint admitted: %q", target)
		}
	}
}

// TestServiceTransportRefusesUnrelatedAuthority rejects alternate authorities,
// ports and raw endpoints before the fixture transport can reach them.
func TestServiceTransportRefusesUnrelatedAuthority(t *testing.T) {
	transport := serviceTransport("172.18.0.2:8080")
	defer transport.CloseIdleConnections()
	for _, address := range []string{"elsewhere.invalid:8080", "fixture.localhost:80", "127.0.0.1:8080", "169.254.169.254:80"} {
		if connection, err := transport.DialContext(context.Background(), "tcp", address); err == nil || connection != nil {
			t.Fatalf("unrelated authority admitted: %q", address)
		}
	}
	if connection, err := transport.DialContext(context.Background(), "udp", serviceAuthority); err == nil || connection != nil {
		t.Fatal("non-HTTP transport admitted")
	}
}

func TestServiceTransportKeepsAuthorityWithoutDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != serviceAuthority || r.URL.Path != "/debug/ready" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "ready")
	}))
	defer server.Close()
	// The loopback endpoint belongs only to this unit test; the runtime obtains
	// its private endpoint through the separate owned-network admission above.
	transport := serviceTransport(strings.TrimPrefix(server.URL, "http://"))
	defer transport.CloseIdleConnections()
	b, err := serviceGet(&http.Client{Transport: transport}, "http://"+serviceAuthority, "/debug/ready")
	if err != nil || string(b) != "ready" {
		t.Fatalf("fixed connection lost the fictional HTTP authority: %q %v", b, err)
	}
}

// A service request must reach native-platform admission, rather than being
// mistaken for the existing version-only profile. Actual service acceptance is
// a separate mandatory native Linux CI invocation.
func TestExplicitIdentityServiceProfile(t *testing.T) {
	var out, diagnostic bytes.Buffer
	status := imageRun([]string{"--service-smoke", "--build", t.TempDir(), "--repository", t.TempDir(), "--output", t.TempDir()}, &out, &diagnostic)
	native := runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && runtime.Version() == "go1.26.10"
	if status == 0 || strings.Contains(diagnostic.String(), "unexpected argument") || (!native && !strings.Contains(diagnostic.String(), "native Linux amd64 Go 1.26.10 required")) {
		t.Fatalf("service request did not reach native admission: status=%d diagnostic=%q", status, diagnostic.String())
	}
}

// TestExplicitIdentityBrowserProfile verifies that the opt-in browser request
// reaches native-platform admission rather than an unknown-argument shortcut.
func TestExplicitIdentityBrowserProfile(t *testing.T) {
	var out, diagnostic bytes.Buffer
	status := imageRun([]string{"--browser-smoke", "--build", t.TempDir(), "--repository", t.TempDir(), "--output", t.TempDir()}, &out, &diagnostic)
	native := runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && runtime.Version() == "go1.26.10"
	if status == 0 || strings.Contains(diagnostic.String(), "unexpected argument") || (!native && !strings.Contains(diagnostic.String(), "native Linux amd64 Go 1.26.10 required")) {
		t.Fatalf("browser request did not reach native admission: status=%d diagnostic=%q", status, diagnostic.String())
	}
}

// A running service has a separate closed profile. These literal Docker fields
// must not weaken the existing network-none, no-mount version-command profile.
func TestServiceContainerAdmission(t *testing.T) {
	const raw = `[{"Id":"service-id","Image":"sha256:image","Config":{"Image":"sha256:image","User":"65532:65532","Entrypoint":["/app/zitadel"],"Cmd":["start-from-init","--masterkeyFromEnv","--tlsMode","disabled"],"WorkingDir":"/app","Labels":{"org.devantler-tech.identity-smoke-owner":"fixture-owner"}},"HostConfig":{"ReadonlyRootfs":true,"Privileged":false,"CapDrop":["ALL"],"SecurityOpt":["no-new-privileges"],"NetworkMode":"network-id","IpcMode":"private","PidsLimit":128,"Memory":1073741824,"NanoCpus":2000000000,"Tmpfs":{"/tmp":"rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700"}},"State":{"Status":"running","Running":true,"Pid":4321},"Mounts":[{"Type":"tmpfs","Destination":"/tmp","RW":true}],"NetworkSettings":{"Networks":{"fixture":{"NetworkID":"network-id"}}}}]`
	check := func(b []byte) error {
		_, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", "running")
		return err
	}
	if err := check([]byte(raw)); err != nil {
		t.Fatalf("valid service profile refused: %v", err)
	}
	// Legacy --tmpfs uses HostConfig.Tmpfs rather than MountPoints. The live
	// kernel mount must independently pass before service success is possible.
	var legacy []map[string]any
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy[0]["Mounts"] = nil
	legacyBytes, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(legacyBytes); err != nil {
		t.Fatalf("legacy Docker tmpfs metadata refused: %v", err)
	}
	for _, status := range []string{"created", "exited"} {
		var all []map[string]any
		if err := json.Unmarshal([]byte(raw), &all); err != nil {
			t.Fatal(err)
		}
		all[0]["State"] = map[string]any{"Status": status, "Running": false, "Pid": 0}
		all[0]["Mounts"] = nil
		all[0]["NetworkSettings"] = map[string]any{}
		b, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", status); err != nil {
			t.Fatalf("inactive container incorrectly required active kernel resources: %v", err)
		}
	}
	for name, mutation := range map[string]func(map[string]any){
		"wrong owner":   func(c map[string]any) { c["Config"].(map[string]any)["Labels"] = map[string]any{} },
		"wrong image":   func(c map[string]any) { c["Image"] = "sha256:other" },
		"root":          func(c map[string]any) { c["Config"].(map[string]any)["User"] = "0" },
		"version only":  func(c map[string]any) { c["Config"].(map[string]any)["Cmd"] = []string{"--version"} },
		"writable root": func(c map[string]any) { c["HostConfig"].(map[string]any)["ReadonlyRootfs"] = false },
		"privileged":    func(c map[string]any) { c["HostConfig"].(map[string]any)["Privileged"] = true },
		"publish all":   func(c map[string]any) { c["HostConfig"].(map[string]any)["PublishAllPorts"] = true },
		"unconfined": func(c map[string]any) {
			c["HostConfig"].(map[string]any)["SecurityOpt"] = []string{"no-new-privileges", "seccomp=unconfined"}
		},
		"extra tmpfs":  func(c map[string]any) { c["HostConfig"].(map[string]any)["Tmpfs"].(map[string]any)["/app"] = "rw" },
		"host network": func(c map[string]any) { c["HostConfig"].(map[string]any)["NetworkMode"] = "host" },
		"public bind": func(c map[string]any) {
			c["HostConfig"].(map[string]any)["PortBindings"] = map[string]any{"8080/tcp": []any{map[string]any{"HostIp": "0.0.0.0", "HostPort": "12345"}}}
		},
		"extra mount": func(c map[string]any) {
			c["Mounts"] = append(c["Mounts"].([]any), map[string]any{"Type": "bind", "Destination": "/etc", "RW": false})
		},
		"missing declared tmpfs": func(c map[string]any) { c["HostConfig"].(map[string]any)["Tmpfs"] = nil },
		"missing active network": func(c map[string]any) { c["NetworkSettings"] = map[string]any{} },
		"oom":                    func(c map[string]any) { c["State"].(map[string]any)["OOMKilled"] = true },
		"extra network": func(c map[string]any) {
			c["NetworkSettings"].(map[string]any)["Networks"].(map[string]any)["foreign"] = map[string]any{"NetworkID": "foreign"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var all []map[string]any
			if err := json.Unmarshal([]byte(raw), &all); err != nil {
				t.Fatal(err)
			}
			mutation(all[0])
			b, err := json.Marshal(all)
			if err != nil {
				t.Fatal(err)
			}
			if check(b) == nil {
				t.Fatal("unsafe service profile accepted")
			}
		})
	}
	if _, err := imageInspectContainer([]byte(raw), "service-id", "sha256:image", "running"); err == nil {
		t.Fatal("service profile weakened CLI-only admission")
	}
}

const serviceStatusFixture = `Name:	zitadel
Pid:	12345
NSpid:	12345	1
Uid:	65532	65532	65532	65532
Gid:	65532	65532	65532	65532
VmRSS:	47104 kB
VmHWM:	65536 kB
Threads:	12
CapInh:	0000000000000000
CapPrm:	0000000000000000
CapEff:	0000000000000000
CapBnd:	0000000000000000
CapAmb:	0000000000000000
NoNewPrivs:	1
Seccomp:	2
`

const serviceMountFixture = `100 99 0:101 / / ro,relatime - overlay overlay rw,lowerdir=/fictional/image
101 100 0:102 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
102 100 0:103 / /tmp rw,nosuid,nodev,noexec,relatime - tmpfs tmpfs rw,size=16384k,mode=700,uid=65532,gid=65532
`

func TestActualKernelRootAndApplicationTmpfs(t *testing.T) {
	m, err := serviceMountStatus([]byte(serviceMountFixture))
	if err != nil || !m.RootReadOnly || m.TmpfsBytes != 16777216 || m.UID != 65532 || m.GID != 65532 || m.Mode != "0700" || !m.NoExec || !m.NoSuid || !m.NoDev {
		t.Fatalf("valid kernel mount proof refused: %#v %v", m, err)
	}
	for name, change := range map[string][2]string{
		"writable root":         {"/ / ro,relatime", "/ / rw,relatime"},
		"missing tmpfs":         {"102 100 0:103 / /tmp", "102 100 0:103 / /elsewhere"},
		"bind instead of tmpfs": {"- tmpfs tmpfs", "- ext4 /dev/fixture"},
		"executable":            {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nosuid,nodev,relatime - tmpfs"},
		"suid":                  {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nodev,noexec,relatime - tmpfs"},
		"devices":               {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nosuid,noexec,relatime - tmpfs"},
		"wrong limit":           {"size=16384k", "size=32m"},
		"root-owned tmpfs":      {"uid=65532", "uid=0"},
		"wrong group":           {"gid=65532", "gid=0"},
		"world writable":        {"mode=700", "mode=777"},
		"missing uid":           {"uid=65532,", ""},
		"duplicate option":      {"uid=65532", "uid=65532,uid=0"},
	} {
		t.Run(name, func(t *testing.T) {
			b := strings.Replace(serviceMountFixture, change[0], change[1], 1)
			if b == serviceMountFixture {
				t.Fatal("unchanged mutation")
			}
			if _, err := serviceMountStatus([]byte(b)); err == nil {
				t.Fatal("unsafe kernel mount accepted")
			}
		})
	}
	for _, extra := range []string{"malformed\n", "103 100 0:104 / /tmp rw - tmpfs tmpfs rw\n", "103 102 0:104 / /tmp/hidden rw - ext4 /dev/fixture rw\n"} {
		if _, err := serviceMountStatus([]byte(serviceMountFixture + extra)); err == nil {
			t.Fatal("ambiguous or masked kernel mount accepted")
		}
	}
}

func TestActualProcessRestrictionsAndMeasuredMemory(t *testing.T) {
	p, err := serviceProcessStatus([]byte(serviceStatusFixture), 12345)
	if err != nil || p.PID != 12345 || p.Name != "zitadel" || p.UID != 65532 || p.GID != 65532 || p.RSSKiB != 47104 || p.PeakRSSKiB != 65536 || p.Threads != 12 || p.EffectiveCapabilities != "0000000000000000" || p.BoundingCapabilities != "0000000000000000" || !p.NoNewPrivileges || p.SeccompMode != 2 {
		t.Fatalf("valid measured process refused: %#v %v", p, err)
	}
	for name, replacement := range map[string][2]string{
		"root real uid":        {"65532\t65532\t65532\t65532", "0\t65532\t65532\t65532"},
		"effective root gid":   {"Gid:\t65532\t65532\t65532\t65532", "Gid:\t65532\t0\t65532\t65532"},
		"capability":           {"CapEff:\t0000000000000000", "CapEff:\t0000000000000001"},
		"bounding capability":  {"CapBnd:\t0000000000000000", "CapBnd:\t0000000000000001"},
		"privilege escalation": {"NoNewPrivs:\t1", "NoNewPrivs:\t0"},
		"no seccomp":           {"Seccomp:\t2", "Seccomp:\t0"},
		"not namespace init":   {"12345\t1", "12345\t2"},
		"wrong host pid":       {"Pid:\t12345", "Pid:\t12346"},
		"no resident memory":   {"VmRSS:\t47104 kB", "VmRSS:\t0 kB"},
		"missing peak":         {"VmHWM:\t65536 kB\n", ""},
		"wrong units":          {"47104 kB", "47104 MB"},
		"peak below rss":       {"65536 kB", "1024 kB"},
		"duplicate evidence":   {"Seccomp:\t2", "Seccomp:\t2\nSeccomp:\t0"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(serviceStatusFixture, replacement[0], replacement[1], 1)
			if changed == serviceStatusFixture {
				t.Fatal("negative fixture did not change")
			}
			if _, err := serviceProcessStatus([]byte(changed), 12345); err == nil {
				t.Fatal("unsafe or unmeasured process accepted")
			}
		})
	}
}

func TestLifecyclePhaseMountsSeparateBootstrapAuthority(t *testing.T) {
	owner := strings.Repeat("a", 32)
	root, pathError := filepath.EvalSymlinks(t.TempDir())
	if pathError != nil {
		t.Fatal(pathError)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	material, err := lifecycleMaterials(owner)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := lifecycleWriteInputs(root, material)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"init-user", "init", "setup", "serve", "init-wrong-ca", "init-wrong-host", "init-plaintext"} {
		t.Run(phase, func(t *testing.T) {
			args, err := lifecyclePhaseArgs(phase, root, bindings)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, "\n")
			if strings.Contains(joined, "--env") || strings.Contains(joined, "--masterkeyFromEnv") || strings.Contains(joined, "start-from-init") {
				t.Fatal("implicit authority in native phase")
			}
			if phase == "serve" && (strings.Contains(joined, "init.json") || strings.Contains(joined, "setup.json") || strings.Contains(joined, "steps.json") || strings.Contains(joined, "admin-client.env")) {
				t.Fatal("bootstrap authority reached ordinary service")
			}
			if phase == "setup" && (strings.Contains(joined, "init.json") || strings.Contains(joined, "admin-client.env")) {
				t.Fatal("database administrator reached setup")
			}
			if !strings.Contains(joined, "readonly") || !strings.Contains(joined, "/fixture/ca.pem") {
				t.Fatal("sealed trust input missing")
			}
			if phase == "init-user" && !strings.HasSuffix(joined, "init\nuser") {
				t.Fatal("interruption checkpoint is not the real native user phase")
			}
			if phase == "serve" && !strings.Contains(joined, "start\n--masterkeyFile\n/fixture/master.key\n--tlsMode\nenabled") {
				t.Fatal("ordinary native TLS start differs")
			}
		})
	}
	if _, err := lifecyclePhaseArgs("unknown", root, bindings); err == nil {
		t.Fatal("unknown native command accepted")
	}
	if err := os.Remove(filepath.Join(root, "steps.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecyclePhaseArgs("init", root, bindings); err == nil {
		t.Fatal("incomplete private profile reached the native defaulting command")
	}
}

func TestLifecycleIdentitySnapshotRejectsMissingOrChangedBootstrap(t *testing.T) {
	material, err := lifecycleMaterials(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"instances":1,"humans":1,"verified":0,"instanceId":"123","humanId":"456","instanceName":"Fictional lifecycle instance","userName":"` + material.human + `","email":"` + material.human + `","humanInstanceId":"123","instanceAggregateId":"123","instanceSequence":1,"humanSequence":2}`
	seal, err := lifecycleIdentitySnapshot([]byte(valid), material)
	if err != nil || len(seal) != 64 {
		t.Fatalf("complete fictional bootstrap was rejected: %v", err)
	}
	for _, mutation := range []struct{ from, to string }{
		{`"instances":1`, `"instances":0`}, {`"instances":1`, `"instances":2`},
		{`"humans":1`, `"humans":2`}, {`"verified":0`, `"verified":1`},
		{`"humanInstanceId":"123"`, `"humanInstanceId":"789"`},
		{`"instanceAggregateId":"123"`, `"instanceAggregateId":"789"`},
		{`"Fictional lifecycle instance"`, `"Default instance"`},
		{`"email":"` + material.human + `"`, `"email":"default@example.invalid"`},
		{`"userName":"` + material.human + `"`, `"userName":"default"`},
		{`"humanSequence":2`, `"humanSequence":0`},
	} {
		if _, err := lifecycleIdentitySnapshot([]byte(strings.Replace(valid, mutation.from, mutation.to, 1)), material); err == nil {
			t.Errorf("invalid bootstrap snapshot accepted: %s", mutation.from)
		}
	}
	changed := strings.Replace(valid, `"humanId":"456"`, `"humanId":"457"`, 1)
	if other, err := lifecycleIdentitySnapshot([]byte(changed), material); err != nil || seal == other {
		t.Fatal("replacement fictional user was not distinguishable after restart")
	}
	if _, err := lifecycleIdentitySnapshot([]byte(strings.TrimSuffix(valid, "}")+`,"passwordHash":"never retain"}`), material); err == nil {
		t.Fatal("unexpected private payload accepted")
	}
}

func TestLifecycleInspectionRejectsAuthorityAndMountChanges(t *testing.T) {
	owner, network, id, image := strings.Repeat("a", 32), strings.Repeat("b", 64), strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
	spec := lifecycleContainerSpec{image: image, imageID: image, user: "65532:65532", workdir: "/app", entrypoint: []string{"/app/zitadel"}, command: []string{"--config", "/fixture/serve.json", "start"}, tmpfs: map[string]string{"/tmp": serviceTmpfs}, memory: 1073741824, inputs: map[string]string{"/fixture/serve.json": "/private/serve.json"}}
	var c imageContainer
	c.ID, c.Image = id, image
	c.Config.Image, c.Config.User, c.Config.WorkingDir = image, spec.user, spec.workdir
	c.Config.Entrypoint, c.Config.Cmd = spec.entrypoint, spec.command
	c.Config.Labels = map[string]string{"org.devantler-tech.identity-smoke-owner": owner}
	c.HostConfig.ReadonlyRootfs = true
	c.HostConfig.CapDrop = []string{"ALL"}
	c.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	c.HostConfig.NetworkMode = network
	c.HostConfig.IpcMode = "private"
	c.HostConfig.PidsLimit = 128
	c.HostConfig.Memory = spec.memory
	c.HostConfig.NanoCpus = 2000000000
	c.HostConfig.Tmpfs = spec.tmpfs
	c.State.Status = "created"
	c.Mounts = []json.RawMessage{json.RawMessage(`{"Type":"bind","Source":"/private/serve.json","Destination":"/fixture/serve.json","RW":false}`)}
	encode := func(c imageContainer) []byte {
		b, e := json.Marshal([]imageContainer{c})
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	if _, err := lifecycleInspect(encode(c), id, owner, network, "created", spec); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*imageContainer){
		"writable root":             func(c *imageContainer) { c.HostConfig.ReadonlyRootfs = false },
		"host namespace":            func(c *imageContainer) { c.HostConfig.PidMode = "host" },
		"bootstrap command":         func(c *imageContainer) { c.Config.Cmd = []string{"start-from-init"} },
		"administrator environment": func(c *imageContainer) { c.Config.Env = []string{"ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD=fictional"} },
		"writable private input": func(c *imageContainer) {
			c.Mounts = []json.RawMessage{json.RawMessage(`{"Type":"bind","Source":"/private/serve.json","Destination":"/fixture/serve.json","RW":true}`)}
		},
		"extra bootstrap mount": func(c *imageContainer) {
			c.Mounts = append(c.Mounts, json.RawMessage(`{"Type":"bind","Source":"/private/steps.json","Destination":"/fixture/steps.json","RW":false}`))
		},
		"missing private input": func(c *imageContainer) { c.Mounts = nil },
		"unowned image":         func(c *imageContainer) { c.Image = "sha256:" + strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			var changed imageContainer
			if err := json.Unmarshal(encode(c), &changed); err == nil {
				t.Fatal("wrong fixture encoding")
			}
			var copy []imageContainer
			if err := json.Unmarshal(encode(c), &copy); err != nil {
				t.Fatal(err)
			}
			changed = copy[0]
			mutate(&changed)
			if _, err := lifecycleInspect(encode(changed), id, owner, network, "created", spec); err == nil {
				t.Fatal("unsafe effective container accepted")
			}
		})
	}
}

func TestLifecycleDatabaseProcessHasItsOwnIdentityAndMeasurements(t *testing.T) {
	status := strings.ReplaceAll(strings.Replace(serviceStatusFixture, "zitadel", "postgres", 1), "65532", "999")
	process, err := restrictedProcessStatus([]byte(status), 12345, "postgres", 999)
	if err != nil || process.Name != "postgres" || process.UID != 999 || process.RSSKiB != 47104 {
		t.Fatalf("actual database process could not be measured separately: %v", err)
	}
	for _, bad := range []string{strings.Replace(status, "999\t999\t999\t999", "0\t0\t0\t0", 1), strings.Replace(status, "postgres", "zitadel", 1), strings.Replace(status, "NoNewPrivs:\t1", "NoNewPrivs:\t0", 1)} {
		if _, err := restrictedProcessStatus([]byte(bad), 12345, "postgres", 999); err == nil {
			t.Fatal("wrong database process restrictions accepted")
		}
	}
}

func TestLifecycleResourceOutputIsNumericAndBounded(t *testing.T) {
	if err := lifecycleStats([]byte("98.4MiB / 1GiB|0.15%|8\n")); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "not observed", "98MiB / 1GiB|NaN%|8", "98MiB / 1GiB|1%|0", "98MiB / 1GiB|1%|129", "98MiB / 1GiB|1%|8|unexpected", "secret / value|1%|8", "98MiB / 1GiB|-1%|8"} {
		if lifecycleStats([]byte(raw)) == nil {
			t.Errorf("invalid component measurement accepted: %q", raw)
		}
	}
}

func TestLifecycleModeRemainsExplicitAndPlatformBound(t *testing.T) {
	var out, diagnostic bytes.Buffer
	for _, args := range [][]string{{"--lifecycle-smoke"}, {"--lifecycle-smoke", "--browser-smoke", "--build", "missing", "--repository", "missing", "--output", "missing"}, {"--lifecycle-smoke=false"}} {
		if imageRun(args, &out, &diagnostic) == 0 {
			t.Fatal("implicit or incomplete lifecycle invocation accepted")
		}
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || runtime.Version() != "go1.26.10" {
		diagnostic.Reset()
		if imageRun([]string{"--lifecycle-smoke", "--build", t.TempDir(), "--repository", t.TempDir(), "--output", t.TempDir()}, &out, &diagnostic) == 0 || !strings.Contains(diagnostic.String(), "native Linux amd64 Go 1.26.10 required") {
			t.Fatal("lifecycle mode does not use the actual native-platform guard")
		}
	}
}

func TestLifecycleCleanupRefusesReplacedPrivateDirectory(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "reserved")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	s := &lifecycleSession{root: root, rootInfo: original, specs: map[string]lifecycleContainerSpec{}}
	if err = os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "unrelated.txt")
	if err = os.WriteFile(foreign, []byte("preserve unrelated replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.cleanup(); err == nil {
		t.Fatal("cleanup accepted a replacement directory")
	}
	if _, err = os.Stat(foreign); err != nil {
		t.Fatal("cleanup removed unrelated replacement data")
	}
}

func TestLifecycleTLSFilesCanBeStagedAgainWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("the actual restart copy control requires a non-root process")
	}
	base := t.TempDir()
	inputs, output := filepath.Join(base, "inputs"), filepath.Join(base, "output")
	for _, directory := range []string{inputs, output} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"database.crt", "database.key"} {
		if err := os.WriteFile(filepath.Join(inputs, name), []byte("fictional "+name), 0444); err != nil {
			t.Fatal(err)
		}
	}
	stage := "umask 077\n" + strings.NewReplacer("/fixture/", inputs+"/", "/var/lib/postgresql/data/.fixture-tls/", output+"/").Replace(lifecycleDatabaseTLSStage)
	for trial := 0; trial < 2; trial++ {
		if _, err := exec.Command("sh", "-ec", stage).CombinedOutput(); err != nil {
			t.Fatalf("actual non-root TLS staging failed on start %d: %v", trial+1, err)
		}
	}
	for _, pair := range []struct{ input, output string }{{"database.crt", "server.crt"}, {"database.key", "server.key"}} {
		actual, err := os.ReadFile(filepath.Join(output, pair.output))
		if err != nil || string(actual) != "fictional "+pair.input {
			t.Fatal("staged TLS bytes changed")
		}
		info, err := os.Stat(filepath.Join(output, pair.output))
		if err != nil || info.Mode() != 0600 {
			t.Fatal("persistent TLS file has unsafe permissions")
		}
	}
}

func TestLifecycleCleanupRemovesOnlyItsSealedInputs(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(strconv.FormatBool(foreign), func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			original, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			material, err := lifecycleMaterials(strings.Repeat("b", 32))
			if err != nil {
				t.Fatal(err)
			}
			bindings, err := lifecycleWriteInputs(root, material)
			if err != nil {
				t.Fatal(err)
			}
			s := &lifecycleSession{root: root, rootInfo: original, bindings: bindings, specs: map[string]lifecycleContainerSpec{}}
			if foreign {
				if err = os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err = s.cleanup()
			if foreign {
				if err == nil {
					t.Fatal("cleanup admitted unknown private-directory contents")
				}
				if _, e := os.Stat(filepath.Join(root, "unrelated.txt")); e != nil {
					t.Fatal("unknown private-directory contents were removed")
				}
			} else if err != nil {
				t.Fatal(err)
			} else if _, e := os.Stat(root); !os.IsNotExist(e) {
				t.Fatal("owned private directory was not removed")
			}
		})
	}
}
