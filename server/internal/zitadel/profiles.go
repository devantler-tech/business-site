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
			name := strings.TrimSpace(account.Human.Profile.DisplayName)
			if len(name) > 800 || strings.ContainsFunc(name, unicode.IsControl) {
				name = ""
			}
			email := account.Human.Email.Address
			if address, err := mail.ParseAddress(email); err != nil || address.Name != "" || address.Address != email || len(email) > 254 {
				email = ""
			}
			profiles[account.ID] = customeridentity.Profile{Name: name, Email: email}
		}
	}
	return profiles, nil
}
