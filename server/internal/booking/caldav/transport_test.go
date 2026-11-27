package caldav

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

func TestTransportPinsPublicAddressBeforeCredentialAndRequest(t *testing.T) {
	blockAmbientNetwork(t)
	cases := []struct {
		name    string
		address netip.Addr
	}{
		{"IPv4", fixturePublicAddress},
		{"IPv6", netip.MustParseAddr("2606:4700:4700::1111")},
		{"mapped IPv4", netip.MustParseAddr("::ffff:1.1.1.1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTransportFixture(t, selectedCalendarHost)
			f.answer = func(context.Context) ([]netip.Addr, error) { return []netip.Addr{tc.address}, nil }
			config := Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}
			client := transportClient(t, config, f.readCredential, f.network)
			config.Origin = "https://other.example.test"
			config.Calendars[0] = "/calendars/replaced/"
			snapshot, err := client.Busy(t.Context(), transportCoverage())
			if err != nil || !snapshot.Complete || len(snapshot.Busy) != 1 {
				t.Fatal("complete report through the checked public destination was unavailable", err)
			}
			if !reflect.DeepEqual(f.trace(), []string{"resolve", "credential", "dial", "request"}) || f.lookups.Load() != 1 || f.dials.Load() != 1 || f.requests.Load() != 1 {
				t.Fatal("resolution, constructor-bound credential, pinned dial and request ordering changed")
			}
		})
	}
}

func TestTransportRejectsInvalidResolutionBeforeCredentialOrDial(t *testing.T) {
	blockAmbientNetwork(t)
	cases := []struct {
		name    string
		answers []netip.Addr
		err     error
	}{
		{"empty", nil, nil},
		{"lookup failure", nil, errors.New("fictional-private-resolution-detail")},
		{"invalid address", []netip.Addr{{}}, nil},
		{"IPv4 loopback", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil},
		{"IPv6 loopback", []netip.Addr{netip.MustParseAddr("::1")}, nil},
		{"IPv4 private", []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil},
		{"IPv6 private", []netip.Addr{netip.MustParseAddr("fd00::1")}, nil},
		{"IPv4 link local", []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil},
		{"IPv6 link local", []netip.Addr{netip.MustParseAddr("fe80::1")}, nil},
		{"mapped private", []netip.Addr{netip.MustParseAddr("::ffff:192.168.1.1")}, nil},
		{"public then private", []netip.Addr{fixturePublicAddress, netip.MustParseAddr("172.16.1.1")}, nil},
		{"private then public", []netip.Addr{netip.MustParseAddr("192.168.1.1"), fixturePublicAddress}, nil},
		{"IPv4 unspecified", []netip.Addr{netip.MustParseAddr("0.0.0.0")}, nil},
		{"IPv6 unspecified", []netip.Addr{netip.MustParseAddr("::")}, nil},
		{"IPv4 multicast", []netip.Addr{netip.MustParseAddr("224.0.0.1")}, nil},
		{"IPv6 multicast", []netip.Addr{netip.MustParseAddr("ff02::1")}, nil},
		{"shared carrier range", []netip.Addr{netip.MustParseAddr("100.64.0.1")}, nil},
		{"benchmark range", []netip.Addr{netip.MustParseAddr("198.18.0.1")}, nil},
		{"IPv4 documentation", []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil},
		{"IPv6 documentation", []netip.Addr{netip.MustParseAddr("2001:db8::1")}, nil},
		{"IPv4 reserved", []netip.Addr{netip.MustParseAddr("240.0.0.1")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lookups, credentials, dials atomic.Int32
			network := sourceNetwork{
				resolve: func(context.Context, string) ([]netip.Addr, error) {
					lookups.Add(1)
					return tc.answers, tc.err
				},
				dial: func(context.Context, string, string) (net.Conn, error) {
					dials.Add(1)
					return nil, errors.New("fictional forbidden dial")
				},
			}
			client := transportClient(t, Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(context.Context) (string, string, error) {
				credentials.Add(1)
				return "fictional-user", "fictional-password", nil
			}, network)
			snapshot, err := client.Busy(t.Context(), transportCoverage())
			if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || lookups.Load() != 1 || credentials.Load() != 0 || dials.Load() != 0 {
				t.Fatal("unproved resolution was skipped or reached credentials, dialing or availability")
			}
			if err.Error() != booking.ErrUnavailable.Error() {
				t.Fatal("private resolution detail escaped the adapter")
			}
		})
	}
}

func TestTransportRefusesCredentialFailureAfterResolution(t *testing.T) {
	blockAmbientNetwork(t)
	f := newTransportFixture(t, selectedCalendarHost)
	client := transportClient(t, Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(context.Context) (string, string, error) {
		f.record("credential")
		return "", "", errors.New("fictional-private-supplier-detail")
	}, f.network)
	snapshot, err := client.Busy(t.Context(), transportCoverage())
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || !reflect.DeepEqual(f.trace(), []string{"resolve", "credential"}) || f.dials.Load() != 0 || f.requests.Load() != 0 {
		t.Fatal("supplier failure was never reached or caused a dial, request or availability")
	}
	if err.Error() != booking.ErrUnavailable.Error() {
		t.Fatal("private supplier detail escaped the adapter")
	}
}

