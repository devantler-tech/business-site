package identity

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
)

var errContactConflict = errors.New("contact changed")

// contactData is private display data, never identity or execution authority.
type contactData struct {
	Name, Email, Company string
	Revision             int64
	EmailVerified        bool
}

// contactText bounds UTF-8 display fields and excludes control characters
// before either persistence or reflection into a recovery form.
func contactText(s string, limit int, required bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit || (required && strings.TrimSpace(s) == "") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// parseContact accepts only bounded, single-value contact fields and a valid
// expected revision. Submitted record IDs and verification claims are rejected.
func parseContact(r *http.Request) (contactData, error) {
	if r.ParseForm() != nil {
		return contactData{}, ErrDenied
	}
	allowed := map[string]bool{"name": true, "email": true, "company": true, "revision": true, "csrf_token": true, "lang": true}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			return contactData{}, ErrDenied
		}
	}
	for _, key := range []string{"name", "email", "company", "revision"} {
		if len(r.PostForm[key]) != 1 {
			return contactData{}, ErrDenied
		}
	}
	d := contactData{Name: r.PostForm.Get("name"), Email: r.PostForm.Get("email"), Company: r.PostForm.Get("company")}
	if !contactText(d.Name, 120, true) || !contactText(d.Company, 160, false) || !contactText(d.Email, 254, true) || d.Email != strings.TrimSpace(d.Email) {
		return contactData{}, ErrDenied
	}
	address, err := mail.ParseAddress(d.Email)
	if err != nil || address.Address != d.Email || address.Name != "" {
		return contactData{}, ErrDenied
	}
	d.Name = strings.TrimSpace(d.Name)
	d.Company = strings.TrimSpace(d.Company)
	d.Revision, err = strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if err != nil || d.Revision < 0 || d.Revision == int64(^uint64(0)>>1) {
		return contactData{}, ErrDenied
	}
	return d, nil
}

// readContact scopes its actual data query by current server-owned membership.
// Operators have no client-profile override; no URL/form client ID is accepted.
func (s *Foundation) readContact(ctx context.Context, p Principal) (contactData, error) {
	client, role, err := s.membership(ctx, p)
	if err != nil || role != "client" {
		return contactData{}, ErrDenied
	}
	var d contactData
	err = s.pool.QueryRow(ctx, `SELECT coalesce(c.name,''),coalesce(c.email,''),coalesce(c.company,''),coalesce(c.revision,0),coalesce(c.email_verified,false)
 FROM portal_memberships m LEFT JOIN portal_contacts c ON c.client_id=m.client_id
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked`, p.issuer, p.subject, client).Scan(&d.Name, &d.Email, &d.Company, &d.Revision, &d.EmailVerified)
	if err != nil {
		return contactData{}, ErrDenied
	}
	current, currentRole, check := s.membership(ctx, p)
	if check != nil || currentRole != "client" || current != client {
		return contactData{}, ErrDenied
	}
	return d, nil
}

