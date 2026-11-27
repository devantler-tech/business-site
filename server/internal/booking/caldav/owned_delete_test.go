package caldav

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

func TestDeleteOwnedEventRequiresThePersistedVersion(t *testing.T) {
	const version = `"synthetic-booked-version"`
	const newerVersion = `"synthetic-next-version"`
	const canonicalPath = "/calendars/selected/portal-" + fixtureEventKey + ".ics"
	different := strings.Replace(fixtureOwnedEvent, "SEQUENCE:2", "SEQUENCE:3", 1)

	tests := []struct {
		name    string
		initial string
		tag     string
		request string
		next    string
		status  int
		methods []string
		success bool
	}{
		{"remove matching event", fixtureOwnedEvent, version, version, "", http.StatusNoContent, []string{"GET", "DELETE", "GET"}, true},
		{"already absent", "", "", version, "", 0, []string{"GET"}, true},
		{"changed event", different, version, version, "", 0, []string{"GET"}, false},
		{"changed version", fixtureOwnedEvent, newerVersion, version, "", 0, []string{"GET"}, false},
		{"weak version", fixtureOwnedEvent, version, "W/" + version, "", 0, nil, false},
		{"wildcard version", fixtureOwnedEvent, version, "*", "", 0, nil, false},
		{"list of versions", fixtureOwnedEvent, version, version + ", " + newerVersion, "", 0, nil, false},
		{"response lost after removal", fixtureOwnedEvent, version, version, "", 0, []string{"GET", "DELETE", "GET"}, true},
		{"error after removal", fixtureOwnedEvent, version, version, "", http.StatusServiceUnavailable, []string{"GET", "DELETE", "GET"}, true},
		{"removed concurrently", fixtureOwnedEvent, version, version, "", http.StatusPreconditionFailed, []string{"GET", "DELETE", "GET"}, true},
		{"no longer exists when deleting", fixtureOwnedEvent, version, version, "", http.StatusNotFound, []string{"GET", "DELETE", "GET"}, true},
		{"changed concurrently", fixtureOwnedEvent, version, version, different, http.StatusPreconditionFailed, []string{"GET", "DELETE", "GET"}, false},
		{"success response without removal", fixtureOwnedEvent, version, version, fixtureOwnedEvent, http.StatusAccepted, []string{"GET", "DELETE", "GET"}, false},
		{"credentials revoked", fixtureOwnedEvent, version, version, "", http.StatusUnauthorized, []string{"GET", "DELETE"}, false},
		{"redirect refused", fixtureOwnedEvent, version, version, "", http.StatusTemporaryRedirect, []string{"GET", "DELETE"}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var guard sync.Mutex
			var methods []string
			stored := test.initial
			fixture := newCalendarTransportFixture(t, selectedCalendarHost, "", canonicalPath)
			fixture.handler = func(w http.ResponseWriter, req *http.Request) {
				guard.Lock()
				defer guard.Unlock()
				methods = append(methods, req.Method)
				if req.Method == http.MethodGet {
					writeOwnedEventFixture(w, stored, test.tag)
					return
				}
				if req.Method != http.MethodDelete || req.Header.Get("If-Match") != version ||
					req.Header.Get("If-None-Match") != "" || req.ContentLength > 0 {
					t.Error("expected a single conditional delete of the canonical event")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				stored = test.next
				if test.status != 0 {
					w.Header().Set("Location", "https://fictional-other.example.test/alternate.ics")
					w.WriteHeader(test.status)
					return
				}
				connection, _, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Error("could not simulate losing the removal response")
					return
				}
				_ = connection.Close()
			}
			client := transportClient(t, fixtureOwnedSelection(), standardCredentials, fixture.network)
			err := client.DeleteOwnedEvent(t.Context(), fixtureEvent(), test.request)
			guard.Lock()
			defer guard.Unlock()
			if !slices.Equal(methods, test.methods) || (test.success && err != nil) ||
				(!test.success && !errors.Is(err, booking.ErrUnavailable)) {
				t.Fatalf("unexpected removal evidence: error=%v methods=%v", err, methods)
			}
		})
	}
}
