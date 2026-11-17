package caldav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// Reusing an earlier credential would make the second real report fail.
func TestBusyRereadsCredentialForEachOperation(t *testing.T) {
	var requests, credentialsRead atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		user, password, ok := r.BasicAuth()
		valid := ok && user == "fictional-user" && ((call == 1 && password == "fictional-first") || (call == 2 && password == "fictional-rotated"))
		if !valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Fictional provider//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20270327T210000Z\r\nDTSTART:20270327T210000Z\r\nDTEND:20270327T220000Z\r\nEND:VFREEBUSY\r\nEND:VCALENDAR\r\n")
	}))
	t.Cleanup(server.Close)
	now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	client := freeBusyFixtureClient(t, server, now)
	client.credentials = func(context.Context) (string, string, error) {
		if credentialsRead.Add(1) == 1 {
			return "fictional-user", "fictional-first", nil
		}
		return "fictional-user", "fictional-rotated", nil
	}
	for range 2 {
		snapshot, err := client.Busy(t.Context(), booking.Interval{Start: now, End: now.Add(time.Hour)})
		if err != nil || !snapshot.Complete || len(snapshot.Busy) != 0 {
			t.Fatal("the current credential could not read complete empty availability", err)
		}
	}
	if requests.Load() != 2 || credentialsRead.Load() != 2 {
		t.Fatal("availability retried or retained credential state")
	}
}

// Credential failure must not reach a provider or expose private reader errors.
func TestBusyRefusesCredentialFailureBeforeProvider(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	client := freeBusyFixtureClient(t, server, now)
	client.credentials = func(context.Context) (string, string, error) {
		return "", "", errors.New("fictional-private-credential-reader-error")
	}
	snapshot, err := client.Busy(t.Context(), booking.Interval{Start: now, End: now.Add(time.Hour)})
	if !errors.Is(err, booking.ErrUnavailable) || !reflect.DeepEqual(snapshot, booking.BusySnapshot{}) || requests.Load() != 0 {
		t.Fatal("credential failure reached a provider or returned availability")
	}
	if strings.Contains(err.Error(), "fictional-private-credential-reader-error") {
		t.Fatal("credential reader detail escaped the adapter")
	}
}

// Cancellation during a credential wait must still stop the later HTTP effect.
func TestBusyRechecksCancellationAfterCredentialWait(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	now := time.Date(2027, 3, 27, 21, 0, 0, 0, time.UTC)
	client := freeBusyFixtureClient(t, server, now)
	entered, release := make(chan struct{}), make(chan struct{})
	client.credentials = func(ctx context.Context) (string, string, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "fictional-user", "fictional-password", nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		snapshot, err := client.Busy(ctx, booking.Interval{Start: now, End: now.Add(time.Hour)})
		done <- errors.Is(err, booking.ErrUnavailable) && reflect.DeepEqual(snapshot, booking.BusySnapshot{})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("operation never reached the credential boundary")
	}
	cancel()
	close(release)
	select {
	case refused := <-done:
		if !refused || requests.Load() != 0 {
			t.Fatal("cancelled credential wait reached a provider or returned availability")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled credential wait did not finish")
	}
}
