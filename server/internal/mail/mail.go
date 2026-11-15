// Package mail sends the portal's plain-text emails through one SMTP
// submission server, as the site's send-only SES sender (platform#5400)
// expects: STARTTLS is required before SMTP AUTH, and every message comes from
// one fixed address. Errors never carry an address, subject or body.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// DefaultAddr is the SES SMTP endpoint of the site's sender.
const DefaultAddr = "email-smtp.eu-central-1.amazonaws.com:587"

// DefaultFrom is the only address the sender may use.
const DefaultFrom = "no-reply@devantler.tech"

// Sender submits messages to one server with one credential.
type Sender struct {
	addr, host, username, password string
	from                           mail.Address
	roots                          *x509.CertPool // nil: the system roots
}

// New returns a sender. roots is nil outside tests.
func New(addr, username, password, from string, roots *x509.CertPool) (*Sender, error) {
	host, port, err := net.SplitHostPort(addr)
	sender, fromErr := mail.ParseAddress(from)
	if err != nil || host == "" || port == "" || username == "" || password == "" || fromErr != nil || sender.Name != "" {
		return nil, errors.New("mail: invalid sender configuration")
	}
	return &Sender{addr: addr, host: host, username: username, password: password, from: mail.Address{Name: "Devantler Tech", Address: sender.Address}, roots: roots}, nil
}

// Message is one plain-text email. ID makes its Message-ID unique.
type Message struct {
	ID, To, Subject, Body string
}

// Send delivers one message, or fails without having sent it.
func (s *Sender) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil || to.Name != "" || m.ID == "" || strings.ContainsAny(m.ID+m.Subject, "\r\n<>") || m.Subject == "" || m.Body == "" {
		return errors.New("mail: invalid message")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return errors.New("mail: server unreachable")
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return errors.New("mail: server refused the session")
	}
	defer func() { _ = c.Close() }()
	if err = c.Hello("devantler.tech"); err != nil {
		return errors.New("mail: server refused the greeting")
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("mail: server offers no STARTTLS")
	}
	if err = c.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12, RootCAs: s.roots}); err != nil {
		return errors.New("mail: TLS failed")
	}
	if err = c.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return errors.New("mail: authentication refused")
	}
	if err = c.Mail(s.from.Address); err != nil {
		return errors.New("mail: sender refused")
	}
	if err = c.Rcpt(to.Address); err != nil {
		return errors.New("mail: recipient refused")
	}
	w, err := c.Data()
	if err != nil {
		return errors.New("mail: message refused")
	}
	if _, err = w.Write(s.format(to.Address, m)); err != nil {
		return errors.New("mail: message not sent")
	}
	if err = w.Close(); err != nil {
		return errors.New("mail: message refused")
	}
	_ = c.Quit()
	return nil
}

// format renders the headers and a quoted-printable UTF-8 body.
func (s *Sender) format(to string, m Message) []byte {
	var body bytes.Buffer
	qp := quotedprintable.NewWriter(&body)
	_, _ = qp.Write([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n")))
	_ = qp.Close()
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@devantler.tech>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
		s.from.String(), to, mime.QEncoding.Encode("UTF-8", m.Subject), time.Now().UTC().Format(time.RFC1123Z), m.ID)
	b.Write(body.Bytes())
	b.WriteString("\r\n")
	return b.Bytes()
}
