package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// expiryProbe keeps each real reader's checkpoint, authority and result separate.
type expiryProbe struct {
	name    string
	arm     func(time.Time)
	blocked <-chan struct{}
	release func()
	read    func(context.Context) error
}

// onceRelease makes ordinary completion and failure cleanup equally safe.
func onceRelease(channel chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(channel) }) }
}

// runExpiryCohort starts the clock only after fixture preparation. Every reader
// must reach its own real checkpoint before expiry; only then is the shared
// wall-clock wait allowed. Cancellation/release/join precede all fixture cleanup.
func runExpiryCohort(t *testing.T, probes []expiryProbe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	type pendingRead struct {
		result chan error
		done   chan struct{}
	}
	pending := make([]pendingRead, len(probes))
	defer func() {
		cancel()
		for _, probe := range probes {
			probe.release()
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for _, read := range pending {
			if read.done == nil {
				continue
			}
			select {
			case <-read.done:
			case <-timer.C:
				t.Error("expiry reader did not finish after cancellation")
				return
			}
		}
	}()

	// JWT seconds are intentionally real and rounded exactly as the broker uses.
	expires := time.Now().Add(5 * time.Second).Truncate(time.Second)
	for _, probe := range probes {
		probe.arm(expires)
	}
	for index, probe := range probes {
		read := pendingRead{result: make(chan error, 1), done: make(chan struct{})}
		pending[index] = read
		go func() {
			defer close(read.done)
			read.result <- probe.read(ctx)
		}()
	}
	for index, probe := range probes {
		select {
		case <-probe.blocked:
			if !time.Now().Before(expires) {
				t.Fatalf("%s: authority expired before its checkpoint", probe.name)
			}
		case err := <-pending[index].result:
			t.Fatalf("%s: reader finished before its checkpoint: %v", probe.name, err)
		case <-ctx.Done():
			t.Fatalf("%s: reader did not reach its checkpoint", probe.name)
		}
	}
	waitForFixtureExpiry(ctx, t, expires)
	for _, probe := range probes {
		probe.release()
	}
	for index, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			select {
			case err := <-pending[index].result:
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("expired authority did not deny private data: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("expired reader did not return a bounded result")
			}
		})
	}
}

