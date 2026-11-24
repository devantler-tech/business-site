package zitadel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestProfilesRequireExactCustomerOrganizationAndIDs(t *testing.T) {
	good := `{"userId":"user-one","details":{"resourceOwner":"customers"},"human":{"profile":{"displayName":"Ada Lovelace"},"email":{"email":"ada@example.test"}}}`
	query := map[string]any{
		"query": map[string]any{"limit": "2"},
		"queries": []any{
			map[string]any{"organizationIdQuery": map[string]any{"organizationId": "customers"}},
			map[string]any{"inUserIdsQuery": map[string]any{"userIds": []any{"user-one", "user-two"}}},
		},
	}
	for _, test := range []struct {
		name, rows string
		refuse     bool
	}{
		{"matching human", good, false},
		{"other organization", `{"userId":"user-one","details":{"resourceOwner":"staff"},"human":{}}`, true},
		{"unexpected person", `{"userId":"user-three","details":{"resourceOwner":"customers"},"human":{}}`, true},
		{"duplicate", good + "," + good, true},
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
				profiles["user-one"] != (customeridentity.Profile{Name: "Ada Lovelace", Email: "ada@example.test"}) {
				t.Fatalf("matching customer profile was not returned: %v", err)
			}
		})
	}
}
