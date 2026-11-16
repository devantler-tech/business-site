package identity

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TestOperatorReactivationRetryPreservesMembership catches a retry silently
// replacing the membership incarnation on which existing work approvals depend.
func TestOperatorReactivationRetryPreservesMembership(t *testing.T) {
	b := managementMemberFixture(t, func(context.Context, string, bool) error { return nil })
	var before, after string
	if err := b.pool.QueryRow(t.Context(), `SELECT incarnation::text FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&before); err != nil {
		t.Fatal(err)
	}
	form := operatorMembershipForm(b, customerSubject, "client-a")
	for range 2 {
		if status, _, _ := b.request("POST", operatorAPIRoot+"/members/reactivate", form, b.site.URL); status != http.StatusOK {
			t.Fatal("active-member retry failed", status)
		}
	}
	if err := b.pool.QueryRow(t.Context(), `SELECT incarnation::text FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("reactivation retry invalidated the existing membership and its approvals")
	}
}

// TestOperatorAnonymousSessionsCannotBlockDeactivation exercises real cookie-free
// sign-in requests, rather than assuming every stored session is authenticated.
func TestOperatorAnonymousSessionsCannotBlockDeactivation(t *testing.T) {
	var disabled atomic.Bool
	b := managementMemberFixture(t, func(_ context.Context, _ string, active bool) error {
		disabled.Store(!active)
		return nil
	})
	operatorJar := b.client.Jar
	b.client.Jar = nil
	for range 1001 {
		if status, _, _ := b.request("GET", "/portal/sign-in", nil, ""); status != http.StatusSeeOther {
			t.Fatal("anonymous sign-in did not create its ordinary attempt", status)
		}
	}
	b.client.Jar = operatorJar
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_sessions WHERE expiry>now()`).Scan(&count); err != nil || count < 1001 {
		t.Fatal("anonymous session pressure was not exercised", count, err)
	}
	status, _, _ := b.request("POST", operatorAPIRoot+"/members/deactivate", operatorMembershipForm(b, customerSubject, "client-a"), b.site.URL)
	revoked := b.customerRevoked()
	if status != http.StatusOK || !revoked || !disabled.Load() {
		t.Fatal("unrelated anonymous sessions prevented customer revocation", status, revoked, disabled.Load())
	}
}

// TestOperatorMemberTransitionsSerializeAcrossReplicas catches a provider update
// running after a later, independently constructed replica has restored access.
func TestOperatorMemberTransitionsSerializeAcrossReplicas(t *testing.T) {
	deactivating, release, reactivating := make(chan struct{}), make(chan struct{}), make(chan struct{})
	resume := onceRelease(release)
	t.Cleanup(resume)
	var providerDisabled atomic.Bool
	b := managementMemberFixture(t, func(ctx context.Context, _ string, active bool) error {
		if active {
			close(reactivating)
		} else {
			close(deactivating)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		providerDisabled.Store(!active)
		return nil
	})
	replica := New(b.pool, b.svc.broker, b.svc.flags)
	if err := replica.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	if err := replica.LinkCustomerDirectory(b.svc.customers); err != nil {
		t.Fatal(err)
	}
	handler := browserHandler(t, replica, b.site.URL+"/portal/callback")
	form := operatorMembershipForm(b, customerSubject, "client-a")
	results := make(chan int, 2)
	done := make(chan struct{}, 2)
	call := func(action string) {
		defer func() { done <- struct{}{} }()
		status, _, _ := b.request("POST", operatorAPIRoot+"/members/"+action, form, b.site.URL)
		results <- status
	}
	go call("deactivate")
	select {
	case <-deactivating:
	case <-time.After(5 * time.Second):
		t.Fatal("deactivation never reached the provider")
	}
	b.mu.Lock()
	b.handler = handler
	b.mu.Unlock()
	go call("reactivate")
	t.Cleanup(func() {
		resume()
		for range 2 {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("member transition did not finish")
			}
		}
	})
	observer := migrationDB(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-reactivating:
			t.Fatal("another replica reactivated while provider deactivation was unfinished")
		default:
		}
		var waiting bool
		if err := observer.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second replica never waited on the distributed identity lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	resume()
	for range 2 {
		if status := <-results; status != http.StatusOK {
			t.Fatal("serialized member action failed", status)
		}
	}
	var revoked bool
	if err := observer.QueryRow(t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil || revoked || providerDisabled.Load() {
		t.Fatal("completed transitions left provider and portal access inconsistent", revoked, providerDisabled.Load(), err)
	}
}
