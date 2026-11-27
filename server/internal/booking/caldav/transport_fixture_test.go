package caldav

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

const selectedCalendarHost = "calendar.example.test"

var fixturePublicAddress = netip.MustParseAddr("1.1.1.1")

type transportFixture struct {
	server   *httptest.Server
	network  sourceNetwork
	lookups  atomic.Int32
	dials    atomic.Int32
	requests atomic.Int32
	mu       sync.Mutex
	events   []string
	checked  []netip.Addr
	answer   func(context.Context) ([]netip.Addr, error)
	password func(int32) string
	handler  http.HandlerFunc
}

func newTransportFixture(t *testing.T, certificateHost string) *transportFixture {
	return newCalendarTransportFixture(t, certificateHost, "REPORT", "/calendars/selected/")
}

func newCalendarTransportFixture(t *testing.T, certificateHost, method, path string) *transportFixture {
	t.Helper()
	f := &transportFixture{}
	f.answer = func(context.Context) ([]netip.Addr, error) { return []netip.Addr{fixturePublicAddress}, nil }
	f.password = func(int32) string { return "fictional-password" }
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record("request")
		call := f.requests.Add(1)
		user, password, ok := r.BasicAuth()
		if r.Method != method || r.URL.RequestURI() != path || r.Host != selectedCalendarHost || r.TLS == nil || r.TLS.ServerName != selectedCalendarHost || !ok || user != "fictional-user" || password != f.password(call) {
			t.Error("request escaped its selected source, TLS name or current credential")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if f.handler != nil {
			f.handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fictional provider//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20270327T210000Z\r\nDTSTART:20270327T210000Z\r\nDTEND:20270327T220000Z\r\nFREEBUSY:20270327T211500Z/20270327T213000Z\r\nEND:VFREEBUSY\r\nEND:VCALENDAR\r\n")
	}))
	f.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	certificate, roots := transportFixtureCertificate(t, certificateHost)
	f.server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	f.network = sourceNetwork{
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			f.record("resolve")
			f.lookups.Add(1)
			if host != selectedCalendarHost {
				return nil, errors.New("fictional unexpected resolution name")
			}
			answers, err := f.answer(ctx)
			f.mu.Lock()
			f.checked = append([]netip.Addr(nil), answers...)
			f.mu.Unlock()
			return answers, err
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			f.record("dial")
			f.dials.Add(1)
			endpoint, err := netip.ParseAddrPort(address)
			f.mu.Lock()
			checked := append([]netip.Addr(nil), f.checked...)
			f.mu.Unlock()
			matched := false
			for _, candidate := range checked {
				matched = matched || endpoint.Addr() == candidate.Unmap()
			}
			if err != nil || network != "tcp" || endpoint.Port() != 443 || !matched {
				return nil, errors.New("fictional unchecked dial destination")
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", f.server.Listener.Addr().String())
		},
		roots: roots,
	}
	return f
}

func transportFixtureCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("fictional TLS key could not be generated", err)
	}
	certificate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fictional calendar fixture"},
		NotBefore:             time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC),
		DNSNames:              []string{host},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal("fictional TLS certificate could not be generated", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal("fictional TLS certificate could not be parsed", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func (f *transportFixture) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *transportFixture) trace() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func transportClient(t *testing.T, config Config, credentials func(context.Context) (string, string, error), network sourceNetwork) *Client {
	t.Helper()
	client, err := New(config, credentials)
	if err != nil || client == nil {
		t.Fatal("explicit transport test selection was refused", err)
	}
	return withSourceNetwork(client, network)
}

func selectedTransportClient(t *testing.T, fixture *transportFixture) *Client {
	t.Helper()
	config := Config{Origin: "https://" + selectedCalendarHost, Calendars: []string{"/calendars/selected/"}}
	return transportClient(t, config, fixture.readCredential, fixture.network)
}

func (f *transportFixture) readCredential(context.Context) (string, string, error) {
	f.record("credential")
	return "fictional-user", "fictional-password", nil
}

func requireBusyCancelledDuringCredentials(t *testing.T, client *Client, entered <-chan struct{}, release chan<- struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		snapshot, err := client.Busy(ctx, transportCoverage())
		done <- errors.Is(err, booking.ErrUnavailable) && reflect.DeepEqual(snapshot, booking.BusySnapshot{})
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("operation finished before reaching the credential boundary")
	case <-time.After(2 * time.Second):
		t.Fatal("operation never reached the credential boundary")
	}
	cancel()
	close(release)
	select {
	case refused := <-done:
		if !refused {
			t.Fatal("cancelled operation returned usable availability")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled credential wait did not finish")
	}
}

func transportCoverage() booking.Interval {
	from := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	return booking.Interval{Start: from, End: from.Add(time.Hour)}
}

type ambientRoundTrip func(*http.Request) (*http.Response, error)

func (f ambientRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func blockAmbientNetwork(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	previousClient, previousTransport, previousResolver := http.DefaultClient, http.DefaultTransport, net.DefaultResolver
	blocked := ambientRoundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("fictional ambient HTTP refused")
	})
	http.DefaultClient = &http.Client{Transport: blocked}
	http.DefaultTransport = blocked
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("fictional ambient DNS refused")
	}}
	t.Cleanup(func() {
		http.DefaultClient, http.DefaultTransport, net.DefaultResolver = previousClient, previousTransport, previousResolver
		if calls.Load() != 0 {
			t.Error("adapter consulted ambient HTTP or DNS")
		}
	})
	return &calls
}
