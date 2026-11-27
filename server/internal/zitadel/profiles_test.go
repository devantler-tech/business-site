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
