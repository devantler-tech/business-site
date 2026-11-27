package caldav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

func TestOwnedCalendarBindingRetainsSelectionAcrossPasswordRotation(t *testing.T) {
	config := fixtureOwnedSelection()
	client, err := New(config, standardCredentials)
	if err != nil {
		t.Fatal(err)
	}
	original, err := client.OwnedCalendarBinding(t.Context())
	if err != nil || original != fixtureCalendarBinding {
		t.Fatalf("could not capture the owned calendar binding: %v", err)
	}
	config.Origin = "https://" + strings.ToUpper(selectedCalendarHost) + ":443/"
	config.Calendars = []string{config.OwnedCalendar}
	rotated, err := New(config, func(context.Context) (string, string, error) {
		return "fictional-user", "rotated-fictional-password", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding, err := rotated.OwnedCalendarBinding(t.Context()); binding != original || err != nil {
		t.Fatalf("equivalent selection and a new password changed the binding: %v", err)
	}
}

func TestOwnedCalendarRetriesRefuseChangedDestinationBeforeConnecting(t *testing.T) {
	f := newCalendarTransportFixture(t, selectedCalendarHost, "", "/unused/")
	original := transportClient(t, fixtureOwnedSelection(), standardCredentials, f.network)
	binding, err := original.OwnedCalendarBinding(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		name        string
		change      func(*Config)
		credentials func(context.Context) (string, string, error)
	}{
		{"another host", func(c *Config) { c.Origin = "https://other.example.test" }, standardCredentials},
		{"another collection", func(c *Config) { c.OwnedCalendar = "/calendars/read-only/" }, standardCredentials},
		{"another account", func(*Config) {}, func(context.Context) (string, string, error) {
			return "other-fictional-user", "fictional-password", nil
		}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			config := fixtureOwnedSelection()
			fixture.change(&config)
			client := transportClient(t, config, fixture.credentials, f.network)
			prior := fixtureEvent()
			prior.CalendarBinding = binding
			next := prior
			next.Revision++
			const tag = `"saved-version"`

			observation, err := client.ReadOwnedEvent(t.Context(), prior)
			requireSelectionConflict(t, observation, err)
			observation, err = client.CreateOwnedEvent(t.Context(), prior)
			requireSelectionConflict(t, observation, err)
			observation, err = client.UpdateOwnedEvent(t.Context(), prior, next, tag)
			requireSelectionConflict(t, observation, err)
			requireSelectionConflict(t, OwnedEventObservation{}, client.DeleteOwnedEvent(t.Context(), prior, tag))
		})
	}
	unbound := fixtureEvent()
	unbound.CalendarBinding = ""
	if result, err := original.CreateOwnedEvent(t.Context(), unbound); result.Exists || err != booking.ErrUnavailable {
		t.Fatalf("calendar creation accepted an unbound intent: %v", err)
	}
	if f.lookups.Load() != 0 || f.dials.Load() != 0 || f.requests.Load() != 0 {
		t.Fatal("refusing the saved selection reached a provider")
	}
}

func TestOwnedCalendarRejectsAccountChangeBetweenReadAndWrite(t *testing.T) {
	target := "/calendars/selected/portal-" + fixtureEventKey + ".ics"
	f := newCalendarTransportFixture(t, selectedCalendarHost, http.MethodGet, target)
	f.handler = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	client := transportClient(t, fixtureOwnedSelection(), func(ctx context.Context) (string, string, error) {
		if f.requests.Load() != 0 {
			return "other-fictional-user", "other-fictional-password", nil
		}
		return standardCredentials(ctx)
	}, f.network)
	event := fixtureEvent()
	event.CalendarBinding = ""
	binding, err := client.OwnedCalendarBinding(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	event.CalendarBinding = binding
	result, err := client.CreateOwnedEvent(t.Context(), event)
	requireSelectionConflict(t, result, err)
	if f.requests.Load() != 1 {
		t.Fatal("account change after observation allowed a provider write")
	}
}

func TestOwnedCalendarUsesRotatedPasswordDuringWriteAndReadback(t *testing.T) {
	target := "/calendars/selected/portal-" + fixtureEventKey + ".ics"
	f := newCalendarTransportFixture(t, selectedCalendarHost, "", target)
	const rotated = "rotated-fictional-password"
	f.password = func(call int32) string {
		if call == 1 {
			return "fictional-password"
		}
		return rotated
	}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		switch f.requests.Load() {
		case 1:
			if r.Method != http.MethodGet {
				t.Error("creation did not begin with an observation")
			}
			w.WriteHeader(http.StatusNotFound)
		case 2:
			if r.Method != http.MethodPut || r.Header.Get("If-None-Match") != "*" {
				t.Error("creation lost its absent-resource precondition")
			}
			w.WriteHeader(http.StatusCreated)
		case 3:
			if r.Method != http.MethodGet {
				t.Error("creation did not end with a fresh observation")
			}
			w.Header().Set("Content-Type", "text/calendar")
			w.Header().Set("ETag", `"created-version"`)
			_, _ = io.WriteString(w, fixtureOwnedEvent)
		}
	}
	client := transportClient(t, fixtureOwnedSelection(), func(ctx context.Context) (string, string, error) {
		if f.requests.Load() != 0 {
			return "fictional-user", rotated, nil
		}
		return standardCredentials(ctx)
	}, f.network)
	result, err := client.CreateOwnedEvent(t.Context(), fixtureEvent())
	if err != nil || result != (OwnedEventObservation{Exists: true, ETag: `"created-version"`}) || f.requests.Load() != 3 {
		t.Fatalf("password rotation broke the same calendar operation: %v", err)
	}
}

func requireSelectionConflict(t *testing.T, result OwnedEventObservation, err error) {
	t.Helper()
	if result != (OwnedEventObservation{}) || !errors.Is(err, ErrOwnedEventConflict) || !errors.Is(err, booking.ErrUnavailable) {
		t.Fatalf("expected a conflict without provider data, got %v", err)
	}
}