// saveContact locks current membership against concurrent revocation and uses
// the browser's revision only as a compare-and-set, never as record identity.
func (s *Foundation) saveContact(ctx context.Context, p Principal, d contactData) error {
	if _, role, err := s.membership(ctx, p); err != nil || role != "client" {
		return ErrDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrDenied
	}
	defer func() {
		// Commit decides success. A committed transaction is already closed;
		// pgx discards a failed rollback's connection and returns it to the pool.
		_ = tx.Rollback(ctx)
	}()
	var client string
	if err = tx.QueryRow(ctx, `SELECT client_id FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked FOR SHARE`, p.issuer, p.subject).Scan(&client); err != nil {
		return ErrDenied
	}
	var revision int64
	if d.Revision == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES($1,$2,$3,$4,1) ON CONFLICT(client_id) DO NOTHING RETURNING revision`, client, d.Name, d.Email, d.Company).Scan(&revision)
	} else {
		err = tx.QueryRow(ctx, `UPDATE portal_contacts SET name=$2,email=$3,company=$4,email_verified=false,email_verified_at=NULL,revision=revision+1,updated_at=now() WHERE client_id=$1 AND revision=$5 RETURNING revision`, client, d.Name, d.Email, d.Company, d.Revision).Scan(&revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errContactConflict
	}
	if err != nil {
		return ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return ErrDenied
	}
	return nil
}

// contact renders only the current client's stored contact profile.
func (b *browser) contact(w http.ResponseWriter, r *http.Request) {
	if !b.requireContactClient(w, r) {
		return
	}
	// Optional controls are evaluated before the scoped read's final authority
	// checkpoint. Rendering cannot introduce a new provider wait after that read.
	verification := b.foundation.contactVerificationReady(r.Context())
	d, err := b.foundation.readContact(r.Context(), b.principal(r.Context()))
	if err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	b.renderContact(w, r, 200, d, "", verification)
}

// requireContactClient preserves the authentication and role-denial responses.
func (b *browser) requireContactClient(w http.ResponseWriter, r *http.Request) bool {
	_, role, err := b.foundation.membership(r.Context(), b.principal(r.Context()))
	if err != nil {
		b.render(w, r, 401, "error", "", "")
		return false
	}
	if role != "client" {
		b.render(w, r, 403, "error", "", "")
		return false
	}
	return true
}

// recoverContact retains safe draft text only when its expected revision still
// matches the store. Invalid revisions reload stored values; stale drafts are
// never silently rebased onto a newer revision.
func recoverContact(r *http.Request, current contactData) (contactData, bool) {
	revision, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if len(r.PostForm["revision"]) != 1 || err != nil || revision < 0 || revision == int64(^uint64(0)>>1) {
		return current, false
	}
	if revision != current.Revision {
		return current, true
	}
	for _, field := range []struct {
		key string
		max int
		to  *string
	}{{"name", 120, &current.Name}, {"email", 254, &current.Email}, {"company", 160, &current.Company}} {
		if values := r.PostForm[field.key]; len(values) == 1 && contactText(values[0], field.max, false) {
			if *field.to != values[0] {
				current.EmailVerified = false
			}
			*field.to = values[0]
		}
	}
	return current, false
}

// updateContact validates client-only form writes and renders a recoverable
// rejected edit without resetting its revision or exposing unsafe submitted text.
func (b *browser) updateContact(w http.ResponseWriter, r *http.Request) {
	if !b.requireContactClient(w, r) {
		return
	}
	d, err := parseContact(r)
	if err != nil {
		current, readErr := b.foundation.readContact(r.Context(), b.principal(r.Context()))
		if readErr != nil {
			b.render(w, r, 503, "unavailable", "", "")
			return
		}
		recovered, conflict := recoverContact(r, current)
		if conflict {
			b.renderContact(w, r, 409, recovered, "conflict", false)
		} else {
			b.renderContact(w, r, 400, recovered, "invalid", false)
		}
		return
	}
	err = b.foundation.saveContact(r.Context(), b.principal(r.Context()), d)
	if errors.Is(err, errContactConflict) {
		b.renderContact(w, r, 409, d, "conflict", false)
		return
	}
	if err != nil {
		b.render(w, r, 503, "unavailable", "", "")
		return
	}
	http.Redirect(w, r, "/portal/contact?lang="+language(r), http.StatusSeeOther)
}

type contactPageData struct {
	pageData
	Contact                                                                  contactData
	NameLabel, EmailLabel, CompanyLabel, Save, Pending, FormError, Workspace string
	VerificationEnabled                                                      bool
	Verify, VerificationHelp                                                 string
}

// renderContact escapes bounded profile data using the shared private layout,
// with localized validation/conflict messages and an honest unverified state.
func (b *browser) renderContact(w http.ResponseWriter, r *http.Request, status int, d contactData, problem string, verification bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = browserPage.ExecuteTemplate(w, "contact.html", b.contactPage(r, d, problem, verification))
}

func (b *browser) contactPage(r *http.Request, d contactData, problem string, verification bool) contactPageData {
	page := contactPageData{pageData: b.data(r, "contact", "", nosurf.Token(r)), Contact: d, NameLabel: "Your name", EmailLabel: "Contact email", CompanyLabel: "Company (optional)", Save: "Save contact details", Pending: "This contact email is not verified. Saving it does not change your sign-in or authorize an order.", Workspace: "Back to your workspace"}
	page.Title = "Your contact details"
	page.Intro = "Keep your details together in your private client profile."
	page.VerificationEnabled = problem == "" && d.Revision > 0 && !d.EmailVerified && verification
	page.Verify = "Confirm with your sign-in provider"
	page.VerificationHelp = "Confirm your saved address if your sign-in provider has verified the same email. A different address stays unverified. This does not authorize an order."
	if d.EmailVerified {
		page.Pending = "Confirmed by your sign-in provider. Editing these details clears the confirmation. This does not change your identity or authorize an order."
	}
	if problem == "invalid" {
		page.FormError = "Check your name, contact email and optional company. Use a valid email address and keep each field within its displayed limit."
	}
	if problem == "conflict" {
		page.FormError = "Your profile has changed since this form was opened. Reload your contact details before saving again."
	}
	if problem == "verification" {
		page.FormError = "Your saved address could not be confirmed. Reload your contact details and use the same sign-in account with a verified email. No contact confirmation was added."
	}
	if page.Lang == "da" {
		page.Title = "Dine kontaktoplysninger"
		page.Intro = "Saml dine oplysninger i din private kundeprofil."
		page.NameLabel = "Dit navn"
		page.EmailLabel = "Kontakt-e-mail"
		page.CompanyLabel = "Virksomhed (valgfrit)"
		page.Save = "Gem kontaktoplysninger"
		page.Workspace = "Tilbage til dit arbejdsområde"
		page.Pending = "Denne kontakt-e-mail er ikke bekræftet. Når du gemmer den, ændrer det ikke dit login og godkender ikke en bestilling."
		page.Verify = "Bekræft med din loginudbyder"
		page.VerificationHelp = "Bekræft din gemte adresse, hvis din loginudbyder har bekræftet den samme e-mail. En anden adresse forbliver ubekræftet. Dette godkender ikke en bestilling."
		if d.EmailVerified {
			page.Pending = "Bekræftet af din loginudbyder. Når du ændrer oplysningerne, fjernes bekræftelsen. Dette ændrer ikke din identitet og godkender ikke en bestilling."
		}
		if problem == "invalid" {
			page.FormError = "Kontrollér dit navn, din kontakt-e-mail og eventuelle virksomhed. Brug en gyldig e-mailadresse, og overhold felternes viste grænser."
		}
		if problem == "conflict" {
			page.FormError = "Din profil er ændret, siden formularen blev åbnet. Genindlæs dine kontaktoplysninger, før du gemmer igen."
		}
		if problem == "verification" {
			page.FormError = "Din gemte adresse kunne ikke bekræftes. Genindlæs dine kontaktoplysninger, og brug den samme loginkonto med en bekræftet e-mail. Der blev ikke tilføjet en bekræftelse."
		}
	}
	return page
}
