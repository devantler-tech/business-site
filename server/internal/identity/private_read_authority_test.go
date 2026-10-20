package identity

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// completedReadTracer pauses the real database client's completion callback.
// It neither replaces query results nor adds a production lifecycle hook.
type completedReadTracer struct {
	query            string
	started          atomic.Bool
	reads            atomic.Int32
	blockAt          int32
	blocked, release chan struct{}
}

// TraceQueryStart remembers only the selected synthetic query's context.
func (p *completedReadTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, p.query) {
		return context.WithValue(ctx, p, true)
	}
	return ctx
}

// TraceQueryEnd exposes a bounded checkpoint after real result materialization.
func (p *completedReadTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(p) != true || (p.blockAt > 0 && p.reads.Add(1) != p.blockAt) || !p.started.CompareAndSwap(false, true) {
		return
	}
	close(p.blocked)
	select {
	case <-p.release:
	case <-ctx.Done():
	}
}

// installCompletedRead owns a separate traced pool and its visible cleanup.
func installCompletedRead(t *testing.T, b *browserFixture, query string) *completedReadTracer {
	t.Helper()
	tracer := &completedReadTracer{query: query, blocked: make(chan struct{}), release: make(chan struct{})}
	config := b.pool.Config()
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(b.f.ctx, config)
	if err != nil {
		t.Fatal("fixture traced pool setup failed", err)
	}
	t.Cleanup(pool.Close)
	b.svc.pool = pool
	return tracer
}

// installReadProvider retains the actual flag SDK around a selected checkpoint.
func installReadProvider(t *testing.T, b *browserFixture, key string, blockAt int32) *finalRequestProvider {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{}
	for _, name := range []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-contact-verification"} {
		flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	p := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(flags), key: key, blockAt: blockAt, blocked: make(chan struct{}), release: make(chan struct{})}
	if err := b.flags.SetProviderAndWait(b.f.ctx, p); err != nil {
		t.Fatal("fixture flag setup failed", err)
	}
	return p
}

