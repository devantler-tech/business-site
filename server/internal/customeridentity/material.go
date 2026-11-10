package customeridentity

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const setupMailPhase = "setup-mail"

var errMaterial = errors.New("customer identity private material refused")

var materialIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{2,62}$`)
var materialReference = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

// Materialize prepares one private native ZITADEL phase without executing it.
// Inputs are closed owner-only files. Outputs are not deployment or login proof.
func Materialize(phase, inputDirectory, outputDirectory string) error {
	names, fields, err := materialProfile(phase)
	if err != nil {
		return errMaterial
	}
	snapshot, err := materialRead(inputDirectory, names)
	if err != nil {
		return errMaterial
	}
	defer snapshot.close()
	return materializeSnapshot(phase, outputDirectory, fields, snapshot)
}

// materializeSnapshot validates native documents before reserving private output.
func materializeSnapshot(phase, outputDirectory string, fields []string, snapshot *materialSnapshot) error {
	settings, err := materialSettings(snapshot.files["settings.json"], fields)
	if err != nil {
		return errMaterial
	}
	outputs, err := materialDocuments(phase, settings, snapshot.files)
	if err != nil {
		return errMaterial
	}
	if err := snapshot.unchanged(); err != nil {
		return errMaterial
	}
	if err := materialWriteOutputs(outputDirectory, outputs, snapshot); err != nil {
		return errMaterial
	}
	return nil
}

// materialProfile closes both the input membership and each phase's authority.
func materialProfile(phase string) ([]string, []string, error) {
	names := []string{"settings.json", "database-ca.pem", "database-password"}
	fields := []string{"schemaVersion", "dexIssuer", "zitadelIssuer", "portalCallback", "databaseHost", "databaseName", "databaseUser"}
	switch phase {
	case "init":
		names = append(names, "database-admin-password")
		fields = append(fields, "databaseAdminUser")
	case "setup", setupMailPhase:
		names = append(names, "master.key", "tls.crt", "tls.key", "service-ca.pem", "bootstrap-password")
		fields = append(fields, "instanceName", "organizationName", "humanUsername", "humanFirstName", "humanLastName", "humanEmail")
		if phase == setupMailPhase {
			names = append(names, "smtp-password")
			fields = append(fields, "smtpHost", "smtpPort", "smtpFrom", "smtpUser")
		}
	case "serve":
		names = append(names, "master.key", "tls.crt", "tls.key", "service-ca.pem")
	default:
		return nil, nil, errMaterial
	}
	return names, fields, nil
}

// materialSettings refuses duplicate, unknown, missing and non-string members.
func materialSettings(data []byte, fields []string) (map[string]string, error) {
	if !utf8.Valid(data) {
		return nil, errMaterial
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errMaterial
	}
	settings := make(map[string]string, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] {
			return nil, errMaterial
		}
		if _, exists := settings[key]; exists {
			return nil, errMaterial
		}
		token, err = decoder.Token()
		value, ok := token.(string)
		if err != nil || !ok || value == "" || len(value) > 512 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") || strings.Contains(value, "${") {
			return nil, errMaterial
		}
		settings[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(settings) != len(fields) {
		return nil, errMaterial
	}
	if _, err := decoder.Token(); err != io.EOF || settings["schemaVersion"] != "customer-zitadel-private-v1" {
		return nil, errMaterial
	}
	return settings, nil
}

// materialDocuments composes only fixed native fields with JSON-safe values.
func materialDocuments(phase string, settings map[string]string, files map[string][]byte) (map[string][]byte, error) {
	endpoints := Endpoints{DexIssuer: settings["dexIssuer"], ZitadelIssuer: settings["zitadelIssuer"], PortalCallback: settings["portalCallback"]}
	if _, err := Prepare(true, endpoints); err != nil {
		return nil, errMaterial
	}
	host, err := endpoint("https://"+settings["databaseHost"], "")
	if err != nil || host.Host != settings["databaseHost"] || !materialApplicationDatabase(settings) || !materialPasswordValid(files["database-password"]) {
		return nil, errMaterial
	}
	if _, err := materialRoots(files["database-ca.pem"]); err != nil {
		return nil, errMaterial
	}
	idp, err := endpoint(endpoints.ZitadelIssuer, "")
	if err != nil {
		return nil, errMaterial
	}
	portal, err := endpoint(endpoints.PortalCallback, "/portal/callback")
	if err != nil {
		return nil, errMaterial
	}
	provider := nativeProvider(idp, portal)
	ssl := func() map[string]any {
		return map[string]any{"Mode": "verify-full", "RootCert": "/run/customer-zitadel/ca.pem", "Cert": "", "Key": ""}
	}
	admin := map[string]any{"ExistingDatabase": "postgres", "Username": "", "Password": "", "SSL": ssl()}
	if phase == "init" {
		if !materialIdentifier.MatchString(settings["databaseAdminUser"]) || settings["databaseAdminUser"] == settings["databaseUser"] || !materialPasswordValid(files["database-admin-password"]) || bytes.Equal(files["database-password"], files["database-admin-password"]) {
			return nil, errMaterial
		}
		admin["Username"], admin["Password"] = settings["databaseAdminUser"], string(files["database-admin-password"])
	}
	provider["Database"] = map[string]any{"postgres": map[string]any{"DSN": "", "Host": host.Host, "Port": 5432, "Database": settings["databaseName"], "User": map[string]any{"Username": settings["databaseUser"], "Password": string(files["database-password"]), "SSL": ssl()}, "Admin": admin}}
	outputs := map[string][]byte{"ca.pem": bytes.Clone(files["database-ca.pem"])}
	if phase != "init" {
		if len(files["master.key"]) != 32 || materialReference.Match(files["master.key"]) || materialServiceTLS(idp, files) != nil {
			return nil, errMaterial
		}
		for _, name := range []string{"master.key", "tls.crt", "tls.key"} {
			outputs[name] = bytes.Clone(files[name])
		}
	}
	if phase == "setup" || phase == setupMailPhase {
		address, e := mail.ParseAddress(settings["humanEmail"])
		if e != nil || address.Address != settings["humanEmail"] || !materialIdentifier.MatchString(settings["humanUsername"]) || !materialBootstrapPassword(files["bootstrap-password"]) || bytes.Equal(files["bootstrap-password"], files["database-password"]) {
			return nil, errMaterial
		}
		steps := map[string]any{"FirstInstance": map[string]any{"Skip": false, "InstanceName": settings["instanceName"], "Org": map[string]any{"Name": settings["organizationName"], "Human": map[string]any{"UserName": settings["humanUsername"], "FirstName": settings["humanFirstName"], "LastName": settings["humanLastName"], "NickName": "", "DisplayName": settings["humanFirstName"] + " " + settings["humanLastName"], "PreferredLanguage": "en", "Gender": 0, "Email": map[string]any{"Address": address.Address, "Verified": false}, "Password": string(files["bootstrap-password"]), "PasswordChangeRequired": true}}}}
		outputs["steps.json"], err = json.MarshalIndent(steps, "", "  ")
		if err != nil {
			return nil, errMaterial
		}
	}
	if phase == setupMailPhase {
		mailConfig, err := materialSetupMail(settings, files)
		if err != nil {
			return nil, errMaterial
		}
		provider["DefaultInstance"].(map[string]any)["SMTPConfiguration"] = mailConfig
	}
	outputs["config.json"], err = json.MarshalIndent(provider, "", "  ")
	if err != nil {
		return nil, errMaterial
	}
	return outputs, nil
}

// materialSetupMail emits the pinned native SMTP settings for fresh instance setup.
func materialSetupMail(settings map[string]string, files map[string][]byte) (map[string]any, error) {
	host := settings["smtpHost"]
	port := settings["smtpPort"]
	if _, err := endpoint("https://"+host, ""); err != nil || (port != "465" && port != "587") {
		return nil, errMaterial
	}
	from, user := settings["smtpFrom"], settings["smtpUser"]
	address, err := mail.ParseAddress(from)
	_, domain, hasDomain := strings.Cut(from, "@")
	if err != nil || address.Address != from || !hasDomain || !dnsName.MatchString(domain) ||
		strings.IndexFunc(from+user, func(r rune) bool { return r <= ' ' || r >= 127 }) >= 0 {
		return nil, errMaterial
	}
	password := files["smtp-password"]
	if !materialPasswordValid(password) || bytes.Equal(password, files["database-password"]) ||
		bytes.Equal(password, files["bootstrap-password"]) || bytes.Equal(password, files["master.key"]) {
		return nil, errMaterial
	}
	return map[string]any{
		"Description": "Customer identity email",
		"From":        from,
		"FromName":    "Devantler Tech",
		"Tls":         true,
		"SMTP": map[string]any{
			"Host":      net.JoinHostPort(host, port),
			"PlainAuth": map[string]string{"User": user, "Password": string(password)},
		},
	}, nil
}

// materialApplicationDatabase refuses the built-in shared stores and privileged role namespace.
func materialApplicationDatabase(settings map[string]string) bool {
	name, user := settings["databaseName"], settings["databaseUser"]
	return materialIdentifier.MatchString(name) && materialIdentifier.MatchString(user) && name != "postgres" && name != "template0" && name != "template1" && user != "postgres" && !strings.HasPrefix(user, "pg_")
}

// materialPasswordValid requires a complete opaque password, not an environment reference.
func materialPasswordValid(value []byte) bool {
	return len(value) >= 32 && len(value) <= 1024 && utf8.Valid(value) && !bytes.ContainsAny(value, "\x00\r\n") && !bytes.Contains(value, []byte("${")) && bytes.Equal(bytes.TrimSpace(value), value)
}

// materialBootstrapPassword also satisfies the selected native complexity classes.
func materialBootstrapPassword(value []byte) bool {
	if !materialPasswordValid(value) || len(value) > 72 {
		return false
	}
	lower, upper, digit, symbol := false, false, false, false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			lower = true
		case character >= 'A' && character <= 'Z':
			upper = true
		case character >= '0' && character <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	return lower && upper && digit && symbol
}

// materialRoots admits only currently valid CA certificates with no hidden PEM payload.
func materialRoots(data []byte) (*x509.CertPool, error) {
	roots := x509.NewCertPool()
	count := 0
	for len(bytes.TrimSpace(data)) > 0 {
		if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, errMaterial
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errMaterial
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
			return nil, errMaterial
		}
		roots.AddCert(certificate)
		count++
		data = rest
	}
	if count == 0 {
		return nil, errMaterial
	}
	return roots, nil
}

// materialServiceTLS checks the exact issuer host, current chain and matching key offline.
func materialServiceTLS(idp *url.URL, files map[string][]byte) error {
	pair, err := tls.X509KeyPair(files["tls.crt"], files["tls.key"])
	if err != nil || len(pair.Certificate) == 0 {
		return errMaterial
	}
	roots, err := materialRoots(files["service-ca.pem"])
	if err != nil {
		return errMaterial
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA {
		return errMaterial
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		certificate, e := x509.ParseCertificate(der)
		if e != nil {
			return errMaterial
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: idp.Host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return errMaterial
	}
	return nil
}
