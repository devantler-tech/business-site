package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

type serviceDecisionFixture struct {
	b                   *browserFixture
	form                url.Values
	path, gate, subject string
	countQuery          string
	wantCount           int
}

func prepareServiceDecision(t *testing.T, operation string, retry bool) serviceDecisionFixture {
	t.Helper()
	d := serviceDecisionFixture{}
	if operation == "share" {
		d.b = sharingFixture(t)
		d.form = visibilityForm(t, d.b, "0", "1", "1", "share")
		d.path = "/portal/operator/requests/" + proposalRequestID + "/proposal-visibility"
		d.gate, d.subject = "portal-proposal-sharing", "proposal-operator"
		d.countQuery = `SELECT count(*) FROM portal_proposal_visibility WHERE request_id=$1`
	} else {
		d.b = acceptanceFixture(t)
		d.form = acceptanceForm(t, d.b)
		d.path = "/portal/requests/" + proposalRequestID + "/proposal-acceptance"
		d.gate, d.subject = "portal-proposal-acceptance", "consent-client"
		d.countQuery = `SELECT count(*) FROM portal_proposal_acceptances WHERE request_id=$1`
	}
	if retry {
		if status, _, _ := d.b.request("POST", d.path, d.form, d.b.site.URL); status != 303 {
			t.Fatal("initial decision did not create retry history")
		}
		d.wantCount = 1
	}
	return d
}

func serviceSessionKey(b *browserFixture) string {
	return base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
}

// replacementServiceSession creates another genuinely valid native session for
// the same identity, then restores the original browser used by the paused POST.
func replacementServiceSession(t *testing.T, d serviceDecisionFixture) []byte {
	t.Helper()
	var original []byte
	if err := d.b.pool.QueryRow(t.Context(), `SELECT data FROM portal_sessions WHERE token=$1`, serviceSessionKey(d.b)).Scan(&original); err != nil {
		t.Fatal(err)
	}
	originalClient := d.b.client
	replacementClient := *originalClient
	var err error
	replacementClient.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	d.b.client = &replacementClient
	defer func() { d.b.client = originalClient }()
	d.b.mu.Lock()
	d.b.subject = d.subject
	d.b.mu.Unlock()
	// The identity is already a member. A native Begin/Complete must sign it
	// in without attempting another privileged invitation.
	d.b.finish(d.b.start())
	var data []byte
	if err := d.b.pool.QueryRow(t.Context(), `SELECT data FROM portal_sessions WHERE token=$1`, serviceSessionKey(d.b)).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, original) {
		t.Fatal("replacement fixture did not create distinct valid session bytes")
	}
	return data
}

func changeServiceDecisionSession(ctx context.Context, t *testing.T, d serviceDecisionFixture, key, change string, replacement []byte) {
	t.Helper()
	query, args := "", []any{key}
	switch change {
	case "deleted":
		query = `DELETE FROM portal_sessions WHERE token=$1`
	case "expired":
		query = `UPDATE portal_sessions SET expiry=clock_timestamp()-interval '1 minute' WHERE token=$1`
	case "changed bytes":
		query = `UPDATE portal_sessions SET data=data||decode('00','hex') WHERE token=$1`
	case "replacement":
		query, args = `UPDATE portal_sessions SET data=$2 WHERE token=$1`, []any{key, replacement}
	case "restored epoch":
		for range 2 {
			tag, err := d.b.pool.Exec(ctx, `UPDATE portal_memberships SET revoked=NOT revoked WHERE issuer=$1 AND subject=$2`, d.b.f.issuer, d.subject)
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatal("real membership epoch transition failed", err)
			}
		}
		return
	case "unchanged":
		return
	default:
		t.Fatal("unknown session change")
	}
	tag, err := d.b.pool.Exec(ctx, query, args...)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("original durable session change failed", err)
	}
}

