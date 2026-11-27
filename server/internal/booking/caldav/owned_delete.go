package caldav

import (
	"context"
	"net/http"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// DeleteOwnedEvent confirms absence of the portal's canonical calendar event.
// The caller holds booking authority and supplies the last recorded revision
// and strong validator. An existing event must match both before removal.
func (c *Client) DeleteOwnedEvent(ctx context.Context, event OwnedEvent, expectedTag string) error {
	resource, uid, err := c.ownedEventTarget(event)
	if err != nil || !strongEventTag.MatchString(expectedTag) {
		return booking.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err = c.forOwnedCalendar(ctx, event.CalendarBinding)
	if err != nil {
		return err
	}
	client, closeConnection, err := c.sourceClient(ctx)
	if err != nil {
		return err
	}
	defer closeConnection()
	current, err := c.readOwnedEvent(ctx, client, resource, uid, event)
	if err != nil || !current.Exists {
		return err
	}
	if current.ETag != expectedTag {
		return ErrOwnedEventConflict
	}
	request, err := c.sourceRequest(ctx, http.MethodDelete, resource, nil)
	if err != nil {
		return err
	}
	request.Header.Set("If-Match", expectedTag)
	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()
		status := response.StatusCode
		switch {
		case status/100 == 2, status == http.StatusNotFound, status == http.StatusPreconditionFailed, status/100 == 5:
			// Check the canonical resource after an accepted or uncertain result.
		default:
			return booking.ErrUnavailable
		}
	}
	current, err = c.readOwnedEvent(ctx, client, resource, uid, event)
	if err != nil || !current.Exists {
		return err
	}
	if current.ETag != expectedTag {
		return ErrOwnedEventConflict
	}
	return booking.ErrUnavailable
}
