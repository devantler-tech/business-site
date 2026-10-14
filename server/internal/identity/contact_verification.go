package identity

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"golang.org/x/oauth2"
)

// contactVerification binds one explicit action to server-owned saved data.
// It is neither an identity link nor permission for any order or operator role.
type contactVerification struct {
	Issuer, Subject, Client, Email string
	Revision                       int64
}

func (s *Foundation) contactVerificationReady(ctx context.Context) bool {
	if !s.ready(ctx) {
		return false
	}
	on, err := s.flags.BooleanValue(ctx, "portal-contact-verification", false, openfeature.NewEvaluationContext("", nil))
	// The feature evaluation may wait. Recheck the outer identity gate after it,
	// rather than carrying its earlier verdict through that provider wait.
	return err == nil && on && s.flags.State() == openfeature.ReadyState && s.ready(ctx)
}

// beginContactAttempt snapshots the current client/contact in the INSERT itself;
// the browser supplies only an expected revision, never an address or client ID.
func (s *Foundation) beginContactAttempt(ctx context.Context, p Principal, revision int64) (Attempt, error) {
	if !s.contactVerificationReady(ctx) || revision <= 0 || revision == int64(^uint64(0)>>1) {
		return Attempt{}, ErrDenied
	}
	client, role, err := s.membership(ctx, p)
	if err != nil || role != "client" {
		return Attempt{}, ErrDenied
	}
	a := Attempt{secret(), secret(), secret(), secret() + secret()}
	result, err := s.pool.Exec(ctx, `INSERT INTO portal_login_attempts
 (state_digest,browser_digest,nonce,pkce_verifier,expires_at,purpose,verification_issuer,verification_subject,verification_client,verification_email,verification_revision)
 SELECT $1,$2,$3,$4,least(now()+interval '10 minutes',$5),'contact',m.issuer,m.subject,m.client_id,c.email,c.revision
 FROM portal_memberships m JOIN portal_contacts c ON c.client_id=m.client_id
 WHERE m.issuer=$6 AND m.subject=$7 AND m.client_id=$8 AND m.role='client' AND NOT m.revoked
 AND c.revision=$9 AND NOT c.email_verified`, digest(a.State), digest(a.BrowserBinding), a.Nonce, a.PKCEVerifier, p.expires, p.issuer, p.subject, client, revision)
	if err != nil || result.RowsAffected() != 1 {
		return Attempt{}, ErrDenied
	}
	current, currentRole, err := s.membership(ctx, p)
	if err != nil || currentRole != "client" || current != client || !s.contactVerificationReady(ctx) {
		return Attempt{}, ErrDenied
	}
	return a, nil
}

func (s *Foundation) consumeContactVerification(ctx context.Context, state, browserBinding string) (contactVerification, string, string, error) {
	if !s.contactVerificationReady(ctx) || state == "" || browserBinding == "" {
		return contactVerification{}, "", "", ErrDenied
	}
	var d contactVerification
	var nonce, verifier string
	err := s.pool.QueryRow(ctx, `DELETE FROM portal_login_attempts
 WHERE state_digest=$1 AND browser_digest=$2 AND expires_at>now() AND purpose='contact'
 RETURNING nonce,pkce_verifier,verification_issuer,verification_subject,verification_client,verification_email,verification_revision`, digest(state), digest(browserBinding)).Scan(&nonce, &verifier, &d.Issuer, &d.Subject, &d.Client, &d.Email, &d.Revision)
	if err != nil {
		return contactVerification{}, "", "", ErrDenied
	}
	return d, nonce, verifier, nil
}

// confirmContact uses only validated signed claims and the consumed attempt's
// record scope. The membership lock and revision/address CAS contain races;
// expiry, cancellation and current gates are checked after database lock waits.
func (s *Foundation) confirmContact(ctx context.Context, original Principal, d contactVerification, raw, nonce string) (Principal, error) {
	if !s.contactVerificationReady(ctx) || original.issuer != d.Issuer || original.subject != d.Subject || !time.Now().Before(original.expires) {
		return Principal{}, ErrDenied
	}
	token, err := s.broker.verifiedToken(ctx, raw, nonce)
	if err != nil || token.Subject != d.Subject || s.broker.issuer != d.Issuer {
		return Principal{}, ErrDenied
	}
	var claims struct {
		Email    string `json:"email"`
		Verified *bool  `json:"email_verified"`
	}
	if token.Claims(&claims) != nil || claims.Verified == nil || !*claims.Verified || claims.Email != d.Email {
		return Principal{}, ErrDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Principal{}, ErrDenied
	}
	defer func() {
		// Commit decides success; pgx closes/discards a failed rollback connection.
		_ = tx.Rollback(ctx)
	}()
	var client string
	if err = tx.QueryRow(ctx, `SELECT client_id FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked FOR SHARE`, d.Issuer, d.Subject).Scan(&client); err != nil || client != d.Client {
		return Principal{}, ErrDenied
	}
	result, err := tx.Exec(ctx, `UPDATE portal_contacts SET email_verified=true,email_verified_at=now(),revision=revision+1,updated_at=now()
 WHERE client_id=$1 AND email=$2 AND revision=$3 AND NOT email_verified`, d.Client, d.Email, d.Revision)
	if err != nil || result.RowsAffected() != 1 || !s.contactVerificationReady(ctx) || ctx.Err() != nil || !time.Now().Before(original.expires) || !time.Now().Before(token.Expiry) {
		return Principal{}, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return Principal{}, ErrDenied
	}
	return Principal{s.broker.issuer, token.Subject, s.broker.clientID, token.Expiry}, nil
}

func (b *browser) beginContactVerification(w http.ResponseWriter, r *http.Request) {
	if !b.requireContactClient(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	for key, values := range r.PostForm {
		if (key != "revision" && key != "lang" && key != "csrf_token") || len(values) != 1 {
			b.render(w, r, 400, "error", "", "")
			return
		}
	}
	revision, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if err != nil || len(r.PostForm["revision"]) != 1 {
		b.render(w, r, 400, "error", "", "")
		return
	}
	p := b.principal(r.Context())
	a, err := b.foundation.beginContactAttempt(r.Context(), p, revision)
	if err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	if err = b.sessions.RenewToken(r.Context()); err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	b.sessions.Put(r.Context(), "purpose", "contact")
	b.sessions.Put(r.Context(), "browser", a.BrowserBinding)
	b.sessions.Put(r.Context(), "lang", language(r))
	deadline := time.Now().Add(10 * time.Minute)
	if p.expires.Before(deadline) {
		deadline = p.expires
	}
	b.sessions.SetDeadline(r.Context(), deadline)
	configuration := b.oauth
	configuration.Scopes = []string{oidcScope, "email"}
	address := configuration.AuthCodeURL(a.State, oauth2.S256ChallengeOption(a.PKCEVerifier), oauth2.SetAuthURLParam("nonce", a.Nonce), oauth2.SetAuthURLParam("claims", `{"id_token":{"email":null,"email_verified":null}}`))
	http.Redirect(w, r, address, http.StatusSeeOther)
}

// contactVerificationFailure offers recovery only through current client access.
// A wrong broker account cannot change the original session or choose its record.
func (b *browser) contactVerificationFailure(w http.ResponseWriter, r *http.Request) {
	d, err := b.foundation.readContact(r.Context(), b.principal(r.Context()))
	if err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	b.renderContact(w, r, 400, d, "verification", false)
}
