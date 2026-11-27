package caldav

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/emersion/go-ical"
)

func TestCreateOwnedEventReconcilesTheCanonicalResource(t *testing.T) {
	const etag = `"synthetic-confirmed-version"`
	expected := OwnedEventObservation{Exists: true, ETag: etag}
	different := strings.Replace(fixtureOwnedEvent, "SEQUENCE:2", "SEQUENCE:1", 1)
	tests := []struct {
		name    string
		initial string
		result  string
		status  int
		methods []string
		want    OwnedEventObservation
	}{
		{"new event", "", fixtureOwnedEvent, http.StatusCreated, []string{"GET", "PUT", "GET"}, expected},
		{"already created", fixtureOwnedEvent, "", 0, []string{"GET"}, expected},
		{"foreign event at resource", different, "", 0, []string{"GET"}, OwnedEventObservation{}},
		{"provider committed before failure", "", fixtureOwnedEvent, http.StatusServiceUnavailable, []string{"GET", "PUT", "GET"}, expected},
		{"connection lost after commit", "", fixtureOwnedEvent, 0, []string{"GET", "PUT", "GET"}, expected},
		{"provider failure without evidence", "", "", http.StatusServiceUnavailable, []string{"GET", "PUT", "GET"}, OwnedEventObservation{}},
		{"duplicate create now matches", "", fixtureOwnedEvent, http.StatusPreconditionFailed, []string{"GET", "PUT", "GET"}, expected},
		{"colliding create is different", "", different, http.StatusPreconditionFailed, []string{"GET", "PUT", "GET"}, OwnedEventObservation{}},
		{"authorization revoked", "", "", http.StatusUnauthorized, []string{"GET", "PUT"}, OwnedEventObservation{}},
		{"unexpected redirect", "", "", http.StatusTemporaryRedirect, []string{"GET", "PUT"}, OwnedEventObservation{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var guard sync.Mutex
			var requests []string
			stored := test.initial
			f := newCalendarTransportFixture(t, selectedCalendarHost, "", "/calendars/selected/portal-"+fixtureEventKey+".ics")
			f.handler = func(w http.ResponseWriter, request *http.Request) {
				guard.Lock()
				defer guard.Unlock()
				requests = append(requests, request.Method)
				switch request.Method {
				case http.MethodGet:
					writeOwnedEventFixture(w, stored, etag)
				case http.MethodPut:
					body, err := io.ReadAll(io.LimitReader(request.Body, 1<<15))
					if err != nil || len(body) >= 1<<15 || request.Header.Get("If-None-Match") != "*" ||
						request.Header.Get("If-Match") != "" || request.Header.Get("Content-Type") != ical.MIMEType ||
						!matchesOwnedEvent(body, "portal-"+fixtureEventKey+"@devantler.tech", fixtureEvent()) {
						t.Error("create sent an unexpected condition or appointment")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					stored = test.result
					if test.status == 0 {
						connection, _, err := http.NewResponseController(w).Hijack()
						if err != nil {
							t.Error("could not simulate losing the provider response")
							return
						}
						_ = connection.Close()
						return
					}
					w.Header().Set("Content-Type", "application/xml")
					w.Header().Set("Location", "https://fictional-other.example.test/alternate.ics")
					w.WriteHeader(test.status)
					_, _ = io.WriteString(w, "<error>"+strings.Repeat("private-details", 4096)+"</error>")
				default:
					t.Error("an unexpected method reached the selected resource")
					w.WriteHeader(http.StatusBadRequest)
				}
			}
			client := transportClient(t, fixtureOwnedSelection(), standardCredentials, f.network)
			result, err := client.CreateOwnedEvent(t.Context(), fixtureEvent())
			guard.Lock()
			defer guard.Unlock()
			if result != test.want || !slices.Equal(requests, test.methods) ||
				(test.want.Exists && err != nil) || (!test.want.Exists && !errors.Is(err, booking.ErrUnavailable)) {
				t.Fatalf("unexpected creation evidence: exists=%t error=%v requests=%v", result.Exists, err, requests)
			}
		})
	}
}

func writeOwnedEventFixture(w http.ResponseWriter, data, etag string) {
	if data == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ical.MIMEType)
	w.Header().Set("ETag", etag)
	_, _ = io.WriteString(w, data)
}
