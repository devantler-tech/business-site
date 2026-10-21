package runtimeapp

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// Serve bounds request resources and drains owned work on cancellation. The HTTP
// library's panic/request diagnostics cannot disclose URLs or credentials in logs.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	return ServeReady(ctx, listener, handler, nil)
}

// ServeReady starts the HTTP accept loop before notifying its caller. A slow
// notification consumer cannot prevent the already announced server from serving.
// The callback runs once in its own goroutine. Its caller owns notification
// completion; server shutdown never waits for a blocked advisory output consumer.
func ServeReady(ctx context.Context, listener net.Listener, handler http.Handler, ready func()) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	var accepting <-chan struct{}
	if ready != nil {
		owned := &acceptingListener{Listener: listener, accepting: make(chan struct{})}
		listener, accepting = owned, owned.accepting
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	for {
		select {
		case <-accepting:
			accepting = nil
			if ctx.Err() == nil {
				go ready()
			}
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return errors.New("runtime listener failed")
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
				<-done
				return errors.New("runtime shutdown deadline exceeded")
			}
			<-done
			return nil
		}
	}
}

type acceptingListener struct {
	net.Listener
	once      sync.Once
	accepting chan struct{}
}

func (l *acceptingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepting) })
	return l.Listener.Accept()
}
