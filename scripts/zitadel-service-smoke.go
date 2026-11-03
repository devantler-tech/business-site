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
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const serviceAuthority = "fixture.localhost:8080"

const lifecycleAuthority = "identity.fixture.localhost:8080"

// lifecycleInput keeps private fixture bytes in memory for integrity comparison;
// credentials are neither hashed as password verifiers nor serialized.
type lifecycleInput struct {
	contents []byte
	Mode     os.FileMode
}

// lifecycleMaterial keeps generated fictional authority private to the fixture;
// its unexported fields are not a publication or runtime configuration API.
type lifecycleMaterial struct {
	files                       map[string][]byte
	admin, app, database, human string
}

// lifecycleWriteInputs requires the complete closed profile and a fresh private
// directory before writing any input that native setup could otherwise default.
func lifecycleWriteInputs(root string, material lifecycleMaterial) (map[string]lifecycleInput, error) {
	bad := func() (map[string]lifecycleInput, error) {
		return nil, fmt.Errorf("fresh complete private lifecycle input directory required")
	}
	if _, err := lifecyclePrivateRoot(root); err != nil {
		return bad()
	}
	names := []string{"ca.pem", "service.crt", "service.key", "database.crt", "database.key", "master.key", "untrusted-ca.pem", "init.json", "setup.json", "serve.json", "init-wrong-ca.json", "init-wrong-host.json", "init-plaintext.json", "steps.json", "database.env", "admin-client.env", "app-client.env", "database-hba.conf"}
	if len(material.files) != len(names) {
		return bad()
	}
	bindings := map[string]lifecycleInput{}
	for _, name := range names {
		data, found := material.files[name]
		if !found || len(data) == 0 || len(data) > 65536 {
			return bad()
		}
		mode := os.FileMode(0444)
		if strings.HasSuffix(name, ".env") {
			mode = 0600
		}
		bindings[name] = lifecycleInput{contents: bytes.Clone(data), Mode: mode}
	}
	directory, err := os.Open(root)
	if err != nil {
		return bad()
	}
	entries, readError := directory.ReadDir(1)
	closeError := directory.Close()
	if len(entries) != 0 || readError != io.EOF || closeError != nil {
		return bad()
	}
	for _, name := range names {
		if err = imageWrite(root, name, material.files[name], bindings[name].Mode); err != nil {
			return bad()
		}
	}
	if err = lifecycleValidateInputs(root, bindings); err != nil {
		return bad()
	}
	return bindings, nil
}

// lifecycleMaterials generates fresh fictional secrets and a local CA, separating
// initialization authority, bootstrap steps and the ordinary serving document.
func lifecycleMaterials(owner string) (lifecycleMaterial, error) {
	bad := func() (lifecycleMaterial, error) {
		return lifecycleMaterial{}, fmt.Errorf("fresh complete fictional lifecycle material required")
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owner) {
		return bad()
	}
	m := lifecycleMaterial{files: map[string][]byte{}, admin: "bootstrap_" + owner[:16], app: "identity_" + owner[:16], database: "identity_" + owner[:16], human: "operator_" + owner[:16] + "@example.invalid"}
	password := func() (string, error) {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Fixture!A1%x", entropy), nil
	}
	adminPassword, err := password()
	if err != nil {
		return bad()
	}
	appPassword, err := password()
	if err != nil {
		return bad()
	}
	humanPassword, err := password()
	if err != nil {
		return bad()
	}
	master := make([]byte, 32)
	if _, err = rand.Read(master); err != nil {
		return bad()
	}
	m.files["master.key"] = master
	serial := func() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return bad()
	}
	caSerial, err := serial()
	if err != nil || caSerial.Sign() == 0 {
		return bad()
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "Fictional identity lifecycle CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(6 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caKey.Public(), caKey)
	if err != nil {
		return bad()
	}
	m.files["ca.pem"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	for _, endpoint := range []struct{ prefix, hostname string }{{"service", "identity.fixture.localhost"}, {"database", "database.fixture.localhost"}} {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return bad()
		}
		number, e := serial()
		if e != nil || number.Sign() == 0 {
			return bad()
		}
		leaf := &x509.Certificate{SerialNumber: number, DNSNames: []string{endpoint.hostname}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(6 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, e := x509.CreateCertificate(rand.Reader, leaf, ca, key.Public(), caKey)
		if e != nil {
			return bad()
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return bad()
		}
		m.files[endpoint.prefix+".crt"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		m.files[endpoint.prefix+".key"] = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	}
	// The negative connection probe trusts an unrelated, fresh CA, never the host store.
	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return bad()
	}
	wrongSerial, err := serial()
	if err != nil || wrongSerial.Sign() == 0 {
		return bad()
	}
	wrong := *ca
	wrong.SerialNumber = wrongSerial
	wrong.Subject.CommonName = "Untrusted fictional lifecycle CA"
	wrongDER, err := x509.CreateCertificate(rand.Reader, &wrong, &wrong, wrongKey.Public(), wrongKey)
	if err != nil {
		return bad()
	}
	m.files["untrusted-ca.pem"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: wrongDER})
	config := func(initialization bool, host, root, mode string) map[string]any {
		ssl := func() map[string]any { return map[string]any{"Mode": mode, "RootCert": root, "Cert": "", "Key": ""} }
		admin := map[string]any{"ExistingDatabase": "postgres", "Username": "", "Password": "", "SSL": ssl()}
		if initialization {
			admin["Username"], admin["Password"] = m.admin, adminPassword
		}
		return map[string]any{
			"Port": 8080, "ExternalDomain": "identity.fixture.localhost", "ExternalPort": 8080, "ExternalSecure": true,
			"TLS":             map[string]any{"Enabled": true, "CertPath": "/fixture/service.crt", "KeyPath": "/fixture/service.key"},
			"Database":        map[string]any{"postgres": map[string]any{"DSN": "", "Host": host, "Port": 5432, "Database": m.database, "User": map[string]any{"Username": m.app, "Password": appPassword, "SSL": ssl()}, "Admin": admin}},
			"DefaultInstance": map[string]any{"LoginPolicy": map[string]any{"AllowUsernamePassword": true, "AllowRegister": false, "AllowExternalIDP": false, "ForceMFA": true}, "Restrictions": map[string]bool{"DisallowPublicOrgRegistration": true}, "Features": map[string]any{"LoginV2": map[string]bool{"Required": false}}, "DomainPolicy": map[string]bool{"UserLoginMustBeDomain": false}},
			"Telemetry":       map[string]bool{"Enabled": false}, "ServicePing": map[string]bool{"Enabled": false},
			"Machine": map[string]any{"Identification": map[string]any{"Webhook": map[string]bool{"Enabled": false}, "PrivateIP": map[string]bool{"Enabled": false}, "Hostname": map[string]bool{"Enabled": true}}},
			"Log":     map[string]string{"Level": "warn"},
		}
	}
	for name, document := range map[string]map[string]any{
		"init.json":            config(true, "database.fixture.localhost", "/fixture/ca.pem", "verify-full"),
		"setup.json":           config(false, "database.fixture.localhost", "/fixture/ca.pem", "verify-full"),
		"serve.json":           config(false, "database.fixture.localhost", "/fixture/ca.pem", "verify-full"),
		"init-wrong-ca.json":   config(true, "database.fixture.localhost", "/fixture/untrusted-ca.pem", "verify-full"),
		"init-wrong-host.json": config(true, "other.database.fixture.localhost", "/fixture/ca.pem", "verify-full"),
		"init-plaintext.json":  config(true, "database.fixture.localhost", "/fixture/ca.pem", "disable"),
	} {
		m.files[name], err = json.Marshal(document)
		if err != nil {
			return bad()
		}
	}
	m.files["steps.json"], err = json.Marshal(map[string]any{"FirstInstance": map[string]any{"Skip": false, "InstanceName": "Fictional lifecycle instance", "Org": map[string]any{"Name": "Fictional lifecycle organization", "Human": map[string]any{"UserName": m.human, "FirstName": "Fictional", "LastName": "Operator", "NickName": "", "DisplayName": "Fictional lifecycle operator", "PreferredLanguage": "en", "Gender": 0, "Email": map[string]any{"Address": m.human, "Verified": false}, "Password": humanPassword, "PasswordChangeRequired": true}}}})
	if err != nil {
		return bad()
	}
	m.files["database.env"] = []byte("POSTGRES_USER=" + m.admin + "\nPOSTGRES_PASSWORD=" + adminPassword + "\nPGDATA=/var/lib/postgresql/data/pgdata\nPOSTGRES_INITDB_ARGS=--auth-host=scram-sha-256\n")
	client := func(user, pass, database string) []byte {
		return []byte("PGHOST=database.fixture.localhost\nPGPORT=5432\nPGUSER=" + user + "\nPGPASSWORD=" + pass + "\nPGDATABASE=" + database + "\nPGSSLMODE=verify-full\nPGSSLROOTCERT=/fixture/ca.pem\nPGCONNECT_TIMEOUT=5\nPGAPPNAME=fictional-lifecycle-probe\n")
	}
	m.files["admin-client.env"] = client(m.admin, adminPassword, "postgres")
	m.files["app-client.env"] = client(m.app, appPassword, m.database)
	m.files["database-hba.conf"] = []byte("local all all trust\nhostnossl all all 0.0.0.0/0 reject\nhostnossl all all ::/0 reject\nhostssl all all 0.0.0.0/0 scram-sha-256\nhostssl all all ::/0 scram-sha-256\n")
	return m, nil
}

// lifecyclePrivateRoot admits only the caller's physical, private directory.
func lifecyclePrivateRoot(root string) (os.FileInfo, error) {
	bad := func() (os.FileInfo, error) { return nil, fmt.Errorf("owned private lifecycle directory required") }
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bad()
	}
	physical, err := filepath.EvalSymlinks(root)
	before, statError := os.Lstat(root)
	if err != nil || statError != nil || physical != root || before.Mode() != os.ModeDir|0700 {
		return bad()
	}
	owner, ok := before.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) {
		return bad()
	}
	return before, nil
}

