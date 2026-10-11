package runtimeapp

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

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
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
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
		conn.Close()
		t.Fatal("stopped listener still accepts connections")
	}
}
