package identity

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

const customerAccountsPath = "/portal/operator/customer-accounts"

type customerAccountRow struct {
	Subject, Name, Email, URL string
	Workspaces                []customerAccountWorkspace
	Status                    string
	Deadline                  *time.Time
}

type customerAccountWorkspace struct {
	ID, Name, RemoveURL string
	Invited, Revoked    bool
}

func (row customerAccountRow) RetentionDate() string {
	return row.Deadline.In(copenhagen).Format(time.DateOnly)
}

func (row customerAccountRow) StatusLabel(lang string) string {
	var en, da string
	switch row.Status {
	case "active":
		en, da = "Active", "Aktiv"
	case "former":
		en, da = "Former customer", "Tidligere kunde"
	case "invited":
		en, da = "Invited", "Inviteret"
	default:
		en, da = "No workspace", "Intet arbejdsområde"
	}
	if lang == "da" {
		return da
	}
	return en
}

type customerAccountsPage struct {
	pageData
	Accounts              []customerAccountRow
	Account               *customerAccountRow
	Query, Previous, Next string
	BackURL, DeleteURL    string
	ConfirmDeletion       bool
	Page                  uint64
	Remove                *customerAccountWorkspace
}

func customerAccountID(subject string) string {
	if len(subject) > 512 {
		return ""
	}
	return customeridentity.DexUser(subject, customeridentity.ZitadelConnectorID)
}

func (b *browser) operatorCustomerAccountsPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "customers.accounts.view", func(ctx context.Context, client string) ([]byte, error) {
		if humanOperatorAuthority(ctx) == nil || b.foundation.customers == nil {
			return nil, ErrDenied
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, errOperatorInput
		}
		data := customerAccountsPage{pageData: b.operatorData(r, "operator-customer-accounts", client)}
		if err := b.loadCustomerAccounts(ctx, &data, query); err != nil {
			return nil, err
		}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", data)
		return buffer.Bytes(), err
	})
	switch status {
	case http.StatusOK, http.StatusBadRequest, http.StatusServiceUnavailable:
		b.writeOperatorActionPage(w, r, status, result)
	default:
		b.render(w, r, http.StatusForbidden, "error", "", "")
	}
}

func readCustomerSearch(query url.Values) (string, uint64, error) {
	search := strings.TrimSpace(query.Get("q"))
	if utf8.RuneCountInString(search) > 100 || !utf8.ValidString(search) || strings.ContainsFunc(search, unicode.IsControl) {
		return "", 0, errOperatorInput
	}
	page := uint64(1)
	if query.Has("page") {
		var err error
		page, err = strconv.ParseUint(query.Get("page"), 10, 32)
		if err != nil || page == 0 {
			return "", 0, errOperatorInput
		}
	}
	return search, page, nil
}

func (b *browser) loadCustomerAccounts(ctx context.Context, data *customerAccountsPage, query url.Values) error {
	var err error
	data.Query, data.Page, err = readCustomerSearch(query)
	if err != nil {
		return err
	}
	subject, view := query.Get("account"), query.Get("view")
	workspace := query.Get("workspace")
	if view != "" && ((view != "remove" && view != "delete-account") || subject == "") {
		return errOperatorInput
	}
	if workspace != "" && (view != "remove" || !validCustomerWorkspace(workspace)) {
		return errOperatorInput
	}
	target := url.Values{"lang": {data.Lang}}
	if data.Query != "" {
		target.Set("q", data.Query)
	}
	if data.Page > 1 {
		target.Set("page", strconv.FormatUint(data.Page, 10))
	}
	base := b.visible(customerAccountsPath) + "?"
	data.BackURL = base + target.Encode()

	var accounts []customeridentity.Account
	if subject != "" {
		id := customerAccountID(subject)
		if id == "" || !b.foundation.canInvite(subject, "client") {
			return errOperatorInput
		}
		setOperatorTarget(ctx, subject)
		account, err := b.foundation.customers.Account(ctx, id)
		if err != nil {
			return err
		}
		accounts = []customeridentity.Account{account}
	} else {
		accountsPage, more, err := b.foundation.customers.SearchAccounts(ctx, data.Query, (data.Page-1)*customeridentity.AccountPageSize)
		if err != nil {
			return err
		}
		accounts = accountsPage
		if data.Page > 1 {
			target.Set("page", strconv.FormatUint(data.Page-1, 10))
			data.Previous = base + target.Encode()
		}
		if more {
			target.Set("page", strconv.FormatUint(data.Page+1, 10))
			data.Next = base + target.Encode()
		}
		target.Set("page", strconv.FormatUint(data.Page, 10))
	}

	rows, err := b.customerAccountAccess(ctx, accounts, data.Lang)
	if err != nil {
		return err
	}
	data.Accounts = rows
	if subject != "" {
		if len(data.Accounts) != 1 {
			return errOperatorInput
		}
		data.Account = &data.Accounts[0]
		data.Title = data.Account.Name
		target.Set("account", subject)
		canDelete := b.foundation.customerDeletion != nil && subject != previewSubject
		if view == "delete-account" && !canDelete {
			return errOperatorInput
		}
		if view == "delete-account" {
			data.ConfirmDeletion = true
			data.BackURL = base + target.Encode()
			target.Set("view", view)
		} else if canDelete && view == "" {
			target.Set("view", "delete-account")
			data.DeleteURL = base + target.Encode()
			target.Del("view")
		}
		if view == "remove" {
			for _, candidate := range data.Account.Workspaces {
				if !candidate.Invited && (candidate.ID == workspace || workspace == "" && len(data.Account.Workspaces) == 1) {
					data.Remove = &candidate
				}
			}
			if data.Remove == nil {
				return errOperatorInput
			}
			data.BackURL = base + target.Encode()
			target.Set("view", "remove")
			target.Set("workspace", data.Remove.ID)
		} else if view == "" {
			target.Set("view", "remove")
			for i := range data.Account.Workspaces {
				candidate := &data.Account.Workspaces[i]
				if !candidate.Invited && validCustomerWorkspace(candidate.ID) {
					target.Set("workspace", candidate.ID)
					candidate.RemoveURL = base + target.Encode()
				}
			}
			target.Del("view")
			target.Del("workspace")
		}
	} else {
		for i := range data.Accounts {
			target.Set("account", data.Accounts[i].Subject)
			data.Accounts[i].URL = base + target.Encode()
		}
		target.Del("account")
	}
	other := "da"
	if data.Lang == "da" {
		other = "en"
	}
	target.Set("lang", other)
	data.LanguageURL = base + target.Encode()
	return nil
}

