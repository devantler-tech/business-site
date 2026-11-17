package caldav

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// sourceNetwork is private to the adapter. Tests map checked public addresses to
// their owned TLS listener; there is no public client, resolver or TLS override.
type sourceNetwork struct {
	resolve func(context.Context, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
	roots   *x509.CertPool
}

// withSourceNetwork binds private connection dependencies to a copied selection.
// New does not install them; production composition remains separately closed.
func withSourceNetwork(client *Client, network sourceNetwork) *Client {
	if client == nil {
		return nil
	}
	selected := *client
	if client.origin != nil {
		origin := *client.origin
		selected.origin = &origin
	}
	selected.calendars = append([]string(nil), client.calendars...)
	selected.httpClient = nil
	selected.network = nil
	if network.resolve == nil || network.dial == nil {
		return &selected
	}
	if network.roots != nil {
		network.roots = network.roots.Clone()
	}
	selected.network = &network
	return &selected
}

func (c *Client) reportClient(ctx context.Context) (*http.Client, func(), error) {
	if c.network == nil {
		if c.httpClient == nil || c.httpClient.Transport == nil {
			return nil, nil, booking.ErrUnavailable
		}
		return &http.Client{Transport: c.httpClient.Transport, Timeout: c.httpClient.Timeout, CheckRedirect: refuseRedirect}, func() {}, nil
	}
	addresses, err := c.network.resolve(ctx, c.origin.Hostname())
	if err != nil || ctx.Err() != nil || len(addresses) == 0 || len(addresses) > 16 {
		return nil, nil, booking.ErrUnavailable
	}
	checked := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]bool, len(addresses))
	for _, address := range addresses {
		if !publicAddress(address) {
			return nil, nil, booking.ErrUnavailable
		}
		address = address.Unmap()
		if !seen[address] {
			checked = append(checked, address)
			seen[address] = true
		}
	}
	transport := &http.Transport{
		Proxy:                  nil,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.origin.Hostname(), RootCAs: c.network.roots},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		MaxConnsPerHost:        1,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return c.dialChecked(ctx, checked, network, address)
		},
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: refuseRedirect}, transport.CloseIdleConnections, nil
}

func (c *Client) dialChecked(ctx context.Context, checked []netip.Addr, network, address string) (net.Conn, error) {
	if network != "tcp" || address != net.JoinHostPort(c.origin.Hostname(), "443") {
		return nil, booking.ErrUnavailable
	}
	for _, candidate := range checked {
		if ctx.Err() != nil {
			break
		}
		conn, err := c.network.dial(ctx, "tcp", net.JoinHostPort(candidate.String(), "443"))
		if err == nil && conn != nil {
			return conn, nil
		}
		if conn != nil {
			// A failed dial may return a connection; this operation owns it.
			_ = conn.Close()
		}
	}
	return nil, booking.ErrUnavailable
}

func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// These conservative exceptional-use exclusions follow the IANA special-purpose
// registries. Go's global-unicast classification alone also includes private,
// documentation and other addresses that are unsuitable provider destinations.
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
var exceptionalAddressRanges = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

// https://www.iana.org/assignments/ipv6-unicast-address-assignments/
var ordinaryIPv6Unicast = netip.MustParsePrefix("2000::/3")

func publicAddress(address netip.Addr) bool {
	if address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || (address.Is6() && !ordinaryIPv6Unicast.Contains(address)) {
		return false
	}
	for _, exceptional := range exceptionalAddressRanges {
		if exceptional.Contains(address) {
			return false
		}
	}
	return true
}
