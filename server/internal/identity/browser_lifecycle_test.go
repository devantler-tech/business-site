package identity

import (
	"bytes"
	"context"
	"fmt"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

// constructionLabel marks goroutines started inside one labelled region; Go
// copies profiler labels to every goroutine a labelled goroutine starts.
const constructionLabel = "browser-construction"

// labelledGoroutines counts live goroutines that inherited value as their
// construction label, read from the runtime's own goroutine profile. It ignores
// every unlabelled goroutine, so unrelated fixture and runtime workers cannot
// affect the result.
func labelledGoroutines(t testing.TB, value string) int {
	t.Helper()
	var profile bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&profile, 1); err != nil {
		t.Fatal(err)
	}
	// Each record starts with "<count> @ <pcs>"; its optional labels line follows.
	// Records are sorted by count, so a match may be the first one after the header.
	want := "# labels: " + fmt.Sprintf("{%q:%q}", constructionLabel, value)
	lines := strings.Split(profile.String(), "\n")
	if !strings.HasPrefix(lines[0], "goroutine profile: total ") {
		t.Fatalf("unrecognised goroutine profile header %q", lines[0])
	}
	total, current, records := 0, 0, 0
	for _, line := range lines[1:] {
		if count, _, ok := strings.Cut(line, " @ "); ok && !strings.HasPrefix(line, "#") {
			n, err := strconv.Atoi(count)
			if err != nil || n < 1 {
				t.Fatalf("unreadable goroutine profile record %q", line)
			}
			current = n
			records++
			continue
		}
		if line == want {
			total += current
		}
	}
	if records == 0 {
		t.Fatal("goroutine profile contained no records")
	}
	return total
}

// awaitLabelled polls until the labelled goroutine count satisfies done or the
// bounded deadline passes, and returns the last observed count.
func awaitLabelled(t testing.TB, value string, done func(int) bool) (int, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := labelledGoroutines(t, value)
		if done(n) {
			return n, true
		}
		if time.Now().After(deadline) {
			return n, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDefaultSessionStoreStopsWithoutRace exercises the public API Browser
// calls immediately after scs.New. Under the race detector, scs v2.9.0 reports a
// data race here because its worker creates the stop channel itself.
func TestDefaultSessionStoreStopsWithoutRace(t *testing.T) {
	for range 256 {
		memstore.New().StopCleanup()
	}
}

// TestBrowserLeavesNoSessionWorker constructs the actual TLS/PostgreSQL browser
// handler repeatedly and requires every goroutine started during construction
// to end. scs.New starts a cleanup worker for its default in-memory store, which
// Browser replaces with the durable store; that worker must not outlive it. The
// positive control proves the observer detects exactly that abandoned worker.
func TestBrowserLeavesNoSessionWorker(t *testing.T) {
	f := fixtureBroker(t)
	svc := New(fixtureDB(t), f.broker, flagAPI(t, true).NewClient())
	callback := "https://app.example.invalid/portal/callback"
	// Warm the database pool and broker client outside any labelled region.
	browserHandler(t, svc, callback)

	control := t.Name() + "/control"
	var abandoned *scs.SessionManager
	pprof.Do(context.Background(), pprof.Labels(constructionLabel, control), func(context.Context) {
		abandoned = scs.New()
	})
	if n, ok := awaitLabelled(t, control, func(n int) bool { return n > 0 }); !ok {
		t.Fatalf("observer missed the default store's cleanup worker: %d labelled goroutines", n)
	}
	stopper, ok := abandoned.Store.(interface{ StopCleanup() })
	if !ok {
		t.Fatalf("default session store %T cannot stop its cleanup worker", abandoned.Store)
	}
	stopper.StopCleanup()
	if n, ok := awaitLabelled(t, control, func(n int) bool { return n == 0 }); !ok {
		t.Fatalf("control cleanup worker survived StopCleanup: %d labelled goroutines", n)
	}

	constructed := t.Name() + "/browser"
	for range 8 {
		pprof.Do(context.Background(), pprof.Labels(constructionLabel, constructed), func(context.Context) {
			if h, err := svc.Browser(callback, "synthetic-client-secret"); err != nil || h == nil {
				t.Fatalf("browser construction failed: %v", err)
			}
		})
	}
	if n, ok := awaitLabelled(t, constructed, func(n int) bool { return n == 0 }); !ok {
		t.Fatalf("%d goroutines started by browser construction are still running", n)
	}
}
