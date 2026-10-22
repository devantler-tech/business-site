package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

// contactAuthority binds the original session/identity and the client used to
// select this single contact record.
type contactAuthority struct {
	token     string
	principal Principal
	client    string
	deadline  time.Time
}

func (b *browser) contactAuthority(ctx context.Context, client string) contactAuthority {
	return contactAuthority{b.sessions.Token(ctx), b.principal(ctx), client, b.sessions.Deadline(ctx)}
}

// contactResponseAllowed uses the maintained context-aware store and codec,
// never SessionManager.Load's request cache. Final SQL binds the decoded bytes
// to current durable authority; this read never commits or extends a session.
func (b *browser) contactResponseAllowed(ctx context.Context, original contactAuthority, verification bool) bool {
	if original.token == "" || original.client == "" || ctx.Err() != nil || !time.Now().Before(original.deadline) || !b.foundation.ready(ctx) {
		return false
	}
	// The release checkpoint itself may wait. Recheck identity and any displayed
	// confirmation eligibility after it, before loading original durable authority.
	if verification {
		if !b.foundation.contactVerificationReady(ctx) {
			return false
		}
	} else if !b.foundation.ready(ctx) {
		return false
	}
	return b.durableResponseAllowed(ctx, original, "client", b.foundation.pool)
}

type privateResponseQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type durableResponseSnapshot struct {
	original contactAuthority
	key      string
	data     []byte
}

// captureDurableResponse uses the maintained store and codec. This snapshot is
// comparison material, never authority: final SQL must still bind these exact
// bytes to the current durable row after all later SDK waits. Work operations
// capture before acquiring a transaction, avoiding a second pool connection.
func (b *browser) captureDurableResponse(ctx context.Context, original contactAuthority) (durableResponseSnapshot, bool) {
	if original.token == "" || ctx.Err() != nil || !time.Now().Before(original.deadline) {
		return durableResponseSnapshot{}, false
	}
	fresh, cancel := context.WithDeadline(ctx, original.deadline)
	defer cancel()
	store, ok := b.sessions.Store.(scs.CtxStore)
	if !ok {
		return durableResponseSnapshot{}, false
	}
	key := base64.RawURLEncoding.EncodeToString(digest(original.token))
	data, found, err := store.FindCtx(fresh, key)
	if err != nil || !found {
		return durableResponseSnapshot{}, false
	}
	deadline, values, err := b.sessions.Codec.Decode(data)
	issuer, issuerOK := values["issuer"].(string)
	subject, subjectOK := values["subject"].(string)
	expires, expiresOK := values["expires"].(int64)
	principal := Principal{issuer: issuer, subject: subject, audience: b.foundation.broker.clientID, expires: time.Unix(expires, 0)}
	if err != nil || !issuerOK || !subjectOK || !expiresOK || principal != original.principal || !deadline.Equal(original.deadline) {
		return durableResponseSnapshot{}, false
	}
	return durableResponseSnapshot{original: original, key: key, data: data}, true
}

// durableResponseAllowed retains contact's post-SDK maintained-store read.
func (b *browser) durableResponseAllowed(ctx context.Context, original contactAuthority, role string, query privateResponseQuery) bool {
	snapshot, ok := b.captureDurableResponse(ctx, original)
	return ok && b.durableSnapshotAllowed(ctx, snapshot, original.client, role, query)
}

// durableSnapshotAllowed performs no pool acquisition or SDK wait. The caller
// supplies its held transaction when locks protect an approval or private frame.
func (b *browser) durableSnapshotAllowed(ctx context.Context, snapshot durableResponseSnapshot, client, role string, query privateResponseQuery) bool {
	original := snapshot.original
	if client == "" || ctx.Err() != nil || !time.Now().Before(original.deadline) {
		return false
	}
	fresh, cancel := context.WithDeadline(ctx, original.deadline)
	defer cancel()
	// The SDK wait precedes fresh durable reads. This SQL is scoped to the
	// original client, not whichever client a replacement membership might own.
	var allowed bool
	err := query.QueryRow(fresh, `SELECT EXISTS(SELECT 1 FROM portal_memberships m JOIN portal_sessions s ON s.token=$4 AND s.expiry>clock_timestamp() AND s.data=$5 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role=$6 AND NOT m.revoked)`, original.principal.issuer, original.principal.subject, client, snapshot.key, snapshot.data, role).Scan(&allowed)
	return err == nil && allowed && ctx.Err() == nil && fresh.Err() == nil && b.foundation.flags.State() == openfeature.ReadyState && time.Now().Before(original.deadline) && time.Now().Before(original.principal.expires)
}

// renderContact buffers the entire private Go frame before committing headers.
// A complete rendered document never substitutes for current durable authority.
func (b *browser) renderContact(w http.ResponseWriter, r *http.Request, status int, d contactData, problem string, verification bool, authority contactAuthority) {
	page := b.contactPage(r, d, problem, verification)
	var buffer bytes.Buffer
	if browserPage.ExecuteTemplate(&buffer, "contact.html", page) != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	if !b.contactResponseAllowed(r.Context(), authority, page.VerificationEnabled) {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}
