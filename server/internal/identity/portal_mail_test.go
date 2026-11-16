package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/mail"
)

const fixtureTermsEN = "Project terms\n\nWithdrawal form\nTo: Devantler Tech, Example Road 1, 1234 Exampletown, ned@devantler.tech\nI/We withdraw from the following service:"
const fixtureTermsDA = "Handelsbetingelser\n\nStandardfortrydelsesformular\nTil: Devantler Tech, Example Road 1, 1234 Exampletown, ned@devantler.tech\nJeg/vi ønsker at fortryde følgende tjenesteydelse:"

// fakeMailer records what the portal sends and can refuse it.
type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	fail bool
}

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("mail: server unreachable")
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) messages() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mail.Message(nil), f.sent...)
}

func mailFixture(t *testing.T, b *browserFixture) *fakeMailer {
	t.Helper()
	m := &fakeMailer{}
	if err := b.svc.EnableMail(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func confirmationFixture(t *testing.T, b *browserFixture) {
	t.Helper()
	if err := b.svc.LinkOrderTerms(fixtureTermsEN, fixtureTermsDA); err != nil {
		t.Fatal(err)
	}
	mailFixture(t, b)
}

// TestOrderAndWithdrawalAreConfirmedByEmail queues the order's confirmation
// (Consumer Contracts Act, section 13) and the withdrawal's receipt (section
// 20 a) with what causes them, in the client's language, and sends each once.
func TestOrderAndWithdrawalAreConfirmedByEmail(t *testing.T) {
	b := billingFixture(t)
	m := mailFixture(t, b)
	f := orderForm(t, b, "da")
	f.Set("early_start", "yes")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("order refused: %d", status)
	}
	m.fail = true
	if err := b.svc.SendQueuedMail(t.Context()); err == nil || len(m.messages()) != 0 {
		t.Fatal("a refused email counted as sent")
	}
	m.fail = false
	// Closing the portal stops delivery; reopening it resumes.
	setFixturePortalFlags(t, b, append(acceptanceFlagNames(), "portal-invoice-billing"), []any{false, true, true, true, true, true, true})
	if err := b.svc.SendQueuedMail(t.Context()); err != nil || len(m.messages()) != 0 {
		t.Fatal("mail was sent with the portal closed", err)
	}
	billingFlags(t, b, true)
	for range 2 {
		if err := b.svc.SendQueuedMail(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	sent := m.messages()
	order := orderID(t, b)
	var deliverables string
	if err := b.pool.QueryRow(t.Context(), `SELECT deliverables FROM portal_proposals ORDER BY version DESC LIMIT 1`).Scan(&deliverables); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].To != "client@example.invalid" || sent[0].Subject != "Ordrebekræftelse fra Devantler Tech" {
		t.Fatalf("order confirmation not sent once to the client: %+v", sent)
	}
	for _, want := range []string{fixtureTermsDA, "Ordre: " + order, "Leverancer:\n" + deliverables, "Engangspris: 2995 DKK", "Månedspris: 99 DKK", "Betaling: faktura", "Du bad om, at arbejdet begynder inden for fortrydelsesfristen på 14 dage", "til og med ", "ned@devantler.tech", "CVR 46830385"} {
		if !strings.Contains(sent[0].Body, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, sent[0].Body)
		}
	}
	w := withdrawalForm(t, b, "en")
	if status, _, _ := b.request("POST", withdrawalPath, w, b.site.URL); status != 303 {
		t.Fatalf("withdrawal refused: %d", status)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sent = m.messages(); len(sent) != 2 || sent[1].Subject != "Receipt for your withdrawal" || !strings.Contains(sent[1].Body, "We have received your withdrawal from order "+order) || !strings.Contains(sent[1].Body, "You pay for the work done until you withdrew") {
		t.Fatalf("withdrawal receipt not sent: %+v", sent)
	}
	if status, _, _ := b.request("POST", withdrawalPath, w, b.site.URL); status != 303 {
		t.Fatalf("repeated withdrawal refused: %d", status)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil || len(m.messages()) != 2 {
		t.Fatal("a repeated withdrawal sent a second receipt", err)
	}
	_, body, _ := operatorOrderForm(t, b, "mail-operator")
	if !strings.Contains(body, "Order confirmation: sent ") || !strings.Contains(body, "Withdrawal receipt: sent ") || !strings.Contains(body, "The portal emails the client a receipt") {
		t.Fatal("the operator page does not show the emails")
	}
}

// An order must not bind when there is nowhere to send its durable confirmation.
func TestOrderWithoutContactDoesNotBind(t *testing.T) {
	b := billingFixture(t)
	m := mailFixture(t, b)
	f := orderForm(t, b, "en")
	if _, err := migrationDB(t).Exec(t.Context(), `DELETE FROM portal_contacts`); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 503 {
		t.Fatalf("order without confirmation contact was not refused: %d", status)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil || len(m.messages()) != 0 {
		t.Fatal("an email was sent without a contact address", err)
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_orders)+(SELECT count(*) FROM portal_mail_outbox)+(SELECT count(*) FROM portal_launch_places)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("a refused confirmation left an order, mail or launch place", err)
	}
	if err := (&Foundation{}).SendQueuedMail(t.Context()); err == nil {
		t.Fatal("sending without a sender did not fail")
	}
}

func TestOrderConfirmationRetainsOriginalAgreementOnRetry(t *testing.T) {
	b := billingFixture(t)
	m := mailFixture(t, b)
	f := orderForm(t, b, "en")
	path := "/portal/requests/" + proposalRequestID + "/orders"
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatalf("order refused: %d", status)
	}
	var original, acceptance string
	var version int
	if err := b.pool.QueryRow(t.Context(), `SELECT m.body,a.id,a.proposal_version FROM portal_mail_outbox m JOIN portal_orders o ON o.id=m.order_id JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id`).Scan(&original, &acceptance, &version); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{fixtureTermsEN, "Acceptance: " + acceptance, fmt.Sprintf("Proposal version: %d", version), "Agreement terms SHA256: " + f.Get("terms_hash")} {
		if !strings.Contains(original, want) {
			t.Error("confirmation omitted its frozen agreement or acceptance identity")
		}
	}
	if err := b.svc.LinkOrderTerms(fixtureTermsEN+"\nReplacement terms", fixtureTermsDA); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationDB(t).Exec(t.Context(), `DELETE FROM portal_contacts`); err != nil {
		t.Fatal(err)
	}
	// A changed release, removed contact and missing sender cannot replace or
	// invalidate the original successful order's idempotent acknowledgement.
	b.svc.mail.Store(nil)
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatalf("original order retry rebound to new prerequisites: %d", status)
	}
	if err := b.svc.EnableMail(m); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	sent := m.messages()
	if len(sent) != 1 || sent[0].Body != original || sent[0].To != "client@example.invalid" {
		t.Fatal("retry replaced, duplicated or redirected the original confirmation")
	}
}

func TestNewOrderRefusesMissingConfirmationPrerequisites(t *testing.T) {
	for _, missing := range []string{"terms", "sender"} {
		t.Run(missing, func(t *testing.T) {
			b := billingFixture(t)
			f := orderForm(t, b, "en")
			if missing == "terms" {
				b.svc.orderTerms.Store(nil)
			} else {
				b.svc.mail.Store(nil)
			}
			if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 503 {
				t.Fatalf("order bound without %s: %d", missing, status)
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_orders)+(SELECT count(*) FROM portal_mail_outbox)+(SELECT count(*) FROM portal_launch_places)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("refused confirmation left durable side effects", err)
			}
		})
	}
}

