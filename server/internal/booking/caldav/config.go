package caldav

import (
	"context"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// Config contains only the operator's explicitly selected private source names.
// No browser request or provider discovery supplies these values.
type Config struct {
	Origin    string
	Calendars []string
	// OwnedCalendar is the dedicated collection selected for portal events.
	OwnedCalendar string
}

// New admits an explicit selection without reading credentials or connecting.
// Provider operations stay closed until an owned, confined transport is proved.
func New(config Config, credentials func(context.Context) (string, string, error)) (*Client, error) {
	origin, err := url.Parse(config.Origin)
	if err != nil || len(config.Origin) > 2048 || strings.ContainsAny(config.Origin, "?#") || origin.Scheme != "https" || origin.User != nil || origin.Opaque != "" || origin.RawPath != "" || (origin.Path != "" && origin.Path != "/") || (origin.Port() != "" && origin.Port() != "443") || credentials == nil || !validCollections(config.Calendars) {
		return nil, booking.ErrUnavailable
	}
	if config.OwnedCalendar != "" && !slices.Contains(config.Calendars, config.OwnedCalendar) {
		return nil, booking.ErrUnavailable
	}
	host := strings.ToLower(origin.Hostname())
	_, addressErr := netip.ParseAddr(host)
	localName := strings.TrimSuffix(host, ".")
	if host == "" || addressErr == nil || localName == "localhost" || strings.HasSuffix(localName, ".localhost") {
		return nil, booking.ErrUnavailable
	}
	return &Client{
		origin:        &url.URL{Scheme: "https", Host: host},
		calendars:     append([]string(nil), config.Calendars...),
		ownedCalendar: config.OwnedCalendar,
		credentials:   credentials,
		now:           time.Now,
	}, nil
}

func validCollections(calendars []string) bool {
	if len(calendars) == 0 || len(calendars) > maxCalendars {
		return false
	}
	seen := make(map[string]bool, len(calendars))
	for _, collection := range calendars {
		if collection == "/" || len(collection) > 2048 || !strings.HasPrefix(collection, "/") || !strings.HasSuffix(collection, "/") || strings.ContainsAny(collection, "%?#\\") || strings.ContainsFunc(collection, unicode.IsControl) || path.Clean(collection)+"/" != collection || seen[collection] {
			return false
		}
		seen[collection] = true
	}
	return true
}
