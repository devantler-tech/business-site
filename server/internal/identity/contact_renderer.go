package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/contactrenderer"
	"github.com/devantler-tech/business-site/server/internal/renderwire"
	"github.com/open-feature/go-sdk/openfeature"
)

// contactAuthority never crosses the renderer boundary. It binds the original
// session/identity and the client used to select this single contact record.
type contactAuthority struct {
	token     string
	principal Principal
	client    string
	deadline  time.Time
}

func (b *browser) contactAuthority(ctx context.Context, client string) contactAuthority {
	return contactAuthority{b.sessions.Token(ctx), b.principal(ctx), client, b.sessions.Deadline(ctx)}
}

// contactResponseAllowed deliberately drops request values for SCS Load: Load
// on the request context would return its cached session without reading storage.
// Cancellation and deadline are retained; this read never commits or extends it.
func (b *browser) contactResponseAllowed(ctx context.Context, original contactAuthority, verification, rendered bool) bool {
	if original.token == "" || original.client == "" || ctx.Err() != nil || !time.Now().Before(original.deadline) || !b.foundation.ready(ctx) {
		return false
	}
	if b.renderer != nil {
		err := b.renderer.Check(ctx)
		if (rendered && err != nil) || (!rendered && !errors.Is(err, contactrenderer.ErrDisabled)) {
			return false
		}
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
	fresh, cancel := context.WithDeadline(context.Background(), original.deadline)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		var cancelDeadline context.CancelFunc
		fresh, cancelDeadline = context.WithDeadline(fresh, deadline)
		defer cancelDeadline()
	}
	fresh, err := b.sessions.Load(fresh, original.token)
	if err != nil || b.sessions.Token(fresh) != original.token || b.principal(fresh) != original.principal || !b.sessions.Deadline(fresh).Equal(original.deadline) {
		return false
	}
	// The SDK wait precedes fresh durable reads. This SQL is scoped to the
	// original client, not whichever client a replacement membership might own.
	var allowed bool
	err = b.foundation.pool.QueryRow(fresh, `SELECT EXISTS(SELECT 1 FROM portal_memberships m JOIN portal_sessions s ON s.token=$4 AND s.expiry>now() WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked)`, original.principal.issuer, original.principal.subject, original.client, base64.RawURLEncoding.EncodeToString(digest(original.token))).Scan(&allowed)
	return err == nil && allowed && ctx.Err() == nil && fresh.Err() == nil && b.foundation.flags.State() == openfeature.ReadyState && time.Now().Before(original.deadline) && time.Now().Before(original.principal.expires)
}

// renderContact buffers the entire private frame before committing headers.
// Only the fixed reviewed developer artifact can supply its main body. The
// codec validates framing/projection, not arbitrary HTML or authorization.
func (b *browser) renderContact(w http.ResponseWriter, r *http.Request, status int, d contactData, problem string, verification bool, authority contactAuthority) {
	page := b.contactPage(r, d, problem, verification)
	var buffer bytes.Buffer
	rendered := false
	if b.renderer != nil {
		body, err := b.renderer.Render(r.Context(), contactProjection(page, problem))
		if err != nil && !errors.Is(err, contactrenderer.ErrDisabled) {
			b.render(w, r, 503, "unavailable", "", "")
			return
		}
		if err == nil {
			rendered = true
			buffer.WriteString("<!doctype html>\n")
			if browserPage.ExecuteTemplate(&buffer, "portal-head", page) != nil || browserPage.ExecuteTemplate(&buffer, "portal-header", page) != nil {
				b.render(w, r, 503, "unavailable", "", "")
				return
			}
			// This is solely the reviewed local component, never submitted HTML.
			buffer.WriteString(body)
			if browserPage.ExecuteTemplate(&buffer, "portal-footer", page) != nil {
				b.render(w, r, 503, "unavailable", "", "")
				return
			}
		}
	}
	if buffer.Len() == 0 && browserPage.ExecuteTemplate(&buffer, "contact.html", page) != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	if !b.contactResponseAllowed(r.Context(), authority, page.VerificationEnabled, rendered) {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}

func contactProjection(page contactPageData, problem string) renderwire.Contact {
	state := "normal"
	if problem != "" {
		state = problem
	}
	if problem == "conflict" {
		state = "stale"
	}
	verification := "unverified"
	if page.Contact.EmailVerified {
		verification = "verified"
	} else if page.VerificationEnabled {
		verification = "confirmable"
	}
	reload := "Reload contact details"
	if page.Lang == "da" {
		reload = "Genindlæs kontaktoplysninger"
	}
	return renderwire.Contact{
		Locale: page.Lang, Name: page.Contact.Name, Email: page.Contact.Email, Company: page.Contact.Company,
		Revision: strconv.FormatInt(page.Contact.Revision, 10), State: state, Verification: verification, CSRF: page.CSRF,
		Text: renderwire.Text{Title: page.Title, Intro: page.Intro, NameLabel: page.NameLabel, EmailLabel: page.EmailLabel, CompanyLabel: page.CompanyLabel, SaveLabel: page.Save, PendingText: page.Pending, WorkspaceLabel: page.Workspace, VerifyLabel: page.Verify, VerificationHelp: page.VerificationHelp, ErrorText: page.FormError, ReloadLabel: reload},
	}
}
