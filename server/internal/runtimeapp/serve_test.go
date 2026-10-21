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
	listener := newServerListener(t)
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

// TestServerStopsDuringBlockedReadinessNotification keeps advisory startup
// output outside the server's cancellation and listener-ownership boundary.
func TestServerStopsDuringBlockedReadinessNotification(t *testing.T) {
	listener := newServerListener(t)
	ctx, cancel := context.WithCancel(t.Context())
	notified, release, notificationDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	stopped := false
	go func() {
		done <- ServeReady(ctx, listener, http.NotFoundHandler(), func() {
			defer close(notificationDone)
			close(notified)
			<-release
		})
	}()
	t.Cleanup(func() {
		close(release)
		cancel()
		if !stopped {
			select {
			case err := <-done:
				if err != nil {
					t.Error("notified server cleanup failed", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("notified server cleanup did not join")
			}
		}
		select {
		case <-notificationDone:
		case <-time.After(5 * time.Second):
			t.Error("released notification did not finish")
		}
	})
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP accept loop did not announce startup")
	}
	cancel()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal("notified server did not stop cleanly", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked readiness notification prevented cancellation")
	}
	assertListenerClosed(t, listener)
}

// TestServerDrainsAnActiveRequestAndStops observes a real request during bounded shutdown.
func TestServerDrainsAnActiveRequestAndStops(t *testing.T) {
	listener := newServerListener(t)
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
	assertListenerClosed(t, listener)
}

// newServerListener gives each real-server test a private loopback listener.
func newServerListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

// assertListenerClosed checks that server completion released the actual listener.
func assertListenerClosed(t *testing.T, listener net.Listener) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		if err := conn.Close(); err != nil {
			t.Error("unexpected fixture connection cleanup failed", err)
		}
		t.Fatal("stopped listener still accepts connections")
	}
}
