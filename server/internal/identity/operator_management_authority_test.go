package identity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestOperatorWorkspaceCommitRechecksAuthority catches a write that commits
// after logout or operator revocation while its database work is in flight.
func TestOperatorWorkspaceCommitRechecksAuthority(t *testing.T) {
	for _, change := range []string{"unchanged", "logout", "revocation"} {
		t.Run(change, func(t *testing.T) {
			b := newOperatorBrowserFixture(t)
			form := url.Values{"workspace": {"mid-flight"}, "csrf_token": {b.csrf("/portal/")}}
			pause := installCompletedRead(t, b, "INSERT INTO portal_workspaces")
			resume := onceRelease(pause.release)
			result := make(chan int, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces", form, b.site.URL)
				result <- status
			}()
			t.Cleanup(func() {
				resume()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("management writer did not finish")
				}
			})
			select {
			case <-pause.blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("workspace write missed its completion checkpoint")
			}
			var err error
			switch change {
			case "logout":
				_, err = b.pool.Exec(t.Context(), `DELETE FROM portal_sessions`)
			case "revocation":
				_, err = b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE subject=$1`, operatorSubject)
			}
			if err != nil {
				t.Fatal(err)
			}
			resume()
			var status int
			select {
			case status = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("workspace write did not return")
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_workspaces WHERE id='mid-flight'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if change == "unchanged" {
				if status != http.StatusOK || count != 1 {
					t.Fatal("current operator lost its write", status, count)
				}
			} else if status != http.StatusServiceUnavailable || count != 0 {
				t.Fatal("closed operator authority committed a workspace", status, count)
			}
		})
	}
}

// TestOperatorDeactivationRequiresAudit catches a real member mutation or
// provider effect taking place despite unavailable audit storage.
func TestOperatorDeactivationRequiresAudit(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,'client-a','client')`, b.f.issuer, customerSubject); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{change: func(context.Context, string, bool) error {
		calls.Add(1)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"workspace": {"client-a"}, "subject": {customerSubject}, "csrf_token": {b.csrf("/portal/")}}
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `REVOKE INSERT ON portal_operator_audit FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := migrator.Exec(ctx, `GRANT INSERT ON portal_operator_audit TO portal_runtime`); err != nil {
			t.Error(err)
		}
	})
	status, _, _ := b.request("POST", operatorAPIRoot+"/members/deactivate", form, b.site.URL)
	var revoked bool
	if err := b.pool.QueryRow(t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusServiceUnavailable || revoked || calls.Load() != 0 {
		t.Fatal("unrecordable deactivation changed customer access", status, revoked, calls.Load())
	}
	if _, err := migrator.Exec(t.Context(), `GRANT INSERT ON portal_operator_audit TO portal_runtime`); err != nil {
		t.Fatal(err)
	}
	status, _, _ = b.request("POST", operatorAPIRoot+"/members/deactivate", form, b.site.URL)
	if err := b.pool.QueryRow(t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil || !revoked || status != http.StatusOK || calls.Load() != 1 {
		t.Fatal("audited deactivation positive control failed", status, revoked, calls.Load(), err)
	}
}

// TestOperatorManagementNativeReturnJourney catches native forms displaying raw
// JSON or navigating away from the fixed, localized customer inventory.
func TestOperatorManagementNativeReturnJourney(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	form := url.Values{"workspace": {"native-workspace"}, "csrf_token": {b.csrf("/portal/")}}
	req, err := http.NewRequest("POST", b.site.URL+operatorAPIRoot+"/workspaces?lang=da", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Origin", b.site.URL)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, res.Body)
	closeErr := res.Body.Close()
	location := res.Header.Get("Location")
	if readErr != nil || closeErr != nil || res.StatusCode != http.StatusSeeOther || location != "/portal/operator/customers?lang=da" {
		t.Fatal("native form did not return to the customer inventory", res.StatusCode, location, readErr, closeErr)
	}
	status, body, _ := b.request("GET", location, nil, "")
	if status != http.StatusOK || !strings.Contains(body, "native-workspace") || !strings.Contains(body, "Kunder og arbejdsområder") {
		t.Fatal("native return lost its persisted workspace or language", status)
	}
}
