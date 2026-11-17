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

// Client has no public constructor or runtime composition yet. Until its
// provider protocol and confinement controls are implemented it stays closed.
type Client struct {
	origin      *url.URL
	calendars   []string
	httpClient  *http.Client
	credentials func(context.Context) (string, string, error)
	now         func() time.Time
}

var _ booking.BusyProvider = (*Client)(nil)