func TestTransportRefusesWrongTLSHostname(t *testing.T) {
	blockAmbientNetwork(t)
	f := newTransportFixture(t, "different.example.test")
	client := selectedTransportClient(t, f)
	snapshot, err := client.Busy(t.Context(), transportCoverage())
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || !reflect.DeepEqual(f.trace(), []string{"resolve", "credential", "dial"}) || f.requests.Load() != 0 {
		t.Fatal("wrong TLS hostname reached an authenticated request or was never checked")
	}
}

func TestTransportNeverFollowsRedirect(t *testing.T) {
	blockAmbientNetwork(t)
	var foreignConnections atomic.Int32
	foreign := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect reached a foreign request")
	}))
	foreign.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			foreignConnections.Add(1)
		}
	}
	foreign.StartTLS()
	t.Cleanup(foreign.Close)
	f := newTransportFixture(t, selectedCalendarHost)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/foreign/", http.StatusFound)
	}
	client := selectedTransportClient(t, f)
	snapshot, err := client.Busy(t.Context(), transportCoverage())
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || f.requests.Load() != 1 || f.dials.Load() != 1 || foreignConnections.Load() != 0 {
		t.Fatal("redirect was not observed or reached a foreign connection")
	}
}

func TestTransportIgnoresAmbientProxyAndHTTPClient(t *testing.T) {
	blockAmbientNetwork(t)
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	f := newTransportFixture(t, selectedCalendarHost)
	client := selectedTransportClient(t, f)
	snapshot, err := client.Busy(t.Context(), transportCoverage())
	if err != nil || !snapshot.Complete || f.requests.Load() != 1 || proxyRequests.Load() != 0 {
		t.Fatal("owned transport consulted an ambient proxy or could not read its selected provider", err)
	}
}

func TestTransportRereadsConstructorBoundCredential(t *testing.T) {
	blockAmbientNetwork(t)
	f := newTransportFixture(t, selectedCalendarHost)
	f.password = func(call int32) string {
		if call == 1 {
			return "fictional-first"
		}
		return "fictional-rotated"
	}
	var credentials atomic.Int32
	client := transportClient(t, Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(context.Context) (string, string, error) {
		f.record("credential")
		return "fictional-user", f.password(credentials.Add(1)), nil
	}, f.network)
	for range 2 {
		snapshot, err := client.Busy(t.Context(), transportCoverage())
		if err != nil || !snapshot.Complete || len(snapshot.Busy) != 1 {
			t.Fatal("rotated constructor-bound credential did not read a complete provider report", err)
		}
	}
	if f.lookups.Load() != 2 || credentials.Load() != 2 || f.requests.Load() != 2 {
		t.Fatal("transport retained a resolved or credential verdict across operations")
	}
}

func TestTransportRechecksResolutionBeforeConnectionReuse(t *testing.T) {
	blockAmbientNetwork(t)
	f := newTransportFixture(t, selectedCalendarHost)
	var answers, credentials atomic.Int32
	f.answer = func(context.Context) ([]netip.Addr, error) {
		if answers.Add(1) == 1 {
			return []netip.Addr{fixturePublicAddress}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	client := transportClient(t, Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(context.Context) (string, string, error) {
		f.record("credential")
		credentials.Add(1)
		return "fictional-user", "fictional-password", nil
	}, f.network)
	snapshot, err := client.Busy(t.Context(), transportCoverage())
	if err != nil || !snapshot.Complete {
		t.Fatal("initial public destination could not be observed", err)
	}
	snapshot, err = client.Busy(t.Context(), transportCoverage())
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || f.lookups.Load() != 2 || credentials.Load() != 1 || f.requests.Load() != 1 || f.dials.Load() != 1 {
		t.Fatal("earlier connection bypassed a changed resolution refusal")
	}
}

func TestTransportRechecksCancellationAfterCredentialWait(t *testing.T) {
	blockAmbientNetwork(t)
	f := newTransportFixture(t, selectedCalendarHost)
	entered, release := make(chan struct{}), make(chan struct{})
	client := transportClient(t, Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(ctx context.Context) (string, string, error) {
		f.record("credential")
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "fictional-user", "fictional-password", nil
	}, f.network)
	requireBusyCancelledDuringCredentials(t, client, entered, release)
	if !reflect.DeepEqual(f.trace(), []string{"resolve", "credential"}) || f.dials.Load() != 0 || f.requests.Load() != 0 {
		t.Fatal("cancelled credential wait reached dialing or an authenticated request")
	}
}
