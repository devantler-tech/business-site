package caldav

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
	dav "github.com/emersion/go-webdav"
	webdav "github.com/emersion/go-webdav/caldav"
)

// CreateOwnedEvent creates an absent portal resource once. The caller owns
// booking authority and must supply the persisted event identity and revision.
// The same confined readback settles a success, conflict or ambiguous result.
func (c *Client) CreateOwnedEvent(ctx context.Context, event OwnedEvent) (OwnedEventObservation, error) {
	resource, uid, err := c.ownedEventTarget(event)
	if err != nil {
		return OwnedEventObservation{}, err
	}
	return c.writeOwnedEvent(ctx, resource, uid, event, nil, "")
}

func (c *Client) writeOwnedEvent(ctx context.Context, resource, uid string, next OwnedEvent, prior *OwnedEvent, expectedTag string) (OwnedEventObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stamp := c.now().UTC()
	if stamp.IsZero() || ctx.Err() != nil {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	client, closeConnection, err := c.sourceClient(ctx)
	if err != nil {
		return OwnedEventObservation{}, err
	}
	defer closeConnection()
	targetFound := false
	matchesRevision := func(data []byte) bool {
		targetFound = matchesOwnedEvent(data, uid, next)
		return targetFound || (prior != nil && matchesOwnedEvent(data, uid, *prior))
	}
	current, err := c.readMatchingOwnedEvent(ctx, client, resource, matchesRevision)
	if err != nil {
		return OwnedEventObservation{}, err
	}
	if targetFound && current.ETag != expectedTag {
		return current, nil
	}
	if targetFound || (prior != nil && (!current.Exists || current.ETag != expectedTag)) {
		return OwnedEventObservation{}, ErrOwnedEventConflict
	}
	options := webdav.PutCalendarObjectOptions{IfNoneMatch: "*"}
	if prior != nil {
		options = webdav.PutCalendarObjectOptions{IfMatch: dav.ConditionalMatch(expectedTag)}
	}
	if !c.putOwnedEvent(ctx, client, resource, newOwnedCalendarEvent(uid, stamp, next), options) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	current, err = c.readMatchingOwnedEvent(ctx, client, resource, matchesRevision)
	if err != nil {
		return OwnedEventObservation{}, err
	}
	if !current.Exists || (!targetFound && current.ETag == expectedTag) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	if !targetFound || current.ETag == expectedTag {
		return OwnedEventObservation{}, ErrOwnedEventConflict
	}
	return current, nil
}

// putOwnedEvent reports whether observing the canonical resource can resolve
// the attempted write. It ignores success metadata and never repeats the PUT.
func (c *Client) putOwnedEvent(ctx context.Context, client *http.Client, resource string, event *ical.Calendar, options webdav.PutCalendarObjectOptions) bool {
	create := options.IfNoneMatch == "*" && options.IfMatch == ""
	update := options.IfNoneMatch == "" && strongEventTag.MatchString(string(options.IfMatch))
	if !create && !update {
		return false
	}
	origin := url.URL{Scheme: "https", Host: c.origin.Host}
	target := origin
	target.Path = resource
	attempted := false
	status := 0
	transport := writeClientFunc(func(request *http.Request) (*http.Response, error) {
		if attempted || request.Method != http.MethodPut || request.URL.String() != target.String() ||
			request.Header.Get("If-None-Match") != string(options.IfNoneMatch) ||
			request.Header.Get("If-Match") != string(options.IfMatch) ||
			c.authorizeSourceRequest(request) != nil {
			return nil, booking.ErrUnavailable
		}
		attempted = true
		response, err := client.Do(request)
		if err != nil {
			return nil, booking.ErrUnavailable
		}
		status = response.StatusCode
		// The library decodes DAV error bodies. Keep provider content out of
		// that path and use only the status to decide whether to observe.
		_ = response.Body.Close()
		response.Body = http.NoBody
		return response, nil
	})
	dav, err := webdav.NewClient(transport, origin.String())
	if err != nil {
		return false
	}
	_, _ = dav.PutCalendarObject(ctx, resource, event, &options)
	return attempted && (status == 0 || status/100 == 2 || status == http.StatusPreconditionFailed || status/100 == 5)
}

type writeClientFunc func(*http.Request) (*http.Response, error)

func (write writeClientFunc) Do(request *http.Request) (*http.Response, error) {
	return write(request)
}

func newOwnedCalendarEvent(uid string, stamp time.Time, event OwnedEvent) *ical.Calendar {
	calendar := ical.NewCalendar()
	calendar.Props.SetText(ical.PropVersion, "2.0")
	calendar.Props.SetText(ical.PropProductID, ownedEventProduct)
	appointment := ical.NewEvent()
	appointment.Props.SetText(ical.PropUID, uid)
	appointment.Props.SetText(ical.PropSummary, ownedEventSummary)
	appointment.Props.SetText(ical.PropClass, "PRIVATE")
	appointment.Props.SetDateTime(ical.PropDateTimeStamp, stamp)
	appointment.Props.SetDateTime(ical.PropDateTimeStart, event.Slot.Start.UTC())
	appointment.Props.SetDateTime(ical.PropDateTimeEnd, event.Slot.End.UTC())
	revision := ical.NewProp(ical.PropSequence)
	revision.Value = strconv.Itoa(event.Revision)
	appointment.Props.Set(revision)
	calendar.Children = append(calendar.Children, appointment.Component)
	return calendar
}
