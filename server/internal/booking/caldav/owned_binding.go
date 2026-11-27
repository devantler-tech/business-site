package caldav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// OwnedCalendarBinding identifies the configured provider, selected collection
// and account without connecting. Persist it when recording a new calendar
// intent, then carry the same value through retries and later revisions.
func (c *Client) OwnedCalendarBinding(ctx context.Context) (string, error) {
	if !c.hasOwnedCalendar() {
		return "", booking.ErrUnavailable
	}
	user, _, err := c.sourceCredentials(ctx)
	if err != nil {
		return "", err
	}
	return c.bindingForAccount(user), nil
}

func (c *Client) hasOwnedCalendar() bool {
	return c != nil && c.origin != nil && c.ownedCalendar != "" && slices.Contains(c.calendars, c.ownedCalendar)
}

func (c *Client) bindingForAccount(user string) string {
	value := "devantler.tech/caldav-owned-selection/v1\x00" + c.origin.Host + "\x00" + c.ownedCalendar + "\x00" + user
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (c *Client) forOwnedCalendar(ctx context.Context, expected string) (*Client, error) {
	current, err := c.OwnedCalendarBinding(ctx)
	if err != nil {
		return nil, err
	}
	if current != expected {
		return nil, ErrOwnedEventConflict
	}
	selected := *c
	selected.ownedBinding = expected
	return &selected, nil
}
