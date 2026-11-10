package customeridentity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// materialCompareOutput retains complete membership and byte checks for both input transports.
func materialCompareOutput(t *testing.T, expected, actual string) {
	t.Helper()
	wantEntries, wantErr := os.ReadDir(expected)
	gotEntries, gotErr := os.ReadDir(actual)
	if wantErr != nil || gotErr != nil || len(wantEntries) != len(gotEntries) {
		t.Fatal("native output membership differs from the maintained producer")
	}
	for index, entry := range wantEntries {
		if entry.Name() != gotEntries[index].Name() {
			t.Fatal("native output member differs from the maintained producer")
		}
		want, wantErr := os.ReadFile(filepath.Join(expected, entry.Name()))
		got, gotErr := os.ReadFile(filepath.Join(actual, entry.Name()))
		if wantErr != nil || gotErr != nil || !bytes.Equal(want, got) {
			t.Fatal("native output bytes differ from the maintained producer")
		}
	}
}

// materialFixture creates private fictional inputs, never an actual identity.
func materialFixture(t *testing.T, phase string) (string, string) {
	t.Helper()
	if phase == dexServePhase {
		return dexFixture(t)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(parent, "input"), filepath.Join(parent, "output")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"schemaVersion": "customer-zitadel-private-v1",
		"dexIssuer":     "https://login.example.invalid", "zitadelIssuer": "https://accounts.example.invalid",
		"portalCallback": "https://site.example.invalid/portal/callback",
		"databaseHost":   "database.example.invalid", "databaseName": "customer_zitadel", "databaseUser": "identity_runtime",
	}
	if phase == "init" {
		settings["databaseAdminUser"] = "identity_initializer"
	}
	if phase == "setup" || phase == "setup-mail" {
		for key, value := range map[string]string{"instanceName": "Fictional identity", "organizationName": "Fictional operator", "humanUsername": "fictional_operator", "humanFirstName": "Fictional", "humanLastName": "Operator", "humanEmail": "operator@example.invalid"} {
			settings[key] = value
		}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	materialWrite(t, input, "settings.json", data)
	materialWrite(t, input, "database-password", materialPassword(t))
	ca, cert, key := materialCertificate(t, "accounts.example.invalid", time.Now().Add(time.Hour))
	materialWrite(t, input, "database-ca.pem", ca)
	if phase == "init" {
		materialWrite(t, input, "database-admin-password", materialPassword(t))
	}
	if phase != "init" {
		master := make([]byte, 32)
		if _, err := rand.Read(master); err != nil {
			t.Fatal(err)
		}
		materialWrite(t, input, "master.key", master)
		materialWrite(t, input, "tls.crt", cert)
		materialWrite(t, input, "tls.key", key)
		materialWrite(t, input, "service-ca.pem", ca)
	}
	if phase == "setup" || phase == "setup-mail" {
		materialWrite(t, input, "bootstrap-password", materialPassword(t))
	}
	if phase == "setup-mail" {
		materialMailInputs(t, input)
	}
	return input, output
}

// materialPassword generates a fictional password satisfying the native bootstrap rules.
func materialPassword(t *testing.T) []byte {
	t.Helper()
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return []byte(hex.EncodeToString(value) + "Aa1!")
}

// materialWrite preserves owner-only permissions when creating or replacing fixture inputs.
func materialWrite(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// materialFixtureSettings reads the fixture document for one isolated adverse mutation.
func materialFixtureSettings(t *testing.T, directory string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// materialCertificate creates an independent fictional CA and server identity for TLS controls.
func materialCertificate(t *testing.T, hostname string, expiry time.Time) ([]byte, []byte, []byte) {
	t.Helper()
	caPublic, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fictional test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caPublic, caKey)
	if err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{hostname}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	certDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, public, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// materialAssertOutput checks the documented phase boundary independently of the producer.
func materialAssertOutput(t *testing.T, output, phase string) {
	t.Helper()
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private output reservation missing")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	var members []string
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("unsafe output member")
		}
		members = append(members, entry.Name())
	}
	want, known := map[string][]string{
		"init":       {"ca.pem", "config.json"},
		"setup-mail": {"ca.pem", "config.json", "master.key", "steps.json", "tls.crt", "tls.key"},
		"setup":      {"ca.pem", "config.json", "master.key", "steps.json", "tls.crt", "tls.key"},
		"serve":      {"ca.pem", "config.json", "master.key", "tls.crt", "tls.key"},
	}[phase]
	if !known || !slices.Equal(members, want) {
		t.Fatalf("phase %s emitted unexpected members: %v", phase, members)
	}
}

// TestMaterializeNativePhaseSeparation pins native fields and excludes cross-phase authority.
func TestMaterializeNativePhaseSeparation(t *testing.T) {
	for _, phase := range []string{"init", "setup", "setup-mail", "serve"} {
		t.Run(phase, func(t *testing.T) {
			input, output := materialFixture(t, phase)
			if err := Materialize(phase, input, output); err != nil {
				t.Fatal(err)
			}
			materialAssertOutput(t, output, phase)
			var config map[string]json.RawMessage
			raw, err := os.ReadFile(filepath.Join(output, "config.json"))
			if err != nil || json.Unmarshal(raw, &config) != nil {
				t.Fatal("native config missing")
			}
			var defaults map[string]json.RawMessage
			if json.Unmarshal(config["DefaultInstance"], &defaults) != nil {
				t.Fatal("native defaults missing")
			}
			if _, hasMail := defaults["SMTPConfiguration"]; hasMail != (phase == "setup-mail") {
				t.Fatal("mail settings crossed the selected setup profile")
			}
			var database struct {
				Postgres struct {
					DSN, Host, Database string
					User, Admin         struct {
						Username, Password string
						SSL                struct{ Mode, RootCert string }
					}
				} `json:"postgres"`
			}
			if err := json.Unmarshal(config["Database"], &database); err != nil {
				t.Fatal(err)
			}
			if database.Postgres.DSN != "" || database.Postgres.Host != "database.example.invalid" || database.Postgres.User.SSL.Mode != "verify-full" || database.Postgres.User.SSL.RootCert != "/run/customer-zitadel/ca.pem" {
				t.Fatal("native verified TLS profile differs")
			}
			if (database.Postgres.Admin.Username != "") != (phase == "init") {
				t.Fatal("administrator authority crossed phase")
			}
			if phase == "setup" || phase == "setup-mail" {
				steps, err := os.ReadFile(filepath.Join(output, "steps.json"))
				if err != nil {
					t.Fatal(err)
				}
				var native struct {
					FirstInstance struct {
						Skip bool
						Org  struct {
							Human struct {
								Email                  struct{ Verified bool }
								PasswordChangeRequired bool
								UserName, Password     string
							}
						}
					}
				}
				if json.Unmarshal(steps, &native) != nil || native.FirstInstance.Skip || native.FirstInstance.Org.Human.Email.Verified || !native.FirstInstance.Org.Human.PasswordChangeRequired || native.FirstInstance.Org.Human.UserName == "" || native.FirstInstance.Org.Human.Password == "" {
					t.Fatal("explicit fresh unverified human required")
				}
			} else if _, err := os.Lstat(filepath.Join(output, "steps.json")); !os.IsNotExist(err) {
				t.Fatal("unexpected bootstrap steps")
			}
			if phase == "serve" && strings.Contains(string(raw), "identity_initializer") {
				t.Fatal("bootstrap identity in ordinary config")
			}
		})
	}
}

// TestMaterializeRefusesIncompleteOrUnsafeInputs requires refusal before output reservation.
func TestMaterializeRefusesIncompleteOrUnsafeInputs(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing password": func(t *testing.T, p string) {
			if err := os.Remove(filepath.Join(p, "database-password")); err != nil {
				t.Fatal(err)
			}
		},
		"empty password": func(t *testing.T, p string) { materialWrite(t, p, "database-password", nil) },
		"unresolved password": func(t *testing.T, p string) {
			materialWrite(t, p, "database-password", []byte("${CUSTOMER_DATABASE_PASSWORD}"))
		},
		"short master": func(t *testing.T, p string) { materialWrite(t, p, "master.key", []byte("short")) },
		"invalid CA":   func(t *testing.T, p string) { materialWrite(t, p, "database-ca.pem", []byte("invalid")) },
		"wrong hostname": func(t *testing.T, p string) {
			ca, crt, key := materialCertificate(t, "other.example.invalid", time.Now().Add(time.Hour))
			materialWrite(t, p, "service-ca.pem", ca)
			materialWrite(t, p, "tls.crt", crt)
			materialWrite(t, p, "tls.key", key)
		},
		"expired certificate": func(t *testing.T, p string) {
			ca, crt, key := materialCertificate(t, "accounts.example.invalid", time.Now().Add(-time.Minute))
			materialWrite(t, p, "service-ca.pem", ca)
			materialWrite(t, p, "tls.crt", crt)
			materialWrite(t, p, "tls.key", key)
		},
		"wrong key": func(t *testing.T, p string) {
			_, _, key := materialCertificate(t, "accounts.example.invalid", time.Now().Add(time.Hour))
			materialWrite(t, p, "tls.key", key)
		},
		"extra admin credential": func(t *testing.T, p string) { materialWrite(t, p, "database-admin-password", materialPassword(t)) },
		"bootstrap in serve":     func(t *testing.T, p string) { materialWrite(t, p, "steps.json", []byte("{}")) },
		"unsafe mode": func(t *testing.T, p string) {
			if err := os.Chmod(filepath.Join(p, "database-password"), 0644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, p string) {
			if err := os.Remove(filepath.Join(p, "database-password")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("master.key", filepath.Join(p, "database-password")); err != nil {
				t.Fatal(err)
			}
		},
		"hard link": func(t *testing.T, p string) {
			if err := os.Remove(filepath.Join(p, "database-password")); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(filepath.Join(p, "master.key"), filepath.Join(p, "database-password")); err != nil {
				t.Fatal(err)
			}
		},
		"oversize": func(t *testing.T, p string) { materialWrite(t, p, "settings.json", make([]byte, 65537)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input, output := materialFixture(t, "serve")
			change(t, input)
			if err := Materialize("serve", input, output); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("refusal created output")
			}
		})
	}
}

