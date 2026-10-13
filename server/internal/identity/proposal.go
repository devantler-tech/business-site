package identity

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
	"github.com/open-feature/go-sdk/openfeature"
)

var errProposalConflict = errors.New("proposal version or retry conflict")

type projectProposal struct {
	Version, Expected                                              int
	Key, Scope, Deliverables, Currency, OneTimeTerms, MonthlyTerms string
	OneTimeMinor, MonthlyMinor                                     int64
	Prepared                                                       time.Time
}

func (d projectProposal) OneTimeDKK() int64 { return d.OneTimeMinor / 100 }
func (d projectProposal) MonthlyDKK() int64 { return d.MonthlyMinor / 100 }

func (s *Foundation) proposalsReady(ctx context.Context) bool {
	if !s.triageReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-proposals", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

func (s *Foundation) proposalOperator(ctx context.Context, p Principal) error {
	if !s.proposalsReady(ctx) {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

func proposalNumber(raw string, ceiling int) (int, bool) {
	if raw == "" || len(raw) > 8 || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n <= ceiling
}

func parseProposal(r *http.Request) (projectProposal, bool) {
	if err := r.ParseForm(); err != nil {
		return projectProposal{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return projectProposal{}, false
		}
		switch name {
		case "submission_key", "expected_version", "scope", "deliverables", "currency", "one_time_dkk", "monthly_dkk", "one_time_terms", "monthly_terms", "csrf_token", "lang":
		default:
			return projectProposal{}, false
		}
	}
	version, vok := proposalNumber(r.PostForm.Get("expected_version"), 49)
	one, ook := proposalNumber(r.PostForm.Get("one_time_dkk"), 10000000)
	monthly, mok := proposalNumber(r.PostForm.Get("monthly_dkk"), 10000000)
	d := projectProposal{Key: r.PostForm.Get("submission_key"), Expected: version, Currency: r.PostForm.Get("currency"), OneTimeMinor: int64(one) * 100, MonthlyMinor: int64(monthly) * 100}
	for _, field := range []struct {
		name   string
		limit  int
		target *string
	}{
		{"scope", 1200, &d.Scope}, {"deliverables", 1200, &d.Deliverables}, {"one_time_terms", 600, &d.OneTimeTerms}, {"monthly_terms", 600, &d.MonthlyTerms},
	} {
		text := strings.ReplaceAll(strings.ReplaceAll(r.PostForm.Get(field.name), "\r\n", "\n"), "\r", "\n")
		if !validRequestText(text, field.limit, true) {
			return projectProposal{}, false
		}
		*field.target = strings.TrimSpace(text)
	}
	return d, vok && ook && mok && d.Currency == "DKK" && opaqueRequestID.MatchString(d.Key)
}

const proposalColumns = `p.version,p.expected_version,p.submission_key,p.scope,p.deliverables,p.currency,p.one_time_minor,p.monthly_minor,p.one_time_terms,p.monthly_terms,p.prepared_at`

func scanProposal(row pgx.Row) (projectProposal, error) {
	var d projectProposal
	err := row.Scan(&d.Version, &d.Expected, &d.Key, &d.Scope, &d.Deliverables, &d.Currency, &d.OneTimeMinor, &d.MonthlyMinor, &d.OneTimeTerms, &d.MonthlyTerms, &d.Prepared)
	return d, err
}

// readProposals independently scopes history to the existing request/client and
// current operator. The schema bound makes this complete, not a truncated history.
func (s *Foundation) readProposals(ctx context.Context, p Principal, id string) ([]projectProposal, error) {
	if err := s.proposalOperator(ctx, p); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+proposalColumns+` FROM portal_proposals p JOIN portal_requests q ON q.id=p.request_id AND q.client_id=p.client_id WHERE q.id=$3 AND EXISTS(SELECT 1 FROM portal_memberships m WHERE m.issuer=$1 AND m.subject=$2 AND m.role='operator' AND NOT m.revoked) ORDER BY p.version DESC LIMIT 50`, p.issuer, p.subject, id)
	if err != nil {
		return nil, errTriageStorage
	}
	defer rows.Close()
	var result []projectProposal
	for rows.Next() {
		d, err := scanProposal(rows)
		if err != nil {
			return nil, errTriageStorage
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, errTriageStorage
	}
	if err := s.proposalOperator(ctx, p); err != nil {
		return nil, err
	}
	return result, nil
}

// persistProposal serializes per-request revisions without requesting UPDATE on
// immutable intake. Membership stays locked through commit, ordering revocation.
func (s *Foundation) persistProposal(ctx context.Context, p Principal, id string, d projectProposal) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.proposalOperator(ctx, p); err != nil {
		return err
	}
	if !opaqueRequestID.MatchString(id) {
		return pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errTriageStorage
	}
	defer func() {
		// Commit determines success. pgx discards failed rollback connections;
		// a transaction already committed needs no further cleanup.
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, id); err != nil {
		return errTriageStorage
	}
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='operator' AND NOT revoked FOR SHARE`, p.issuer, p.subject).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		}
		return errTriageStorage
	}
	var client string
	if err = tx.QueryRow(ctx, `SELECT client_id FROM portal_requests WHERE id=$1`, id).Scan(&client); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return errTriageStorage
	}
	var keyOwner bool
	err = tx.QueryRow(ctx, `SELECT operator_issuer=$4 AND operator_subject=$5 FROM portal_proposals WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key, p.issuer, p.subject).Scan(&keyOwner)
	if err == nil && !keyOwner {
		return errProposalConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	saved, err := scanProposal(tx.QueryRow(ctx, `SELECT `+proposalColumns+` FROM portal_proposals p WHERE p.request_id=$1 AND p.client_id=$2 AND p.submission_key=$3`, id, client, d.Key))
	if err == nil {
		if saved.Expected != d.Expected || saved.Scope != d.Scope || saved.Deliverables != d.Deliverables || saved.Currency != d.Currency || saved.OneTimeMinor != d.OneTimeMinor || saved.MonthlyMinor != d.MonthlyMinor || saved.OneTimeTerms != d.OneTimeTerms || saved.MonthlyTerms != d.MonthlyTerms {
			return errProposalConflict
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return errTriageStorage
		}
		var version int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM portal_proposals WHERE request_id=$1 AND client_id=$2`, id, client).Scan(&version); err != nil {
			return errTriageStorage
		}
		if version != d.Expected || version >= 50 {
			return errProposalConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO portal_proposals(request_id,client_id,version,expected_version,submission_key,operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, client, version+1, d.Expected, d.Key, p.issuer, p.subject, d.Scope, d.Deliverables, d.Currency, d.OneTimeMinor, d.MonthlyMinor, d.OneTimeTerms, d.MonthlyTerms); err != nil {
			return errTriageStorage
		}
	}
	if !s.proposalsReady(ctx) || !time.Now().Before(p.expires) {
		return ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}

func (b *browser) submitProposal(w http.ResponseWriter, r *http.Request) {
	p := b.principal(r.Context())
	if err := b.foundation.proposalOperator(r.Context(), p); err != nil {
		b.triageError(w, r, err)
		return
	}
	d, valid := parseProposal(r)
	if !valid {
		b.render(w, r, 400, "proposal-invalid", "", "")
		return
	}
	err := b.foundation.persistProposal(r.Context(), p, r.PathValue("id"), d)
	if errors.Is(err, errProposalConflict) {
		b.render(w, r, 409, "proposal-conflict", "", "")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	http.Redirect(w, r, "/portal/operator/requests/"+r.PathValue("id")+"?lang="+language(r), http.StatusSeeOther)
}

func (b *browser) proposalPresentation(r *http.Request, page *triagePageData, rows []projectProposal) {
	page.ProposalsEnabled = true
	page.Proposals = rows
	page.ProposalKey = secret()
	page.CSRF = nosurf.Token(r)
	if len(rows) > 0 {
		page.ExpectedVersion = rows[0].Version
	}
}
