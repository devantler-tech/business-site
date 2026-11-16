package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const eligibilityAPIPath = operatorAPIRoot + "/work-eligibility"

func TestWorkEligibilityAPIHumanOnlyAndSanitized(t *testing.T) {
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	setEligibilityFlags(t, b, eligibilityFlags())
	seedTriageRequest(t, b, proposalRequestID, "Private inquiry title", time.Now().UTC())
	other := "BBBBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES($1,'second-private-client',$1,'website','Another private title','Private customer brief')`, other); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("GET", eligibilityAPIPath, nil, ""); status != http.StatusUnauthorized {
		t.Fatal("anonymous eligibility read was admitted", status)
	}
	for _, client := range []string{"client-a", "client-b"} {
		b.finish(b.startInvited(client, client, "client"))
		if status, body, _ := b.request("GET", eligibilityAPIPath, nil, ""); status != http.StatusForbidden || strings.Contains(body, proposalRequestID) {
			t.Fatal("client read operator eligibility", status)
		}
		b.signOut()
	}
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	status, body, headers := b.request("GET", eligibilityAPIPath, nil, "")
	var page struct {
		Items []struct {
			Request    string    `json:"requestId"`
			State      string    `json:"state"`
			Submitted  time.Time `json:"submittedAt"`
			ObservedAt time.Time `json:"observedAt"`
		} `json:"items"`
		Next string `json:"nextCursor"`
	}
	if status != http.StatusOK || headers.Get("Cache-Control") != "no-store" || headers.Get("Content-Type") != "application/json" || json.Unmarshal([]byte(body), &page) != nil || len(page.Items) != 2 || page.Items[0].Request != proposalRequestID || page.Items[1].Request != other || page.Next != "" {
		t.Fatalf("operator eligibility response is missing or unbounded: %d %s", status, body)
	}
	for _, item := range page.Items {
		if item.State != "approval-required" || item.Submitted.IsZero() || item.ObservedAt.IsZero() {
			t.Fatal("inquiry became approved work or lost its original time")
		}
	}
	for _, private := range []string{"Private inquiry title", "hostile-brief", "Private customer brief", "private-client-key", "second-private-client", fixtureFundingReference} {
		if strings.Contains(body, private) {
			t.Fatal("eligibility output included unrelated private content")
		}
	}
	if got := b.auditRows(); len(got) != 3 || got[2] != "operator work.eligibility.list allowed" {
		t.Fatalf("eligibility read did not keep one audit row per call: %q", got)
	}
	subject := linkFixtureAgent(t, b, nil)
	token := fixtureAgentToken(t, b, subject)
	agentClient := &http.Client{Transport: b.site.Client().Transport}
	for _, inject := range []bool{false, true} {
		if inject {
			b.svc.agents.subjects[subject]["work.eligibility.list"] = true
		}
		if status, body, _ := agentRequest(t, b, agentClient, "GET", eligibilityAPIPath, token); status != http.StatusForbidden || strings.Contains(body, proposalRequestID) {
			t.Fatal("administrative agent gained work-reading authority", status)
		}
	}
}

func TestWorkEligibilityAPIClosedGateAndInvalidInput(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	seedTriageRequest(t, b, proposalRequestID, "Fictional hidden inquiry", time.Now().UTC())
	if status, body, _ := b.request("GET", eligibilityAPIPath, nil, ""); status != http.StatusServiceUnavailable || strings.Contains(body, proposalRequestID) {
		t.Fatal("default-off reader disclosed observations", status)
	}
	setEligibilityFlags(t, b, eligibilityFlags())
	for _, query := range []string{"?cursor=", "?cursor=forged", "?cursor=a&cursor=b", "?actorKind=operator", "?client_id=private-client-key", "?grants=work.eligibility.list", "?cursor=%zz"} {
		if status, body, _ := b.request("GET", eligibilityAPIPath+query, nil, ""); status != http.StatusBadRequest || strings.Contains(body, proposalRequestID) {
			t.Fatalf("invalid eligibility query was accepted: %q status=%d", query, status)
		}
	}
	restore := pauseOperatorAudit(t)
	if status, body, _ := b.request("GET", eligibilityAPIPath, nil, ""); status != http.StatusServiceUnavailable || strings.Contains(body, proposalRequestID) {
		t.Fatal("unrecorded observations were released", status)
	}
	restore()
}