// TestPrivateReadersRetainRealExpiry moves the same eight direct and ten browser
// expiry selectors out of serial fixture loops. Other authority-change controls
// remain in their original tests. No constructor runs in a worker or resets a
// database after another reader has been prepared.
func TestPrivateReadersRetainRealExpiry(t *testing.T) {
	base := sharingFixture(t)
	saveVisibility(t, base, visibilityForm(t, base, "0", "1", "1", "share"))
	const emptyRequestID = "BBBBBBBBBBBBBBBBBBBBBBBBBB"
	seedTriageRequest(t, base, emptyRequestID, "Fictional empty proposal", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	operator := enroll(t, base.svc, base.f, "unshared-proposal-operator", "operator-workspace", "operator")
	if err := base.svc.persistProposal(t.Context(), operator, emptyRequestID, projectProposal{Key: secret(), Scope: "Fictional unshared scope", Deliverables: "Fictional draft", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One iteration", MonthlyTerms: "Hosting"}); err != nil {
		t.Fatal("unshared expiry proposal preparation failed", err)
	}
	var probes []expiryProbe
	for _, selector := range []string{"shared", "empty", "visibility", "contact-empty", "contact-saved", "membership", "request-list", "request-receipt"} {
		probes = append(probes, directExpiryProbe(t, base, selector, emptyRequestID))
	}
	for _, outcome := range []string{"false", "error"} {
		probes = append(probes, browserExpiryProbe(t, base, "client", "portal-proposal-acceptance", outcome))
		for _, key := range []string{"portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices"} {
			probes = append(probes, browserExpiryProbe(t, base, "operator", key, outcome))
		}
	}
	if len(probes) != 18 {
		t.Fatal("expiry cohort lost an original selector")
	}
	runExpiryCohort(t, probes)
}

// directExpiryProbe prepares one native service reader without starting expiry.
func directExpiryProbe(t *testing.T, base *browserFixture, selector, emptyRequestID string) expiryProbe {
	t.Helper()
	b := newBrowserFixtureOnPool(t, base.pool)
	role, client := "client", "private-client-key"
	if selector == "visibility" {
		role = "operator"
	}
	if strings.HasPrefix(selector, "contact-") || strings.HasPrefix(selector, "request-") {
		client = "expiry-" + selector
	}
	p := enroll(t, b.svc, b.f, "expiry-reader", client, role)
	key, blockAt, query := "portal-proposal-sharing", int32(2), ""
	var read func(context.Context, Principal) (bool, error)
	switch selector {
	case "shared", "empty":
		id := proposalRequestID
		if selector == "empty" {
			id = emptyRequestID
		}
		read = func(ctx context.Context, principal Principal) (bool, error) {
			value, err := b.svc.readSharedProposal(ctx, principal, id)
			return value != nil, err
		}
	case "visibility":
		blockAt = 3
		read = func(ctx context.Context, principal Principal) (bool, error) {
			value, err := b.svc.readVisibility(ctx, principal, proposalRequestID)
			return value.Shared != nil || value.Event != 0 || value.Active != 0, err
		}
	case "contact-empty", "contact-saved":
		if selector == "contact-saved" {
			if err := b.svc.saveContact(t.Context(), p, contactData{Name: "Fictional client", Email: "fictional@example.test", Company: "Synthetic company"}); err != nil {
				t.Fatal("expiry contact preparation failed", err)
			}
		}
		key, blockAt, query = "portal-identity", 0, "LEFT JOIN portal_contacts"
		read = func(ctx context.Context, principal Principal) (bool, error) {
			value, err := b.svc.readContact(ctx, principal)
			return value != (contactData{}), err
		}
	case "membership":
		key, blockAt, query = "portal-identity", 0, "SELECT client_id,role FROM portal_memberships"
		read = func(ctx context.Context, principal Principal) (bool, error) {
			clientID, currentRole, err := b.svc.membership(ctx, principal)
			return clientID != "" || currentRole != "", err
		}
	case "request-list", "request-receipt":
		requestFlag(t, b, true)
		request, err := b.svc.persistRequest(t.Context(), p, projectRequest{Key: secret(), Product: "website", Title: "Fictional private request", Brief: "Synthetic checkpoint control"})
		if err != nil {
			t.Fatal("expiry request preparation failed", err)
		}
		id := ""
		if selector == "request-receipt" {
			id = request.ID
		}
		key = "portal-requests"
		read = func(ctx context.Context, principal Principal) (bool, error) {
			rows, err := b.svc.readRequests(ctx, principal, id)
			return len(rows) != 0, err
		}
	default:
		t.Fatal("unknown expiry reader")
	}
	provider := installReadProvider(t, b, key, blockAt)
	blocked, release := provider.blocked, provider.release
	if query != "" {
		tracer := installCompletedRead(t, b, query)
		blocked, release = tracer.blocked, tracer.release
	}
	if err := b.svc.pool.Ping(t.Context()); err != nil {
		t.Fatal("expiry reader pool warmup failed", err)
	}
	return expiryProbe{
		name:    "direct/" + selector,
		arm:     func(expires time.Time) { p.expires = expires },
		blocked: blocked,
		release: onceRelease(release),
		read: func(ctx context.Context) error {
			data, err := read(ctx, p)
			if data {
				return errors.New("expired reader exposed private data")
			}
			return err
		},
	}
}

// browserExpiryProbe completes invitation/PKCE setup before choosing signed-token
// expiry. Each actual TLS/session handler waits in its own closed display SDK.
func browserExpiryProbe(t *testing.T, base *browserFixture, role, key, outcome string) expiryProbe {
	t.Helper()
	b := newBrowserFixtureOnPool(t, base.pool)
	client := "private-client-key"
	path := "/portal/requests/" + proposalRequestID
	if role == "operator" {
		client = "operator-workspace"
		path = "/portal/operator/requests/" + proposalRequestID
		if outcome == "error" {
			path += "?lang=da"
		}
	}
	callback := beginTriageSignIn(t, b, "expiry-"+role, client, role)
	provider, release := closedDisplayCheckpoint(t, b, key, outcome)
	b.client.Timeout = 10 * time.Second
	return expiryProbe{
		name: "browser/" + role + "/" + key + "/" + outcome,
		arm: func(expires time.Time) {
			b.configure(map[string]any{"exp": expires.Unix()}, false)
			b.finish(callback)
		},
		blocked: provider.blocked,
		release: release,
		read: func(ctx context.Context) error {
			response, err := expiryDisplayGET(ctx, b, path)
			if err != nil {
				return err
			}
			visible := strings.Contains(response.body, "Fictional proposal request") || strings.Contains(response.body, "hostile-brief") || strings.Contains(response.body, "A fictional first iteration")
			if response.status == http.StatusOK || visible {
				return fmt.Errorf("expired display reader exposed a private result (status %d)", response.status)
			}
			return ErrDenied
		},
	}
}

// expiryDisplayGET propagates errors to the parent instead of calling Fatal in
// a worker. The request's context and the client bound both cover TLS and reads.
func expiryDisplayGET(ctx context.Context, b *browserFixture, path string) (displayResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.site.URL+path, nil)
	if err != nil {
		return displayResponse{}, err
	}
	res, err := b.client.Do(req)
	if err != nil {
		return displayResponse{}, err
	}
	body, readErr := io.ReadAll(res.Body)
	closeErr := res.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return displayResponse{}, err
	}
	return displayResponse{status: res.StatusCode, body: string(body)}, nil
}
