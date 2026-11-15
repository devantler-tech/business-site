package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/mail"
)

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
	for range 2 {
		if err := b.svc.SendQueuedMail(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	sent := m.messages()
	order := orderID(t, b)
	if len(sent) != 1 || sent[0].To != "client@example.invalid" || sent[0].Subject != "Ordrebekræftelse fra Devantler Tech" {
		t.Fatalf("order confirmation not sent once to the client: %+v", sent)
	}
	for _, want := range []string{"Ordre: " + order, "Engangspris: 2995 DKK", "Månedspris: 99 DKK", "Betaling: faktura", "Du bad om, at arbejdet begynder inden for fortrydelsesfristen på 14 dage", "til og med ", "ned@devantler.tech", "CVR 46830385"} {
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

// TestOrderWithoutContactQueuesNoEmail queues nothing for a client without a
// contact address, and the operator's page says so.
func TestOrderWithoutContactQueuesNoEmail(t *testing.T) {
	b := billingFixture(t)
	m := mailFixture(t, b)
	if _, err := migrationDB(t).Exec(t.Context(), `DELETE FROM portal_contacts`); err != nil {
		t.Fatal(err)
	}
	placeInvoiceOrder(t, b)
	if err := b.svc.SendQueuedMail(t.Context()); err != nil || len(m.messages()) != 0 {
		t.Fatal("an email was sent without a contact address", err)
	}
	if _, body, _ := operatorOrderForm(t, b, "no-mail-operator"); !strings.Contains(body, "No email is queued for this order.") {
		t.Fatal("the operator page does not say that no email was queued")
	}
	if err := (&Foundation{}).SendQueuedMail(t.Context()); err == nil {
		t.Fatal("sending without a sender did not fail")
	}
}