// TestMaterializeRejectsAmbiguousSettings exercises duplicate, unknown and mistyped JSON fields.
func TestMaterializeRejectsAmbiguousSettings(t *testing.T) {
	for _, suffix := range []string{`,"databaseUser":"other"}`, `,"admin":"forbidden"}`, `,"databaseHost":true}`, `} {}`, `,"databaseAdminUser":"identity_initializer"}`, `,"phase":"setup"}`} {
		t.Run(suffix, func(t *testing.T) {
			input, output := materialFixture(t, "serve")
			data := materialFixtureSettings(t, input)
			data = append(data[:len(data)-1], suffix...)
			materialWrite(t, input, "settings.json", data)
			if err := Materialize("serve", input, output); err == nil {
				t.Fatal("ambiguous input accepted")
			}
		})
	}
}

// TestMaterializePreservesOccupiedOutput protects content that this invocation did not create.
func TestMaterializePreservesOccupiedOutput(t *testing.T) {
	input, output := materialFixture(t, "serve")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	materialWrite(t, output, "keep", []byte("foreign content"))
	if err := Materialize("serve", input, output); err == nil {
		t.Fatal("occupied output accepted")
	}
	data, err := os.ReadFile(filepath.Join(output, "keep"))
	if err != nil || string(data) != "foreign content" {
		t.Fatal("foreign content changed")
	}
}

