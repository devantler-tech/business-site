package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Exercise the real shared domains and each table's independent NOT NULL
// protection, including the mutable review's update path.
func TestConsentBindingConstraints(t *testing.T) {
	b, _, review := preparedConsentFixture(t, "constraint-client")
	validKey := secret()
	for _, tc := range []struct {
		name, code       string
		key, event, hash any
	}{
		{"key shape", "23514", "invalid", 1, review.Hash},
		{"null key", "23502", nil, 1, review.Hash},
		{"event below floor", "23514", validKey, 0, review.Hash},
		{"event above bound", "23514", validKey, 101, review.Hash},
		{"null event", "23502", validKey, nil, review.Hash},
		{"hash shape", "23514", validKey, 1, strings.Repeat("g", 64)},
		{"null hash", "23502", validKey, 1, nil},
	} {
		for _, operation := range []string{"insert review", "update review", "insert receipt"} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				tx, err := b.pool.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer rollbackProposalWrite(t.Context(), tx)
				var query string
				switch operation {
				case "insert review":
					query = `INSERT INTO portal_proposal_reviews(request_id,client_id,client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash,expires_at) VALUES($1,'private-client-key','https://fixture.invalid','constraint-insert',$2,1,$3,$4,now()+interval '1 minute')`
				case "update review":
					query = `UPDATE portal_proposal_reviews SET context_key=$2,visibility_event=$3,content_hash=$4 WHERE request_id=$1`
				case "insert receipt":
					query = `INSERT INTO portal_proposal_acceptances(id,request_id,client_id,client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash) VALUES('AAAAAAAAAAAAAAAAAAAAAAAAAA',$1,'private-client-key','https://fixture.invalid','constraint-insert',$2,1,$3,$4)`
				}
				_, err = tx.Exec(t.Context(), query, proposalRequestID, tc.key, tc.event, tc.hash)
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != tc.code {
					t.Fatal("expected binding constraint did not reject the value")
				}
			})
		}
	}
}
