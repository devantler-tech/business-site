package identity

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

const (
	supportViewAction = "support.view"
	supportBasisAudit = "support.request-recorded"
)

var supportCase = regexp.MustCompile(`^CASE-[0-9]{4,10}$`)

type supportPage struct {
	pageData
	Case         string
	ContactSaved bool
	SharingOpen  bool
	Requests     []supportRequest
}

type supportRequest struct {
	Product        string
	Submitted      time.Time
	ProposalShared bool
}

type supportMember struct {
	workspace, subject, incarnation string
}

func (s *Foundation) supportReady(ctx context.Context) bool {
	if !s.ready(ctx) || s.operatorOnly {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-support", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.flags.State() == openfeature.ReadyState
}

func (b *browser) operatorSupportPage(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.supportReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	status, result := b.operatorCall(r, supportViewAction, func(ctx context.Context, _ string) ([]byte, error) {
		return b.renderSupport(ctx, r)
	})
	if status != http.StatusOK && status != http.StatusBadRequest && status != http.StatusServiceUnavailable {
		status = http.StatusForbidden
	}
	b.writeOperatorActionPage(w, r, status, result)
}

// renderSupport keeps the human operator's principal and participates in
// operatorCall's final session check and mandatory audit before disclosure.
func (b *browser) renderSupport(ctx context.Context, r *http.Request) ([]byte, error) {
	authority, _ := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority)
	if authority == nil || authority.kind != "operator" || !b.foundation.supportReady(ctx) {
		return nil, ErrDenied
	}
	var input struct {
		Subject   string `json:"subject"`
		Workspace string `json:"workspace"`
		Case      string `json:"case"`
		Confirmed string `json:"confirmed"`
	}
	fields := map[string]*string{"subject": &input.Subject, "workspace": &input.Workspace, "case": &input.Case, "confirmed": &input.Confirmed}
	if err := decodeOperatorInput(r, &input, fields); err != nil || input.Confirmed != "yes" ||
		!validCustomerWorkspace(input.Workspace) || len(input.Subject) > 512 ||
		customeridentity.DexUser(input.Subject, customeridentity.ZitadelConnectorID) == "" || !supportCase.MatchString(input.Case) {
		return nil, errOperatorInput
	}
	page := supportPage{pageData: b.operatorData(r, "operator-support", input.Workspace), Case: input.Case}
	page.RequestsEnabled = b.foundation.requestsReady(ctx)
	page.SharingOpen = page.RequestsEnabled && b.foundation.sharingReady(ctx)
	if !b.operatorMutationAllowed(ctx, b.foundation.pool) {
		return nil, ErrDenied
	}
	if err := b.bindSupportMember(ctx, authority, input.Workspace, input.Subject); err != nil {
		return nil, err
	}
	target := input.Case + "/" + authority.support.incarnation
	setOperatorTarget(ctx, target)
	if err := b.foundation.audit(ctx, authority.principal, authority.kind, supportBasisAudit, target, "allowed"); err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, b.foundation.pool) {
		return nil, ErrDenied
	}
	if err := b.foundation.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_contacts WHERE client_id=$1)`, input.Workspace).Scan(&page.ContactSaved); err != nil {
		return nil, err
	}
	if page.RequestsEnabled {
		var err error
		page.Requests, err = b.supportRequests(ctx, input.Workspace, page.SharingOpen)
		if err != nil {
			return nil, err
		}
	}
	var buffer bytes.Buffer
	if err := b.templates().ExecuteTemplate(&buffer, "operator.html", page); err != nil {
		return nil, err
	}
	if !b.foundation.supportReady(ctx) {
		return nil, ErrDenied
	}
	return buffer.Bytes(), nil
}

func (b *browser) bindSupportMember(ctx context.Context, authority *operatorAuthority, workspace, subject string) error {
	var incarnation string
	err := b.foundation.pool.QueryRow(ctx, `SELECT incarnation::text FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' AND NOT revoked`,
		authority.principal.issuer, subject, workspace).Scan(&incarnation)
	if errors.Is(err, pgx.ErrNoRows) {
		return errOperatorInput
	}
	if err != nil {
		return err
	}
	authority.support = &supportMember{workspace: workspace, subject: subject, incarnation: incarnation}
	return nil
}

// supportRequests exposes the client's visible request metadata. Free text,
// draft proposals and billing records stay outside this query.
func (b *browser) supportRequests(ctx context.Context, workspace string, sharing bool) ([]supportRequest, error) {
	rows, err := b.foundation.pool.Query(ctx, `SELECT q.product,q.submitted_at,
 CASE WHEN $2 THEN COALESCE((SELECT v.action='share' FROM portal_proposal_visibility v
 WHERE v.client_id=q.client_id AND v.request_id=q.id ORDER BY v.event DESC LIMIT 1),false)
 ELSE false END FROM portal_requests q WHERE q.client_id=$1 ORDER BY q.submitted_at DESC,q.id LIMIT 20`, workspace, sharing)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[supportRequest])
}