func runServiceSessionDecision(t *testing.T, operation, checkpoint, change string, retry bool) {
	t.Helper()
	d := prepareServiceDecision(t, operation, retry)
	key := serviceSessionKey(d.b)
	var replacement []byte
	if change == "replacement" {
		replacement = replacementServiceSession(t, d)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if checkpoint != "request lock" {
		at := int32(3)
		gate := d.gate
		if operation == "share" {
			at = 4 // The session-aware gate, handler and domain checks precede commit.
		}
		if checkpoint == "initial SDK" {
			at = 1
		}
		if checkpoint == "outer SDK" {
			gate, at = "portal-identity", 1
		}
		provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(acceptanceMemoryFlags()), key: gate, blockAt: at, blocked: make(chan struct{}), release: make(chan struct{})}
		if err := d.b.flags.SetProviderAndWait(ctx, provider); err != nil {
			t.Fatal(err)
		}
		release := onceRelease(provider.release)
		done, finished := startBrowserRequest(t, d.b, "POST", d.path, d.form, d.b.site.URL, release)
		select {
		case <-provider.blocked:
		case <-ctx.Done():
			t.Fatal("decision missed its SDK wait")
		}
		changeServiceDecisionSession(ctx, t, d, key, change, replacement)
		release()
		assertServiceSessionDecision(t, d, key, change, done, finished)
		return
	}
	migrator := migrationDB(t)
	tx := blockFixtureProposalWrite(ctx, t, migrator)
	release := func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("request lock cleanup failed", err)
		}
	}
	done, finished := startBrowserRequest(t, d.b, "POST", d.path, d.form, d.b.site.URL, release)
	waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
	changeServiceDecisionSession(ctx, t, d, key, change, replacement)
	release()
	assertServiceSessionDecision(t, d, key, change, done, finished)
}

func assertServiceSessionDecision(t *testing.T, d serviceDecisionFixture, key, change string, done <-chan contactResponse, finished <-chan struct{}) {
	t.Helper()
	select {
	case got := <-done:
		if change == "unchanged" {
			if got.status != 303 {
				t.Errorf("unchanged original session refused: %d", got.status)
			}
			d.wantCount = 1
		} else if got.status < 400 {
			t.Errorf("changed original session authorized a human decision: %d", got.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native decision did not return")
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("native decision worker did not finish")
	}
	var count int
	if err := d.b.pool.QueryRow(t.Context(), d.countQuery, proposalRequestID).Scan(&count); err != nil || count != d.wantCount {
		t.Error("refused decision changed immutable history", err)
	}
	if change == "deleted" {
		var restored bool
		if err := d.b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_sessions WHERE token=$1)`, key).Scan(&restored); err != nil || restored {
			t.Error("decision restored a deleted original session", err)
		}
	}
}

func TestServiceDecisionOriginalSession(t *testing.T) {
	for _, operation := range []string{"share", "accept"} {
		for _, checkpoint := range []string{"initial SDK", "final SDK", "request lock"} {
			for _, retry := range []bool{false, true} {
				phase := "new"
				if retry {
					phase = "retry"
				}
				changes := []string{"deleted", "expired", "changed bytes", "replacement", "unchanged"}
				if checkpoint == "request lock" {
					changes = append(changes, "restored epoch")
				}
				for _, change := range changes {
					t.Run(operation+"/"+checkpoint+"/"+phase+"/"+change, func(t *testing.T) {
						runServiceSessionDecision(t, operation, checkpoint, change, retry)
					})
				}
			}
		}
	}
}

// The outer portal gate runs before the ordinary session middleware. Pausing
// that real SDK evaluation must not let a later row become the original session.
func TestServiceDecisionOuterSDKOriginalSession(t *testing.T) {
	for _, operation := range []string{"share", "accept"} {
		for _, retry := range []bool{false, true} {
			phase := "new"
			if retry {
				phase = "retry"
			}
			for _, change := range []string{"changed bytes", "replacement", "unchanged"} {
				t.Run(operation+"/"+phase+"/"+change, func(t *testing.T) {
					runServiceSessionDecision(t, operation, "outer SDK", change, retry)
				})
			}
		}
	}
}

func TestServiceDecisionClosedGatePrecedesCSRF(t *testing.T) {
	for _, operation := range []string{"share", "accept"} {
		t.Run(operation, func(t *testing.T) {
			d := prepareServiceDecision(t, operation, false)
			d.form.Set("csrf_token", "invalid")
			values := []any{true, true, true, true, true, true}
			if operation == "share" {
				values[4] = false // The sharing route's independently closed gate.
			} else {
				values[1] = false // The client's existing outer requests gate.
			}
			acceptanceFlags(t, d.b, values...)
			if status, _, _ := d.b.request("POST", d.path, d.form, d.b.site.URL); status != 404 {
				t.Fatalf("closed human decision route reached CSRF: status=%d", status)
			}
			var count int
			if err := d.b.pool.QueryRow(t.Context(), d.countQuery, proposalRequestID).Scan(&count); err != nil || count != 0 {
				t.Fatal("closed decision changed immutable history", err)
			}
		})
	}
}
