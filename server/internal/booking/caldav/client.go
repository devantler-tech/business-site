// Package caldav defines private operator-selected calendar observations.
// It grants no booking authority and has no runtime composition.
package caldav

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// Client admits a private source selection but has no runtime composition.
// Provider operations stay closed until connection confinement is proved.
type Client struct {
	origin        *url.URL
	calendars     []string
	ownedCalendar string
	ownedBinding  string
	httpClient    *http.Client
	network       *sourceNetwork
	credentials   func(context.Context) (string, string, error)
	now           func() time.Time
}

var _ booking.BusyProvider = (*Client)(nil)