// lifecycleValidateInputs rechecks complete membership, ownership, modes and byte
// bindings; native configuration semantics remain a separate execution check.
func lifecycleValidateInputs(root string, expected map[string]lifecycleInput) error {
	bad := func() error { return fmt.Errorf("complete owned private lifecycle inputs required") }
	if len(expected) < 1 || len(expected) > 32 {
		return bad()
	}
	before, err := lifecyclePrivateRoot(root)
	if err != nil {
		return bad()
	}
	checkMembers := func() error {
		directory, e := os.Open(root)
		if e != nil {
			return bad()
		}
		entries, readError := directory.ReadDir(33)
		extra, endError := directory.ReadDir(1)
		closeError := directory.Close()
		if (readError != nil && readError != io.EOF) || len(entries) != len(expected) || len(extra) != 0 || endError != io.EOF || closeError != nil {
			return bad()
		}
		for _, entry := range entries {
			if _, found := expected[entry.Name()]; !found {
				return bad()
			}
		}
		return nil
	}
	if err = checkMembers(); err != nil {
		return err
	}
	for name, binding := range expected {
		if !regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`).MatchString(name) || len(binding.contents) < 1 || len(binding.contents) > 65536 || (binding.Mode != 0444 && binding.Mode != 0600) {
			return bad()
		}
		path := filepath.Join(root, name)
		entry, e := os.Lstat(path)
		if e != nil || entry.Mode() != binding.Mode {
			return bad()
		}
		metadata, valid := entry.Sys().(*syscall.Stat_t)
		if !valid || metadata.Uid != uint32(os.Getuid()) || metadata.Nlink != 1 {
			return bad()
		}
		data, e := imageRead(root, name, 65536)
		after, afterError := os.Lstat(path)
		if e != nil || afterError != nil || !os.SameFile(entry, after) || entry.Size() != after.Size() || !entry.ModTime().Equal(after.ModTime()) || after.Mode() != binding.Mode || !bytes.Equal(data, binding.contents) {
			return bad()
		}
	}
	if err = checkMembers(); err != nil {
		return err
	}
	after, err := os.Lstat(root)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
		return bad()
	}
	return nil
}

// lifecycleTransport confines even plaintext or redirected request attempts to
// the fixture's explicitly verified HTTPS authority.
type lifecycleTransport struct{ transport *http.Transport }

// RoundTrip rejects authority changes before the underlying transport can dial.
func (t *lifecycleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL == nil || request.URL.Scheme != "https" || request.URL.Host != lifecycleAuthority || request.URL.User != nil || (request.Host != "" && request.Host != lifecycleAuthority) {
		return nil, fmt.Errorf("lifecycle request escaped its verified authority")
	}
	return t.transport.RoundTrip(request)
}

// CloseIdleConnections releases the retained fixture transport's connections.
func (t *lifecycleTransport) CloseIdleConnections() { t.transport.CloseIdleConnections() }

// The native caller selects the inspected private endpoint with serviceTarget.
// Loopback is also admitted here for real TLS unit fixtures; no DNS or ambient
// proxy participates, and this transport cannot select an endpoint itself.
func lifecycleTLSClient(target string, roots []byte) (*http.Client, error) {
	host, port, err := net.SplitHostPort(target)
	ip := net.ParseIP(host)
	p, portError := strconv.Atoi(port)
	if err != nil || portError != nil || p < 1 || p > 65535 || strconv.Itoa(p) != port || ip == nil || ip.To4() == nil || ip.String() != host || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return nil, fmt.Errorf("lifecycle numeric fixture endpoint required")
	}
	if len(roots) == 0 || len(roots) > 32768 {
		return nil, fmt.Errorf("one bounded lifecycle trust root required")
	}
	trimmed := bytes.TrimSpace(roots)
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 || !bytes.Equal(trimmed, bytes.TrimSpace(pem.EncodeToMemory(block))) {
		return nil, fmt.Errorf("one complete lifecycle trust root required")
	}
	root, err := x509.ParseCertificate(block.Bytes)
	now := time.Now()
	if err != nil || !root.IsCA || !root.BasicConstraintsValid || root.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(root.NotBefore) || !now.Before(root.NotAfter) || root.CheckSignatureFrom(root) != nil {
		return nil, fmt.Errorf("valid self-signed lifecycle CA required")
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "identity.fixture.localhost", MinVersion: tls.VersionTLS13},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != lifecycleAuthority {
				return nil, fmt.Errorf("lifecycle connection escaped its inspected endpoint")
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", target)
		},
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: &lifecycleTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

// Docker does not publish ports for an internal-only network. Native Linux
// can reach its bridge directly; bind that connection to the inspected owned
// container without resolving the fictional HTTP authority through DNS.
func serviceTarget(c imageContainer, network string) (string, error) {
	if network != "" && len(c.NetworkSettings.Networks) == 1 {
		for _, endpoint := range c.NetworkSettings.Networks {
			ip := net.ParseIP(endpoint.IPAddress)
			if endpoint.NetworkID == network && ip != nil && ip.To4() != nil && ip.IsPrivate() && ip.String() == endpoint.IPAddress {
				return net.JoinHostPort(endpoint.IPAddress, "8080"), nil
			}
		}
	}
	return "", fmt.Errorf("owned internal service endpoint unavailable")
}

func serviceTransport(target string) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != serviceAuthority {
			return nil, fmt.Errorf("fixture connection escaped")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", target)
	}}
}

// Keep failure diagnostics structural: never return response bodies, URLs,
// container output or configuration from this fictional fixture.
func serviceGet(client *http.Client, base, path string) ([]byte, error) {
	response, err := client.Get(base + path)
	if err != nil {
		var networkError net.Error
		timedOut := errors.As(err, &networkError) && networkError.Timeout()
		return nil, fmt.Errorf("fixture HTTP transport failed (timeout=%t connectionRefused=%t connectionReset=%t)", timedOut, errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ECONNRESET))
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || len(b) == 2<<20 || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fixture HTTP response invalid (status=%d bodyReadFailed=%t oversized=%t)", response.StatusCode, err != nil, len(b) == 2<<20)
	}
	return b, nil
}

type serviceProcess struct {
	PID                   int           `json:"pid"`
	Name                  string        `json:"name"`
	UID                   int           `json:"uid"`
	GID                   int           `json:"gid"`
	RSSKiB                int64         `json:"rssKiB"`
	PeakRSSKiB            int64         `json:"peakRssKiB"`
	Threads               int64         `json:"threads"`
	EffectiveCapabilities string        `json:"effectiveCapabilities"`
	BoundingCapabilities  string        `json:"boundingCapabilities"`
	NoNewPrivileges       bool          `json:"noNewPrivileges"`
	SeccompMode           int           `json:"seccompMode"`
	Mounts                serviceMounts `json:"mounts"`
}

type serviceMounts struct {
	RootReadOnly bool   `json:"rootReadOnly"`
	TmpfsBytes   int64  `json:"tmpfsBytes"`
	UID          int    `json:"uid"`
	GID          int    `json:"gid"`
	Mode         string `json:"mode"`
	NoExec       bool   `json:"noExec"`
	NoSuid       bool   `json:"noSuid"`
	NoDev        bool   `json:"noDev"`
}

func serviceMountStatus(b []byte) (serviceMounts, error) {
	bad := func() (serviceMounts, error) {
		return serviceMounts{}, fmt.Errorf("actual kernel root or application tmpfs differs")
	}
	if len(b) == 0 || len(b) > 1<<20 {
		return bad()
	}
	options := func(raw string) (map[string]string, bool) {
		out := map[string]string{}
		for _, option := range strings.Split(raw, ",") {
			key, value, _ := strings.Cut(option, "=")
			if key == "" {
				return nil, false
			}
			if _, exists := out[key]; exists {
				return nil, false
			}
			out[key] = value
		}
		return out, true
	}
	has := func(m map[string]string, key string) bool { _, ok := m[key]; return ok }
	root, tmp := false, false
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		f := strings.Fields(line)
		sep := -1
		for i, field := range f {
			if field == "-" {
				sep = i
				break
			}
		}
		if len(f) < 10 || sep < 6 || len(f) != sep+4 {
			return bad()
		}
		if strings.HasPrefix(f[4], "/tmp/") {
			return bad()
		}
		if f[4] != "/" && f[4] != "/tmp" {
			continue
		}
		mount, ok := options(f[5])
		if !ok {
			return bad()
		}
		if f[4] == "/" {
			if root || !has(mount, "ro") || has(mount, "rw") {
				return bad()
			}
			root = true
			continue
		}
		if tmp || f[3] != "/" || f[sep+1] != "tmpfs" || f[sep+2] != "tmpfs" {
			return bad()
		}
		for _, required := range []string{"rw", "nosuid", "nodev", "noexec"} {
			if !has(mount, required) {
				return bad()
			}
		}
		for _, unsafe := range []string{"ro", "suid", "dev", "exec"} {
			if has(mount, unsafe) {
				return bad()
			}
		}
		super, ok := options(f[sep+3])
		if !ok || !has(super, "rw") || super["uid"] != "65532" || super["gid"] != "65532" {
			return bad()
		}
		mode, e := strconv.ParseUint(super["mode"], 8, 32)
		if e != nil || mode != 0700 {
			return bad()
		}
		size := super["size"]
		multiplier := int64(1)
		if len(size) > 0 {
			switch size[len(size)-1] {
			case 'k':
				multiplier = 1024
				size = size[:len(size)-1]
			case 'm':
				multiplier = 1024 * 1024
				size = size[:len(size)-1]
			case 'g':
				multiplier = 1024 * 1024 * 1024
				size = size[:len(size)-1]
			}
		}
		n, e := strconv.ParseInt(size, 10, 64)
		if e != nil || n <= 0 || n > 16777216 || n*multiplier != 16777216 {
			return bad()
		}
		tmp = true
	}
	if !root || !tmp {
		return bad()
	}
	return serviceMounts{RootReadOnly: true, TmpfsBytes: 16777216, UID: 65532, GID: 65532, Mode: "0700", NoExec: true, NoSuid: true, NoDev: true}, nil
}

// serviceProcessStatus retains the ordinary ZITADEL process restriction profile.
func serviceProcessStatus(b []byte, pid int) (serviceProcess, error) {
	return restrictedProcessStatus(b, pid, "zitadel", 65532)
}

// restrictedProcessStatus measures one actual namespace-init process while
// retaining the same capability, seccomp and memory checks for each component.
func restrictedProcessStatus(b []byte, pid int, name string, uid int) (serviceProcess, error) {
	bad := func() (serviceProcess, error) {
		return serviceProcess{}, fmt.Errorf("actual service process restrictions or measurements differ")
	}
	if !((name == "zitadel" && uid == 65532) || (name == "postgres" && uid == 999)) {
		return bad()
	}
	fields := map[string][]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, exists := fields[key]; exists {
			return bad()
		}
		fields[key] = strings.Fields(value)
	}
	identity := strings.TrimSpace(strings.Repeat(strconv.Itoa(uid)+" ", 4))
	for key, expected := range map[string]string{"Name": name, "Pid": strconv.Itoa(pid), "Uid": identity, "Gid": identity, "NoNewPrivs": "1", "Seccomp": "2", "CapInh": "0000000000000000", "CapPrm": "0000000000000000", "CapEff": "0000000000000000", "CapBnd": "0000000000000000", "CapAmb": "0000000000000000"} {
		if strings.Join(fields[key], " ") != expected {
			return bad()
		}
	}
	ns := fields["NSpid"]
	if pid <= 1 || len(ns) != 2 || ns[0] != strconv.Itoa(pid) || ns[1] != "1" {
		return bad()
	}
	measure := func(key string, memory bool) (int64, bool) {
		f := fields[key]
		if (memory && (len(f) != 2 || f[1] != "kB")) || (!memory && len(f) != 1) {
			return 0, false
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		return n, err == nil && n > 0 && n < 1<<40
	}
	rss, a := measure("VmRSS", true)
	peak, c := measure("VmHWM", true)
	threads, d := measure("Threads", false)
	if !a || !c || !d || peak < rss {
		return bad()
	}
	return serviceProcess{PID: pid, Name: name, UID: uid, GID: uid, RSSKiB: rss, PeakRSSKiB: peak, Threads: threads, EffectiveCapabilities: fields["CapEff"][0], BoundingCapabilities: fields["CapBnd"][0], NoNewPrivileges: true, SeccompMode: 2}, nil
}

const serviceTmpfs = "rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700"
const serviceDatabaseImage = "postgres:17.11-bookworm@sha256:3645570cccdfa447589da9f57dd740faa29b30938e861289a5574b6ca6b03826"

var serviceArgs = []string{"start-from-init", "--masterkeyFromEnv", "--tlsMode", "disabled"}

func serviceInspect(raw []byte, id, image, owner, network, status string) (imageContainer, error) {
	var all []imageContainer
	bad := func() (imageContainer, error) {
		state, exit, mounts := "unknown", 0, 0
		if len(all) == 1 {
			c := all[0]
			switch c.State.Status {
			case "created", "running", "exited", "dead", "paused", "restarting":
				state = c.State.Status
			}
			exit = c.State.ExitCode
			mounts = len(c.Mounts)
		}
		return imageContainer{}, fmt.Errorf("service container restrictions or state differ (state=%s exit=%d mountEntries=%d)", state, exit, mounts)
	}
	if err := imageDecode(raw, &all); err != nil || len(all) != 1 {
		return bad()
	}
	c := all[0]
	h := c.HostConfig
	if c.ID != id || c.Image != image || c.Config.Image != image || c.Config.Labels["org.devantler-tech.identity-smoke-owner"] != owner || c.Config.User != "65532:65532" || c.Config.WorkingDir != "/app" || !reflect.DeepEqual(c.Config.Entrypoint, []string{"/app/zitadel"}) || !reflect.DeepEqual(c.Config.Cmd, serviceArgs) || !h.ReadonlyRootfs || h.Privileged || h.PublishAllPorts || !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || len(h.CapAdd) != 0 || !reflect.DeepEqual(h.SecurityOpt, []string{"no-new-privileges"}) || h.NetworkMode != network || h.PidMode != "" || h.IpcMode != "private" || h.PidsLimit != 128 || h.Memory != 1073741824 || h.NanoCpus != 2000000000 || len(h.Binds) != 0 || len(h.Devices) != 0 || len(h.Tmpfs) != 1 || h.Tmpfs["/tmp"] != serviceTmpfs || len(h.PortBindings) != 0 || len(c.Mounts) > 1 || len(c.NetworkSettings.Networks) > 1 || c.State.Status != status || c.State.Dead || c.State.OOMKilled || c.State.ExitCode != 0 {
		return bad()
	}
	var mount struct {
		Type, Destination string
		RW                bool
	}
	if len(c.Mounts) == 1 {
		if err := json.Unmarshal(c.Mounts[0], &mount); err != nil || mount.Type != "tmpfs" || mount.Destination != "/tmp" || !mount.RW {
			return bad()
		}
	}
	for _, n := range c.NetworkSettings.Networks {
		if n.NetworkID != network && !(status != "running" && n.NetworkID == "") {
			return bad()
		}
	}
	if (status != "running" && status != "created" && status != "exited") || (status == "running" && (!c.State.Running || c.State.Pid <= 1 || len(c.NetworkSettings.Networks) != 1)) || (status == "created" && (c.State.Running || c.State.Pid != 0)) || (status == "exited" && (c.State.Running || c.State.Pid != 0)) {
		return bad()
	}
	return c, nil
}

// This is an isolated HTTP fixture, not the production TLS/authentication
// profile. All database/master/bootstrap values are fictional and short-lived.
func imageService(image, owner, repo, output string, run imageCommand, browser bool) (result error) {
	source, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil {
		return err
	}
	label := "org.devantler-tech.identity-smoke-owner"
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owner) {
		return fmt.Errorf("invalid service owner")
	}
	ids := []string{}
	network := ""
	cleaned := false
	cleanup := func() error {
		failed := false
		for i := len(ids) - 1; i >= 0; i-- {
			if _, e := run(time.Minute, "docker", "rm", "--force", ids[i]); e != nil {
				failed = true
			}
		}
		if network != "" {
			if _, e := run(30*time.Second, "docker", "network", "rm", network); e != nil {
				failed = true
			}
		}
		if failed {
			return fmt.Errorf("owned service fixture cleanup failed")
		}
		return nil
	}
	defer func() {
		if !cleaned {
			if e := cleanup(); e != nil {
				if result == nil {
					result = e
				} else {
					result = fmt.Errorf("%w; %s", result, e)
				}
			}
		}
	}()
	if _, err = run(3*time.Minute, "docker", "pull", "--platform=linux/amd64", serviceDatabaseImage); err != nil {
		return fmt.Errorf("pinned fixture database unavailable")
	}
	b, err := run(30*time.Second, "docker", "network", "create", "--internal", "--label", label+"="+owner, "identity-service-"+owner)
	if err != nil {
		return err
	}
	network = strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(network) {
		network = ""
		return fmt.Errorf("invalid fixture network identity")
	}
	b, err = run(30*time.Second, "docker", "network", "inspect", network)
	if err != nil {
		return err
	}
	var networks []struct {
		ID         string
		Internal   bool
		Labels     map[string]string
		Containers map[string]json.RawMessage
	}
	if err = imageDecode(b, &networks); err != nil || len(networks) != 1 || networks[0].ID != network || !networks[0].Internal || networks[0].Labels[label] != owner || len(networks[0].Containers) != 0 {
		network = ""
		return fmt.Errorf("fixture network ownership or isolation not confirmed")
	}
	create := func(args ...string) (string, error) {
		b, e := run(30*time.Second, "docker", append([]string{"create", "--label", label + "=" + owner}, args...)...)
		if e != nil {
			return "", fmt.Errorf("fixture container create failed")
		}
		id := strings.TrimSpace(string(b))
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
			return "", fmt.Errorf("invalid fixture container identity")
		}
		b, e = run(30*time.Second, "docker", "inspect", id)
		if e != nil {
			return "", e
		}
		var cs []imageContainer
		if e = imageDecode(b, &cs); e != nil || len(cs) != 1 || cs[0].ID != id || cs[0].Config.Labels[label] != owner {
			return "", fmt.Errorf("fixture container ownership not confirmed")
		}
		ids = append(ids, id)
		return id, nil
	}
	db, err := create("--pull=never", "--platform=linux/amd64", "--network="+network, "--network-alias=database", "--user=999:999", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=512m", "--cpus=2", "--tmpfs=/var/lib/postgresql/data:rw,nosuid,nodev,size=268435456,uid=999,gid=999,mode=0700", "--tmpfs=/var/run/postgresql:rw,nosuid,nodev,size=1048576,uid=999,gid=999,mode=0700", "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16777216,uid=999,gid=999,mode=0700", "--env=POSTGRES_PASSWORD=FictionalDatabaseOnly1!", serviceDatabaseImage)
	if err != nil {
		return err
	}
	if _, err = run(30*time.Second, "docker", "start", db); err != nil {
		return fmt.Errorf("fixture database start failed")
	}
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err = run(10*time.Second, "docker", "exec", db, "pg_isready", "--host=127.0.0.1", "--port=5432", "--username=postgres"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("fixture database not ready")
		}
		time.Sleep(time.Second)
	}
	args := []string{"--pull=never", "--platform=linux/amd64", "--network=" + network, "--user=65532:65532", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--cpus=2", "--tmpfs=/tmp:" + serviceTmpfs}
	environment := []string{
		"ZITADEL_MASTERKEY=00000000000000000000000000000000", "ZITADEL_EXTERNALDOMAIN=fixture.localhost", "ZITADEL_EXTERNALPORT=8080", "ZITADEL_EXTERNALSECURE=false", "ZITADEL_TLS_ENABLED=false",
		"ZITADEL_DATABASE_POSTGRES_HOST=database", "ZITADEL_DATABASE_POSTGRES_DATABASE=zitadel", "ZITADEL_DATABASE_POSTGRES_USER_USERNAME=zitadel", "ZITADEL_DATABASE_POSTGRES_USER_PASSWORD=FictionalServiceOnly1!", "ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD=FictionalDatabaseOnly1!", "ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE=disable", "ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE=disable",
		"ZITADEL_FIRSTINSTANCE_SKIP=false", "ZITADEL_FIRSTINSTANCE_INSTANCENAME=Fictional Local Fixture", "ZITADEL_FIRSTINSTANCE_ORG_NAME=Fixture", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_USERNAME=operator@example.invalid", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_ADDRESS=operator@example.invalid", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_VERIFIED=true", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD=FictionalOperatorOnly1!", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORDCHANGEREQUIRED=false",
		"ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED=false", "ZITADEL_DEFAULTINSTANCE_LOGINPOLICY_ALLOWREGISTER=false", "ZITADEL_DEFAULTINSTANCE_LOGINPOLICY_ALLOWEXTERNALIDP=false", "ZITADEL_DEFAULTINSTANCE_DOMAINPOLICY_USERLOGINMUSTBEDOMAIN=false", "ZITADEL_TELEMETRY_ENABLED=false", "ZITADEL_SERVICEPING_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_WEBHOOK_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_PRIVATEIP_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_HOSTNAME_ENABLED=true",
	}
	for _, e := range environment {
		args = append(args, "--env="+e)
	}
	args = append(args, image)
	args = append(args, serviceArgs...)
	service, err := create(args...)
	if err != nil {
		return err
	}
	inspect := func(status string) (imageContainer, error) {
		b, e := run(30*time.Second, "docker", "inspect", service)
		if e != nil {
			return imageContainer{}, e
		}
		c, e := serviceInspect(b, service, image, owner, network, status)
		if e != nil {
			return c, e
		}
		seen := map[string]string{}
		for _, entry := range c.Config.Env {
			key, value, ok := strings.Cut(entry, "=")
			if !ok {
				return imageContainer{}, fmt.Errorf("malformed fixture environment")
			}
			if strings.HasPrefix(key, "ZITADEL_") {
				if _, exists := seen[key]; exists {
					return imageContainer{}, fmt.Errorf("duplicate fixture environment")
				}
				seen[key] = value
			}
		}
		if len(seen) != len(environment) {
			return imageContainer{}, fmt.Errorf("unexpected fixture environment")
		}
		for _, entry := range environment {
			key, value, _ := strings.Cut(entry, "=")
			if seen[key] != value {
				return imageContainer{}, fmt.Errorf("fixture environment differs")
			}
		}
		return c, nil
	}
	if _, err = inspect("created"); err != nil {
		return err
	}
	started := time.Now()
	if _, err = run(30*time.Second, "docker", "start", service); err != nil {
		return fmt.Errorf("identity service start failed")
	}
	c, err := inspect("running")
	if err != nil {
		return err
	}
	target, err := serviceTarget(c, network)
	if err != nil {
		return err
	}
	base := "http://" + serviceAuthority
	client := &http.Client{Timeout: 5 * time.Second, Transport: serviceTransport(target), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "http" || req.URL.Host != serviceAuthority {
			return fmt.Errorf("fixture redirect escaped")
		}
		return nil
	}}
	defer client.CloseIdleConnections()
	get := func(path string) ([]byte, error) {
		return serviceGet(client, base, path)
	}
	deadline = time.Now().Add(3 * time.Minute)
	for {
		if _, err = get("/debug/ready"); err == nil {
			break
		}
		if _, e := inspect("running"); e != nil {
			return fmt.Errorf("service readiness inspection failed: %w", e)
		}
		if time.Now().After(deadline) {
			// This diagnostic is never an alternate success path. It separates
			// application readiness from the host's published-port path, and
			// discards all command output before reporting a boolean.
			_, insideErr := run(10*time.Second, "docker", "exec", service, "/app/zitadel", "ready")
			return fmt.Errorf("identity service did not become ready: %w (inContainerReady=%t)", err, insideErr == nil)
		}
		time.Sleep(2 * time.Second)
	}
	readyMs := time.Since(started).Milliseconds()
	b, err = get("/.well-known/openid-configuration")
	if err != nil {
		return err
	}
	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err = json.Unmarshal(b, &discovery); err != nil || discovery.Issuer != base || discovery.AuthorizationEndpoint != base+"/oauth/v2/authorize" || discovery.TokenEndpoint != base+"/oauth/v2/token" {
		return fmt.Errorf("actual OIDC discovery differs")
	}
	b, err = get("/ui/console/")
	if err != nil || !strings.Contains(string(b), "<cnsl-root>") {
		return fmt.Errorf("actual embedded console unavailable")
	}
	processes := []serviceProcess{}
	for sample := 0; sample < 3; sample++ {
		c, e := inspect("running")
		if e != nil {
			return e
		}
		top, e := run(30*time.Second, "docker", "top", service, "-eo", "pid,ppid,uid,gid,comm")
		if e != nil {
			return e
		}
		lines := strings.Split(strings.TrimSpace(string(top)), "\n")
		if len(lines) != 2 {
			return fmt.Errorf("unexpected service process inventory")
		}
		f := strings.Fields(lines[1])
		if len(f) != 5 || f[0] != strconv.Itoa(c.State.Pid) || f[2] != "65532" || f[3] != "65532" || f[4] != "zitadel" {
			return fmt.Errorf("service process inventory differs")
		}
		b, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(c.State.Pid), "status"))
		if e != nil {
			return fmt.Errorf("native Linux process evidence unavailable")
		}
		process, e := serviceProcessStatus(b, c.State.Pid)
		if e != nil {
			return e
		}
		mountInfo, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(c.State.Pid), "mountinfo"))
		if e != nil {
			return fmt.Errorf("native Linux mount evidence unavailable")
		}
		process.Mounts, e = serviceMountStatus(mountInfo)
		if e != nil {
			return e
		}
		processes = append(processes, process)
		if sample < 2 {
			time.Sleep(time.Second)
		}
	}
	if browser {
		c, e := inspect("running")
		if e != nil {
			return e
		}
		if e = serviceBrowser(c.State.Pid, image, repo, output, run); e != nil {
			return e
		}
	}
	b, err = run(30*time.Second, "docker", "network", "inspect", network)
	if err != nil {
		return err
	}
	if err = imageDecode(b, &networks); err != nil || len(networks) != 1 || !networks[0].Internal || networks[0].ID != network || networks[0].Labels[label] != owner || len(networks[0].Containers) != 2 || networks[0].Containers[service] == nil || networks[0].Containers[db] == nil {
		return fmt.Errorf("running fixture network differs")
	}
	if _, err = run(time.Minute, "docker", "stop", "--time=20", service); err != nil {
		return fmt.Errorf("service shutdown failed")
	}
	if _, err = inspect("exited"); err != nil {
		return err
	}
	after, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil || imageHash(after) != imageHash(source) {
		return fmt.Errorf("service harness changed during execution")
	}
	if err = cleanup(); err != nil {
		return err
	}
	cleaned = true
	receipt := struct {
		SchemaVersion       int              `json:"schemaVersion"`
		Profile             string           `json:"profile"`
		UpstreamCommit      string           `json:"upstreamCommit"`
		LocalImageID        string           `json:"localImageId"`
		ServiceSourceSHA256 string           `json:"serviceSourceSha256"`
		DatabaseImage       string           `json:"databaseImage"`
		ReadyMilliseconds   int64            `json:"readyMilliseconds"`
		Processes           []serviceProcess `json:"processSamples"`
		ReadOnlyRoot        bool             `json:"readOnlyRoot"`
		WritablePaths       []string         `json:"configuredTmpfsPaths"`
		Network             string           `json:"network"`
		HTTP                string           `json:"transport"`
		Discovery           bool             `json:"oidcDiscovery"`
		Console             bool             `json:"embeddedConsoleHTTP"`
		BrowserProof        bool             `json:"browserProof"`
		AuthenticationProof bool             `json:"authenticationProof"`
		Cleanup             string           `json:"cleanup"`
		ImageAdmission      string           `json:"imageAdmission"`
		LicenseClosure      string           `json:"licenseClosure"`
	}{1, "isolated-service-smoke", imagePin, image, imageHash(source), serviceDatabaseImage, readyMs, processes, true, []string{"/tmp"}, "internal-two-container-direct", "fictional-http-only", true, true, false, false, "complete", "unknown", "unknown"}
	b, err = json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "service-smoke.json", append(b, '\n'), 0444)
}

// Chrome and its matching WebDriver run as the invoking non-root CI user inside
// the already-inspected service network namespace. The fixture has no host ports
// or external network; the browser's native sandbox remains enabled.
func serviceBrowser(pid int, image, repo, output string, run imageCommand) error {
	if pid <= 1 || os.Getuid() <= 0 || os.Getgid() <= 0 {
		return fmt.Errorf("non-root browser fixture user required")
	}
	source, err := imageRead(repo, "scripts/zitadel-browser.go", 1<<20)
	if err != nil {
		return err
	}
	observed, err := run(30*time.Second, "sudo", "-n", "readlink", fmt.Sprintf("/proc/%d/ns/net", pid))
	namespace := strings.TrimSpace(string(observed))
	hostNamespace, hostErr := os.Readlink("/proc/self/ns/net")
	if err != nil || hostErr != nil || namespace == hostNamespace || !regexp.MustCompile(`^net:\[\d+\]$`).MatchString(namespace) {
		return fmt.Errorf("service network namespace unavailable")
	}
	// Only the network namespace changes; no container binary, filesystem,
	// bootstrap token or real account is exposed to the browser process.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := imageRead(filepath.Dir(exe), filepath.Base(exe), 64<<20)
	if err != nil {
		return err
	}
	// Cover the preserved 100s operator and 120s provider phases, the new 60s
	// framework phase, and the existing cleanup margin without truncating a phase.
	diagnostic, err := run(browserWorkerTimeout, "sudo", "-n", "nsenter", "--target", strconv.Itoa(pid), "--net", "--",
		"setpriv", "--reuid="+strconv.Itoa(os.Getuid()), "--regid="+strconv.Itoa(os.Getgid()), "--clear-groups", "--no-new-privs",
		"env", "-i", "PATH="+os.Getenv("PATH"), "HOME="+os.Getenv("HOME"), "LANG=C", "LC_ALL=C",
		exe, "--browser-worker", output, namespace, imageHash(source))
	if err != nil {
		if stage := regexp.MustCompile(`browser stage=(admission|driver|session|login|password|console|providers|cleanup|receipt) category=(observation|session-not-created|timeout|no-such-element|invalid-argument|javascript-error|webdriver|transport-timeout|transport|incomplete-response|invalid-envelope|invalid-result|invalid-session|missing-browser-version)`).Find(diagnostic); stage != nil {
			return fmt.Errorf("native browser journey failed: %s", stage)
		}
		return fmt.Errorf("native browser journey failed")
	}
	after, err := imageRead(repo, "scripts/zitadel-browser.go", 1<<20)
	if err != nil || imageHash(after) != imageHash(source) {
		return fmt.Errorf("browser source changed")
	}
	raw, err := imageRead(output, "browser-smoke.json", 65536)
	if err != nil {
		return err
	}
	var receipt struct {
		SchemaVersion   int    `json:"schemaVersion"`
		Profile         string `json:"profile"`
		Source          string `json:"browserSourceSha256"`
		Executable      string `json:"workerExecutableSha256"`
		Version         string `json:"browserVersion"`
		Namespace       string `json:"networkNamespace"`
		SandboxDisabled bool   `json:"browserSandboxDisabled"`
		Pages           []struct {
			Kind          string `json:"kind"`
			Font          string `json:"font"`
			Loaded        bool   `json:"loaded"`
			SameOrigin    bool   `json:"sameOriginResources"`
			FontAbsent    bool   `json:"unadmittedFontAbsent"`
			SecureContext bool   `json:"secureContext"`
			WebCrypto     bool   `json:"webCrypto"`
			Presented     bool   `json:"presentationReady"`
			Screenshot    struct {
				Path   string `json:"path"`
				Bytes  int    `json:"bytes"`
				SHA256 string `json:"sha256"`
			} `json:"screenshot"`
		} `json:"pages"`
		ProviderIcons  []browserProviderCase    `json:"providerIcons"`
		SMTPIcons      []browserProviderCase    `json:"smtpIcons"`
		MDIIcons       []browserProviderCase    `json:"mdiIcons"`
		FrameworkIcons []browserProviderCase    `json:"frameworkIcons"`
		SourceOffer    []browserSourceOfferCase `json:"sourceOffer"`
		Cleanup        string                   `json:"cleanup"`
		Portal         bool                     `json:"portalFederation"`
		TLS            bool                     `json:"productionTLS"`
		Invitation     bool                     `json:"invitationProof"`
		MFA            bool                     `json:"mfaProof"`
		Admission      string                   `json:"imageAdmission"`
		License        string                   `json:"licenseClosure"`
	}
	if err = imageDecode(raw, &receipt); err != nil || receipt.SchemaVersion != 1 || receipt.Profile != "fictional-native-browser" || receipt.Source != imageHash(source) || receipt.Executable != imageHash(binary) || receipt.Version == "" || receipt.Namespace != namespace || receipt.SandboxDisabled || len(receipt.Pages) != 3 || receipt.Cleanup != "complete" || receipt.Portal || receipt.TLS || receipt.Invitation || receipt.MFA || receipt.Admission != "unknown" || receipt.License != "unknown" {
		return fmt.Errorf("browser observation binding differs")
	}
	for i, kind := range []string{"login", "password", "console"} {
		page := receipt.Pages[i]
		if page.Kind != kind || page.Font == "" || strings.Contains(strings.ToLower(page.Font), "ailerons") || !page.Loaded || !page.SameOrigin || !page.FontAbsent || !page.SecureContext || !page.WebCrypto || !page.Presented || page.Screenshot.Path != kind+".png" {
			return fmt.Errorf("browser page observation incomplete")
		}
		png, e := imageRead(output, kind+".png", 4<<20)
		if e != nil || len(png) != page.Screenshot.Bytes || imageHash(png) != page.Screenshot.SHA256 {
			return fmt.Errorf("browser screenshot binding differs")
		}
	}
	if err = browserProviderReceipt(output, receipt.ProviderIcons); err != nil {
		return err
	}
	if err = browserSMTPReceipt(output, receipt.SMTPIcons); err != nil {
		return err
	}
	if err = browserMDIReceipt(output, receipt.MDIIcons); err != nil {
		return err
	}
	if err = browserFrameworkReceipt(output, receipt.FrameworkIcons); err != nil {
		return err
	}
	if err = browserSourceOfferReceipt(output, receipt.SourceOffer); err != nil {
		return err
	}
	bound, err := json.MarshalIndent(map[string]any{"schemaVersion": 1, "upstreamCommit": imagePin, "localImageId": image, "browserReceiptSha256": imageHash(raw), "browserSourceSha256": imageHash(source), "cleanup": "complete", "imageAdmission": "unknown", "licenseClosure": "unknown"}, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "browser-binding.json", append(bound, '\n'), 0444)
}

// lifecyclePhaseArgs admits a complete sealed private profile, then gives each
// native phase only its own inputs. The ordinary start has no bootstrap document.
func lifecyclePhaseArgs(phase, root string, bindings map[string]lifecycleInput) ([]string, error) {
	if strings.ContainsAny(root, ",\n\r") {
		return nil, fmt.Errorf("closed lifecycle mount paths required")
	}
	if err := lifecycleValidateInputs(root, bindings); err != nil {
		return nil, err
	}
	config := ""
	files := []string{"ca.pem", "service.crt", "service.key"}
	command := []string{}
	switch phase {
	case "init-user", "init", "init-wrong-ca", "init-wrong-host", "init-plaintext":
		config = "init.json"
		if strings.HasPrefix(phase, "init-") && phase != "init-user" {
			config = phase + ".json"
		}
		command = []string{"init"}
		if phase == "init-user" {
			command = append(command, "user")
		}
		if phase == "init-wrong-ca" {
			files = append(files, "untrusted-ca.pem")
		}
	case "setup":
		config = "setup.json"
		files = append(files, "steps.json", "master.key")
		command = []string{"setup", "--steps", "/fixture/steps.json", "--masterkeyFile", "/fixture/master.key", "--tlsMode", "enabled"}
	case "serve":
		config = "serve.json"
		files = append(files, "master.key")
		command = []string{"start", "--masterkeyFile", "/fixture/master.key", "--tlsMode", "enabled"}
	default:
		return nil, fmt.Errorf("unsupported lifecycle phase")
	}
	files = append(files, config)
	args := []string{}
	for _, name := range files {
		args = append(args, "--mount=type=bind,src="+filepath.Join(root, name)+",dst=/fixture/"+name+",readonly")
	}
	return append(args, append([]string{"--", "--config", "/fixture/" + config}, command...)...), nil
}

// lifecycleIdentitySnapshot accepts only the two intended fictional creation
// events. Its seal can detect replacement across restart without retaining raw
// event payloads, which can contain password-derived material.
func lifecycleIdentitySnapshot(raw []byte, material lifecycleMaterial) (string, error) {
	var snapshot struct {
		Instances, Humans, Verified int
		InstanceID                  string `json:"instanceId"`
		HumanID                     string `json:"humanId"`
		InstanceName                string `json:"instanceName"`
		UserName                    string `json:"userName"`
		Email                       string `json:"email"`
		HumanInstanceID             string `json:"humanInstanceId"`
		InstanceAggregateID         string `json:"instanceAggregateId"`
		InstanceSequence            int64  `json:"instanceSequence"`
		HumanSequence               int64  `json:"humanSequence"`
	}
	bad := func() (string, error) {
		return "", fmt.Errorf("complete intended fictional instance and unverified identity required")
	}
	var document map[string]json.RawMessage
	if len(raw) > 4096 || imageDecode(raw, &document) != nil || len(document) != 12 {
		return bad()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || snapshot.Instances != 1 || snapshot.Humans != 1 || snapshot.Verified != 0 || snapshot.InstanceName != "Fictional lifecycle instance" || snapshot.UserName != material.human || snapshot.Email != material.human || snapshot.InstanceID != snapshot.HumanInstanceID || snapshot.InstanceID != snapshot.InstanceAggregateID || snapshot.InstanceSequence <= 0 || snapshot.HumanSequence <= 0 || !regexp.MustCompile(`^[1-9][0-9]{0,19}$`).MatchString(snapshot.InstanceID) || !regexp.MustCompile(`^[1-9][0-9]{0,19}$`).MatchString(snapshot.HumanID) {
		return bad()
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return bad()
	}
	return imageHash(encoded), nil
}

// lifecycleContainerSpec is the complete permitted container interface for one
// fixture phase. It is compared with Docker's effective inspection before use.
type lifecycleContainerSpec struct {
	image, imageID, user, workdir string
	entrypoint, command           []string
	tmpfs                         map[string]string
	memory                        int64
	inputs                        map[string]string // destination -> physical read-only input
	volume                        string
}

// lifecycleInspect refuses an unexpected writable mount, credential environment,
// namespace, image, execution mode or owner before any native phase is started.
func lifecycleInspect(raw []byte, id, owner, network, state string, spec lifecycleContainerSpec) (imageContainer, error) {
	bad := func() (imageContainer, error) {
		return imageContainer{}, fmt.Errorf("owned restricted lifecycle container required")
	}
	var all []imageContainer
	if imageDecode(raw, &all) != nil || len(all) != 1 {
		return bad()
	}
	c := all[0]
	h := c.HostConfig
	if c.ID != id || c.Image != spec.imageID || c.Config.Image != spec.image || c.Config.User != spec.user || c.Config.WorkingDir != spec.workdir || !reflect.DeepEqual(c.Config.Entrypoint, spec.entrypoint) || !reflect.DeepEqual(c.Config.Cmd, spec.command) || c.Config.Labels["org.devantler-tech.identity-smoke-owner"] != owner || !h.ReadonlyRootfs || h.Privileged || h.PublishAllPorts || !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || len(h.CapAdd) != 0 || !reflect.DeepEqual(h.SecurityOpt, []string{"no-new-privileges"}) || h.NetworkMode != network || h.PidMode != "" || h.IpcMode != "private" || h.PidsLimit != 128 || h.Memory != spec.memory || h.NanoCpus != 2000000000 || len(h.Binds) != 0 || len(h.Devices) != 0 || len(h.PortBindings) != 0 || !reflect.DeepEqual(h.Tmpfs, spec.tmpfs) || c.State.Status != state || c.State.Dead || c.State.OOMKilled || len(c.NetworkSettings.Networks) > 1 {
		return bad()
	}
	if state != "created" && state != "running" && state != "exited" {
		return bad()
	}
	if state == "running" && (!c.State.Running || c.State.Pid <= 1 || len(c.NetworkSettings.Networks) != 1) {
		return bad()
	}
	if state != "running" && (c.State.Running || c.State.Pid != 0) {
		return bad()
	}
	for _, entry := range c.Config.Env {
		if strings.HasPrefix(entry, "ZITADEL_") {
			return bad()
		}
	}
	for _, n := range c.NetworkSettings.Networks {
		if n.NetworkID != network && !(state != "running" && n.NetworkID == "") {
			return bad()
		}
	}
	seen := map[string]bool{}
	for _, rawMount := range c.Mounts {
		var mount struct {
			Type, Source, Destination, Name, Driver string
			RW                                      bool
		}
		if json.Unmarshal(rawMount, &mount) != nil || seen[mount.Destination] {
			return bad()
		}
		seen[mount.Destination] = true
		if source, ok := spec.inputs[mount.Destination]; ok {
			if mount.Type != "bind" || mount.Source != source || mount.RW {
				return bad()
			}
		} else if mount.Destination == "/var/lib/postgresql/data" && spec.volume != "" {
			if mount.Type != "volume" || mount.Name != spec.volume || mount.Driver != "local" || !mount.RW {
				return bad()
			}
		} else if _, ok := spec.tmpfs[mount.Destination]; !ok || mount.Type != "tmpfs" || !mount.RW {
			return bad()
		}
	}
	for destination := range spec.inputs {
		if !seen[destination] {
			return bad()
		}
	}
	if spec.volume != "" && !seen["/var/lib/postgresql/data"] {
		return bad()
	}
	// Some Docker versions omit tmpfs entries from Mounts; HostConfig is still
	// checked exactly, and native process mount inspection is separate evidence.
	return c, nil
}

// lifecycleSession owns only random, positively inspected fixture resources.
// Private command output is never written to the public evidence directory.
type lifecycleSession struct {
	rootInfo                                        os.FileInfo
	run                                             imageCommand
	owner, root, network, volume, database, service string
	bindings                                        map[string]lifecycleInput
	material                                        lifecycleMaterial
	specs                                           map[string]lifecycleContainerSpec
}

// inspect rebinds a resource's actual owner and full restrictions on every use.
func (s *lifecycleSession) inspect(id, state string) (imageContainer, error) {
	spec, ok := s.specs[id]
	if !ok {
		return imageContainer{}, fmt.Errorf("unowned lifecycle container")
	}
	raw, err := s.run(30*time.Second, "docker", "inspect", id)
	if err != nil {
		return imageContainer{}, fmt.Errorf("lifecycle container inspection failed")
	}
	return lifecycleInspect(raw, id, s.owner, s.network, state, spec)
}

// create starts no process until the daemon's effective restrictions are read
// back. Every private mount is individually read-only; no private directory is
// passed through to ordinary serving.
func (s *lifecycleSession) create(spec lifecycleContainerSpec, extra ...string) (string, error) {
	if err := lifecycleValidateInputs(s.root, s.bindings); err != nil {
		return "", err
	}
	args := []string{"create", "--pull=never", "--platform=linux/amd64", "--label=org.devantler-tech.identity-smoke-owner=" + s.owner, "--network=" + s.network, "--user=" + spec.user, "--workdir=" + spec.workdir, "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=" + strconv.FormatInt(spec.memory, 10), "--cpus=2", "--entrypoint=" + spec.entrypoint[0]}
	for destination, options := range spec.tmpfs {
		args = append(args, "--tmpfs="+destination+":"+options)
	}
	for destination, source := range spec.inputs {
		args = append(args, "--mount=type=bind,src="+source+",dst="+destination+",readonly")
	}
	if spec.volume != "" {
		args = append(args, "--mount=type=volume,src="+spec.volume+",dst=/var/lib/postgresql/data")
	}
	args = append(args, extra...)
	args = append(args, spec.image)
	args = append(args, spec.command...)
	raw, err := s.run(30*time.Second, "docker", args...)
	if err != nil {
		return "", fmt.Errorf("lifecycle container creation failed")
	}
	id := strings.TrimSpace(string(raw))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
		return "", fmt.Errorf("invalid lifecycle container identity")
	}
	// Record only after a separate minimal ownership read, so cleanup never
	// acts on an arbitrary identifier returned by an unsuccessful producer.
	actual, err := s.run(30*time.Second, "docker", "inspect", id)
	var all []imageContainer
	if err != nil || imageDecode(actual, &all) != nil || len(all) != 1 || all[0].ID != id || all[0].Config.Labels["org.devantler-tech.identity-smoke-owner"] != s.owner {
		return "", fmt.Errorf("lifecycle container ownership unconfirmed")
	}
	s.specs[id] = spec
	if _, err = s.inspect(id, "created"); err != nil {
		return "", err
	}
	return id, nil
}

// remove retires only a recorded and freshly reverified owner-bound resource.
func (s *lifecycleSession) remove(id string) error {
	if _, ok := s.specs[id]; !ok {
		return fmt.Errorf("unowned lifecycle cleanup refused")
	}
	raw, err := s.run(30*time.Second, "docker", "inspect", id)
	var all []imageContainer
	if err != nil || imageDecode(raw, &all) != nil || len(all) != 1 || all[0].ID != id || all[0].Config.Labels["org.devantler-tech.identity-smoke-owner"] != s.owner {
		return fmt.Errorf("lifecycle cleanup ownership unknown")
	}
	if _, err = s.run(time.Minute, "docker", "rm", "--force", id); err != nil {
		return fmt.Errorf("owned lifecycle container cleanup failed")
	}
	delete(s.specs, id)
	return nil
}

// phase runs a real native command against its sealed phase-specific profile.
// A negative result requires the intended transport error and a non-OOM exit;
// an unrelated crash, timeout or unhealthy database cannot pass a TLS control.
func (s *lifecycleSession) phase(image, phase string, negative []string) error {
	plan, err := lifecyclePhaseArgs(phase, s.root, s.bindings)
	if err != nil {
		return err
	}
	spec := lifecycleContainerSpec{image: image, imageID: image, user: "65532:65532", workdir: "/app", entrypoint: []string{"/app/zitadel"}, tmpfs: map[string]string{"/tmp": serviceTmpfs}, memory: 1073741824, inputs: map[string]string{}}
	marker := -1
	for i, arg := range plan {
		if arg == "--" {
			marker = i
			break
		}
		parts := strings.Split(strings.TrimPrefix(arg, "--mount="), ",")
		if len(parts) != 4 || parts[0] != "type=bind" || parts[3] != "readonly" {
			return fmt.Errorf("closed lifecycle mount plan required")
		}
		spec.inputs[strings.TrimPrefix(parts[2], "dst=")] = strings.TrimPrefix(parts[1], "src=")
	}
	if marker < 0 {
		return fmt.Errorf("native lifecycle command missing")
	}
	spec.command = plan[marker+1:]
	id, err := s.create(spec)
	if err != nil {
		return err
	}
	if phase == "serve" {
		if _, err = s.run(30*time.Second, "docker", "start", id); err != nil {
			return fmt.Errorf("ordinary identity startup failed")
		}
		if _, err = s.inspect(id, "running"); err != nil {
			return err
		}
		s.service = id
		return nil
	}
	output, executionError := s.run(4*time.Minute, "docker", "start", "--attach", id)
	actual, err := s.inspect(id, "exited")
	if err != nil {
		return err
	}
	if len(negative) == 0 {
		if executionError != nil || actual.State.ExitCode != 0 {
			return fmt.Errorf("native lifecycle %s phase failed", phase)
		}
	} else {
		matched := false
		for _, fragment := range negative {
			if strings.Contains(strings.ToLower(string(output)), fragment) {
				matched = true
			}
		}
		if actual.State.ExitCode == 0 || !matched {
			return fmt.Errorf("native lifecycle transport refusal unproven")
		}
	}
	return s.remove(id)
}

// query uses a private env file, not secret command arguments. Statements and
// returned values are a closed fixture interface, never arbitrary user input.
func (s *lifecycleSession) query(admin bool, statement string) ([]byte, error) {
	if err := lifecycleValidateInputs(s.root, s.bindings); err != nil {
		return nil, err
	}
	if _, err := s.inspect(s.database, "running"); err != nil {
		return nil, err
	}
	name := "app-client.env"
	if admin {
		name = "admin-client.env"
	}
	raw, err := s.run(20*time.Second, "docker", "exec", "--user=999:999", "--env-file", filepath.Join(s.root, name), s.database, "psql", "--no-psqlrc", "--quiet", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command", statement)
	if err != nil {
		return nil, fmt.Errorf("verified lifecycle database query refused")
	}
	if len(raw) > 4096 {
		return nil, fmt.Errorf("lifecycle query output exceeds its bound")
	}
	return bytes.TrimSpace(raw), nil
}

// lifecycleDatabaseTLSStage is shared by both database starts and the actual
// non-root file-copy control. The host inputs remain sealed and read-only.
const lifecycleDatabaseTLSStage = `cp /fixture/database.crt /var/lib/postgresql/data/.fixture-tls/server.crt
 cp /fixture/database.key /var/lib/postgresql/data/.fixture-tls/server.key
 chmod 0600 /var/lib/postgresql/data/.fixture-tls/server.key /var/lib/postgresql/data/.fixture-tls/server.crt`

// databaseStart reuses one owned local volume. The server key is derived inside
// that volume under UID 999 with mode 0600; the sealed host input stays read-only.
func (s *lifecycleSession) databaseStart(imageID string, initialized bool) error {
	spec := lifecycleContainerSpec{image: serviceDatabaseImage, imageID: imageID, user: "999:999", workdir: "/", entrypoint: []string{"/bin/sh"}, memory: 536870912, volume: s.volume, tmpfs: map[string]string{"/tmp": "rw,noexec,nosuid,nodev,size=16777216,uid=999,gid=999,mode=0700", "/var/run/postgresql": "rw,noexec,nosuid,nodev,size=1048576,uid=999,gid=999,mode=0700"}, inputs: map[string]string{}}
	for _, name := range []string{"ca.pem", "database.crt", "database.key", "database-hba.conf"} {
		spec.inputs["/fixture/"+name] = filepath.Join(s.root, name)
	}
	script := `test "$(id -u):$(id -g)" = 999:999
 test "$(stat -c '%u:%g' /var/lib/postgresql/data)" = 999:999
 chmod 0700 /var/lib/postgresql/data
 umask 077
 mkdir -p /var/lib/postgresql/data/.fixture-tls
 ` + lifecycleDatabaseTLSStage + `
 test "$(stat -c '%u:%g:%a' /var/lib/postgresql/data/.fixture-tls/server.key)" = 999:999:600
 test "$(stat -c '%u:%g:%a' /var/lib/postgresql/data/.fixture-tls/server.crt)" = 999:999:600
 exec /usr/local/bin/docker-entrypoint.sh postgres -c ssl=on -c ssl_min_protocol_version=TLSv1.3 -c ssl_cert_file=/var/lib/postgresql/data/.fixture-tls/server.crt -c ssl_key_file=/var/lib/postgresql/data/.fixture-tls/server.key -c hba_file=/fixture/database-hba.conf`
	if initialized {
		script = "test -f /var/lib/postgresql/data/pgdata/PG_VERSION\n" + script
	} else {
		script = "test ! -e /var/lib/postgresql/data/pgdata/PG_VERSION\n" + script
	}
	spec.command = []string{"-ec", script}
	extra := []string{"--network-alias=database.fixture.localhost", "--network-alias=other.database.fixture.localhost"}
	if initialized {
		extra = append(extra, "--env=PGDATA=/var/lib/postgresql/data/pgdata")
	} else {
		extra = append(extra, "--env-file="+filepath.Join(s.root, "database.env"))
	}
	id, err := s.create(spec, extra...)
	if err != nil {
		return err
	}
	s.database = id
	if _, err = s.run(30*time.Second, "docker", "start", id); err != nil {
		return fmt.Errorf("owned TLS database start failed")
	}
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err = s.inspect(id, "running"); err != nil {
			return err
		}
		if initialized {
			_, err = s.query(false, "SELECT 1")
		} else {
			_, err = s.query(true, "SELECT 1")
		}
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("owned TLS database did not become ready")
		}
		time.Sleep(time.Second)
	}
}

// cleanup never searches by a broad Docker filter. It removes only resources
// returned to this session and rechecks their exact ownership immediately first.
func (s *lifecycleSession) cleanup() error {
	failed := false
	for id := range s.specs {
		if s.remove(id) != nil {
			failed = true
		}
	}
	if s.volume != "" {
		raw, err := s.run(30*time.Second, "docker", "volume", "inspect", s.volume)
		var volumes []struct {
			Name, Driver    string
			Labels, Options map[string]string
		}
		if err != nil || imageDecode(raw, &volumes) != nil || len(volumes) != 1 || volumes[0].Name != s.volume || volumes[0].Driver != "local" || volumes[0].Labels["org.devantler-tech.identity-smoke-owner"] != s.owner || len(volumes[0].Options) != 0 {
			failed = true
		} else if _, err = s.run(30*time.Second, "docker", "volume", "rm", s.volume); err != nil {
			failed = true
		} else {
			s.volume = ""
		}
	}
	if s.network != "" {
		raw, err := s.run(30*time.Second, "docker", "network", "inspect", s.network)
		var networks []struct {
			ID         string
			Internal   bool
			Labels     map[string]string
			Containers map[string]json.RawMessage
		}
		if err != nil || imageDecode(raw, &networks) != nil || len(networks) != 1 || networks[0].ID != s.network || !networks[0].Internal || networks[0].Labels["org.devantler-tech.identity-smoke-owner"] != s.owner || len(networks[0].Containers) != 0 {
			failed = true
		} else if _, err = s.run(30*time.Second, "docker", "network", "rm", s.network); err != nil {
			failed = true
		} else {
			s.network = ""
		}
	}
	if !failed && s.root != "" {
		current, err := lifecyclePrivateRoot(s.root)
		if err != nil || s.rootInfo == nil || !os.SameFile(s.rootInfo, current) {
			failed = true
		} else {
			// A held root confines each unlink to the reserved directory even
			// if its path is renamed. Never recursively erase unknown contents.
			handle, openError := os.OpenRoot(s.root)
			if openError != nil {
				failed = true
			} else {
				opened, statError := handle.Stat(".")
				if statError != nil || !os.SameFile(s.rootInfo, opened) {
					failed = true
				} else if len(s.bindings) > 0 && lifecycleValidateInputs(s.root, s.bindings) != nil {
					failed = true
				} else {
					for name := range s.bindings {
						if handle.Remove(name) != nil {
							failed = true
						}
					}
				}
				if handle.Close() != nil {
					failed = true
				}
				current, statError = lifecyclePrivateRoot(s.root)
				if !failed && (statError != nil || !os.SameFile(s.rootInfo, current) || os.Remove(s.root) != nil) {
					failed = true
				}
				if !failed {
					s.root = ""
					s.rootInfo = nil
				}
			}
		}
	}
	if failed {
		return fmt.Errorf("owned lifecycle cleanup incomplete")
	}
	return nil
}

// ready verifies the actual service certificate and discovery authority on the
// owned network. No browser warning, host trust store or plaintext fallback is used.
func (s *lifecycleSession) ready() (*http.Client, string, error) {
	actual, err := s.inspect(s.service, "running")
	if err != nil {
		return nil, "", err
	}
	target, err := serviceTarget(actual, s.network)
	if err != nil {
		return nil, "", err
	}
	client, err := lifecycleTLSClient(target, s.material.files["ca.pem"])
	if err != nil {
		return nil, "", err
	}
	base := "https://" + lifecycleAuthority
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if _, err = serviceGet(client, base, "/debug/ready"); err == nil {
			break
		}
		if _, err = s.inspect(s.service, "running"); err != nil {
			client.CloseIdleConnections()
			return nil, "", err
		}
		if time.Now().After(deadline) {
			client.CloseIdleConnections()
			return nil, "", fmt.Errorf("verified TLS service readiness failed")
		}
		time.Sleep(time.Second)
	}
	raw, err := serviceGet(client, base, "/.well-known/openid-configuration")
	var document struct {
		Issuer        string `json:"issuer"`
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
	}
	if err != nil || json.Unmarshal(raw, &document) != nil || document.Issuer != base || document.Authorization != base+"/oauth/v2/authorize" || document.Token != base+"/oauth/v2/token" {
		client.CloseIdleConnections()
		return nil, "", fmt.Errorf("verified lifecycle OIDC discovery differs")
	}
	return client, target, nil
}

// imageLifecycle is a separate opt-in native acceptance fixture. It reuses the
// original image; it does not authenticate a real customer or authorize deployment.
func imageLifecycle(image, owner, repo, output string, run imageCommand) (result error) {
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(image) || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owner) {
		return fmt.Errorf("original image and fresh fixture owner required")
	}
	source, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil {
		return err
	}
	originalRun := run
	deadline := time.Now().Add(12 * time.Minute)
	run = func(duration time.Duration, name string, args ...string) ([]byte, error) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("native lifecycle execution budget exhausted")
		}
		if duration > remaining {
			duration = remaining
		}
		return originalRun(duration, name, args...)
	}
	s := &lifecycleSession{run: run, owner: owner, specs: map[string]lifecycleContainerSpec{}}
	defer func() {
		s.run = originalRun
		if err := s.cleanup(); err != nil {
			if result == nil {
				result = err
			} else {
				result = fmt.Errorf("%w; %s", result, err)
			}
		}
	}()
	root, err := os.MkdirTemp("", "identity-lifecycle-"+owner+"-")
	if err != nil {
		return fmt.Errorf("private lifecycle workspace unavailable")
	}
	s.root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("physical private lifecycle workspace unavailable")
	}
	s.rootInfo, err = lifecyclePrivateRoot(s.root)
	if err != nil {
		return err
	}
	if strings.ContainsAny(s.root, ",\n\r") {
		return fmt.Errorf("closed lifecycle mount paths required")
	}
	s.material, err = lifecycleMaterials(owner)
	if err != nil {
		return err
	}
	s.bindings, err = lifecycleWriteInputs(s.root, s.material)
	if err != nil {
		return err
	}
	started := time.Now().UTC()
	if _, err = run(3*time.Minute, "docker", "pull", "--platform=linux/amd64", serviceDatabaseImage); err != nil {
		return fmt.Errorf("pinned lifecycle database unavailable")
	}
	raw, err := run(30*time.Second, "docker", "image", "inspect", serviceDatabaseImage)
	var images []struct {
		ID string `json:"Id"`
	}
	if err != nil || json.Unmarshal(raw, &images) != nil || len(images) != 1 || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(images[0].ID) {
		return fmt.Errorf("pinned lifecycle database identity unknown")
	}
	databaseImageID := images[0].ID
	label := "org.devantler-tech.identity-smoke-owner=" + owner
	raw, err = run(30*time.Second, "docker", "network", "create", "--internal", "--label", label, "identity-lifecycle-"+owner)
	if err != nil {
		return fmt.Errorf("owned lifecycle network unavailable")
	}
	network := strings.TrimSpace(string(raw))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(network) {
		return fmt.Errorf("invalid lifecycle network identity")
	}
	raw, err = run(30*time.Second, "docker", "network", "inspect", network)
	var networks []struct {
		ID         string
		Internal   bool
		Labels     map[string]string
		Containers map[string]json.RawMessage
	}
	if err != nil || imageDecode(raw, &networks) != nil || len(networks) != 1 || networks[0].ID != network || !networks[0].Internal || networks[0].Labels["org.devantler-tech.identity-smoke-owner"] != owner || len(networks[0].Containers) != 0 {
		return fmt.Errorf("lifecycle network isolation unconfirmed")
	}
	s.network = network
	volume := "identity-lifecycle-" + owner
	raw, err = run(30*time.Second, "docker", "volume", "create", "--driver=local", "--label", label, volume)
	if err != nil || strings.TrimSpace(string(raw)) != volume {
		return fmt.Errorf("owned lifecycle volume unavailable")
	}
	raw, err = run(30*time.Second, "docker", "volume", "inspect", volume)
	var volumes []struct {
		Name, Driver    string
		Labels, Options map[string]string
	}
	if err != nil || imageDecode(raw, &volumes) != nil || len(volumes) != 1 || volumes[0].Name != volume || volumes[0].Driver != "local" || volumes[0].Labels["org.devantler-tech.identity-smoke-owner"] != owner || len(volumes[0].Options) != 0 {
		return fmt.Errorf("lifecycle volume ownership unconfirmed")
	}
	s.volume = volume
	if err = s.databaseStart(databaseImageID, false); err != nil {
		return err
	}
	roleCount := "SELECT count(*) FROM pg_roles WHERE rolname='" + s.material.app + "'"
	for _, control := range []struct {
		phase     string
		fragments []string
	}{{"init-wrong-ca", []string{"unknown authority"}}, {"init-wrong-host", []string{"certificate is valid for", "certificate is not valid for"}}, {"init-plaintext", []string{"no encryption", "pg_hba.conf rejects"}}} {
		if err = s.phase(image, control.phase, control.fragments); err != nil {
			return err
		}
		count, e := s.query(true, roleCount)
		if e != nil || string(count) != "0" {
			return fmt.Errorf("refused TLS initialization changed database authority")
		}
	}
	if err = s.phase(image, "init-user", nil); err != nil {
		return err
	}
	checkpoint, e := s.query(true, "SELECT (SELECT count(*) FROM pg_roles WHERE rolname='"+s.material.app+"')::text || '|' || (SELECT count(*) FROM pg_database WHERE datname='"+s.material.database+"')::text")
	if e != nil || string(checkpoint) != "1|0" {
		return fmt.Errorf("native phase-boundary interruption checkpoint differs")
	}
	if err = s.phase(image, "init", nil); err != nil {
		return err
	}
	if err = s.phase(image, "setup", nil); err != nil {
		return err
	}
	if _, err = s.query(true, "ALTER ROLE "+s.material.admin+" NOLOGIN"); err != nil {
		return err
	}
	if _, err = s.query(true, "SELECT 1"); err == nil {
		return fmt.Errorf("retired database administrator can still log in")
	}
	privileges, e := s.query(false, "SELECT (NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND NOT rolbypassrls AND NOT pg_has_role(current_user,'"+s.material.admin+"','MEMBER'))::text FROM pg_roles WHERE rolname=current_user")
	if e != nil || string(privileges) != "true" {
		return fmt.Errorf("ordinary database application authority differs")
	}
	const snapshotSQL = `WITH i AS (SELECT * FROM eventstore.events2 WHERE event_type='instance.added' AND aggregate_type='instance'), u AS (SELECT * FROM eventstore.events2 WHERE event_type='user.human.added' AND aggregate_type='user') SELECT json_build_object('instances',(SELECT count(*) FROM eventstore.events2 WHERE event_type='instance.added'),'humans',(SELECT count(*) FROM eventstore.events2 WHERE event_type='user.human.added'),'verified',(SELECT count(*) FROM eventstore.events2 WHERE event_type='user.human.email.verified'),'instanceId',(SELECT instance_id FROM i),'humanId',(SELECT aggregate_id FROM u),'instanceName',(SELECT payload->>'name' FROM i),'userName',(SELECT payload->>'userName' FROM u),'email',(SELECT payload->>'email' FROM u),'humanInstanceId',(SELECT instance_id FROM u),'instanceAggregateId',(SELECT aggregate_id FROM i),'instanceSequence',(SELECT sequence FROM i),'humanSequence',(SELECT sequence FROM u))`
	raw, err = s.query(false, snapshotSQL)
	if err != nil {
		return err
	}
	before, err := lifecycleIdentitySnapshot(raw, s.material)
	if err != nil {
		return err
	}
	for trial := 0; trial < 2; trial++ {
		if trial == 1 {
			for _, id := range []string{s.service, s.database} {
				if _, err = run(time.Minute, "docker", "stop", "--time=20", id); err != nil {
					return fmt.Errorf("durable lifecycle shutdown failed")
				}
				actual, e := s.inspect(id, "exited")
				if e != nil || actual.State.ExitCode != 0 {
					return fmt.Errorf("durable lifecycle shutdown unclean")
				}
				if err = s.remove(id); err != nil {
					return err
				}
			}
			if err = s.databaseStart(databaseImageID, true); err != nil {
				return err
			}
		}
		if err = s.phase(image, "serve", nil); err != nil {
			return err
		}
		client, target, e := s.ready()
		if e != nil {
			return e
		}
		client.CloseIdleConnections()
		badCA, e := lifecycleTLSClient(target, s.material.files["untrusted-ca.pem"])
		if e != nil {
			return e
		}
		_, e = serviceGet(badCA, "https://"+lifecycleAuthority, "/debug/ready")
		badCA.CloseIdleConnections()
		if e == nil {
			return fmt.Errorf("native service accepted an untrusted CA")
		}
		badName, e := lifecycleTLSClient(target, s.material.files["ca.pem"])
		if e != nil {
			return e
		}
		badName.Transport.(*lifecycleTransport).transport.TLSClientConfig.ServerName = "other.identity.fixture.localhost"
		_, e = serviceGet(badName, "https://"+lifecycleAuthority, "/debug/ready")
		badName.CloseIdleConnections()
		if e == nil {
			return fmt.Errorf("native service accepted the wrong TLS hostname")
		}
		plain := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != lifecycleAuthority {
				return nil, fmt.Errorf("plaintext refusal probe escaped")
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", target)
		}}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("plaintext redirect refused") }}
		_, e = serviceGet(plain, "http://"+lifecycleAuthority, "/debug/ready")
		plain.CloseIdleConnections()
		if e == nil {
			return fmt.Errorf("native TLS service accepted plaintext")
		}
		raw, e = s.query(false, snapshotSQL)
		if e != nil {
			return e
		}
		after, e := lifecycleIdentitySnapshot(raw, s.material)
		if e != nil || after != before {
			return fmt.Errorf("durable identity changed across ordinary start or restart")
		}
		ip, _, e := net.SplitHostPort(target)
		if e != nil {
			return e
		}
		connections, e := s.query(false, "SELECT count(*) FROM pg_stat_activity a JOIN pg_stat_ssl t USING(pid) WHERE a.usename=current_user AND a.client_addr='"+ip+"'::inet AND t.ssl AND t.version='TLSv1.3'")
		count, parseError := strconv.Atoi(string(connections))
		if e != nil || parseError != nil || count < 1 {
			return fmt.Errorf("ordinary service database TLS connection unproven")
		}
		if _, e = s.query(true, "SELECT 1"); e == nil {
			return fmt.Errorf("database administrator login revived")
		}
	}
	resources, err := s.resources()
	if err != nil {
		return err
	}
	usage, err := run(30*time.Second, "docker", "exec", "--user=999:999", s.database, "du", "-sk", "/var/lib/postgresql/data")
	fields := strings.Fields(string(usage))
	if err != nil || len(fields) != 2 || fields[1] != "/var/lib/postgresql/data" {
		return fmt.Errorf("durable fixture size unknown")
	}
	diskKiB, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || diskKiB <= 0 || diskKiB > 262144 {
		return fmt.Errorf("durable fixture exceeds declared 256MiB observation budget")
	}
	afterSource, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil || imageHash(afterSource) != imageHash(source) {
		return fmt.Errorf("lifecycle fixture changed during execution")
	}
	s.run = originalRun
	if err = s.cleanup(); err != nil {
		return err
	}
	receipt := struct {
		SchemaVersion             int                       `json:"schemaVersion"`
		Profile                   string                    `json:"profile"`
		Image                     string                    `json:"localImageId"`
		Upstream                  string                    `json:"upstreamCommit"`
		Source                    string                    `json:"serviceSourceSha256"`
		Database                  string                    `json:"databaseImage"`
		Started                   time.Time                 `json:"startedAt"`
		Elapsed                   int64                     `json:"elapsedMilliseconds"`
		DiskKiB                   int64                     `json:"databaseVolumeKiB"`
		Identity                  string                    `json:"fictionalIdentitySha256"`
		VerifiedTLS               bool                      `json:"verifiedTls"`
		PhaseSeparation           bool                      `json:"phaseSeparation"`
		AdministratorRevoked      bool                      `json:"administratorRevoked"`
		DurableRestart            bool                      `json:"durableRestart"`
		PhaseBoundaryInterruption bool                      `json:"phaseBoundaryInterruption"`
		Samples                   []lifecycleResourceSample `json:"resourceSamples"`
		AuthenticationProof       bool                      `json:"authenticationProof"`
		Cleanup                   string                    `json:"cleanup"`
		Scope                     string                    `json:"observationScope"`
	}{1, "isolated-tls-durable-lifecycle", image, imagePin, imageHash(source), serviceDatabaseImage, started, time.Since(started).Milliseconds(), diskKiB, before, true, true, true, true, true, resources, false, "complete", "two owned containers; initialization/setup, two ordinary starts and three idle rounds with a one-second minimum delay between rounds; process measurements are namespace-init only, container measurements include children; 12-minute execution plus bounded owned cleanup; not Dex, portal, authentication, load sizing or backup/restore"}
	raw, err = json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "lifecycle-smoke.json", append(raw, '\n'), 0444)
}

// lifecycleResourceSample separates a namespace-init process observation from
// Docker's whole-container accounting; process RSS is never summed across forks.
type lifecycleResourceSample struct {
	Component         string    `json:"component"`
	ObservedAt        time.Time `json:"observedAt"`
	PID               int       `json:"namespaceInitPid"`
	UID               int       `json:"namespaceInitUid"`
	ProcessRSSKiB     int64     `json:"namespaceInitRssKiB"`
	ProcessPeakRSSKiB int64     `json:"namespaceInitPeakRssKiB"`
	ProcessThreads    int64     `json:"namespaceInitThreads"`
	DockerMemoryUsage string    `json:"dockerMemoryUsage"`
	DockerCPUPercent  string    `json:"dockerCpuPercent"`
	DockerPIDs        int       `json:"dockerPids"`
}

// lifecycleStats accepts only Docker's small numeric memory/CPU/PID projection,
// not arbitrary daemon output or container names that could disclose host details.
func lifecycleStats(raw []byte) error {
	parts := strings.Split(strings.TrimSpace(string(raw)), "|")
	if len(raw) > 200 || len(parts) != 3 || !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:B|kB|KiB|MB|MiB|GB|GiB) / [0-9]+(?:\.[0-9]+)?(?:B|kB|KiB|MB|MiB|GB|GiB)$`).MatchString(parts[0]) || !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?%$`).MatchString(parts[1]) {
		return fmt.Errorf("numeric component resource observation required")
	}
	pids, err := strconv.Atoi(parts[2])
	if err != nil || pids < 1 || pids > 128 {
		return fmt.Errorf("bounded component PID observation required")
	}
	return nil
}

// resources collects three declared idle samples of both included components.
// Every process comes from the freshly inspected container's actual host PID.
func (s *lifecycleSession) resources() ([]lifecycleResourceSample, error) {
	samples := []lifecycleResourceSample{}
	for trial := 0; trial < 3; trial++ {
		for _, component := range []struct {
			id, name string
			uid      int
		}{{s.service, "zitadel", 65532}, {s.database, "postgres", 999}} {
			actual, err := s.inspect(component.id, "running")
			if err != nil {
				return nil, err
			}
			raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(actual.State.Pid), "status"))
			if err != nil {
				return nil, fmt.Errorf("native component process observation unavailable")
			}
			process, err := restrictedProcessStatus(raw, actual.State.Pid, component.name, component.uid)
			if err != nil {
				return nil, err
			}
			if component.name == "zitadel" {
				raw, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(actual.State.Pid), "mountinfo"))
				if err != nil {
					return nil, fmt.Errorf("native service mount observation unavailable")
				}
				if _, err = serviceMountStatus(raw); err != nil {
					return nil, err
				}
			}
			raw, err = s.run(20*time.Second, "docker", "stats", "--no-stream", "--format", "{{.MemUsage}}|{{.CPUPerc}}|{{.PIDs}}", component.id)
			if err != nil || lifecycleStats(raw) != nil {
				return nil, fmt.Errorf("native component container observation unavailable")
			}
			parts := strings.Split(strings.TrimSpace(string(raw)), "|")
			pids, _ := strconv.Atoi(parts[2])
			after, err := s.inspect(component.id, "running")
			if err != nil || after.State.Pid != actual.State.Pid {
				return nil, fmt.Errorf("component changed during resource observation")
			}
			samples = append(samples, lifecycleResourceSample{component.name, time.Now().UTC(), process.PID, process.UID, process.RSSKiB, process.PeakRSSKiB, process.Threads, parts[0], parts[1], pids})
		}
		if trial < 2 {
			time.Sleep(time.Second)
		}
	}
	return samples, nil
}
