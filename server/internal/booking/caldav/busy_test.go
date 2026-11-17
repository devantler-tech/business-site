package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// This catches a missing/unbounded REPORT, a wrong selected collection, and
// returning anything other than the complete provider's UTC busy intervals.
func TestBusyRequestsCompleteUTCFreeBusy(t *testing.T) {
	observed := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	coverage := booking.Interval{Start: observed.Add(time.Hour), End: time.Date(2027, 3, 29, 0, 0, 0, 0, time.UTC)}
	reports := make(chan bool, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report struct {
			XMLName xml.Name `xml:"urn:ietf:params:xml:ns:caldav free-busy-query"`
			Range   struct {
				Start string `xml:"start,attr"`
				End   string `xml:"end,attr"`
			} `xml:"urn:ietf:params:xml:ns:caldav time-range"`
		}
		decoder := xml.NewDecoder(io.LimitReader(r.Body, 4097))
		err := decoder.Decode(&report)
		var extra any
		end := decoder.Decode(&extra)
		user, password, authenticated := r.BasicAuth()
		valid := err == nil && errors.Is(end, io.EOF) && r.Method == "REPORT" && r.URL.Path == "/calendars/selected/" && r.URL.RawQuery == "" && r.Header.Get("Depth") == "1" && authenticated && user == "fictional-user" && password == "fictional-password" && report.Range.Start == "20270327T220000Z" && report.Range.End == "20270329T000000Z"
		select {
		case reports <- valid:
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if !valid {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fictional provider//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20270327T210000Z\r\nDTSTART:20270327T220000Z\r\nDTEND:20270329T000000Z\r\nFREEBUSY:20270327T230000Z/20270328T220000Z\r\nFREEBUSY;FBTYPE=BUSY-TENTATIVE:20270328T223000Z/PT30M\r\nEND:VFREEBUSY\r\nEND:VCALENDAR\r\n")
	}))
	t.Cleanup(server.Close)
	client := freeBusyFixtureClient(t, server, observed)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	snapshot, err := client.Busy(ctx, coverage)
	if err != nil {
		t.Fatal("complete bounded provider report was unavailable", err)
	}
	select {
	case valid := <-reports:
		if !valid {
			t.Fatal("availability did not use the bounded selected-calendar report")
		}
	default:
		t.Fatal("availability returned without querying the selected calendar")
	}
	want := []booking.Interval{
		{Start: time.Date(2027, 3, 27, 23, 0, 0, 0, time.UTC), End: time.Date(2027, 3, 28, 22, 0, 0, 0, time.UTC)},
		{Start: time.Date(2027, 3, 28, 22, 30, 0, 0, time.UTC), End: time.Date(2027, 3, 28, 23, 0, 0, 0, time.UTC)},
	}
	if !snapshot.Complete || snapshot.Coverage != coverage || !snapshot.ObservedAt.Equal(observed) || !reflect.DeepEqual(snapshot.Busy, want) {
		t.Fatal("provider coverage or UTC busy intervals were changed", snapshot)
	}
}

// An unavailable or truncated report must not become a successful empty snapshot.
func TestBusyRefusesIncompleteProviderReport(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/calendar")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nBEGIN:VFREEBUSY\r\nCOMMENT:fictional-private-response\r\n")
			}))
			t.Cleanup(server.Close)
			now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
			client := freeBusyFixtureClient(t, server, now)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			snapshot, err := client.Busy(ctx, booking.Interval{Start: now, End: now.Add(time.Hour)})
			if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) {
				t.Fatal("incomplete provider data produced usable availability")
			}
			if strings.Contains(err.Error(), "fictional-private-response") {
				t.Fatal("provider content escaped its private adapter")
			}
		})
	}
}

// Following a redirect would reveal the credential to an unselected resource.
func TestBusyNeverFollowsProviderRedirect(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(other.Close)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/unselected/", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	client := freeBusyFixtureClient(t, server, now)
	snapshot, err := client.Busy(t.Context(), booking.Interval{Start: now, End: now.Add(time.Hour)})
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || redirected.Load() != 0 {
		t.Fatal("redirect reached an unselected calendar or returned availability")
	}
}

// A successful first calendar cannot hide an unavailable second calendar.
func TestBusyRequiresEverySelectedCalendar(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/calendars/unavailable/" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fictional provider//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20270327T210000Z\r\nDTSTART:20270327T210000Z\r\nDTEND:20270327T220000Z\r\nFREEBUSY:20270327T211500Z/PT15M\r\nEND:VFREEBUSY\r\nEND:VCALENDAR\r\n")
	}))
	t.Cleanup(server.Close)
	now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	client := freeBusyFixtureClient(t, server, now)
	client.calendars = append(client.calendars, "/calendars/unavailable/")
	snapshot, err := client.Busy(t.Context(), booking.Interval{Start: now, End: now.Add(time.Hour)})
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) {
		t.Fatal("an incomplete calendar set released partial availability")
	}
}

// These are provider uncertainty, not evidence that the calendar is empty.
func TestBusyRefusesUncertainCalendarSemantics(t *testing.T) {
	const complete = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fictional provider//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20270327T210000Z\r\nDTSTART:20270327T210000Z\r\nDTEND:20270327T220000Z\r\nFREEBUSY:20270327T211500Z/PT15M\r\nEND:VFREEBUSY\r\nEND:VCALENDAR\r\n"
	cases := map[string]string{
		"missing coverage":      strings.Replace(complete, "DTEND:20270327T220000Z\r\n", "", 1),
		"short coverage":        strings.Replace(complete, "DTEND:20270327T220000Z", "DTEND:20270327T213000Z", 1),
		"floating period":       strings.Replace(complete, "20270327T211500Z/PT15M", "20270327T211500/PT15M", 1),
		"unexpanded recurrence": strings.Replace(complete, "END:VFREEBUSY", "RRULE:FREQ=DAILY\r\nEND:VFREEBUSY", 1),
		"extra calendar":        complete + complete,
		"overflowing duration":  strings.Replace(complete, "PT15M", "P18446744073709551615D", 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/calendar")
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(server.Close)
			now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
			client := freeBusyFixtureClient(t, server, now)
			snapshot, err := client.Busy(t.Context(), booking.Interval{Start: now, End: now.Add(time.Hour)})
			if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) {
				t.Fatal("uncertain provider semantics produced availability")
			}
		})
	}
}

// Only the unexported fixture construction admits this owned loopback server.
// Production construction and resolved-address refusal need their own controls.
func freeBusyFixtureClient(t *testing.T, server *httptest.Server, now time.Time) *Client {
	t.Helper()
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{
		origin:      origin,
		calendars:   []string{"/calendars/selected/"},
		httpClient:  server.Client(),
		credentials: func(context.Context) (string, string, error) { return "fictional-user", "fictional-password", nil },
		now:         func() time.Time { return now },
	}
}
