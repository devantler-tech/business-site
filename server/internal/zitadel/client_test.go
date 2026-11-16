package zitadel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func allowDirectoryEffect() bool { return true }

func TestDirectoryRechecksAuthorityAfterProviderRead(t *testing.T) {
	for _, action := range []string{"create", "deactivate", "reactivate"} {
		t.Run(action, func(t *testing.T) {
			var allowed atomic.Bool
			allowed.Store(true)
			writes := 0
			c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/users" {
					allowed.Store(false)
					_, _ = fmt.Fprint(w, `{"result":[]}`)
					return
				}
				if r.Method == "GET" {
					allowed.Store(false)
					state := "USER_STATE_ACTIVE"
					if action == "reactivate" {
						state = "USER_STATE_INACTIVE"
					}
					_, _ = fmt.Fprintf(w, `{"user":{"userId":"user-42","details":{"resourceOwner":"customers"},"state":%q,"human":{}}}`, state)
					return
				}
				writes++
				_, _ = fmt.Fprint(w, `{"userId":"user-42","details":{"resourceOwner":"customers"}}`)
			})
			permit := func() bool { return allowed.Load() }
			var err error
			switch action {
			case "create":
				_, err = c.EnsureUser(t.Context(), "Ada Lovelace", "ada@example.test", permit)
			case "deactivate":
				err = c.DeactivateUser(t.Context(), "user-42", permit)
			case "reactivate":
				err = c.ReactivateUser(t.Context(), "user-42", permit)
			}
			if !errors.Is(err, ErrUnavailable) || writes != 0 {
				t.Fatalf("expired authority reached provider effect: writes=%d err=%v", writes, err)
			}
		})
	}
}

func directoryFixture(t *testing.T, serve http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("x-zitadel-orgid") != "customers" {
			t.Error("request omitted its credential or exact organization")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		serve(w, r)
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "customers", func() (string, error) { return "fixture-token", nil }, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEnsureUserCreatesOrReusesWithinOrganization(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			creates := 0
			c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/users":
					var search struct {
						Queries []json.RawMessage `json:"queries"`
						Query   struct {
							Limit uint64 `json:"limit,string"`
						} `json:"query"`
					}
					if json.NewDecoder(r.Body).Decode(&search) != nil || search.Query.Limit != 2 || len(search.Queries) != 2 {
						t.Error("lookup was not bounded and organization-filtered")
					}
					if existing {
						_, _ = fmt.Fprint(w, `{"result":[{"userId":"user-42","details":{"resourceOwner":"customers"},"human":{"email":{"email":"ada@example.test"}}}]}`)
					} else {
						_, _ = fmt.Fprint(w, `{"result":[]}`)
					}
				case "/management/v1/users/human":
					creates++
					var body map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["password"] != nil || string(body["userName"]) != `"ada@example.test"` {
						t.Error("creation supplied a password or different login name")
					}
					var email struct {
						Email    string `json:"email"`
						Verified bool   `json:"isEmailVerified"`
					}
					if json.Unmarshal(body["email"], &email) != nil || email.Email != "ada@example.test" || email.Verified {
						t.Error("creation suppressed native initialization mail")
					}
					_, _ = fmt.Fprint(w, `{"userId":"user-42","details":{"resourceOwner":"customers"}}`)
				default:
					t.Error("unexpected provider route")
					w.WriteHeader(404)
				}
			})
			id, err := c.EnsureUser(t.Context(), "Ada Lovelace", "ada@example.test", allowDirectoryEffect)
			if err != nil || id != "user-42" || (existing && creates != 0) || (!existing && creates != 1) {
				t.Fatalf("ensure user: id=%q creates=%d err=%v", id, creates, err)
			}
		})
	}
}

func TestEnsureUserFailsClosedOnProviderErrorsAndAmbiguousOwners(t *testing.T) {
	for _, response := range []string{
		`{"result":[{"userId":"foreign","details":{"resourceOwner":"other"},"human":{"email":{"email":"ada@example.test"}}}]}`,
		`{"result":[{"userId":"wrong","details":{"resourceOwner":"customers"},"human":{"email":{"email":"other@example.test"}}}]}`,
		`{"result":[{"userId":"one"},{"userId":"two"}]}`,
		`{"result":[]}`, `not-json`, strings.Repeat("x", 65537),
	} {
		t.Run(fmt.Sprint(len(response)), func(t *testing.T) {
			c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/users" {
					_, _ = fmt.Fprint(w, response)
				} else {
					w.WriteHeader(503)
					_, _ = fmt.Fprint(w, "private-provider-diagnostic")
				}
			})
			if id, err := c.EnsureUser(t.Context(), "Ada Lovelace", "ada@example.test", allowDirectoryEffect); id != "" || !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private-provider-diagnostic") {
				t.Fatalf("unsafe provider response accepted or exposed: id=%q err=%v", id, err)
			}
		})
	}
}

