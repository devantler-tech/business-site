// Package contactrenderer owns the experiment's bounded presentation child.
// It is not an HTTP handler, authorization decision or production activation.
package contactrenderer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/devantler-tech/business-site/server/internal/renderwire"
	"github.com/open-feature/go-sdk/openfeature"
)

// ErrDisabled selects the retained Go presentation only before work is admitted.
var ErrDisabled = errors.New("contact renderer disabled")

// ErrUnavailable is the only enabled-failure diagnostic; it contains no input.
var ErrUnavailable = errors.New("contact renderer unavailable")

type result struct {
	body string
	err  error
}

type pending struct {
	reply    chan string
	answered bool
}

type generation struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	writes  chan []byte
	pending map[string]*pending
	done    chan struct{}
	reaped  chan struct{}
	failed  bool // protected, like pending, by Supervisor.mu
}

// Supervisor starts at most one fixed child and retains at most eight renders.
// Construction does not spawn a process. Paths are deployment-owned, not input.
type Supervisor struct {
	mu                sync.Mutex
	program, artifact string
	flags             *openfeature.Client
	g                 *generation
	lastStart         time.Time
	slots             chan struct{}
	done              chan struct{}
	closed            bool
}

// New validates fixed absolute local paths without inheriting a shell or keys.
// A nil flag client remains closed; no environment variable enables rendering.
func New(program, artifact string, flags *openfeature.Client) (*Supervisor, error) {
	for _, path := range []string{program, artifact} {
		if !filepath.IsAbs(path) {
			return nil, ErrUnavailable
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, ErrUnavailable
		}
	}
	return &Supervisor{program: program, artifact: artifact, flags: flags, slots: make(chan struct{}, 8), done: make(chan struct{})}, nil
}

// Render buffers a complete result within 250 ms, including cold start and gate
// evaluation. The HTTP caller MUST reauthorize its original session/client/record
// after this wait and before sending anything; a returned body is not authority.
func (s *Supervisor) Render(ctx context.Context, contact renderwire.Contact) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	select {
	case <-s.done:
		return "", ErrUnavailable
	default:
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return "", ErrUnavailable
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		<-s.slots
		return "", ErrUnavailable
	}
	id := hex.EncodeToString(idBytes)
	frame, err := renderwire.RequestFrame(renderwire.Request{ID: id, Contact: contact})
	if err != nil {
		<-s.slots
		return "", ErrUnavailable
	}
	finished := make(chan result, 1)
	// A misbehaving SDK provider can ignore context. Its retained operation still
	// owns a slot, so even that case cannot create an unbounded goroutine queue.
	go func() {
		defer func() { <-s.slots }()
		body, err := s.render(ctx, id, frame)
		finished <- result{body: body, err: err}
	}()
	select {
	case value := <-finished:
		if ctx.Err() != nil {
			s.cancelPending(id)
			return "", ErrUnavailable
		}
		return value.body, value.err
	case <-ctx.Done():
		s.cancelPending(id)
		return "", ErrUnavailable
	case <-s.done:
		return "", ErrUnavailable
	}
}