// checkChangedRead collects every worker before pools and synthetic brokers close.
func checkChangedRead(t *testing.T, b *browserFixture, p Principal, provider *finalRequestProvider, blocked, release chan struct{}, change string, operator bool, wantData bool, read func(context.Context, Principal) (bool, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if change == "expiry" {
		p.expires = time.Now().Add(2 * time.Second)
	}
	type result struct {
		data bool
		err  error
	}
	results := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		data, err := read(ctx, p)
		results <- result{data, err}
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("fixture private reader did not finish after cancellation")
		}
	}()
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("private reader did not reach selected completion checkpoint")
	}
	if change == "expiry" && !time.Now().Before(p.expires) {
		t.Fatal("fixture authority expired before its private-read checkpoint")
	}
	if change == "role" && operator {
		tag, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE issuer=$1 AND subject=$2`, p.issuer, p.subject)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("fixture operator role change failed", err)
		}
	} else {
		closeRequestReadAuthority(ctx, t, b, p, provider, change, cancel)
	}
	close(release)
	var got result
	select {
	case got = <-results:
	case <-time.After(time.Second):
		t.Fatal("private reader did not return a bounded result")
	}
	if change == "unchanged" || (operator && change == "rebind") {
		if got.err != nil || got.data != wantData {
			t.Fatal("valid authority lost its expected private result", got.err)
		}
		return
	}
	wantErr := ErrDenied
	if change == "storage" && provider.key == "portal-proposal-sharing" {
		wantErr = errTriageStorage
	}
	if !errors.Is(got.err, wantErr) || got.data {
		t.Fatal("closed authority returned private data or an unclassified result", got.err)
	}
}

// TestSharingReadFinalGateRechecksAuthority covers both content and empty previews
// plus the operator's deliberately cross-client visibility history.
func TestSharingReadFinalGateRechecksAuthority(t *testing.T) {
	for _, selector := range []string{"shared", "empty", "visibility"} {
		for _, change := range []string{"unchanged", "revocation", "role", "rebind", "flag", "cancel", "storage"} {
			t.Run(selector+"/"+change, func(t *testing.T) {
				b := sharingFixture(t)
				if selector != "empty" {
					saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
				}
				role, blockAt := "client", int32(2)
				if selector == "visibility" {
					role, blockAt = "operator", 3
				}
				p := enroll(t, b.svc, b.f, "checkpoint-reader", "private-client-key", role)
				provider := installReadProvider(t, b, "portal-proposal-sharing", blockAt)
				read := func(ctx context.Context, principal Principal) (bool, error) {
					if selector == "visibility" {
						v, err := b.svc.readVisibility(ctx, principal, proposalRequestID)
						return v.Shared != nil || v.Event != 0 || v.Active != 0, err
					}
					v, err := b.svc.readSharedProposal(ctx, principal, proposalRequestID)
					return v != nil, err
				}
				checkChangedRead(t, b, p, provider, provider.blocked, provider.release, change, role == "operator", selector != "empty", read)
			})
		}
	}
}

// TestContactReadRechecksMaterializedAuthority retains empty-profile success but
// never returns stale display fields when client access closes during the read.
func TestContactReadRechecksMaterializedAuthority(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, change := range []string{"unchanged", "revocation", "role", "rebind", "flag", "cancel", "storage"} {
			name := "empty/" + change
			if saved {
				name = "saved/" + change
			}
			t.Run(name, func(t *testing.T) {
				b := newBrowserFixture(t)
				p := enroll(t, b.svc, b.f, "checkpoint-contact", "checkpoint-owner", "client")
				if saved {
					if err := b.svc.saveContact(b.f.ctx, p, contactData{Name: "Fictional client", Email: "fictional@example.test", Company: "Synthetic company"}); err != nil {
						t.Fatal("fixture contact setup failed", err)
					}
				}
				provider := installReadProvider(t, b, "portal-identity", 0)
				tracer := installCompletedRead(t, b, "LEFT JOIN portal_contacts")
				read := func(ctx context.Context, principal Principal) (bool, error) {
					d, err := b.svc.readContact(ctx, principal)
					return d != (contactData{}), err
				}
				checkChangedRead(t, b, p, provider, tracer.blocked, tracer.release, change, false, saved, read)
			})
		}
	}
}

// TestMembershipCompletionRechecksExpiry proves that valid SQL results do not
// survive expiry or cancellation during the real client's completion callback.
func TestMembershipCompletionRechecksExpiry(t *testing.T) {
	for _, change := range []string{"unchanged", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b := newBrowserFixture(t)
			p := enroll(t, b.svc, b.f, "checkpoint-membership", "checkpoint-owner", "client")
			provider := installReadProvider(t, b, "portal-identity", 0)
			tracer := installCompletedRead(t, b, "SELECT client_id,role FROM portal_memberships")
			read := func(ctx context.Context, principal Principal) (bool, error) {
				client, role, err := b.svc.membership(ctx, principal)
				return client != "" || role != "", err
			}
			checkChangedRead(t, b, p, provider, tracer.blocked, tracer.release, change, false, true, read)
		})
	}
}

// TestPrivateReadExpiryAllowsCheckpointSetup retains real expiry after a bounded
// scheduling delay, rather than spending the fixture's authority before its read.
func TestPrivateReadExpiryAllowsCheckpointSetup(t *testing.T) {
	b := newBrowserFixture(t)
	p := enroll(t, b.svc, b.f, "delayed-checkpoint", "checkpoint-owner", "client")
	provider := installReadProvider(t, b, "portal-identity", 0)
	tracer := installCompletedRead(t, b, "SELECT client_id,role FROM portal_memberships")
	read := func(ctx context.Context, principal Principal) (bool, error) {
		timer := time.NewTimer(750 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		client, role, err := b.svc.membership(ctx, principal)
		return client != "" || role != "", err
	}
	checkChangedRead(t, b, p, provider, tracer.blocked, tracer.release, "expiry", false, true, read)
}