// TestMaterializeRejectsInvalidNativeSettings refuses unsafe database roles and issuer settings.
func TestMaterializeRejectsInvalidNativeSettings(t *testing.T) {
	for name, value := range map[string]any{"databaseUser": "postgres", "databaseName": "postgres", "databaseHost": true, "schemaVersion": "unknown", "portalCallback": "http://site.example.invalid/portal/callback"} {
		t.Run(name, func(t *testing.T) {
			input, output := materialFixture(t, "serve")
			data := materialFixtureSettings(t, input)
			var settings map[string]any
			if json.Unmarshal(data, &settings) != nil {
				t.Fatal("bad fixture")
			}
			settings[name] = value
			data, err := json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			materialWrite(t, input, "settings.json", data)
			if Materialize("serve", input, output) == nil {
				t.Fatal("invalid native setting accepted")
			}
		})
	}
}

// TestMaterializeRequiresEveryPhaseMemberAndSetting removes each required input independently.
func TestMaterializeRequiresEveryPhaseMemberAndSetting(t *testing.T) {
	for _, phase := range []string{"init", "setup", "setup-mail", "serve"} {
		input, _ := materialFixture(t, phase)
		entries, err := os.ReadDir(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			t.Run(phase+"/missing/"+entry.Name(), func(t *testing.T) {
				p, out := materialFixture(t, phase)
				if err := os.Remove(filepath.Join(p, entry.Name())); err != nil {
					t.Fatal(err)
				}
				if Materialize(phase, p, out) == nil {
					t.Fatal("missing input accepted")
				}
			})
		}
		data := materialFixtureSettings(t, input)
		var settings map[string]string
		if json.Unmarshal(data, &settings) != nil {
			t.Fatal("bad fixture")
		}
		for key := range settings {
			t.Run(phase+"/setting/"+key, func(t *testing.T) {
				p, out := materialFixture(t, phase)
				remaining := map[string]string{}
				for k, v := range settings {
					if k != key {
						remaining[k] = v
					}
				}
				raw, err := json.Marshal(remaining)
				if err != nil {
					t.Fatal(err)
				}
				materialWrite(t, p, "settings.json", raw)
				if Materialize(phase, p, out) == nil {
					t.Fatal("missing setting accepted")
				}
			})
		}
	}
}

