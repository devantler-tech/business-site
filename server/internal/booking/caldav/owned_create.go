package caldav

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
	calendarclient "github.com/emersion/go-webdav/caldav"
)

// CreateOwnedEvent creates an absent portal resource once. The caller owns
// booking authority and must supply the persisted event identity and revision.
// The same confined readback settles a success, conflict or ambiguous result.
func (c *Client) CreateOwnedEvent(ctx context.Context, event OwnedEvent) (OwnedEventObservation, error) {
	resource, uid, err := c.ownedEventTarget(event)
	if err != nil {
		return OwnedEventObservation{}, err
	}
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
	current, err := c.readOwnedEvent(ctx, client, resource, uid, event)
	if err != nil || current.Exists {
		return current, err
	}
	if !c.putNewOwnedEvent(ctx, client, resource, newOwnedCalendarEvent(uid, stamp, event)) {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	current, err = c.readOwnedEvent(ctx, client, resource, uid, event)
	if err != nil || !current.Exists {
		return OwnedEventObservation{}, booking.ErrUnavailable
	}
	return current, nil
}

// putNewOwnedEvent reports whether observing the canonical resource can resolve
// the attempted write. It ignores success metadata and never repeats the PUT.
func (c *Client) putNewOwnedEvent(ctx context.Context, client *http.Client, resource string, event *ical.Calendar) bool {
	origin := url.URL{Scheme: "https", Host: c.origin.Host}
	target := origin
	target.Path = resource
	attempted := false
	status := 0
	transport := writeClientFunc(func(request *http.Request) (*http.Response, error) {
		if attempted || request.Method != http.MethodPut || request.URL.String() != target.String() ||
			request.Header.Get("If-None-Match") != "*" || request.Header.Get("If-Match") != "" ||
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
	dav, err := calendarclient.NewClient(transport, origin.String())
	if err != nil {
		return false
	}
	_, _ = dav.PutCalendarObject(ctx, resource, event, &calendarclient.PutCalendarObjectOptions{IfNoneMatch: "*"})
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
