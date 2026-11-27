package caldav

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

func TestUpdateOwnedEventRequiresThePersistedVersion(t *testing.T) {
	const oldTag = `"synthetic-original"`
	const newTag = `"synthetic-rescheduled"`
	prior := fixtureEvent()
	next := prior
	next.Revision++
	next.Slot.Start = prior.Slot.Start.Add(time.Hour)
	next.Slot.End = prior.Slot.End.Add(time.Hour)
	rescheduled := strings.NewReplacer("SEQUENCE:2", "SEQUENCE:3", "T210000Z", "T220000Z", "T213000Z", "T223000Z").Replace(fixtureOwnedEvent)
	foreign := strings.Replace(fixtureOwnedEvent, fixtureEventKey, strings.Repeat("a", 64), 1)

	tests := []struct {
		name    string
		initial string
		tag     string
		expect  string
		store   bool
		status  int
		methods []string
		success bool
	}{
		{"change the same appointment", fixtureOwnedEvent, oldTag, oldTag, true, http.StatusNoContent, []string{"GET", "PUT", "GET"}, true},
		{"reuse a confirmed update", rescheduled, newTag, oldTag, false, 0, []string{"GET"}, true},
		{"reject reused provider validator", rescheduled, oldTag, oldTag, false, 0, []string{"GET"}, false},
		{"do not recreate a missing appointment", "", "", oldTag, false, 0, []string{"GET"}, false},
		{"do not replace a different appointment", foreign, oldTag, oldTag, false, 0, []string{"GET"}, false},
		{"do not adopt an external version", fixtureOwnedEvent, newTag, oldTag, false, 0, []string{"GET"}, false},
		{"reject a weak validator before sending", fixtureOwnedEvent, oldTag, "W/" + oldTag, false, 0, nil, false},
		{"reject a wildcard before sending", fixtureOwnedEvent, oldTag, "*", false, 0, nil, false},
		{"confirm after a provider error", fixtureOwnedEvent, oldTag, oldTag, true, http.StatusServiceUnavailable, []string{"GET", "PUT", "GET"}, true},
		{"concurrent matching update", fixtureOwnedEvent, oldTag, oldTag, true, http.StatusPreconditionFailed, []string{"GET", "PUT", "GET"}, true},
		{"conflict without the new revision", fixtureOwnedEvent, oldTag, oldTag, false, http.StatusPreconditionFailed, []string{"GET", "PUT", "GET"}, false},
		{"do not trust a successful response alone", fixtureOwnedEvent, oldTag, oldTag, false, http.StatusNoContent, []string{"GET", "PUT", "GET"}, false},
		{"a revoked credential stops the update", fixtureOwnedEvent, oldTag, oldTag, false, http.StatusUnauthorized, []string{"GET", "PUT"}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var guard sync.Mutex
			var methods []string
			stored, validator := test.initial, test.tag
			fixture := newCalendarTransportFixture(t, selectedCalendarHost, "", "/calendars/selected/portal-"+fixtureEventKey+".ics")
			fixture.handler = func(w http.ResponseWriter, request *http.Request) {
				guard.Lock()
				defer guard.Unlock()
				methods = append(methods, request.Method)
				switch request.Method {
				case http.MethodGet:
					writeOwnedEventFixture(w, stored, validator)
				case http.MethodPut:
					data, err := io.ReadAll(io.LimitReader(request.Body, maxOwnedEventBytes+1))
					if err != nil || request.Header.Get("If-Match") != oldTag ||
						request.Header.Get("If-None-Match") != "" ||
						!matchesOwnedEvent(data, "portal-"+fixtureEventKey+"@devantler.tech", next) {
						t.Error("update must send the exact precondition and new appointment revision")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if test.store {
						stored, validator = rescheduled, newTag
					}
					w.WriteHeader(test.status)
				default:
					t.Error("an unexpected method reached the calendar resource")
					w.WriteHeader(http.StatusBadRequest)
				}
			}
			client := transportClient(t, fixtureOwnedSelection(), standardCredentials, fixture.network)
			got, err := client.UpdateOwnedEvent(t.Context(), prior, next, test.expect)
			guard.Lock()
			defer guard.Unlock()
			ok := (err == nil && got == (OwnedEventObservation{Exists: true, ETag: newTag}))
			refused := errors.Is(err, booking.ErrUnavailable) && got == (OwnedEventObservation{})
			if !slices.Equal(methods, test.methods) || (test.success && !ok) || (!test.success && !refused) {
				t.Fatalf("unexpected update evidence: exists=%t error=%v methods=%v", got.Exists, err, methods)
			}
		})
	}
}

func TestUpdateOwnedEventRejectsUnrelatedRevisionsBeforeNetworkAccess(t *testing.T) {
	prior := fixtureEvent()
	fixture := newCalendarTransportFixture(t, selectedCalendarHost, "", "/calendars/selected/portal-"+fixtureEventKey+".ics")
	client := transportClient(t, fixtureOwnedSelection(), standardCredentials, fixture.network)
	for _, next := range []OwnedEvent{
		prior,
		{Key: prior.Key, Slot: prior.Slot, Revision: prior.Revision + 2},
		{Key: strings.Repeat("a", 64), Slot: prior.Slot, Revision: prior.Revision + 1},
		{Key: prior.Key, Revision: prior.Revision + 1},
	} {
		if got, err := client.UpdateOwnedEvent(t.Context(), prior, next, `"synthetic-original"`); !errors.Is(err, booking.ErrUnavailable) || got.Exists {
			t.Fatalf("unrelated revision yielded exists=%t error=%v", got.Exists, err)
		}
	}
	if fixture.lookups.Load() != 0 || fixture.requests.Load() != 0 {
		t.Fatal("unrelated revisions reached the provider")
	}
}
