package identity

import (
	"context"
	"encoding/base32"

	"github.com/jackc/pgx/v5"
)

// These are fictional starting quotes. They give the operator a complete
// client checkout to try without submitting or sharing a real proposal.
var previewFixtures = []struct {
	product, title string
	setup, monthly int64
}{
	{"website", "Demo website", 299500, 9900},
	{"app", "Demo app", 799500, 29900},
	{"service", "Demo service", 499500, 19900},
}

func seedPreviewFixtures(ctx context.Context, tx pgx.Tx) error {
	for _, fixture := range previewFixtures {
		id := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest("client-preview-v1:" + fixture.product))[:26]
		_, err := tx.Exec(ctx, `WITH requested AS (
  INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief)
  VALUES($1,$2,$1,$3,$4,'Fictional request for testing the client portal.')
  ON CONFLICT(id) DO NOTHING RETURNING id
), quoted AS (
  INSERT INTO portal_proposals(request_id,client_id,version,expected_version,submission_key,
    operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms)
  SELECT id,$2,1,0,$1,'urn:devantler:preview','preview:fictional-operator',
    'Example scope','Fictional deliverable','DKK',$5,$6,'Test only','Test only' FROM requested
  RETURNING request_id
)
INSERT INTO portal_proposal_visibility(request_id,client_id,event,expected_event,proposal_version,expected_proposal,
  action,submission_key,operator_issuer,operator_subject)
SELECT request_id,$2,1,0,1,1,'share',$1,'urn:devantler:preview','preview:fictional-operator' FROM quoted`,
			id, previewWorkspace, fixture.product, fixture.title, fixture.setup, fixture.monthly)
		if err != nil {
			return err
		}
		var seeded bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_requests r
 JOIN portal_proposals p ON p.request_id=r.id AND p.client_id=r.client_id AND p.version=1
 JOIN portal_proposal_visibility v ON v.request_id=r.id AND v.client_id=r.client_id AND v.event=1
 WHERE r.id=$1 AND r.client_id=$2 AND r.product=$3 AND p.operator_subject='preview:fictional-operator'
 AND v.action='share' AND v.proposal_version=1)`, id, previewWorkspace, fixture.product).Scan(&seeded)
		if err != nil {
			return err
		}
		if !seeded {
			return ErrDenied
		}
	}
	return nil
}
