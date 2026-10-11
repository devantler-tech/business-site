package runtimeapp

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

// Serve bounds request resources and drains owned work on cancellation. The HTTP
// library's panic/request diagnostics cannot disclose URLs or credentials in logs.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
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
