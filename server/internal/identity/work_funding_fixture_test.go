package identity

import "testing"

// fixtureFundingReference is the fixture charge's payment reference; it must
// never reach anyone without current operator access.
const fixtureFundingReference = "fixture-funding-reference"

// fundFixtureOrder records the client's invoice order for the latest
// acceptance and settles its first charge, as the Numina sync would once the
// invoice is paid. The client asked for an early start, so work may start
// within the withdrawal period. The migration owner writes the rows directly.
func fundFixtureOrder(t *testing.T) string {
	t.Helper()
	order := secret()
	var gross int64
	err := migrationDB(t).QueryRow(t.Context(), `WITH a AS (SELECT a.id,a.request_id,a.client_id,a.client_issuer,a.client_subject,p.one_time_minor,p.monthly_minor FROM portal_proposal_acceptances a
 JOIN portal_proposals p ON p.request_id=a.request_id AND p.version=a.proposal_version WHERE a.request_id=$1 ORDER BY a.recorded_at DESC LIMIT 1),
 o AS (INSERT INTO portal_orders(id,request_id,client_id,acceptance_id,client_issuer,client_subject,collection,one_time_minor,monthly_minor,monthly_from,early_start_at)
 SELECT $2,request_id,client_id,id,client_issuer,client_subject,'invoice',one_time_minor,monthly_minor,(date_trunc('month',now())+interval '1 month')::date,clock_timestamp() FROM a RETURNING id,one_time_minor)
 INSERT INTO portal_order_charges(order_id,period,identity,gross_minor,net_minor,vat_minor,state,provider_ref,settled_at)
 SELECT id,'acceptance','portal-'||id||'-acceptance',one_time_minor,one_time_minor*100/125,one_time_minor-one_time_minor*100/125,'settled',$3,clock_timestamp() FROM o RETURNING gross_minor`,
		proposalRequestID, order, fixtureFundingReference).Scan(&gross)
	if err != nil || gross <= 0 {
		t.Fatal("fixture funding unavailable", err)
	}
	return order
}

// changeFixtureFunding applies one change to the fixture's funding charge.
func changeFixtureFunding(t *testing.T, change string) {
	t.Helper()
	statement, ok := map[string]string{
		"unsettled":      `UPDATE portal_order_charges SET state='booked',settled_at=NULL`,
		"refunded":       `INSERT INTO portal_charge_reversals(order_id,period,reason,reference) SELECT order_id,period,'refunded','ch_fixture' FROM portal_order_charges`,
		"disputed":       `INSERT INTO portal_charge_reversals(order_id,period,reason,reference) SELECT order_id,period,'disputed','du_fixture' FROM portal_order_charges`,
		"amount":         `UPDATE portal_order_charges SET gross_minor=gross_minor-125,net_minor=net_minor-100,vat_minor=vat_minor-25`,
		"reference":      `UPDATE portal_order_charges SET provider_ref='fixture-funding-reference-2'`,
		"withdrawn":      `INSERT INTO portal_order_withdrawals(order_id,client_issuer,client_subject) SELECT order_id,'https://fixture.invalid','fixture-client' FROM portal_order_charges`,
		"no-early-start": `UPDATE portal_orders SET early_start_at=NULL`,
		"period-passed":  `UPDATE portal_orders SET early_start_at=NULL,placed_at=placed_at-interval '30 days'`,
	}[change]
	if !ok {
		t.Fatal("unsupported funding change")
	}
	if _, err := migrationDB(t).Exec(t.Context(), statement); err != nil {
		t.Fatal("fixture funding change failed", err)
	}
}
