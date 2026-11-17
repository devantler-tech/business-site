package caldav

import (
	"context"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// Config contains only the operator's explicitly selected private source names.
// No browser request or provider discovery supplies these values.
type Config struct {
	Origin    string
	Calendars []string
}

// New remains closed until constructor and connection-confinement controls are
// implemented and verified. It performs no DNS, transport or credential effect.
func New(Config, func(context.Context) (string, string, error)) (*Client, error) {
	return nil, booking.ErrUnavailable
}
