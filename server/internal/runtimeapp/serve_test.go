package runtimeapp

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServerServesDuringReadinessNotification keeps slow startup diagnostics off
// the actual HTTP accept path and joins the real server on fixture teardown.
func TestServerServesDuringReadinessNotification(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	notified, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ServeReady(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "serving")
		}), func() { close(notified); <-release })
	}()
	defer func() {
		close(release)
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("notified server did not stop cleanly", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("notified server did not join")
		}
	}()
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP accept loop did not announce startup")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal("readiness consumer blocked the HTTP server", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(body) != "serving" {
		t.Fatal("notified server did not serve its real handler")
	}
}

// TestServerDrainsAnActiveRequestAndStops observes a real request during bounded shutdown.
func TestServerDrainsAnActiveRequestAndStops(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = io.WriteString(w, "completed")
		}))
	}()
	response := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			response <- "error"
			return
		}
		data, readErr := io.ReadAll(res.Body)
		closeErr := res.Body.Close()
		if readErr != nil || closeErr != nil {
			response <- "error"
			return
		}
		response <- string(data)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not start")
	}
	cancel()
	close(release)
	select {
	case got := <-response:
		if got != "completed" {
			t.Fatal("active request was dropped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		if err := conn.Close(); err != nil {
			t.Error("unexpected fixture connection cleanup failed")
		}
		t.Fatal("stopped listener still accepts connections")
	}
}
