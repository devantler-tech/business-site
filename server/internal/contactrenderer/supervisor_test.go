package contactrenderer

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/renderwire"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// TestMain uses this exact test binary as a real, bounded pipe peer. The sole
// argument is an owned fixture artifact, just as Node receives its fixed bundle.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && strings.HasSuffix(os.Args[1], ".renderer-test") {
		mode, err := os.ReadFile(os.Args[1])
		if err != nil {
			os.Exit(2)
		}
		peer(string(mode))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func peer(mode string) {
	var requests []map[string]any
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(os.Stdin, prefix[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(prefix[:])
		if size == 0 || size > 8192-4 {
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(os.Stdin, payload); err != nil {
			return
		}
		var request map[string]any
		if json.Unmarshal(payload, &request) != nil {
			return
		}
		switch mode {
		case "hold":
			continue
		case "exit":
			_, _ = fmt.Fprint(os.Stderr, "fictional-private-diagnostic")
			return
		case "oversize":
			binary.BigEndian.PutUint32(prefix[:], 65533)
			_, _ = os.Stdout.Write(prefix[:])
			continue
		case "truncated":
			_, _ = os.Stdout.Write([]byte{0, 0, 0, 100, '{'})
			return
		case "reverse":
			requests = append(requests, request)
			if len(requests) != 8 {
				continue
			}
			for i := len(requests) - 1; i >= 0; i-- {
				peerResponse(requests[i], "normal")
			}
		default:
			peerResponse(request, mode)
		}
	}
}

func peerResponse(request map[string]any, mode string) {
	contact, _ := request["contact"].(map[string]any)
	body, _ := contact["name"].(string)
	if mode == "environment" && (len(os.Environ()) != 2 || os.Getenv("LC_ALL") != "C" || os.Getenv("LANG") != "C") {
		body = "unexpected inherited environment"
	}
	id, _ := request["id"].(string)
	if mode == "foreign" {
		id = strings.Repeat("f", 32)
	}
	payload, _ := json.Marshal(map[string]any{"version": 1, "id": id, "body": "<main>" + body + "</main>"})
	if mode == "malformed" {
		payload = []byte(`{"version":1,"version":1,"id":"` + id + `","body":"private"}`)
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(payload)))
	frame := append(prefix[:], payload...)
	if mode == "duplicate" {
		frame = append(frame, frame...)
	}
	_, _ = os.Stdout.Write(frame)
}

func projection() renderwire.Contact {
	return renderwire.Contact{
		Locale: "en", Name: "Example Client", Email: "client@example.test", Company: "Example",
		Revision: "9223372036854775807", State: "normal", Verification: "unverified", CSRF: "fictional-nonce",
		Text: renderwire.Text{Title: "Contact", Intro: "Your profile", NameLabel: "Name", EmailLabel: "Email",
			CompanyLabel: "Company", SaveLabel: "Save", PendingText: "Unverified", WorkspaceLabel: "Workspace", ReloadLabel: "Reload"},
	}
}

func flags(t *testing.T, value any) *openfeature.EvaluationAPI {
	t.Helper()
	values := map[string]memprovider.InMemoryFlag{}
	if value != nil {
		values["portal-contact-renderer"] = memprovider.InMemoryFlag{Key: "portal-contact-renderer", State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": value}}
	}
	return providerAPI(t, memprovider.NewInMemoryProvider(values))
}

func providerAPI(t *testing.T, provider openfeature.FeatureProvider) *openfeature.EvaluationAPI {
	t.Helper()
	api := isolated.NewAPI()
	if err := api.SetProviderAndWait(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })
	return api
}

func fixture(t *testing.T, mode string, value any) *Supervisor {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), "peer.renderer-test")
	if err := os.WriteFile(artifact, []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(program, artifact, flags(t, value).NewClient())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRealChildReusesGenerationAndCorrelatesEightReversedReplies(t *testing.T) {
	s := fixture(t, "reverse", true)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			c := projection()
			c.Name = fmt.Sprintf("Example %d", i)
			body, err := s.Render(t.Context(), c)
			if err != nil || body != "<main>"+c.Name+"</main>" {
				t.Errorf("wrong correlated result: %q %v", body, err)
			}
		})
	}
	wg.Wait()
}

func TestDisabledAndWrongTypeStartNoChild(t *testing.T) {
	for _, value := range []any{nil, false, "true"} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			s := fixture(t, "normal", value)
			body, err := s.Render(t.Context(), projection())
			if body != "" || !errors.Is(err, ErrDisabled) {
				t.Fatal("closed gate returned a result", err)
			}
			s.mu.Lock()
			started := !s.lastStart.IsZero()
			s.mu.Unlock()
			if started {
				t.Fatal("closed gate started a child")
			}
		})
	}
}