func (s *Supervisor) enabled(ctx context.Context) bool {
	if ctx.Err() != nil || s.flags == nil || s.flags.State() != openfeature.ReadyState {
		return false
	}
	on, err := s.flags.BooleanValue(ctx, "portal-contact-renderer", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && on && ctx.Err() == nil && s.flags.State() == openfeature.ReadyState
}

func (s *Supervisor) render(ctx context.Context, id string, frame []byte) (string, error) {
	if !s.enabled(ctx) {
		if ctx.Err() != nil {
			return "", ErrUnavailable
		}
		s.stop()
		return "", ErrDisabled
	}
	s.mu.Lock()
	if ctx.Err() != nil || s.closed {
		s.mu.Unlock()
		return "", ErrUnavailable
	}
	g, err := s.generationLocked()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	p := &pending{reply: make(chan string, 1)}
	g.pending[id] = p
	s.mu.Unlock()
	// Cancellation after registration always invalidates this generation,
	// including a cancellation during a provider's post-render evaluation.
	stopCancel := context.AfterFunc(ctx, func() { s.cancelPending(id) })
	defer stopCancel()
	defer func() {
		s.mu.Lock()
		delete(g.pending, id)
		s.mu.Unlock()
	}()
	select {
	case g.writes <- frame:
	case <-g.done:
		return "", ErrUnavailable
	case <-ctx.Done():
		s.cancelPending(id)
		return "", ErrUnavailable
	}
	var body string
	select {
	case body = <-p.reply:
	case <-g.done:
		return "", ErrUnavailable
	case <-ctx.Done():
		s.cancelPending(id)
		return "", ErrUnavailable
	}
	if !s.enabled(ctx) {
		// A late provider result belongs only to this generation. It must not
		// kill a replacement started after this request's deadline/reaping or
		// select a success fallback after rendering was already admitted.
		s.fail(g)
		return "", ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.closed || g.failed || s.g != g {
		return "", ErrUnavailable
	}
	return body, nil
}

// generationLocked retains a failed child until Wait has reaped it. A new
// caller cannot overlap generations, including during restart throttling.
func (s *Supervisor) generationLocked() (*generation, error) {
	if s.g != nil {
		if s.g.failed {
			return nil, ErrUnavailable
		}
		return s.g, nil
	}
	if time.Since(s.lastStart) < time.Second {
		return nil, ErrUnavailable
	}
	s.lastStart = time.Now()
	cmd := exec.Command(s.program, s.artifact) // fixed paths; never a shell
	cmd.Env = []string{"LC_ALL=C", "LANG=C"}
	cmd.Dir = filepath.Dir(s.artifact)
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, ErrUnavailable
	}
	if cmd.Start() != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, ErrUnavailable
	}
	g := &generation{cmd: cmd, input: input, output: output, writes: make(chan []byte, 8), pending: make(map[string]*pending), done: make(chan struct{}), reaped: make(chan struct{})}
	s.g = g
	go s.read(g)
	go s.write(g)
	go func() {
		_ = cmd.Wait() // exactly one owner reaps; no raw child error is exposed
		s.mu.Lock()
		s.failLocked(g)
		if s.g == g {
			s.g = nil
		}
		close(g.reaped)
		s.mu.Unlock()
	}()
	return g, nil
}

func (s *Supervisor) read(g *generation) {
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(g.output, prefix[:]); err != nil {
			s.fail(g)
			return
		}
		size, err := renderwire.ResponseSize(prefix[:])
		if err != nil {
			s.fail(g)
			return
		}
		frame := make([]byte, size+4)
		copy(frame, prefix[:])
		if _, err := io.ReadFull(g.output, frame[4:]); err != nil {
			s.fail(g)
			return
		}
		response, err := renderwire.DecodeResponse(frame)
		if err != nil {
			s.fail(g)
			return
		}
		s.mu.Lock()
		p := g.pending[response.ID]
		if g.failed || s.g != g || p == nil || p.answered {
			s.failLocked(g)
			s.mu.Unlock()
			return
		}
		p.answered = true
		p.reply <- response.Body
		s.mu.Unlock()
	}
}

func (s *Supervisor) write(g *generation) {
	for {
		select {
		case <-g.done:
			return
		case frame := <-g.writes:
			if _, err := io.Copy(g.input, bytes.NewReader(frame)); err != nil {
				s.fail(g)
				return
			}
		}
	}
}

func (s *Supervisor) cancelPending(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.g != nil && s.g.pending[id] != nil {
		s.failLocked(s.g)
	}
}

func (s *Supervisor) fail(g *generation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failLocked(g)
}

func (s *Supervisor) failLocked(g *generation) {
	if g.failed {
		return
	}
	g.failed = true
	close(g.done)
	_ = g.input.Close()
	_ = g.output.Close()
	_ = g.cmd.Process.Kill()
}

func (s *Supervisor) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.g != nil {
		s.failLocked(s.g)
	}
}

// Close rejects new work, invalidates pending renders and reaps its sole child.
// It never signals a process group, another session's process or a host service.
func (s *Supervisor) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	g := s.g
	if g != nil {
		s.failLocked(g)
	}
	s.mu.Unlock()
	if g != nil {
		<-g.reaped
	}
}