func TestNewOrderRequiresTheTermsShownToTheClient(t *testing.T) {
	b := billingFixture(t)
	f := orderForm(t, b, "en")
	path := "/portal/requests/" + proposalRequestID + "/orders"
	originalHash := f.Get("terms_hash")
	for _, hash := range []string{"", strings.Repeat("0", 64)} {
		f.Set("terms_hash", hash)
		if status, _, _ := b.request("POST", path, f, b.site.URL); status != 409 {
			t.Fatalf("missing/forged agreement revision accepted: %d", status)
		}
	}
	f.Set("terms_hash", originalHash)
	if err := b.svc.LinkOrderTerms(fixtureTermsEN+"\nReviewed replacement terms", fixtureTermsDA); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 409 {
		t.Fatalf("stale agreement revision accepted: %d", status)
	}
	fresh := orderForm(t, b, "en")
	if fresh.Get("terms_hash") == f.Get("terms_hash") {
		t.Fatal("new terms retained the old agreement revision")
	}
	if status, _, _ := b.request("POST", path, fresh, b.site.URL); status != 303 {
		t.Fatalf("fresh agreement refused: %d", status)
	}
}

func TestWithdrawalRetainsReceiptWithoutCurrentContact(t *testing.T) {
	b := billingFixture(t)
	m := mailFixture(t, b)
	placeInvoiceOrder(t, b)
	if _, err := migrationDB(t).Exec(t.Context(), `DELETE FROM portal_contacts`); err != nil {
		t.Fatal(err)
	}
	b.svc.orderTerms.Store(nil)
	b.svc.mail.Store(nil)
	if status, _, _ := b.request("POST", withdrawalPath, withdrawalForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatalf("withdrawal depended on new-order prerequisites: %d", status)
	}
	if err := b.svc.EnableMail(m); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	sent := m.messages()
	if len(sent) != 2 || sent[1].Subject != "Receipt for your withdrawal" || sent[1].To != "client@example.invalid" {
		t.Fatal("withdrawal lost its receipt after the contact changed")
	}
}