func TestReleaseCheckpointNeverStartsAChild(t *testing.T) {
	for _, value := range []any{nil, false, "true", true} {
		s := fixture(t, "normal", value)
		err := s.Check(t.Context())
		if (value == true && err != nil) || (value != true && !errors.Is(err, ErrDisabled)) {
			t.Fatal("release checkpoint returned an incorrect gate result", err)
		}
		s.mu.Lock()
		started := !s.lastStart.IsZero() || s.g != nil
		s.mu.Unlock()
		if started {
			t.Fatal("release checkpoint started a child")
		}
	}
}

func TestReleaseCheckpointSharesIgnoringProviderCapacity(t *testing.T) {
	s := fixture(t, "normal", true)
	p, finished := retainEightEvaluations(t, s, func() error { return s.Check(t.Context()) })
	defer close(p.release)
	if _, err := s.Render(t.Context(), projection()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("gate checks escaped the shared render capacity")
	}
	for range 8 {
		if err := awaitSignal(t, finished); !errors.Is(err, ErrUnavailable) {
			t.Fatal("gate check ignored its deadline", err)
		}
	}
	if err := s.Check(t.Context()); !errors.Is(err, ErrUnavailable) || p.calls.Load() != 8 {
		t.Fatal("timed-out evaluations released retained capacity")
	}
}

func TestRealProtocolFailuresReturnNoBodyAndReap(t *testing.T) {
	for _, mode := range []string{"foreign", "malformed", "oversize", "truncated", "exit"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t, mode, true)
			body, err := s.Render(t.Context(), projection())
			if body != "" || err != ErrUnavailable {
				t.Fatal("peer failure escaped", err)
			}
			s.Close()
			// Close returns only after its actual process has been reaped.
			s.mu.Lock()
			active := s.g != nil
			s.mu.Unlock()
			if active {
				t.Fatal("child was not reaped")
			}
		})
	}
}

func TestDeadlineIncludesColdStartAndEarlierCancellation(t *testing.T) {
	for _, budget := range []time.Duration{20 * time.Millisecond, time.Second} {
		t.Run(budget.String(), func(t *testing.T) {
			s := fixture(t, "hold", true)
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			start := time.Now()
			body, err := s.Render(ctx, projection())
			if body != "" || err != ErrUnavailable {
				t.Fatal("timeout escaped", err)
			}
			limit := min(budget, 250*time.Millisecond)
			if elapsed := time.Since(start); elapsed < limit/2 || elapsed > limit+150*time.Millisecond {
				t.Fatal("render budget not bounded", elapsed)
			}
			s.Close()
			if body, err := s.Render(t.Context(), projection()); body != "" || err != ErrUnavailable {
				t.Fatal("closed supervisor admitted work")
			}
		})
	}
}

// blockingProvider deliberately ignores context to exercise the SDK boundary,
// not a cooperative fake whose cancellation would conceal unbounded work.
type blockingProvider struct {
	openfeature.FeatureProvider
	calls    atomic.Int32
	blockAt  int32
	reached  chan int32
	release  chan struct{}
	offAfter bool
}

func (p *blockingProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, evaluation openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	n := p.calls.Add(1)
	if p.blockAt == 0 || n == p.blockAt {
		p.reached <- n
		<-p.release
	}
	if p.offAfter && n >= 2 {
		return openfeature.BoolResolutionDetail{Value: false}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, evaluation)
}

func blocking(t *testing.T, s *Supervisor, blockAt int32, offAfter bool) *blockingProvider {
	t.Helper()
	p := &blockingProvider{
		FeatureProvider: memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
			"portal-contact-renderer": {State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}},
		}),
		blockAt: blockAt, reached: make(chan int32, 8), release: make(chan struct{}), offAfter: offAfter,
	}
	s.flags = providerAPI(t, p).NewClient()
	return p
}

func awaitSignal[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("fixture event did not arrive")
	}
	var zero T
	return zero
}

func TestCapacityAlsoBoundsAnSDKProviderThatIgnoresContext(t *testing.T) {
	s := fixture(t, "normal", true)
	p, finished := retainEightEvaluations(t, s, func() error {
		_, err := s.Render(t.Context(), projection())
		return err
	})
	defer close(p.release)
	start := time.Now()
	if body, err := s.Render(t.Context(), projection()); body != "" || err != ErrUnavailable {
		t.Fatal("ninth render queued")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("ninth render waited")
	}
	for range 8 {
		if err := awaitSignal(t, finished); err != ErrUnavailable {
			t.Fatal("provider exceeded deadline", err)
		}
	}
	// Timed-out caller goroutines must not release capacity while an ignoring
	// provider still retains their operations. Otherwise repeated calls grow it.
	if _, err := s.Render(t.Context(), projection()); err != ErrUnavailable || p.calls.Load() != 8 {
		t.Fatal("timed-out provider work escaped the capacity bound")
	}
	s.mu.Lock()
	started := !s.lastStart.IsZero()
	s.mu.Unlock()
	if started {
		t.Fatal("expired provider evaluation started a child")
	}
}

