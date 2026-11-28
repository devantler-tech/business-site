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

// TestOperatorAnonymousSessionsCannotBlockRemoval exercises real cookie-free
// sign-in requests, rather than assuming every stored session is authenticated.
func TestOperatorAnonymousSessionsCannotBlockRemoval(t *testing.T) {
	b := managementMemberFixture(t, func(context.Context, string, bool) error {
		t.Error("removing membership contacted the sign-in provider")
		return ErrDenied
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
	status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", operatorMembershipForm(b, customerSubject, "client-a"), b.site.URL)
	if status != http.StatusOK || b.memberships(customerSubject, "client") != 0 {
		t.Fatal("unrelated anonymous sessions prevented membership removal", status)
	}
}

// TestOperatorMemberTransitionsSerializeAcrossReplicas catches membership
// removal racing ahead of legacy account recovery on another replica.
func TestOperatorMemberTransitionsSerializeAcrossReplicas(t *testing.T) {
	reactivating, release := make(chan struct{}), make(chan struct{})
	resume := onceRelease(release)
	t.Cleanup(resume)
	var providerEnabled atomic.Bool
	b := managementMemberFixture(t, func(ctx context.Context, _ string, active bool) error {
		if !active {
			t.Error("workspace removal blocked the customer’s sign-in")
			return ErrDenied
		}
		close(reactivating)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		providerEnabled.Store(true)
		return nil
	})
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE subject=$1`, customerSubject); err != nil {
		t.Fatal(err)
	}
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
	go call("reactivate")
	select {
	case <-reactivating:
	case <-time.After(5 * time.Second):
		t.Fatal("legacy account recovery never reached the provider")
	}
	b.mu.Lock()
	b.handler = handler
	b.mu.Unlock()
	go call("remove")
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
		case <-done:
			t.Fatal("another replica removed membership before legacy recovery finished")
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
	if b.memberships(customerSubject, "client") != 0 || !providerEnabled.Load() {
		t.Fatal("completed transitions did not leave an enabled account without workspace access")
	}
}