// TestMaterializeRefusesChangedSnapshotAndLeavesNoUsablePackage checks the final commit boundary.
func TestMaterializeRefusesChangedSnapshotAndLeavesNoUsablePackage(t *testing.T) {
	input, output := materialFixture(t, "serve")
	names, fields, err := materialProfile("serve")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := materialRead(input, names)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	settings, err := materialSettings(snapshot.files["settings.json"], fields)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := materialDocuments("serve", settings, snapshot.files)
	if err != nil {
		t.Fatal(err)
	}
	materialWrite(t, input, "database-password", materialPassword(t))
	if snapshot.unchanged() == nil {
		t.Fatal("changed snapshot accepted")
	}
	if materialWriteOutputs(output, outputs, snapshot) == nil {
		t.Fatal("changed input committed output")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("failed reservation retains usable inputs")
	}
}

// TestMaterializeCompiledCommand exercises real argument handling, phase output and safe diagnostics.
func TestMaterializeCompiledCommand(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "customer-identity-materialize")
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "../../cmd/customer-identity-materialize")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("command build failed: %v %s", err, data)
	}
	if data, err := exec.Command(binary).CombinedOutput(); err != nil || len(data) != 0 {
		t.Fatal("ordinary command is not inert")
	}
	for _, phase := range []string{"init", "setup", "setup-mail", "serve"} {
		t.Run(phase, func(t *testing.T) {
			input, output := materialFixture(t, phase)
			if data, err := exec.Command(binary, "--phase", phase, "--inputs", input, "--output", output).CombinedOutput(); err != nil || len(data) != 0 {
				t.Fatal("compiled private phase failed or emitted data")
			}
			materialAssertOutput(t, output, phase)
		})
	}
	for _, args := range [][]string{{"--phase"}, {"--phase", "serve", "--inputs", "/sensitive-input", "--output", "/sensitive-output"}, {"--phase", "serve", "--phase", "setup", "--output", "/sensitive-output"}} {
		data, err := exec.Command(binary, args...).CombinedOutput()
		if err == nil || string(data) != "customer identity private material refused\n" {
			t.Fatal("command refusal reflected or accepted private input")
		}
	}
}

// TestMaterializeBootstrapMatchesNativePasswordConsumer pins ASCII complexity and bcrypt limits.
func TestMaterializeBootstrapMatchesNativePasswordConsumer(t *testing.T) {
	cases := map[string]struct {
		password string
		allowed  bool
	}{
		"72 bytes":               {strings.Repeat("a", 69) + "A1!", true},
		"73 bytes":               {strings.Repeat("a", 70) + "A1!", false},
		"unicode digit only":     {strings.Repeat("a", 30) + "A١!", false},
		"unicode uppercase only": {strings.Repeat("a", 30) + "Ä1!", false},
		"unicode lowercase only": {strings.Repeat("A", 30) + "ä1!", false},
		"native unicode symbol":  {strings.Repeat("a", 30) + "A1é", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			input, output := materialFixture(t, "setup")
			materialWrite(t, input, "bootstrap-password", []byte(tc.password))
			err := Materialize("setup", input, output)
			if (err == nil) != tc.allowed {
				t.Fatal("bootstrap preflight differs from native password consumer")
			}
		})
	}
	if !materialPasswordValid([]byte(strings.Repeat("a", 100) + "A1!")) {
		t.Fatal("database credential inherited bootstrap hash limit")
	}
}

type materialListingProbe struct {
	file  *os.File
	limit int
}

// ReadDir records the producer's bound without replacing the filesystem listing.
func (probe *materialListingProbe) ReadDir(limit int) ([]os.DirEntry, error) {
	probe.limit = limit
	return probe.file.ReadDir(limit)
}

// TestMaterializeBoundsDirectoryListingBeforeRefusal prevents unbounded input-directory reads.
func TestMaterializeBoundsDirectoryListingBeforeRefusal(t *testing.T) {
	directory := t.TempDir()
	for index := range 100 {
		if err := os.WriteFile(filepath.Join(directory, strings.Repeat("a", index+1)), []byte("fictional"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	probe := &materialListingProbe{file: file}
	if materialDirectoryMembers(probe, []string{"one", "two"}) == nil {
		t.Fatal("extra members accepted")
	}
	if probe.limit < 1 || probe.limit > 3 {
		t.Fatal("directory read exceeded expected membership plus one")
	}
}

// TestMaterializeRefusesUnresolvedRawMasterKey distinguishes exact-length references from key bytes.
func TestMaterializeRefusesUnresolvedRawMasterKey(t *testing.T) {
	input, output := materialFixture(t, "serve")
	reference := []byte("${" + strings.Repeat("A", 29) + "}")
	if len(reference) != 32 {
		t.Fatal("invalid reference control")
	}
	materialWrite(t, input, "master.key", reference)
	if Materialize("serve", input, output) == nil {
		t.Fatal("unresolved reference accepted as raw master key")
	}
}
