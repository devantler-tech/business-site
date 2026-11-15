package runtimeapp

import (
	"path/filepath"
	"testing"
)

func TestMailConfigIsOptionalAndStrict(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	if c, err := readMailConfig(env(nil)); err != nil || c != (mailConfig{}) {
		t.Fatal("absent mail configuration was not empty", err)
	}
	files := map[string]string{"PORTAL_SMTP_USERNAME_FILE": "/run/webapp-inputs/smtp-username", "PORTAL_SMTP_PASSWORD_FILE": "/run/webapp-inputs/smtp-password"}
	c, err := readMailConfig(env(files))
	if err != nil || c.addr != "email-smtp.eu-central-1.amazonaws.com:587" || c.from != "no-reply@devantler.tech" {
		t.Fatalf("defaults differ: %+v %v", c, err)
	}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for key, value := range files {
			m[key] = value
		}
		m[k] = v
		return m
	}
	for name, values := range map[string]map[string]string{
		"username without password": {"PORTAL_SMTP_USERNAME_FILE": "/u"},
		"password without username": {"PORTAL_SMTP_PASSWORD_FILE": "/p"},
		"relative files":            {"PORTAL_SMTP_USERNAME_FILE": "u", "PORTAL_SMTP_PASSWORD_FILE": "p"},
		"server without files":      {"PORTAL_SMTP_ADDR": "email-smtp.eu-central-1.amazonaws.com:587"},
		"implicit TLS port":         with("PORTAL_SMTP_ADDR", "email-smtp.eu-central-1.amazonaws.com:465"),
		"server without port":       with("PORTAL_SMTP_ADDR", "email-smtp.eu-central-1.amazonaws.com"),
		"named sender":              with("PORTAL_MAIL_FROM", "Devantler <no-reply@devantler.tech>"),
		"not an address":            with("PORTAL_MAIL_FROM", "no-reply"),
	} {
		if _, err := readMailConfig(env(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := ReadConfig(env(files)); err == nil {
		t.Fatal("mail configured without a portal")
	}
	if stop, err := startMail(nil, mailConfig{}); err != nil || stop == nil {
		t.Fatal(err)
	}
	if _, err := startMail(nil, mailConfig{usernameFile: filepath.Join(t.TempDir(), "absent"), passwordFile: "/absent", addr: "email-smtp.eu-central-1.amazonaws.com:587", from: "no-reply@devantler.tech"}); err == nil {
		t.Fatal("a missing credential file started mail")
	}
}
