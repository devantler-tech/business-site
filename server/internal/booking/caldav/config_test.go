package caldav

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/booking"
)

// Explicit source admission cannot depend on a credential read. The reserved
// hostname also requires construction to work without a reachable provider.
func TestConstructorAdmitsSelectionWithoutCredentialRead(t *testing.T) {
	var credentialsRead atomic.Int32
	client, err := New(Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/selected/"}}, func(context.Context) (string, string, error) {
		credentialsRead.Add(1)
		return "", "", errors.New("constructor read a fictional credential")
	})
	if err != nil || client == nil {
		t.Fatal("explicit HTTPS calendar selection was refused", err)
	}
	if credentialsRead.Load() != 0 {
		t.Fatal("constructor read a credential")
	}
}

// These configurations must fail before any credential or connection effect.
func TestConstructorRefusesUnconfinedSources(t *testing.T) {
	cases := []struct {
		name   string
		config Config
	}{
		{"no selection", Config{Origin: "https://calendar.example.test"}},
		{"HTTP origin", Config{Origin: "http://calendar.example.test", Calendars: []string{"/selected/"}}},
		{"origin userinfo", Config{Origin: "https://fictional:fictional@calendar.example.test", Calendars: []string{"/selected/"}}},
		{"origin query", Config{Origin: "https://calendar.example.test?target=other", Calendars: []string{"/selected/"}}},
		{"origin fragment", Config{Origin: "https://calendar.example.test#other", Calendars: []string{"/selected/"}}},
		{"origin path", Config{Origin: "https://calendar.example.test/other/", Calendars: []string{"/selected/"}}},
		{"loopback address", Config{Origin: "https://127.0.0.1", Calendars: []string{"/selected/"}}},
		{"mapped loopback", Config{Origin: "https://[::ffff:127.0.0.1]", Calendars: []string{"/selected/"}}},
		{"link-local address", Config{Origin: "https://169.254.169.254", Calendars: []string{"/selected/"}}},
		{"localhost name", Config{Origin: "https://localhost", Calendars: []string{"/selected/"}}},
		{"root collection", Config{Origin: "https://calendar.example.test", Calendars: []string{"/"}}},
		{"parent traversal", Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/../other/"}}},
		{"encoded traversal", Config{Origin: "https://calendar.example.test", Calendars: []string{"/calendars/%2e%2e/other/"}}},
		{"absolute collection URL", Config{Origin: "https://calendar.example.test", Calendars: []string{"https://other.example.test/selected/"}}},
		{"collection query", Config{Origin: "https://calendar.example.test", Calendars: []string{"/selected/?other"}}},
		{"duplicate collection", Config{Origin: "https://calendar.example.test", Calendars: []string{"/selected/", "/selected/"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var credentialsRead atomic.Int32
			client, err := New(tc.config, func(context.Context) (string, string, error) {
				credentialsRead.Add(1)
				return "fictional-user", "fictional-password", nil
			})
			if !errors.Is(err, booking.ErrUnavailable) || client != nil || credentialsRead.Load() != 0 {
				t.Fatal("unconfined calendar configuration was admitted or read a credential")
			}
		})
	}
}
