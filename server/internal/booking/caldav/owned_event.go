package caldav

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
)

const (
	maxOwnedEventBytes = 16 << 10
	ownedEventProduct  = "-//Devantler Tech//Booking//EN"
	ownedEventSummary  = "Devantler Tech appointment"
)

var (
	ownedEventKey = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Accept the bounded ASCII subset of RFC 9110 strong entity tags.
	strongEventTag = regexp.MustCompile(`^"[\x21\x23-\x7e]{1,128}"$`)
)

// OwnedEvent contains the persisted portal appointment identity and revision.
// Key must be an independent server-generated value, never a browser value.
type OwnedEvent struct {
	Key      string
	Slot     booking.Interval
	Revision int
}

// OwnedEventObservation is limited to the matching resource and its strong
// validator. Provider event data stays inside the adapter.
type OwnedEventObservation struct {
	Exists bool
	ETag   string
}

// ReadOwnedEvent observes one canonical object in the dedicated calendar.
// An existing object with any unexpected event content requires reconciliation.
func (c *Client) ReadOwnedEvent(ctx context.Context, event OwnedEvent) (OwnedEventObservation, error) {
	resource, uid, err := c.ownedEventTarget(event)
	if err != nil {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, closeConnection, err := c.sourceClient(ctx)
	if err != nil {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	defer closeConnection()
	return c.readOwnedEvent(ctx, client, resource, uid, event)
}

func (c *Client) ownedEventTarget(event OwnedEvent) (resource, uid string, err error) {
	if !c.validSources() || c.ownedCalendar == "" || !slices.Contains(c.calendars, c.ownedCalendar) ||
		!ownedEventKey.MatchString(event.Key) || event.Revision < 1 || event.Revision > 1<<31-1 ||
		event.Slot.Start.IsZero() || !event.Slot.Start.Before(event.Slot.End) ||
		event.Slot.End.Sub(event.Slot.Start) > 24*time.Hour ||
		event.Slot.Start.Nanosecond() != 0 || event.Slot.End.Nanosecond() != 0 {
		return "", "", booking.ErrUnavailable
	}
	stem := "portal-" + event.Key
	return c.ownedCalendar + stem + ".ics", stem + "@devantler.tech", nil
}

func (c *Client) readOwnedEvent(ctx context.Context, client *http.Client, resource, uid string, event OwnedEvent) (OwnedEventObservation, error) {
	return c.readMatchingOwnedEvent(ctx, client, resource, func(data []byte) bool {
		return matchesOwnedEvent(data, uid, event)
	})
}

func (c *Client) readMatchingOwnedEvent(ctx context.Context, client *http.Client, resource string, matches func([]byte) bool) (OwnedEventObservation, error) {
	request, err := c.sourceRequest(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return OwnedEventObservation{}, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound && ctx.Err() == nil {
		return OwnedEventObservation{}, nil
	}
	tags := response.Header.Values("ETag")
	if len(tags) != 1 || !strongEventTag.MatchString(tags[0]) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	data, err := readCalendarBody(ctx, response, maxOwnedEventBytes)
	if err != nil || !matches(data) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	return OwnedEventObservation{Exists: true, ETag: tags[0]}, nil
}

func matchesOwnedEvent(data []byte, uid string, event OwnedEvent) bool {
	decoder := ical.NewDecoder(bytes.NewReader(data))
	calendar, err := decoder.Decode()
	if err != nil || calendar == nil || len(calendar.Children) != 1 || len(calendar.Props) != 2 ||
		!matchesPlainProperty(calendar.Props, ical.PropVersion, "2.0") ||
		!matchesPlainProperty(calendar.Props, ical.PropProductID, ownedEventProduct) {
		return false
	}
	if _, err := decoder.Decode(); !errors.Is(err, io.EOF) {
		return false
	}
	component := calendar.Children[0]
	if component.Name != ical.CompEvent || len(component.Children) != 0 || len(component.Props) != 7 {
		return false
	}
	for key, value := range map[string]string{
		ical.PropUID:           uid,
		ical.PropSummary:       ownedEventSummary,
		ical.PropClass:         "PRIVATE",
		ical.PropSequence:      strconv.Itoa(event.Revision),
		ical.PropDateTimeStart: event.Slot.Start.UTC().Format(calendarUTCLayout),
		ical.PropDateTimeEnd:   event.Slot.End.UTC().Format(calendarUTCLayout),
	} {
		if !matchesPlainProperty(component.Props, key, value) {
			return false
		}
	}
	stamp := component.Props.Values(ical.PropDateTimeStamp)
	if len(stamp) != 1 || len(stamp[0].Params) != 0 {
		return false
	}
	_, err = utcValue(stamp[0].Value)
	return err == nil
}

func matchesPlainProperty(properties ical.Props, key, value string) bool {
	values := properties.Values(key)
	return len(values) == 1 && len(values[0].Params) == 0 && values[0].Value == value
}
