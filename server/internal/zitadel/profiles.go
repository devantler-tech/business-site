package zitadel

import (
	"context"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// Profiles looks up only the requested humans in the configured organization.
// Keep batches small enough for the client's bounded JSON response reader.
func (c *Client) Profiles(ctx context.Context, ids []string) (map[string]customeridentity.Profile, error) {
	profiles := make(map[string]customeridentity.Profile, len(ids))
	for _, id := range ids {
		if !identifier.MatchString(id) {
			return nil, ErrUnavailable
		}
	}
	for batch := range slices.Chunk(ids, 16) {
		query := map[string]any{
			"query": map[string]string{"limit": strconv.Itoa(len(batch))},
			"queries": []any{
				map[string]any{"organizationIdQuery": map[string]string{"organizationId": c.org}},
				map[string]any{"inUserIdsQuery": map[string][]string{"userIds": batch}},
			},
		}
		var found struct {
			Result []user `json:"result"`
		}
		if _, err := c.call(ctx, http.MethodPost, "/v2/users", query, &found, nil); err != nil {
			return profiles, err
		}
		for _, account := range found.Result {
			_, duplicate := profiles[account.ID]
			if duplicate || !slices.Contains(batch, account.ID) || account.Details.Owner != c.org || account.Human == nil {
				return nil, ErrUnavailable
			}
			profiles[account.ID] = profileFor(account)
		}
	}
	return profiles, nil
}

func profileFor(account user) customeridentity.Profile {
	name := strings.TrimSpace(account.Human.Profile.DisplayName)
	if len(name) > 800 || strings.ContainsFunc(name, unicode.IsControl) {
		name = ""
	}
	email := account.Human.Email.Address
	if address, err := mail.ParseAddress(email); err != nil || address.Name != "" || address.Address != email || len(email) > 254 {
		email = ""
	}
	return customeridentity.Profile{
		Name: name, Email: email,
		EmailVerified: email != "" && account.State == "USER_STATE_ACTIVE" && account.Human.Email.Verified,
	}
}

func (c *Client) Account(ctx context.Context, id string) (customeridentity.Account, error) {
	account, err := c.readHuman(ctx, id)
	if err != nil || account.State == "USER_STATE_DELETED" {
		return customeridentity.Account{}, ErrUnavailable
	}
	return accountFor(account), nil
}

// SearchAccounts lets the identity provider search and page its own directory.
// Validate every returned identity before making any profile available.
func (c *Client) SearchAccounts(ctx context.Context, nameOrEmail string, offset uint64) ([]customeridentity.Account, bool, error) {
	const limit = customeridentity.AccountPageSize + 1
	queries := []any{
		map[string]any{"organizationIdQuery": map[string]string{"organizationId": c.org}},
		map[string]any{"typeQuery": map[string]string{"type": "TYPE_HUMAN"}},
		map[string]any{"notQuery": map[string]any{"query": map[string]any{"stateQuery": map[string]string{"state": "USER_STATE_DELETED"}}}},
	}
	if nameOrEmail != "" {
		const method = "TEXT_QUERY_METHOD_CONTAINS_IGNORE_CASE"
		queries = append(queries, map[string]any{"orQuery": map[string]any{"queries": []any{
			map[string]any{"displayNameQuery": map[string]string{"displayName": nameOrEmail, "method": method}},
			map[string]any{"emailQuery": map[string]string{"emailAddress": nameOrEmail, "method": method}},
		}}})
	}
	query := map[string]any{
		"query":         map[string]any{"offset": strconv.FormatUint(offset, 10), "limit": limit, "asc": true},
		"sortingColumn": "USER_FIELD_NAME_DISPLAY_NAME",
		"queries":       queries,
	}
	var found struct {
		Result []user `json:"result"`
	}
	if _, err := c.call(ctx, http.MethodPost, "/v2/users", query, &found, nil); err != nil || len(found.Result) > limit {
		return nil, false, ErrUnavailable
	}
	accounts := make([]customeridentity.Account, 0, len(found.Result))
	seen := make(map[string]bool, len(found.Result))
	for _, account := range found.Result {
		if seen[account.ID] || !identifier.MatchString(account.ID) || account.Details.Owner != c.org || account.Human == nil || account.State == "USER_STATE_DELETED" {
			return nil, false, ErrUnavailable
		}
		seen[account.ID] = true
		accounts = append(accounts, accountFor(account))
	}
	if len(accounts) == limit {
		return accounts[:customeridentity.AccountPageSize], true, nil
	}
	return accounts, false, nil
}

func accountFor(account user) customeridentity.Account {
	return customeridentity.Account{ID: account.ID, Profile: profileFor(account)}
}
