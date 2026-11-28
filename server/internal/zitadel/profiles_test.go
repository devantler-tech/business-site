package zitadel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestProfilesRequireExactCustomerOrganizationAndIDs(t *testing.T) {
	good := `{"userId":"user-one","state":"USER_STATE_ACTIVE","details":{"resourceOwner":"customers"},"human":{"profile":{"displayName":"Ada Lovelace"},"email":{"email":"ada@example.test","isVerified":true}}}`
	query := map[string]any{
		"query": map[string]any{"limit": "2"},
		"queries": []any{
			map[string]any{"organizationIdQuery": map[string]any{"organizationId": "customers"}},
			map[string]any{"inUserIdsQuery": map[string]any{"userIds": []any{"user-one", "user-two"}}},
		},
	}
	for _, test := range []struct {
		name, rows       string
		refuse, verified bool
	}{
		{"matching human", good, false, true},
		{"missing verification", strings.Replace(good, `,"isVerified":true`, "", 1), false, false},
		{"inactive human", strings.Replace(good, "USER_STATE_ACTIVE", "USER_STATE_INACTIVE", 1), false, false},
		{"other organization", `{"userId":"user-one","details":{"resourceOwner":"staff"},"human":{}}`, true, false},
		{"unexpected person", `{"userId":"user-three","details":{"resourceOwner":"customers"},"human":{}}`, true, false},
		{"duplicate", good + "," + good, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var received any
				if r.Method != http.MethodPost || r.URL.Path != "/v2/users" ||
					json.NewDecoder(r.Body).Decode(&received) != nil || !reflect.DeepEqual(received, query) {
					t.Error("customer profile request omitted the exact organization or requested IDs")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `{"result":[%s]}`, test.rows)
			})
			profiles, err := c.Profiles(t.Context(), []string{"user-one", "user-two"})
			if test.refuse {
				if !errors.Is(err, ErrUnavailable) || len(profiles) != 0 {
					t.Fatalf("unexpected account was returned: %v", err)
				}
			} else if err != nil || len(profiles) != 1 ||
				profiles["user-one"] != (customeridentity.Profile{Name: "Ada Lovelace", Email: "ada@example.test", EmailVerified: test.verified}) {
				t.Fatalf("matching customer profile was not returned: %v", err)
			}
		})
	}
}

func TestSearchAccountsScopesAndPagesHumanCustomers(t *testing.T) {
	rows := make([]string, 0, customeridentity.AccountPageSize+1)
	for i := 0; i <= customeridentity.AccountPageSize; i++ {
		rows = append(rows, fmt.Sprintf(`{"userId":"user-%d","state":"USER_STATE_INACTIVE","details":{"resourceOwner":"customers"},"human":{"profile":{"displayName":"Ada Example"}}}`, i))
	}
	const organization = `"organizationIdQuery":{"organizationId":"customers"}`
	const searchEmail = `"emailQuery":{"emailAddress":"ada@example.test","method":"TEXT_QUERY_METHOD_CONTAINS_IGNORE_CASE"}`
	for _, test := range []struct {
		name, response string
		refuse         bool
	}{
		{"full page", strings.Join(rows, ","), false},
		{"outside organization", strings.Replace(rows[0], "customers", "staff", 1), true},
		{"machine account", `{"userId":"machine","details":{"resourceOwner":"customers"},"machine":{}}`, true},
		{"duplicate", rows[0] + "," + rows[0], true},
		{"deleted", strings.Replace(rows[0], "USER_STATE_INACTIVE", "USER_STATE_DELETED", 1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var received json.RawMessage
				if r.Method != http.MethodPost || r.URL.Path != "/v2/users" ||
					r.Header.Get("x-zitadel-orgid") != "customers" || json.NewDecoder(r.Body).Decode(&received) != nil {
					t.Error("incorrect customer account request")
					http.Error(w, "", http.StatusBadRequest)
					return
				}
				for _, bound := range []string{organization, searchEmail, `"typeQuery":{"type":"TYPE_HUMAN"}`, `"offset":"25"`, `"limit":26`} {
					if !strings.Contains(string(received), bound) {
						t.Errorf("query lacks %s", bound)
					}
				}
				_, _ = fmt.Fprintf(w, `{"result":[%s]}`, test.response)
			})
			accounts, more, err := c.SearchAccounts(t.Context(), "ada@example.test", 25)
			if test.refuse {
				if !errors.Is(err, ErrUnavailable) || more || len(accounts) > 0 {
					t.Fatalf("unexpected account was returned: %v", err)
				}
			} else if err != nil || !more || len(accounts) != customeridentity.AccountPageSize || accounts[0].ID != "user-0" {
				t.Fatalf("paged customer directory was not returned: %v", err)
			}
		})
	}
}
