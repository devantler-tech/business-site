package identity

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const auditPageSize = 25
const auditSearchDate = "2006-01-02"

type auditSearch struct {
	Query, Actor, Outcome string
	From, To              string
	Start, End            *time.Time
	Cursor                time.Time
	ID                    pgtype.UUID
	Newer                 bool
}

type auditSearchPage struct {
	pageData
	Search auditSearch
	Older  string
	Newer  string
	Viewer string
}

type auditSearchEvent struct {
	auditEntry
	ID string
}

type auditChoice struct {
	Value, Label string
	Selected     bool
}

func (p auditSearchPage) ActorChoices() []auditChoice {
	if p.Lang == "da" {
		return p.actorChoices("Operatør", "Anden aktør")
	}
	return p.actorChoices("Operator", "Other actor")
}

func (p auditSearchPage) actorChoices(operator, other string) []auditChoice {
	return []auditChoice{
		{"operator", operator, p.Search.Actor == "operator"},
		{"agent", "Agent", p.Search.Actor == "agent"},
		{"other", other, p.Search.Actor == "other"},
	}
}

func (p auditSearchPage) OutcomeChoices() []auditChoice {
	if p.Lang == "da" {
		return p.outcomeChoices("Tilladt", "Afvist", "Fejl")
	}
	return p.outcomeChoices("Allowed", "Refused", "Failed")
}

func (p auditSearchPage) outcomeChoices(allowed, refused, failed string) []auditChoice {
	return []auditChoice{
		{"allowed", allowed, p.Search.Outcome == "allowed"},
		{"refused", refused, p.Search.Outcome == "refused"},
		{"failed", failed, p.Search.Outcome == "failed"},
	}
}

func readAuditSearch(rawQuery string) (auditSearch, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return auditSearch{}, errOperatorInput
	}
	for key, values := range query {
		switch key {
		case "lang", "q", "actor", "outcome", "from", "to", "before", "after":
			if len(values) == 1 && len(values[0]) <= 120 && !strings.ContainsFunc(values[0], unicode.IsControl) {
				continue
			}
		}
		return auditSearch{}, errOperatorInput
	}
	f := auditSearch{Query: strings.TrimSpace(query.Get("q")), Actor: query.Get("actor"), Outcome: query.Get("outcome"), From: query.Get("from"), To: query.Get("to")}
	switch f.Actor {
	case "", "operator", "agent", "other":
	default:
		return auditSearch{}, errOperatorInput
	}
	switch f.Outcome {
	case "", "allowed", "refused", "failed":
	default:
		return auditSearch{}, errOperatorInput
	}
	if f.Start, err = auditDate(f.From, false); err != nil {
		return auditSearch{}, err
	}
	if f.End, err = auditDate(f.To, true); err != nil || (f.Start != nil && f.End != nil && !f.Start.Before(*f.End)) {
		return auditSearch{}, errOperatorInput
	}
	before, after := query.Get("before"), query.Get("after")
	if query.Has("before") && query.Has("after") {
		return auditSearch{}, errOperatorInput
	}
	if query.Has("before") || query.Has("after") {
		cursor := before + after
		at, id, ok := strings.Cut(cursor, ",")
		f.Cursor, err = time.Parse(time.RFC3339Nano, at)
		if !ok || err != nil || f.ID.Scan(id) != nil || !f.ID.Valid {
			return auditSearch{}, errOperatorInput
		}
		f.Newer = query.Has("after")
	}
	return f, nil
}

func auditDate(value string, through bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	date, err := time.Parse(auditSearchDate, value)
	if err != nil || date.Format(auditSearchDate) != value {
		return nil, errOperatorInput
	}
	if through {
		date = date.AddDate(0, 0, 1)
	}
	return &date, nil
}

func (f auditSearch) pageLink(path, lang, direction string, entry auditSearchEvent) string {
	params := url.Values{"lang": {lang}, direction: {entry.At.UTC().Format(time.RFC3339Nano) + "," + entry.ID}}
	for key, value := range map[string]string{"q": f.Query, "actor": f.Actor, "outcome": f.Outcome, "from": f.From, "to": f.To} {
		if value != "" {
			params.Set(key, value)
		}
	}
	return path + "?" + params.Encode()
}

func (b *browser) searchOperatorAudit(ctx context.Context, f auditSearch) ([]auditSearchEvent, bool, error) {
	order := `AND ($7::uuid IS NULL OR (at,id)<($6::timestamptz,$7)) ORDER BY at DESC,id DESC`
	if f.Newer {
		order = `AND ($7::uuid IS NULL OR (at,id)>($6::timestamptz,$7)) ORDER BY at,id`
	}
	rows, err := b.foundation.pool.Query(ctx, `
 SELECT id::text,at,actor_kind,actor_subject,action,target,outcome FROM portal_operator_audit
 WHERE ($1='' OR strpos(lower(action),lower($1))>0 OR strpos(lower(target),lower($1))>0
 OR strpos(lower(actor_subject),lower($1))>0)
 AND ($2='' OR actor_kind=$2) AND ($3='' OR outcome=$3)
 AND ($4::timestamptz IS NULL OR at>=$4) AND ($5::timestamptz IS NULL OR at<$5)
 `+order+` LIMIT $8`, f.Query, f.Actor, f.Outcome, f.Start, f.End, f.Cursor, f.ID, auditPageSize+1)
	if err != nil {
		return nil, false, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (auditSearchEvent, error) {
		var e auditSearchEvent
		err := row.Scan(&e.ID, &e.At, &e.Kind, &e.Subject, &e.Action, &e.Target, &e.Outcome)
		return e, err
	})
	more := len(list) > auditPageSize
	if more {
		list = list[:auditPageSize]
	}
	if f.Newer {
		slices.Reverse(list)
	}
	return list, more, err
}

func (b *browser) populateAuditPage(ctx context.Context, page *auditSearchPage) error {
	authority := humanOperatorAuthority(ctx)
	if authority == nil {
		return ErrDenied
	}
	page.Viewer = authority.principal.subject
	list, more, err := b.searchOperatorAudit(ctx, page.Search)
	if err != nil || len(list) == 0 {
		return err
	}
	for _, entry := range list {
		page.Audit = append(page.Audit, entry.auditEntry)
	}
	path := b.visible("/portal/operator/audit")
	if more && page.Search.Newer || page.Search.ID.Valid && !page.Search.Newer {
		page.Newer = page.Search.pageLink(path, page.Lang, "after", list[0])
	}
	if more && !page.Search.Newer || page.Search.ID.Valid && page.Search.Newer {
		page.Older = page.Search.pageLink(path, page.Lang, "before", list[len(list)-1])
	}
	return nil
}
