package caldav

import (
	"context"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// UpdateOwnedEvent changes only a matching appointment at the last persisted
// provider version. The caller must supply consecutive portal revisions and
// hold booking authority over the appointment.
func (c *Client) UpdateOwnedEvent(ctx context.Context, prior, next OwnedEvent, expectedTag string) (OwnedEventObservation, error) {
	resource, uid, err := c.ownedEventTarget(prior)
	if err != nil || prior.Key != next.Key || !strongEventTag.MatchString(expectedTag) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	if _, _, err := c.ownedEventTarget(next); err != nil || prior.Revision != next.Revision-1 {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	return c.writeOwnedEvent(ctx, resource, uid, next, &prior, expectedTag)
}