// retainEightEvaluations saturates actual SDK waits for either public operation.
func retainEightEvaluations(t *testing.T, s *Supervisor, call func() error) (*blockingProvider, <-chan error) {
	t.Helper()
	p := blocking(t, s, 0, false)
	finished := make(chan error, 8)
	for range 8 {
		go func() { finished <- call() }()
	}
	for range 8 {
		awaitSignal(t, p.reached)
	}
	return p, finished
}

func TestDuplicateReplyInvalidatesACompletedButNotReleasedRender(t *testing.T) {
	s := fixture(t, "duplicate", true)
	p := blocking(t, s, 2, false)
	defer close(p.release)
	finished := make(chan result, 1)
	go func() { body, err := s.Render(t.Context(), projection()); finished <- result{body: body, err: err} }()
	awaitSignal(t, p.reached)
	s.mu.Lock()
	g := s.g
	s.mu.Unlock()
	if g == nil {
		t.Fatal("no real generation")
	}
	awaitSignal(t, g.done)
	// The API must finish at its deadline even if the post-render SDK is stuck.
	value := awaitSignal(t, finished)
	if value.body != "" || value.err != ErrUnavailable {
		t.Fatal("duplicate response escaped")
	}
}

func TestPostRenderGateRevocationCannotSelectFallback(t *testing.T) {
	s := fixture(t, "normal", true)
	p := blocking(t, s, -1, true)
	defer close(p.release)
	body, err := s.Render(t.Context(), projection())
	if body != "" || err != ErrUnavailable {
		t.Fatal("post-render revocation returned a body or selected fallback", err)
	}
	s.Close()
}

func TestShutdownCancelsPendingWorkAndReapsOnlyItsChild(t *testing.T) {
	s := fixture(t, "hold", true)
	finished := make(chan error, 1)
	go func() { _, err := s.Render(t.Context(), projection()); finished <- err }()
	// Inspect the owned generation under its lock, not a system-wide process scan.
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	var g *generation
	for g == nil {
		select {
		case <-deadline.C:
			t.Fatal("child not started")
		case <-tick.C:
			s.mu.Lock()
			g = s.g
			s.mu.Unlock()
		}
	}
	s.Close()
	if err := awaitSignal(t, finished); err != ErrUnavailable {
		t.Fatal("shutdown admitted a pending body")
	}
	awaitSignal(t, g.reaped)
	s.Close() // idempotent
}

func TestRestartIsThrottledAndSuccessfulCallsReuseOneProcess(t *testing.T) {
	s := fixture(t, "normal", true)
	first, err := s.Render(t.Context(), projection())
	if err != nil || first == "" {
		t.Fatal("initial render failed", err)
	}
	s.mu.Lock()
	g := s.g
	s.mu.Unlock()
	for range 3 {
		body, err := s.Render(t.Context(), projection())
		if body != first || err != nil {
			t.Fatal("healthy generation not reused")
		}
		s.mu.Lock()
		same := s.g == g
		s.mu.Unlock()
		if !same {
			t.Fatal("render spawned another child")
		}
	}
	s.fail(g)
	awaitSignal(t, g.reaped)
	if body, err := s.Render(t.Context(), projection()); body != "" || err != ErrUnavailable {
		t.Fatal("restart throttle bypassed")
	}
	<-time.After(time.Second)
	if body, err := s.Render(t.Context(), projection()); body != first || err != nil {
		t.Fatal("eligible restart failed", err)
	}
}

func TestInvalidProjectionAndRelativePathsCannotStartAChild(t *testing.T) {
	s := fixture(t, "normal", true)
	for _, edit := range []func(*renderwire.Contact){
		func(c *renderwire.Contact) { c.Revision = "9007199254740993.0" },
		func(c *renderwire.Contact) { c.CSRF = "" },
		func(c *renderwire.Contact) { c.Name = strings.Repeat("x", 121) },
	} {
		c := projection()
		edit(&c)
		if body, err := s.Render(t.Context(), c); body != "" || err != ErrUnavailable {
			t.Fatal("invalid input admitted")
		}
	}
	if _, err := New("node", s.artifact, nil); err != ErrUnavailable {
		t.Fatal("PATH-selected executable admitted")
	}
	if _, err := New(s.program, ".", nil); err != ErrUnavailable {
		t.Fatal("relative artifact admitted")
	}
	s.mu.Lock()
	started := !s.lastStart.IsZero()
	s.mu.Unlock()
	if started {
		t.Fatal("invalid input started a child")
	}
}

type eventProvider struct {
	openfeature.FeatureProvider
	events chan openfeature.Event
	once   sync.Once
}

