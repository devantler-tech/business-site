package identity

import (
	"net/url"
	"testing"
)

func serviceProposalForm(t *testing.T, b *browserFixture, kind proposalServiceKind) url.Values {
	t.Helper()
	form := proposalForm(t, b)
	service := serviceQuoteForm(string(kind))
	for _, name := range []string{"service_kind", "appointment_minutes", "appointment_terms_en", "appointment_terms_da"} {
		if service.Has(name) {
			form.Set(name, service.Get(name))
		}
	}
	return form
}

// acceptedServiceQuoteFixture creates a fresh typed quote through native human
// Save, Share and Accept, preserving the fictional prices used by order forms.
// It does not retrofit billingFixture's already accepted legacy agreement.
func acceptedServiceQuoteFixture(t *testing.T, kind proposalServiceKind) *browserFixture {
	t.Helper()
	if kind != proposalServiceSoftware {
		t.Fatal("typed appointment acceptance is held until the complete real commitment store is integrated")
	}
	b := proposalFixture(t)
	sharingFlags(t, b, true, true, true, true, true)
	saveFixtureProposal(t, b, serviceProposalForm(t, b, kind))
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	acceptanceFlags(t, b, true, true, true, true, true, true)
	triageSignIn(t, b, "consent-client", "private-client-key", "client")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", acceptanceForm(t, b), b.site.URL); status != 303 {
		t.Fatalf("typed quote acceptance=%d", status)
	}
	billingFlags(t, b, true)
	confirmationFixture(t, b)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES('private-client-key','Fictional Client','client@example.invalid','Fictional Bakery ApS',1)`); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestServiceQuotePersistenceRetainsContract(t *testing.T) {
	for _, kind := range []proposalServiceKind{proposalServiceSoftware, proposalServiceAppointment} {
		t.Run(string(kind), func(t *testing.T) {
			b := proposalFixture(t)
			form := serviceProposalForm(t, b, kind)
			want, ok := parseServiceQuoteForm(form)
			if !ok {
				t.Fatal("valid fixture quote was refused")
			}
			saveFixtureProposal(t, b, form)
			p := authenticateExistingMember(t, b, "proposal-operator")
			rows, err := b.svc.readProposals(t.Context(), p, proposalRequestID)
			if err != nil || len(rows) != 1 {
				t.Fatal("saved typed quote is unavailable", err)
			}
			got := rows[0]
			if got.ContentFormat != 2 || got.Service != want.Service {
				t.Fatal("native save discarded or downgraded the typed service contract")
			}
			wantHash, err := proposalContentHash(want)
			if err != nil {
				t.Fatal(err)
			}
			// JSON projection makes the missing-feature baseline an assertion,
			// not an SQL error merely because the old schema lacks the column.
			var storedHash *string
			if err := b.pool.QueryRow(t.Context(), `SELECT to_jsonb(p)->>'content_sha256' FROM portal_proposals p WHERE request_id=$1 AND version=1`, proposalRequestID).Scan(&storedHash); err != nil || storedHash == nil || *storedHash != wantHash {
				t.Fatal("immutable parent has no exact writer-computed content hash", err)
			}
		})
	}
}

func TestServiceQuoteRetryCannotChangeContract(t *testing.T) {
	for _, change := range []string{"kind", "duration", "English terms", "Danish terms"} {
		t.Run(change, func(t *testing.T) {
			b := proposalFixture(t)
			form := serviceProposalForm(t, b, proposalServiceAppointment)
			saveFixtureProposal(t, b, form)
			switch change {
			case "kind":
				form.Set("service_kind", "software")
				for _, name := range []string{"appointment_minutes", "appointment_terms_en", "appointment_terms_da"} {
					form.Del(name)
				}
			case "duration":
				form.Set("appointment_minutes", "45")
			case "English terms":
				form.Set("appointment_terms_en", "A separately agreed synthetic appointment.")
			case "Danish terms":
				form.Set("appointment_terms_da", "En særskilt aftalt syntetisk samtale.")
			}
			if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", form, b.site.URL); status != 409 {
				t.Fatalf("changed contract reused immutable retry: status=%d", status)
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_proposals WHERE request_id=$1`, proposalRequestID).Scan(&count); err != nil || count != 1 {
				t.Fatal("changed retry altered the version ledger", err)
			}
		})
	}
}