type customerWorkspaceAccess struct {
	Subject, Workspace, WorkspaceName string
	Invited, Revoked, Former          bool
	Deadline                          *time.Time
}

const customerAccountAccessSQL = `WITH access AS (
 SELECT subject,client_id,false AS invited,revoked FROM portal_memberships
 WHERE issuer=$1 AND subject=ANY($2::text[]) AND role='client' AND client_id NOT IN ($3,$4)
 UNION
 SELECT i.subject,i.client_id,true,false
 FROM portal_invitations i
 WHERE i.issuer=$1 AND i.subject=ANY($2::text[]) AND i.role='client'
  AND NOT i.revoked AND i.expires_at>now() AND i.client_id NOT IN ($3,$4)
  AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.issuer=i.issuer AND m.subject=i.subject AND m.client_id=i.client_id)
), open_access AS (
 SELECT a.*,COALESCE(w.display_name,a.client_id) AS name FROM access a
 LEFT JOIN portal_workspace_settings w ON w.workspace_id=a.client_id WHERE w.closed_at IS NULL
), former AS (
 SELECT DISTINCT ON (h.subject) h.subject,h.incarnation,c.remove_by
 FROM portal_membership_transitions h
 LEFT JOIN portal_customer_account_closures c ON c.membership_event=h.incarnation
 WHERE h.issuer=$1 AND h.subject=ANY($2::text[]) AND h.role='client' AND h.kind='delete' AND h.client_id NOT IN ($3,$4)
 ORDER BY h.subject,h.recorded_at DESC,h.incarnation
)
 SELECT person.subject, COALESCE(a.client_id,''),COALESCE(a.name,''),
 COALESCE(a.invited,false),COALESCE(a.revoked,false),
 h.incarnation IS NOT NULL, h.remove_by
 FROM unnest($2::text[]) AS person(subject)
 LEFT JOIN open_access a ON a.subject=person.subject
 LEFT JOIN former h ON h.subject=person.subject
 WHERE NOT EXISTS (SELECT 1 FROM portal_memberships op WHERE op.issuer=$1 AND op.subject=person.subject AND op.role='operator')
 ORDER BY a.name,a.client_id`

func (b *browser) customerAccountAccess(ctx context.Context, accounts []customeridentity.Account, lang string) ([]customerAccountRow, error) {
	subjects := make([]string, 0, len(accounts))
	for _, account := range accounts {
		subject := customeridentity.DexSubject(account.ID, customeridentity.ZitadelConnectorID)
		if b.foundation.canInvite(subject, "client") {
			subjects = append(subjects, subject)
		}
	}
	if len(subjects) == 0 {
		return nil, nil
	}
	rows, err := b.foundation.pool.Query(ctx, customerAccountAccessSQL, b.foundation.broker.issuer, subjects, operatorWorkspace, previewWorkspace)
	if err != nil {
		return nil, err
	}
	records, err := pgx.CollectRows(rows, pgx.RowToStructByPos[customerWorkspaceAccess])
	if err != nil {
		return nil, err
	}
	access := make(map[string][]customerWorkspaceAccess, len(accounts))
	for _, record := range records {
		access[record.Subject] = append(access[record.Subject], record)
	}
	out := make([]customerAccountRow, 0, len(accounts))
	for _, account := range accounts {
		subject := customeridentity.DexSubject(account.ID, customeridentity.ZitadelConnectorID)
		records, ok := access[subject]
		if !ok {
			continue
		}
		out = append(out, accountRow(account, records, lang))
	}
	return out, nil
}

func accountRow(account customeridentity.Account, access []customerWorkspaceAccess, lang string) customerAccountRow {
	name, email := account.Name, account.Email
	if name == "" || name == email {
		name, email = email, ""
	}
	if name == "" {
		name = "Unnamed customer"
		if lang == "da" {
			name = "Kunde uden navn"
		}
	}
	row := customerAccountRow{Subject: access[0].Subject, Name: name, Email: email}
	var member, invited, revoked bool
	for _, record := range access {
		if record.Workspace == "" {
			continue
		}
		row.Workspaces = append(row.Workspaces, customerAccountWorkspace{
			ID: record.Workspace, Name: record.WorkspaceName, Invited: record.Invited, Revoked: record.Revoked,
		})
		invited = invited || record.Invited
		revoked = revoked || record.Revoked
		member = member || !record.Invited && !record.Revoked
	}
	switch {
	case member:
		row.Status = "active"
	case invited:
		row.Status = "invited"
	case revoked || access[0].Former:
		row.Status, row.Deadline = "former", access[0].Deadline
	default:
		row.Status = "unassigned"
	}
	return row
}
