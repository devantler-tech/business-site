package identity

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

type eligibilityReadResult struct {
	page workEligibilityPage
	err  error
}

type eligibilityCheckpoint struct {
	ctx      context.Context
	cancel   context.CancelFunc
	provider *finalRequestProvider
	release  func()
	result   <-chan eligibilityReadResult
}

// newEligibilityCheckpoint owns one real delayed read and always joins it before
// fixture cleanup. Tests change only the selected SDK result or real authority.
func newEligibilityCheckpoint(t *testing.T, b *browserFixture, p Principal, at int32) *eligibilityCheckpoint {
	t.Helper()
	ctx, cancel := approvalWaitContext(t)
	provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(eligibilityFlags()), key: "portal-work-eligibility", blockAt: at, blocked: make(chan struct{}), release: make(chan struct{})}
	checkpoint := &eligibilityCheckpoint{ctx: ctx, cancel: cancel, provider: provider, release: installAcceptanceWait(t, b, provider, provider.release)}
	results, finished := make(chan eligibilityReadResult, 1), make(chan struct{})
	checkpoint.result = results
	go func() {
		defer close(finished)
		page, err := b.svc.readWorkEligibility(ctx, p, triageCursor{})
		results <- eligibilityReadResult{page, err}
	}()
	t.Cleanup(func() {
		checkpoint.release()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("delayed eligibility read did not finish before cleanup")
		}
	})
	select {
	case <-provider.blocked:
	case <-ctx.Done():
		t.Fatal("eligibility read did not reach its SDK checkpoint")
	}
	return checkpoint
}

func singleConnectionFixturePool(t *testing.T, b *browserFixture) *pgxpool.Pool {
	t.Helper()
	config := b.pool.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertEligibilityState(t *testing.T, b *browserFixture, op Principal, want workEligibilityState) {
	t.Helper()
	page, err := b.svc.readWorkEligibility(t.Context(), op, triageCursor{})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != want {
		t.Fatalf("eligibility state mismatch: want=%s page=%+v err=%v", want, page, err)
	}
}

// exerciseEligibilitySender proves both outcomes through the ordinary sender and
// the real eligibility reader, with the caller choosing its expected final state.
func exerciseEligibilitySender(t *testing.T, b *browserFixture, op Principal, m *fakeMailer, accepted workEligibilityState) {
	t.Helper()
	m.fail = true
	if err := b.svc.SendQueuedMail(t.Context()); err == nil || len(m.messages()) != 0 {
		t.Fatal("refused sender positive control failed", err)
	}
	assertEligibilityState(t, b, op, eligibilityConfirmationPending)
	m.fail = false
	if err := b.svc.SendQueuedMail(t.Context()); err != nil || len(m.messages()) != 1 {
		t.Fatal("accepted sender positive control failed", err)
	}
	assertEligibilityState(t, b, op, accepted)
}

func syncEligibilityInvoice(t *testing.T, b *browserFixture, cfg InvoiceBilling) {
	t.Helper()
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
}
