package runtimeapp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// buildActualCommand binds a real executable to disposable public fixture bytes.
func buildActualCommand(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	public := filepath.Join(dir, "public")
	for _, route := range []string{"", "da", "projects", "blog"} {
		target := filepath.Join(public, route)
		if err := os.MkdirAll(target, 0700); err != nil {
			t.Fatal("public fixture creation failed")
		}
		if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("<html>public fixture</html>"), 0600); err != nil {
			t.Fatal("public fixture write failed")
		}
	}
	artifact, err := webapp.Load(os.DirFS(public))
	if err != nil {
		t.Fatal("public fixture validation failed")
	}
	binary := filepath.Join(dir, "business-site")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "-ldflags",
		"-X main.revision="+strings.Repeat("a", 40)+" -X main.publicDigest="+artifact.Digest()+" -X main.buildMode=development", "./cmd/business-site")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("actual command compilation failed: %s", output)
	}
	return binary, public
}

// startupDiagnostic never exposes captured child output, including on overflow.
// The process copier and failure observer may use it concurrently.
type startupDiagnostic struct {
	mu       sync.Mutex
	data     [512]byte
	size     int
	overflow bool
}

// Write retains bounded private bytes without blocking the child's stderr copier.
func (d *startupDiagnostic) Write(data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := copy(d.data[d.size:], data)
	d.size += n
	d.overflow = d.overflow || n != len(data)
	return len(data), nil
}

// label releases only a closed category, never captured child diagnostics.
func (d *startupDiagnostic) label() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.overflow {
		return "startup-diagnostic-limit"
	}
	if string(d.data[:d.size]) == "runtime listener is unavailable\n" {
		return "startup-listener-unavailable"
	}
	return "startup-unclassified"
}

// actualReadyAddress accepts only the explicit child-owned loopback endpoint.
func actualReadyAddress(line string) string {
	value, ok := strings.CutPrefix(line, "runtime ready; listen=")
	if !ok {
		return ""
	}
	address, ok := strings.CutSuffix(value, "; private routes remain independently gated")
	if !ok {
		return ""
	}
	host, port, err := net.SplitHostPort(address)
	number, conversion := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || conversion != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return ""
	}
	return address
}

type actualCommandProcess struct {
	ctx        context.Context
	cancel     context.CancelFunc
	command    *exec.Cmd
	ready      chan actualReadiness
	exited     chan struct{}
	waitErr    error // Read only after exited closes.
	diagnostic startupDiagnostic
	started    bool
	stopOnce   sync.Once
	stopErr    error
}

type actualReadiness struct {
	address   string
	announced bool
}

// launchActualCommand lets the actual child bind once; it never reserves a port.
func launchActualCommand(t *testing.T, binary, public, listen string, environment []string) *actualCommandProcess {
	t.Helper()
	// testing cancels its context before Cleanup; the owner still needs its
	// bounded opportunity to interrupt and reap the child normally.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 45*time.Second)
	p := &actualCommandProcess{ctx: ctx, cancel: cancel, ready: make(chan actualReadiness, 1), exited: make(chan struct{})}
	p.command = exec.CommandContext(ctx, binary, "--public", public, "--listen", listen)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTAL_") && !strings.HasPrefix(value, "portal-") && !strings.HasPrefix(value, "SSL_CERT_FILE=") {
			p.command.Env = append(p.command.Env, value)
		}
	}
	p.command.Env = append(p.command.Env, environment...)
	output, err := p.command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal("fixture output channel failed")
	}
	p.command.Stderr = &p.diagnostic
	if err := p.command.Start(); err != nil {
		cancel()
		t.Fatal("actual command did not start")
	}
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 512), 512)
		readiness := actualReadiness{announced: scanner.Scan()}
		if readiness.announced {
			readiness.address = actualReadyAddress(scanner.Text())
		}
		if scanner.Err() != nil {
			readiness.announced = true // Oversized readiness is invalid, not an exit.
		}
		p.ready <- readiness
		_, _ = io.Copy(io.Discard, output)
		p.waitErr = p.command.Wait()
		close(p.exited)
	}()
	t.Cleanup(func() {
		if err := p.stop(); err != nil {
			t.Error(err)
		}
	})
	return p
}

// awaitReady observes the complete owned endpoint or a bounded safe refusal.
func (p *actualCommandProcess) awaitReady() (string, error) {
	select {
	case readiness := <-p.ready:
		if readiness.address != "" {
			p.started = true
			return readiness.address, nil
		}
		if readiness.announced {
			return "", errors.New("startup-readiness-invalid")
		}
		select {
		case <-p.exited:
			return "", errors.New(p.diagnostic.label())
		case <-p.ctx.Done():
			return "", errors.New("startup-readiness-deadline")
		}
	case <-p.ctx.Done():
		return "", errors.New("startup-readiness-deadline")
	}
}