func TestWorkEligibilityAPIEmptyAndUnavailableAreDistinct(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	status, body, _ := b.request("GET", eligibilityAPIPath, nil, "")
	if status != http.StatusOK || strings.TrimSpace(body) != `{"items":[]}` {
		t.Fatalf("genuine empty page was not explicit: %d %s", status, body)
	}
	owner := migrationDB(t)
	if _, err := owner.Exec(t.Context(), `REVOKE SELECT ON portal_requests FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `GRANT SELECT ON portal_requests TO portal_runtime`); err != nil {
			t.Error(err)
		}
	})
	status, body, _ = b.request("GET", eligibilityAPIPath, nil, "")
	if status != http.StatusServiceUnavailable || strings.TrimSpace(body) != `{"error":"unavailable"}` {
		t.Fatalf("unreadable page became empty or leaked storage details: %d %s", status, body)
	}
}

func TestWorkEligibilityAPIOriginalCursorAndSingleConnection(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	setEligibilityFlags(t, b, eligibilityFlags())
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	for i := 20; i >= 0; i-- {
		seedTriageRequest(t, b, string(rune('A'+i))+strings.Repeat("A", 25), "Fictional cursor inquiry", at)
	}
	config := b.pool.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := New(pool, b.f.broker, b.flags.NewClient())
	if err := svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.handler = browserHandler(t, svc, b.site.URL+"/portal/callback")
	b.mu.Unlock()
	status, body, _ := b.request("GET", eligibilityAPIPath, nil, "")
	var page operatorWorkEligibility
	if status != http.StatusOK || json.Unmarshal([]byte(body), &page) != nil || len(page.Items) != 20 || page.Next == "" {
		t.Fatalf("one-connection page did not finish: %d %s", status, body)
	}
	status, body, _ = b.request("GET", eligibilityAPIPath+"?cursor="+page.Next, nil, "")
	var next operatorWorkEligibility
	if status != http.StatusOK || json.Unmarshal([]byte(body), &next) != nil || len(next.Items) != 1 || next.Next != "" || next.Items[0].Request != "U"+strings.Repeat("A", 25) || !next.Items[0].Submitted.Equal(at) {
		t.Fatalf("canonical continuation changed original order: %d %s", status, body)
	}
}

func TestWorkEligibilityAPIRechecksOriginalDurableSession(t *testing.T) {
	for _, change := range []string{"unchanged", "deleted", "expired", "membership-restored"} {
		t.Run(change, func(t *testing.T) {
			b := newOperatorBrowserFixture(t)
			setEligibilityFlags(t, b, eligibilityFlags())
			seedTriageRequest(t, b, proposalRequestID, "Fictional delayed inquiry", time.Now().UTC())
			pause := installCompletedRead(t, b, "FROM portal_work_current WHERE")
			b.mu.Lock()
			b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
			b.mu.Unlock()
			release := onceRelease(pause.release)
			done := make(chan contactResponse, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				status, body, _ := b.request("GET", eligibilityAPIPath, nil, "")
				done <- contactResponse{status, body}
			}()
			t.Cleanup(func() {
				release()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("eligibility reader did not stop")
				}
			})
			select {
			case <-pause.blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("eligibility observation checkpoint was not reached")
			}
			query := ""
			switch change {
			case "deleted":
				query = "DELETE FROM portal_sessions"
			case "expired":
				query = "UPDATE portal_sessions SET expiry=now()-interval '1 minute'"
			case "membership-restored":
				for range 2 {
					if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=NOT revoked WHERE issuer=$1 AND subject=$2`, b.f.issuer, operatorSubject); err != nil {
						t.Fatal(err)
					}
				}
			}
			if query != "" {
				if _, err := b.pool.Exec(t.Context(), query); err != nil {
					t.Fatal(err)
				}
			}
			release()
			select {
			case got := <-done:
				if change == "unchanged" {
					if got.status != http.StatusOK || !strings.Contains(got.body, proposalRequestID) {
						t.Fatal("unchanged session positive control failed", got.status)
					}
				} else if got.status != http.StatusServiceUnavailable || strings.Contains(got.body, proposalRequestID) {
					t.Fatal("changed durable session released observations", got.status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("delayed eligibility read did not finish")
			}
			if change == "deleted" {
				var count int
				if err := b.pool.QueryRow(context.Background(), `SELECT count(*) FROM portal_sessions`).Scan(&count); err != nil || count != 0 {
					t.Fatal("refused read recreated deleted authority", err)
				}
			}
			if change == "membership-restored" {
				b.finish(b.startAt(operatorLink))
				if status, body, _ := b.request("GET", eligibilityAPIPath, nil, ""); status != http.StatusOK || !strings.Contains(body, proposalRequestID) {
					t.Fatal("fresh operator sign-in did not use its current membership epoch", status)
				}
			}
		})
	}
}
