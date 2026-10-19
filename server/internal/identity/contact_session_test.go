package identity

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
)

// TestContactResponseRequiresOriginalDurableSession changes the real session
// store after the contact row is read. The request's SCS cache is not authority.
func TestContactResponseRequiresOriginalDurableSession(t *testing.T) {
	for _, change := range []string{"unchanged", "deleted", "expired", "replacement identity", "extended deadline"} {
		t.Run(change, func(t *testing.T) {
			b := newBrowserFixture(t)
			b.finish(b.start(b.invite("client-a", "client-a", "client")))
			status, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "0", "Private Fictional Contact", "a@example.invalid", ""), b.site.URL)
			if status != http.StatusSeeOther {
				t.Fatal("fixture contact was not saved")
			}
			pause := installCompletedRead(t, b, "coalesce(c.name")
			b.mu.Lock()
			b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
			b.mu.Unlock()
			release := onceRelease(pause.release)
			type response struct {
				status int
				body   string
			}
			result := make(chan response, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s, body, _ := b.request("GET", "/portal/contact", nil, "")
				result <- response{s, body}
			}()
			defer func() {
				release()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("contact reader did not finish")
				}
			}()
			select {
			case <-pause.blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("actual contact read did not reach checkpoint")
			}
			query := ""
			switch change {
			case "deleted":
				query = "DELETE FROM portal_sessions"
			case "expired":
				query = "UPDATE portal_sessions SET expiry=now()-interval '1 minute'"
			}
			if query != "" {
				if _, err := b.pool.Exec(t.Context(), query); err != nil {
					t.Fatal("session control failed", err)
				}
			}
			if change == "replacement identity" || change == "extended deadline" {
				var data []byte
				if err := b.pool.QueryRow(t.Context(), `SELECT data FROM portal_sessions`).Scan(&data); err != nil {
					t.Fatal("session fixture read failed", err)
				}
				codec := scs.New().Codec
				deadline, values, err := codec.Decode(data)
				if err != nil {
					t.Fatal("maintained session decode failed", err)
				}
				if change == "replacement identity" {
					// A valid replacement identity must not authorize the old buffer.
					_ = enroll(t, b.svc, b.f, "client-b", "client-b", "client")
					values["subject"] = "client-b"
				} else {
					deadline = deadline.Add(time.Minute)
				}
				data, err = codec.Encode(deadline, values)
				if err != nil {
					t.Fatal("maintained session encode failed", err)
				}
				if _, err = b.pool.Exec(t.Context(), `UPDATE portal_sessions SET data=$1`, data); err != nil {
					t.Fatal("session replacement failed", err)
				}
			}
			release()
			select {
			case got := <-result:
				if change == "unchanged" {
					if got.status != 200 || !strings.Contains(got.body, "Private Fictional Contact") {
						t.Fatal("unchanged session did not retain contact access")
					}
				} else if got.status != 503 || strings.Contains(got.body, "Private Fictional Contact") || strings.Contains(got.body, "a@example.invalid") {
					t.Errorf("%s session released stale contact data: status=%d", change, got.status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("bounded contact response missing")
			}
			if change == "deleted" {
				var count int
				if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_sessions`).Scan(&count); err != nil || count != 0 {
					t.Fatal("read resurrected a deleted session", err)
				}
			}
		})
	}
}