// stop joins the sole reaper and avoids secondary shutdown noise after refusal.
func (p *actualCommandProcess) stop() error {
	p.stopOnce.Do(func() {
		defer p.cancel()
		select {
		case <-p.exited:
		default:
			if err := p.command.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.cancel()
				p.stopErr = errors.New("actual command shutdown signal failed")
			}
			select {
			case <-p.exited:
			case <-p.ctx.Done():
				p.cancel()
				select {
				case <-p.exited:
				case <-time.After(5 * time.Second):
					p.stopErr = errors.New("actual command reaping exceeded its deadline")
					return
				}
				p.stopErr = errors.New("actual command shutdown exceeded its deadline")
			}
		}
		if p.started && p.waitErr != nil && p.stopErr == nil {
			p.stopErr = errors.New("actual command did not shut down cleanly")
		}
	})
	return p.stopErr
}

func TestActualExecutableListenerOwnership(t *testing.T) {
	binary, public := buildActualCommand(t)
	var listen net.ListenConfig
	occupied, err := listen.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("fixture owned listener failed")
	}
	t.Cleanup(func() { _ = occupied.Close() })
	refused := launchActualCommand(t, binary, public, occupied.Addr().String(), nil)
	if address, err := refused.awaitReady(); address != "" || err == nil || err.Error() != "startup-listener-unavailable" {
		t.Fatal("actual occupied listener was not safely classified")
	}
	if err := refused.stop(); err != nil {
		t.Fatal("already exited child produced secondary shutdown noise")
	}
	// An unrelated owned port stays occupied while the child binds its own one.
	child := launchActualCommand(t, binary, public, "127.0.0.1:0", nil)
	address, err := child.awaitReady()
	if err != nil || address == occupied.Addr().String() {
		t.Fatal("actual child did not report its independently owned listener")
	}
	commandStatus(t, address, "/", 200)
	commandStatus(t, address, "/portal/sign-in", 404)
	if err := child.stop(); err != nil {
		t.Fatal("ready child did not stop cleanly")
	}
	if err := child.stop(); err != nil {
		t.Fatal("repeated cleanup did not retain the joined result")
	}
}

func TestActualExecutableCleanupOwnsChild(t *testing.T) {
	binary, public := buildActualCommand(t)
	t.Run("ordinary return drains the still-live child", func(t *testing.T) {
		child := launchActualCommand(t, binary, public, "127.0.0.1:0", nil)
		address, err := child.awaitReady()
		if err != nil {
			t.Fatal(err)
		}
		commandStatus(t, address, "/", 200)
		// Cleanup is LIFO and testing cancels its own context first. The
		// process owner must still have its independent drain/reap window.
		t.Cleanup(func() {
			if child.ctx.Err() != nil {
				t.Error("child lifetime ended before owned cleanup")
			}
		})
		// Deliberately return without an explicit stop: registered cleanup owns it.
	})
}

func TestActualCommandReadinessAndDiagnostics(t *testing.T) {
	for _, line := range []string{"runtime ready;", "runtime ready; listen=localhost:1234; private routes remain independently gated", "runtime ready; listen=0.0.0.0:1234; private routes remain independently gated", "runtime ready; listen=127.0.0.1:0; private routes remain independently gated", "runtime ready; listen=127.0.0.1:65536; private routes remain independently gated", "runtime ready; listen=127.0.0.1:001; private routes remain independently gated", "runtime ready; listen=127.0.0.1:1234; private routes remain independently gated extra"} {
		if actualReadyAddress(line) != "" {
			t.Fatal("unowned or malformed readiness was accepted")
		}
	}
	if actualReadyAddress("runtime ready; listen=127.0.0.1:1234; private routes remain independently gated") != "127.0.0.1:1234" {
		t.Fatal("explicit owned loopback readiness was rejected")
	}
	for _, tc := range []struct{ input, label string }{
		{"", "startup-unclassified"},
		{"runtime listener is unavailable\n", "startup-listener-unavailable"},
		{"runtime listener is unavailable\n/private/synthetic-detail", "startup-unclassified"},
		{"synthetic private startup detail", "startup-unclassified"},
		{strings.Repeat("x", 513), "startup-diagnostic-limit"},
	} {
		var diagnostic startupDiagnostic
		if n, err := diagnostic.Write([]byte(tc.input)); n != len(tc.input) || err != nil || diagnostic.label() != tc.label {
			t.Fatal("bounded diagnostic did not return only its closed safe category")
		}
	}
	var concurrent startupDiagnostic
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 20 {
				_, _ = concurrent.Write([]byte(strings.Repeat("x", 20)))
				_ = concurrent.label()
			}
		})
	}
	workers.Wait()
	if concurrent.label() != "startup-diagnostic-limit" {
		t.Fatal("concurrent capture lost its bounded overflow category")
	}
}
