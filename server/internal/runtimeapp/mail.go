package runtimeapp

import (
	"context"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"time"

	"github.com/devantler-tech/business-site/server/internal/identity"
	pmail "github.com/devantler-tech/business-site/server/internal/mail"
)

// mailConfig is the optional sender for the portal's own emails: the site's
// send-only SES credential (platform#5400) in two projected files.
type mailConfig struct {
	usernameFile, passwordFile, addr, from string
}

// readMailConfig reads both credential files or neither. The server and the
// sender address are optional and default to the site's SES sender.
func readMailConfig(getenv func(string) string) (mailConfig, error) {
	c := mailConfig{usernameFile: getenv("PORTAL_SMTP_USERNAME_FILE"), passwordFile: getenv("PORTAL_SMTP_PASSWORD_FILE"), addr: getenv("PORTAL_SMTP_ADDR"), from: getenv("PORTAL_MAIL_FROM")}
	if c == (mailConfig{}) {
		return c, nil
	}
	if !filepath.IsAbs(c.usernameFile) || !filepath.IsAbs(c.passwordFile) {
		return mailConfig{}, ErrConfig
	}
	if c.addr == "" {
		c.addr = pmail.DefaultAddr
	}
	if c.from == "" {
		c.from = pmail.DefaultFrom
	}
	host, port, err := net.SplitHostPort(c.addr)
	from, fromErr := mail.ParseAddress(c.from)
	if err != nil || host == "" || port != "587" || fromErr != nil || from.Name != "" || from.Address != c.from {
		return mailConfig{}, ErrConfig
	}
	return c, nil
}

// startMail reads the credential from its projected files and starts sending
// the queued emails. The returned function stops it.
func startMail(f *identity.Foundation, c mailConfig) (func(), error) {
	if c == (mailConfig{}) {
		return func() {}, nil
	}
	if os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return nil, ErrConfig
	}
	var values [2]string
	for i, file := range []string{c.usernameFile, c.passwordFile} {
		data, err := projectionBytes(file)
		if err != nil {
			return nil, ErrConfig
		}
		values[i] = trimInputLineEnding(string(data))
	}
	sender, err := pmail.New(c.addr, values[0], values[1], c.from, nil)
	if err != nil {
		return nil, ErrConfig
	}
	if err = f.EnableMail(sender); err != nil {
		return nil, ErrConfig
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.RunMail(ctx, time.Minute)
	}()
	return func() { cancel(); <-done }, nil
}
