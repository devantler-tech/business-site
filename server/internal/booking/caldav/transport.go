package caldav

import (
	"context"
	"crypto/x509"
	"net"
	"net/netip"
)

// sourceNetwork is private to the adapter. Tests map checked public addresses to
// their owned TLS listener; there is no public client, resolver or TLS override.
type sourceNetwork struct {
	resolve func(context.Context, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
	roots   *x509.CertPool
}

// withSourceNetwork stays closed until actual resolution, ordering and TLS
// boundary regressions have failed and the confined transport is implemented.
func withSourceNetwork(client *Client, _ sourceNetwork) *Client {
	return client
}