func TestEnsureUserRechecksConflictWithoutDuplicateCreation(t *testing.T) {
	reads, creates := 0, 0
	c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/users" {
			reads++
			if reads == 1 {
				_, _ = fmt.Fprint(w, `{"result":[]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"result":[{"userId":"existing","details":{"resourceOwner":"customers"},"human":{"email":{"email":"ada@example.test"}}}]}`)
			}
		} else {
			creates++
			w.WriteHeader(http.StatusConflict)
		}
	})
	if id, err := c.EnsureUser(t.Context(), "Ada Lovelace", "ada@example.test", allowDirectoryEffect); err != nil || id != "existing" || creates != 1 || reads != 2 {
		t.Fatalf("conflict was not safely resolved: id=%q reads=%d creates=%d err=%v", id, reads, creates, err)
	}
}

func TestStateChangesCheckOrganizationAndNeverDelete(t *testing.T) {
	for _, owner := range []string{"customers", "other"} {
		for _, active := range []bool{false, true} {
			t.Run(owner+fmt.Sprint(active), func(t *testing.T) {
				writes := 0
				c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						state := "USER_STATE_ACTIVE"
						if active {
							state = "USER_STATE_INACTIVE"
						}
						_, _ = fmt.Fprintf(w, `{"user":{"userId":"user-42","details":{"resourceOwner":%q},"state":%q,"human":{"email":{"email":"ada@example.test"}}}}`, owner, state)
						return
					}
					want := "/management/v1/users/user-42/_deactivate"
					if active {
						want = "/management/v1/users/user-42/_reactivate"
					}
					if r.Method != "POST" || r.URL.Path != want {
						t.Error("state change used an unexpected route")
					}
					writes++
					_, _ = fmt.Fprint(w, `{}`)
				})
				var err error
				if active {
					err = c.ReactivateUser(t.Context(), "user-42", allowDirectoryEffect)
				} else {
					err = c.DeactivateUser(t.Context(), "user-42", allowDirectoryEffect)
				}
				if (owner == "customers" && (err != nil || writes != 1)) || (owner != "customers" && (!errors.Is(err, ErrUnavailable) || writes != 0)) {
					t.Fatalf("state change owner was not enforced: writes=%d err=%v", writes, err)
				}
			})
		}
	}
}

func TestStateChangeRetriesConvergeAndInitialCannotActivate(t *testing.T) {
	for _, state := range []string{"USER_STATE_ACTIVE", "USER_STATE_INACTIVE", "USER_STATE_INITIAL", ""} {
		for _, activate := range []bool{false, true} {
			t.Run(state+fmt.Sprint(activate), func(t *testing.T) {
				writes := 0
				c := directoryFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "GET" {
						_, _ = fmt.Fprintf(w, `{"user":{"userId":"user-42","details":{"resourceOwner":"customers"},"state":%q,"human":{}}}`, state)
						return
					}
					writes++
					_, _ = fmt.Fprint(w, `{}`)
				})
				var err error
				if activate {
					err = c.ReactivateUser(t.Context(), "user-42", allowDirectoryEffect)
				} else {
					err = c.DeactivateUser(t.Context(), "user-42", allowDirectoryEffect)
				}
				if state == "USER_STATE_INITIAL" || state == "" {
					if !errors.Is(err, ErrUnavailable) || writes != 0 {
						t.Fatal("unknown or initial state was changed")
					}
					return
				}
				wantWrites := 1
				if (activate && state == "USER_STATE_ACTIVE") || (!activate && state == "USER_STATE_INACTIVE") {
					wantWrites = 0
				}
				if err != nil || writes != wantWrites {
					t.Fatalf("retry did not converge: writes=%d err=%v", writes, err)
				}
			})
		}
	}
}

func TestCredentialRotationAndRedirectRefusal(t *testing.T) {
	var received []string
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Get("Authorization"))
		http.Redirect(w, r, "https://unrelated.example.test/", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(s.Close)
	token := "old"
	c, err := New(s.URL, "customers", func() (string, error) { return token, nil }, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"old", "new"} {
		token = value
		if _, err := c.EnsureUser(context.Background(), "Ada Lovelace", "ada@example.test", allowDirectoryEffect); !errors.Is(err, ErrUnavailable) {
			t.Fatal("redirect was accepted")
		}
	}
	if len(received) != 2 || received[0] != "Bearer old" || received[1] != "Bearer new" {
		t.Fatal("projected credential was cached")
	}
}