func (*eventProvider) Init(openfeature.EvaluationContext) error { return nil }
func (p *eventProvider) Shutdown()                              { p.once.Do(func() { close(p.events) }) }
func (p *eventProvider) EventChannel() <-chan openfeature.Event { return p.events }

func TestSDKErrorAndStaleStatesNeverStartAChildEvenWithTrueValues(t *testing.T) {
	for _, event := range []openfeature.EventType{openfeature.ProviderError, openfeature.ProviderStale} {
		t.Run(string(event), func(t *testing.T) {
			s := fixture(t, "normal", true)
			p := &eventProvider{
				FeatureProvider: memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
					"portal-contact-renderer": {State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}},
				}),
				events: make(chan openfeature.Event, 1),
			}
			s.flags = providerAPI(t, p).NewClient()
			settled := make(chan struct{})
			handler := func(openfeature.EventDetails) { close(settled) }
			s.flags.AddHandler(event, &handler)
			p.events <- openfeature.Event{EventType: event}
			awaitSignal(t, settled)
			if body, err := s.Render(t.Context(), projection()); body != "" || err != ErrDisabled {
				t.Fatal("unhealthy SDK admitted rendering", err)
			}
			if err := s.Check(t.Context()); !errors.Is(err, ErrDisabled) {
				t.Fatal("unhealthy SDK passed the release checkpoint", err)
			}
			s.mu.Lock()
			started := !s.lastStart.IsZero()
			s.mu.Unlock()
			if started {
				t.Fatal("unhealthy SDK started a child")
			}
		})
	}
}

func TestMissingSDKAndExpiredContextStayClosed(t *testing.T) {
	s := fixture(t, "normal", true)
	s.flags = nil
	if body, err := s.Render(t.Context(), projection()); body != "" || err != ErrDisabled {
		t.Fatal("nil SDK admitted rendering")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if body, err := s.Render(ctx, projection()); body != "" || err != ErrUnavailable {
		t.Fatal("cancelled caller admitted rendering")
	}
	s.mu.Lock()
	started := !s.lastStart.IsZero()
	s.mu.Unlock()
	if started {
		t.Fatal("missing SDK or expired caller started a child")
	}
}

func TestChildDoesNotInheritHostSecretsOrRuntimeOptions(t *testing.T) {
	t.Setenv("FICTIONAL_PRIVATE_SECRET", "must-not-reach-child")
	t.Setenv("NODE_OPTIONS", "--require=fictional-untrusted-module")
	s := fixture(t, "environment", true)
	body, err := s.Render(t.Context(), projection())
	if body != "<main>Example Client</main>" || err != nil {
		t.Fatal("child inherited host settings", err)
	}
}

func TestOneCancellationInvalidatesEveryPendingResultInItsGeneration(t *testing.T) {
	s := fixture(t, "hold", true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 8)
	go func() { _, err := s.Render(ctx, projection()); finished <- err }()
	for range 7 {
		go func() { _, err := s.Render(t.Context(), projection()); finished <- err }()
	}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	registered := false
	for !registered {
		select {
		case <-timer.C:
			t.Fatal("eight concurrent requests were not registered")
		case <-tick.C:
			s.mu.Lock()
			registered = s.g != nil && len(s.g.pending) == 8
			s.mu.Unlock()
		}
	}
	start := time.Now()
	cancel()
	for range 8 {
		if err := awaitSignal(t, finished); err != ErrUnavailable {
			t.Fatal("generation retained an invalid body")
		}
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatal("other pending calls waited for their own deadlines")
	}
	s.Close()
}

func TestExpiredOldProviderWaitCannotKillAReplacementGeneration(t *testing.T) {
	s := fixture(t, "normal", true)
	p := blocking(t, s, 2, false)
	var release sync.Once
	defer release.Do(func() { close(p.release) })
	first := make(chan error, 1)
	go func() { _, err := s.Render(t.Context(), projection()); first <- err }()
	awaitSignal(t, p.reached)
	if err := awaitSignal(t, first); err != ErrUnavailable {
		t.Fatal("old provider wait did not expire")
	}
	<-time.After(time.Second) // production restart throttle, not a modified test limit
	if body, err := s.Render(t.Context(), projection()); body == "" || err != nil {
		t.Fatal("replacement child failed", err)
	}
	s.mu.Lock()
	replacement := s.g
	s.mu.Unlock()
	release.Do(func() { close(p.release) })
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for len(s.slots) != 0 {
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("old provider operation did not finish")
		}
	}
	s.mu.Lock()
	healthy := s.g == replacement && !replacement.failed
	s.mu.Unlock()
	if !healthy {
		t.Fatal("expired old operation invalidated a replacement generation")
	}
	if body, err := s.Render(t.Context(), projection()); body == "" || err != nil {
		t.Fatal("late old provider killed a healthy child", err)
	}
}
