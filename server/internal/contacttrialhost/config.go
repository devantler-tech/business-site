// Package contacttrialhost composes the explicit, loopback-only experiment host.
// It neither provisions fixtures nor belongs to the production runtime graph.
package contacttrialhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
)

var errSetup = errors.New("invalid trial setup; use the documented private pipe and disposable loopback fixture")

type setup struct {
	mode, database, issuer, callback, certificate, key, trust, node, worker string
}

var databaseName = regexp.MustCompile(`^/portal_trial_[0-9a-f]{32}$`)

// readSetup accepts one small, flat object with no duplicate or unknown fields.
func readSetup(input io.Reader) (setup, error) {
	var c setup
	data, err := io.ReadAll(io.LimitReader(input, 65537))
	if err != nil || len(data) > 65536 {
		return c, errSetup
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return c, errSetup
	}
	fields := map[string]*string{"mode": &c.mode, "database": &c.database, "issuer": &c.issuer, "callback": &c.callback, "certificate": &c.certificate, "key": &c.key, "trust": &c.trust, "node": &c.node, "worker": &c.worker}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] || fields[name] == nil {
			return c, errSetup
		}
		value, err := d.Token()
		text, ok := value.(string)
		if err != nil || !ok {
			return c, errSetup
		}
		*fields[name], seen[name] = text, true
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return c, errSetup
	}
	if _, err := d.Token(); err != io.EOF || len(seen) != len(fields) {
		return c, errSetup
	}
	db, err := url.Parse(c.database)
	if err != nil || db == nil || db.Scheme != "postgres" || db.User == nil || db.User.Username() != "portal_runtime" || (db.Host != "127.0.0.1:5432" && db.Host != "127.0.0.1:55444") || !databaseName.MatchString(db.Path) || db.RawPath != "" || db.RawQuery != "sslmode=disable" || db.Fragment != "" {
		return c, errSetup
	}
	if password, ok := db.User.Password(); !ok || password != "fixture-only" {
		return c, errSetup
	}
	if !loopbackURL(c.issuer, "") || !loopbackURL(c.callback, "/portal/callback") || c.certificate == "" || c.key == "" || c.trust == "" {
		return c, errSetup
	}
	switch c.mode {
	case "go", "closed":
		if c.node != "" || c.worker != "" {
			return c, errSetup
		}
	case "svelte":
		if !filepath.IsAbs(c.node) || !filepath.IsAbs(c.worker) {
			return c, errSetup
		}
	default:
		return c, errSetup
	}
	return c, nil
}

func loopbackURL(raw, path string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() == "127.0.0.1" && u.Port() != "" && u.User == nil && u.Path == path && u.RawPath == "" && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}
