package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
)

type customerInvitationInput struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Workspace string `json:"workspace"`
}

func validCustomerWorkspace(workspace string) bool {
	return workspace != "" && workspace != operatorWorkspace && len(workspace) <= 100 && strings.TrimSpace(workspace) == workspace && utf8.ValidString(workspace) && !strings.ContainsFunc(workspace, unicode.IsControl)
}

// decodeOperatorInput admits one bounded JSON object or the native form's exact
// named fields. Authentication and CSRF happen before this callback is entered.
func decodeOperatorInput(r *http.Request, value any, fields map[string]*string) error {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return errOperatorInput
	}
	switch kind {
	case "application/json":
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
			return errOperatorInput
		}
	case "application/x-www-form-urlencoded":
		if r.ParseForm() != nil {
			return errOperatorInput
		}
		for name, values := range r.PostForm {
			if len(values) != 1 {
				return errOperatorInput
			}
			if name == "csrf_token" {
				continue
			}
			field, ok := fields[name]
			if !ok {
				return errOperatorInput
			}
			*field = values[0]
		}
	default:
		return errOperatorInput
	}
	return nil
}

func (b *browser) createCustomerInvitation(r *http.Request, ctx context.Context) (any, error) {
	var input customerInvitationInput
	if err := decodeOperatorInput(r, &input, map[string]*string{"name": &input.Name, "email": &input.Email, "workspace": &input.Workspace}); err != nil {
		return nil, err
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Name != "" || address.Address != input.Email || len(input.Email) > 254 || len(strings.Fields(input.Name)) < 2 || len(input.Name) > 200 || strings.TrimSpace(input.Name) != input.Name || !utf8.ValidString(input.Name) || strings.ContainsFunc(input.Name, unicode.IsControl) || !validCustomerWorkspace(input.Workspace) {
		return nil, errOperatorInput
	}
	if b.foundation.customers == nil || !b.operatorMutationAllowed(ctx, b.foundation.pool) {
		return nil, ErrDenied
	}
	userID, err := b.foundation.customers.EnsureUser(ctx, input.Name, input.Email, func() bool { return b.operatorMutationAllowed(ctx, b.foundation.pool) })
	if err != nil {
		return nil, err
	}
	subject := customeridentity.DexSubject(userID, customeridentity.ZitadelConnectorID)
	invitation := Invitation{b.foundation.broker.issuer, subject, input.Workspace, "client", time.Now().Add(7 * 24 * time.Hour)}
	setOperatorTarget(ctx, input.Workspace+"/"+subject)
	err = b.foundation.invite(ctx, invitation, func(tx pgx.Tx) bool { return b.operatorMutationAllowed(ctx, tx) })
	if err != nil {
		return nil, err
	}
	return pendingInvitation{subject, input.Workspace, "client", invitation.Expires}, nil
}

// operatorInvitationPage uses the API's exact authorization and audit boundary.
// Native forms POST to that API; their success returns to the customer inventory.
func (b *browser) operatorInvitationPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "invitations.view", func(_ context.Context, client string) ([]byte, error) {
		d := b.data(r, "operator-invite", client, nosurf.Token(r))
		var buffer bytes.Buffer
		err := b.templates().ExecuteTemplate(&buffer, "portal.html", d)
		return buffer.Bytes(), err
	})
	if status != http.StatusOK {
		if status != http.StatusServiceUnavailable {
			status = http.StatusForbidden
		}
		b.render(w, r, status, "operator-action-error", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(result)
}
