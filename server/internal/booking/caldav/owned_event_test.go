package caldav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
)

const fixtureEventKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const fixtureOwnedEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Devantler Tech//Booking//EN\r\n" +
	"BEGIN:" + ical.CompEvent + "\r\nUID:portal-" + fixtureEventKey + "@devantler.tech\r\n" +
	"DTSTAMP:20270327T200000Z\r\nDTSTART:20270327T210000Z\r\nDTEND:20270327T213000Z\r\n" +
	"SUMMARY:Devantler Tech appointment\r\nCLASS:PRIVATE\r\nSEQUENCE:2\r\nEND:" + ical.CompEvent + "\r\nEND:VCALENDAR\r\n"

func fixtureOwnedSelection() Config {
	const collection = "/calendars/selected/"
	return Config{Origin: "https://" + selectedCalendarHost, Calendars: []string{collection, "/calendars/read-only/"}, OwnedCalendar: collection}
}

func fixtureEvent() OwnedEvent {
	start := time.Date(2027, time.March, 27, 21, 0, 0, 0, time.UTC)
	return OwnedEvent{Key: fixtureEventKey, Slot: booking.Interval{Start: start, End: start.Add(30 * time.Minute)}, Revision: 2}
}

func ownedEventErrorKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrOwnedEventConflict) && errors.Is(err, booking.ErrUnavailable):
		return "conflict"
	case err == booking.ErrUnavailable:
		return "unavailable"
	default:
		return "unexpected"
	}
}

func TestReadOwnedEventRequiresExactResourceAndVersion(t *testing.T) {
	const version = `"fictional-version"`
	tests := []struct {
		name    string
		status  int
		tags    []string
		body    string
		want    OwnedEventObservation
		errKind string
	}{
		{"current", http.StatusOK, []string{version}, fixtureOwnedEvent, OwnedEventObservation{Exists: true, ETag: version}, ""},
		{"absent", http.StatusNotFound, nil, "", OwnedEventObservation{}, ""},
		{"return only provider-independent absence", http.StatusForbidden, nil, "fictional-private-diagnostic", OwnedEventObservation{}, "unavailable"},
		{"failure with a version and body", http.StatusServiceUnavailable, []string{version}, fixtureOwnedEvent, OwnedEventObservation{}, "unavailable"},
		{"location is never followed", http.StatusTemporaryRedirect, nil, "", OwnedEventObservation{}, "unavailable"},
		{"missing version", http.StatusOK, nil, fixtureOwnedEvent, OwnedEventObservation{}, "unavailable"},
		{"duplicate versions", http.StatusOK, []string{version, version}, fixtureOwnedEvent, OwnedEventObservation{}, "unavailable"},
		{"weak version", http.StatusOK, []string{"W/" + version}, fixtureOwnedEvent, OwnedEventObservation{}, "unavailable"},
		{"changed interval", http.StatusOK, []string{version}, strings.Replace(fixtureOwnedEvent, "213000Z", "214500Z", 1), OwnedEventObservation{}, "conflict"},
		{"old revision", http.StatusOK, []string{version}, strings.Replace(fixtureOwnedEvent, "SEQUENCE:2", "SEQUENCE:1", 1), OwnedEventObservation{}, "conflict"},
		{"different appointment", http.StatusOK, []string{version}, strings.Replace(fixtureOwnedEvent, fixtureEventKey, strings.Repeat("f", 64), 1), OwnedEventObservation{}, "conflict"},
		{"external details", http.StatusOK, []string{version}, strings.Replace(fixtureOwnedEvent, "SEQUENCE:2", "DESCRIPTION:fictional-private-detail\r\nSEQUENCE:2", 1), OwnedEventObservation{}, "conflict"},
		{"duplicate appointment", http.StatusOK, []string{version}, fixtureOwnedEvent + fixtureOwnedEvent, OwnedEventObservation{}, "conflict"},
		{"oversized body", http.StatusOK, []string{version}, strings.Repeat("a", 1<<15), OwnedEventObservation{}, "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newCalendarTransportFixture(t, selectedCalendarHost, "GET", "/calendars/selected/portal-"+fixtureEventKey+".ics")
			f.handler = func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "text/calendar" || r.Header.Get("Cache-Control") != "no-cache" {
					t.Error("expected a fresh calendar object request")
				}
				w.Header().Set("Content-Type", "text/calendar")
				w.Header().Set("Location", "https://fictional-other.example.test/alternate.ics")
				for _, etag := range test.tags {
					w.Header().Add("ETag", etag)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}
			client := transportClient(t, fixtureOwnedSelection(), standardCredentials, f.network)
			got, err := client.ReadOwnedEvent(t.Context(), fixtureEvent())
			if got != test.want || ownedEventErrorKind(err) != test.errKind || f.requests.Load() != 1 {
				t.Fatalf("unexpected ownership result: exists=%t error=%v requests=%d", got.Exists, err, f.requests.Load())
			}
		})
	}
}

func TestReadOwnedEventRejectsUnselectedResourcesBeforeProviderAccess(t *testing.T) {
	f := newCalendarTransportFixture(t, selectedCalendarHost, "GET", "/unused/")
	config := fixtureOwnedSelection()
	client := transportClient(t, config, standardCredentials, f.network)
	invalid := fixtureEvent()
	invalid.Key = "/unrelated.ics"
	if result, err := client.ReadOwnedEvent(t.Context(), invalid); result.Exists || !errors.Is(err, booking.ErrUnavailable) {
		t.Fatal("a caller-supplied path could select a resource")
	}
	config.OwnedCalendar = "/calendars/unselected/"
	if _, err := New(config, standardCredentials); !errors.Is(err, booking.ErrUnavailable) {
		t.Fatal("an unselected calendar could become the owned event source")
	}
	config.OwnedCalendar = ""
	client = transportClient(t, config, standardCredentials, f.network)
	if result, err := client.ReadOwnedEvent(t.Context(), fixtureEvent()); result.Exists || !errors.Is(err, booking.ErrUnavailable) {
		t.Fatal("observing owned events should require explicit selection")
	}
	if f.lookups.Load() != 0 || f.requests.Load() != 0 {
		t.Fatal("input refusal touched the provider")
	}
}

func standardCredentials(context.Context) (string, string, error) {
	return "fictional-user", "fictional-password", nil
}
