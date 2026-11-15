package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is a minimal SMTP submission server: STARTTLS first, then AUTH
// PLAIN, one message per session.
type fakeServer struct {
	addr     string
	roots    *x509.CertPool
	tls      bool // offer STARTTLS
	mu       sync.Mutex
	auth     string
	from, to string
	data     string
}

func newFakeServer(t *testing.T, offerTLS bool) *fakeServer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	f := &fakeServer{addr: "localhost:" + port, roots: roots, tls: offerTLS}
	config := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, config)
		}
	}()
	return f
}

func (f *fakeServer) serve(conn net.Conn, config *tls.Config) {
	defer func() { _ = conn.Close() }()
	r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
	reply := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	reply("220 localhost fake")
	secure := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch {
		case verb == "EHLO":
			if f.tls && !secure {
				reply("250-localhost\r\n250 STARTTLS")
			} else {
				reply("250-localhost\r\n250 AUTH PLAIN")
			}
		case verb == "STARTTLS" && f.tls:
			reply("220 go ahead")
			conn = tls.Server(conn, config)
			r, w = bufio.NewReader(conn), bufio.NewWriter(conn)
			secure = true
		case verb == "AUTH":
			f.mu.Lock()
			f.auth = strings.TrimPrefix(line, "AUTH PLAIN ")
			f.mu.Unlock()
			if decoded, _ := base64.StdEncoding.DecodeString(f.auth); string(decoded) != "\x00user\x00secret" {
				reply("535 authentication failed")
				continue
			}
			reply("235 ok")
		case verb == "MAIL":
			f.mu.Lock()
			f.from = line
			f.mu.Unlock()
			reply("250 ok")
		case verb == "RCPT":
			f.mu.Lock()
			f.to = line
			f.mu.Unlock()
			reply("250 ok")
		case verb == "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			reply("250 queued")
		case verb == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown")
		}
	}
}

func TestSendRequiresTLSAndAuthentication(t *testing.T) {
	f := newFakeServer(t, true)
	s, err := New(f.addr, "user", "secret", DefaultFrom, f.roots)
	if err != nil {
		t.Fatal(err)
	}
	body := "Tak for din ordre.\nBeløb: 3750 DKK — inkl. moms\n" + strings.Repeat("x", 100)
	if err := s.Send(t.Context(), Message{ID: "order-1", To: "client@example.invalid", Subject: "Ordrebekræftelse", Body: body}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "MAIL FROM:<no-reply@devantler.tech>" || f.to != "RCPT TO:<client@example.invalid>" {
		t.Fatalf("envelope differs: %q %q", f.from, f.to)
	}
	head, encoded, _ := strings.Cut(f.data, "\r\n\r\n")
	subject := ""
	for _, line := range strings.Split(head, "\r\n") {
		if value, ok := strings.CutPrefix(line, "Subject: "); ok {
			subject, _ = new(mime.WordDecoder).DecodeHeader(value)
		}
	}
	if subject != "Ordrebekræftelse" {
		t.Errorf("subject decodes to %q", subject)
	}
	for _, want := range []string{"From: \"Devantler Tech\" <no-reply@devantler.tech>", "To: <client@example.invalid>", "Message-ID: <order-1@devantler.tech>", "Content-Type: text/plain; charset=UTF-8"} {
		if !strings.Contains(head, want) {
			t.Errorf("header %q missing from %q", want, head)
		}
	}
	decoded := new(strings.Builder)
	if _, err := decodeQP(decoded, encoded); err != nil || strings.TrimRight(decoded.String(), "\r\n") != strings.ReplaceAll(body, "\n", "\r\n") {
		t.Fatalf("body differs: %q %v", decoded.String(), err)
	}
}

func decodeQP(dst *strings.Builder, s string) (int64, error) {
	return io.Copy(dst, quotedprintable.NewReader(strings.NewReader(s)))
}

func TestSendRefusesWithoutTLSOrWithAWrongCredential(t *testing.T) {
	plain := newFakeServer(t, false)
	s, _ := New(plain.addr, "user", "secret", DefaultFrom, plain.roots)
	if err := s.Send(t.Context(), Message{ID: "x", To: "client@example.invalid", Subject: "s", Body: "b"}); err == nil || plain.auth != "" {
		t.Fatal("a server without STARTTLS received the credential")
	}
	secure := newFakeServer(t, true)
	wrong, _ := New(secure.addr, "user", "wrong", DefaultFrom, secure.roots)
	if err := wrong.Send(t.Context(), Message{ID: "x", To: "client@example.invalid", Subject: "s", Body: "b"}); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("a refused credential was not reported cleanly: %v", err)
	}
	other, _ := New(secure.addr, "user", "secret", DefaultFrom, x509.NewCertPool())
	if err := other.Send(t.Context(), Message{ID: "x", To: "client@example.invalid", Subject: "s", Body: "b"}); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("an untrusted certificate was accepted: %v", err)
	}
	for name, m := range map[string]Message{
		"named recipient":  {ID: "x", To: "Client <client@example.invalid>", Subject: "s", Body: "b"},
		"header injection": {ID: "x", To: "client@example.invalid", Subject: "s\r\nBcc: a@example.invalid", Body: "b"},
		"no body":          {ID: "x", To: "client@example.invalid", Subject: "s"},
	} {
		if err := s.Send(t.Context(), m); err == nil {
			t.Errorf("%s was sent", name)
		}
	}
	for name, bad := range map[string][4]string{
		"no port":       {"localhost", "user", "secret", DefaultFrom},
		"no credential": {DefaultAddr, "", "", DefaultFrom},
		"named sender":  {DefaultAddr, "user", "secret", "Me <no-reply@devantler.tech>"},
	} {
		if _, err := New(bad[0], bad[1], bad[2], bad[3], nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
