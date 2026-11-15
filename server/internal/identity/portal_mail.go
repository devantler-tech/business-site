package identity

import (
	"context"
	"errors"
	"log"
	"strings"
	"text/template"
	"time"

	"github.com/devantler-tech/business-site/server/internal/mail"
	"github.com/jackc/pgx/v5"
)

// Mailer sends one email; *mail.Sender is the production one.
type Mailer interface {
	Send(ctx context.Context, m mail.Message) error
}

type mailer struct{ Mailer }

// mailLock makes one replica send each pass; others skip the tick.
const mailLock = 1869820521

// EnableMail supplies the site's mail sender. The portal queues email only
// for orders and withdrawals, which their false-default gates control.
func (s *Foundation) EnableMail(m Mailer) error {
	if m == nil {
		return errors.New("mail is not configured")
	}
	s.mail.Store(&mailer{m})
	return nil
}

// mailContext is what an email needs from the request that causes it.
type mailContext struct {
	Lang, Portal string
}

// orderMail is the data of an order confirmation or withdrawal receipt.
type orderMail struct {
	OrderID, Scope, Deliverables, OneTimeTerms, MonthlyTerms, Collection string
	OneTimeDKK, MonthlyDKK                                               int64
	MonthlyStart, WithdrawUntil, WorkStartsFrom                          string
	Placed, Withdrawn                                                    string
	EarlyStart                                                           bool
	Launch                                                               *launchPlace
	Portal, Terms                                                        string
}

var mailTemplates = template.Must(template.New("mail").Parse(`
{{define "order-confirmation-da"}}Tak for din ordre. Her er bekræftelsen af vores aftale.

Ordre: {{.OrderID}}
Afgivet: {{.Placed}} (UTC)

Omfang:
{{.Scope}}

Leverancer:
{{.Deliverables}}

{{if .OneTimeDKK}}Engangspris: {{.OneTimeDKK}} DKK{{with .Launch}} (normalpris {{.ListDKK}} DKK, lanceringsrabat {{.Percent}} % −{{.DiscountDKK}} DKK){{end}}{{with .OneTimeTerms}} ({{.}}){{end}}
{{end}}{{if .MonthlyDKK}}Månedspris: {{.MonthlyDKK}} DKK forud den 1. i hver måned fra {{.MonthlyStart}}{{with .MonthlyTerms}} ({{.}}){{end}}
{{end}}Betaling: {{if eq .Collection "card"}}kort eller MobilePay via Stripe{{else}}faktura{{end}}

{{if .EarlyStart}}Du bad om, at arbejdet begynder inden for fortrydelsesfristen på 14 dage. Du er klar over, at du betaler for det udførte arbejde, hvis du fortryder, og at du mister fortrydelsesretten, når arbejdet er helt udført.{{else}}Arbejdet begynder tidligst {{.WorkStartsFrom}}, når fortrydelsesfristen er udløbet.{{end}}

Du kan fortryde aftalen til og med {{.WithdrawUntil}} med "Fortryd aftale" i portalen{{with .Portal}} ({{.}}){{end}} eller ved at skrive til ned@devantler.tech.{{with .Terms}} Betingelserne og standardfortrydelsesformularen finder du her: {{.}}{{end}}

Venlig hilsen
Devantler Tech · CVR 46830385 · ned@devantler.tech
{{end}}
{{define "order-confirmation-en"}}Thank you for your order. This confirms our agreement.

Order: {{.OrderID}}
Placed: {{.Placed}} (UTC)

Scope:
{{.Scope}}

Deliverables:
{{.Deliverables}}

{{if .OneTimeDKK}}One-time price: {{.OneTimeDKK}} DKK{{with .Launch}} (list price {{.ListDKK}} DKK, {{.Percent}}% launch discount −{{.DiscountDKK}} DKK){{end}}{{with .OneTimeTerms}} ({{.}}){{end}}
{{end}}{{if .MonthlyDKK}}Monthly price: {{.MonthlyDKK}} DKK in advance on the 1st of each month from {{.MonthlyStart}}{{with .MonthlyTerms}} ({{.}}){{end}}
{{end}}Payment: {{if eq .Collection "card"}}card or MobilePay through Stripe{{else}}invoice{{end}}

{{if .EarlyStart}}You asked for work to begin within the 14-day withdrawal period. You understand that if you withdraw, you pay for the work done until then, and that you lose the right of withdrawal once the work is completely finished.{{else}}Work begins on {{.WorkStartsFrom}} at the earliest, after the withdrawal period.{{end}}

You can withdraw from the agreement until {{.WithdrawUntil}} inclusive with "Withdraw from contract" in the portal{{with .Portal}} ({{.}}){{end}}, or by writing to ned@devantler.tech.{{with .Terms}} The terms and the model withdrawal form are here: {{.}}{{end}}

Kind regards
Devantler Tech · CVR 46830385 · ned@devantler.tech
{{end}}
{{define "withdrawal-receipt-da"}}Vi har modtaget din fortrydelse af ordre {{.OrderID}} den {{.Withdrawn}} (UTC).

Aftalen er ophørt, og der kommer ingen nye betalinger. {{if .EarlyStart}}Du betaler for det arbejde, der er udført, indtil du fortrød, og du får resten af det, du har betalt, tilbage uden unødig forsinkelse og senest 14 dage efter med samme betalingsmiddel.{{else}}Du får det, du har betalt, tilbage uden unødig forsinkelse og senest 14 dage efter med samme betalingsmiddel.{{end}}

Venlig hilsen
Devantler Tech · CVR 46830385 · ned@devantler.tech
{{end}}
{{define "withdrawal-receipt-en"}}We have received your withdrawal from order {{.OrderID}} on {{.Withdrawn}} (UTC).

The agreement has ended and nothing more is charged. {{if .EarlyStart}}You pay for the work done until you withdrew, and you get back the rest of what you have paid without undue delay and within 14 days, by the same payment method.{{else}}You get back what you have paid without undue delay and within 14 days, by the same payment method.{{end}}

Kind regards
Devantler Tech · CVR 46830385 · ned@devantler.tech
{{end}}`))

var mailSubjects = map[string]string{
	"order-confirmation-da": "Ordrebekræftelse fra Devantler Tech",
	"order-confirmation-en": "Order confirmation from Devantler Tech",
	"withdrawal-receipt-da": "Kvittering for din fortrydelse",
	"withdrawal-receipt-en": "Receipt for your withdrawal",
}

// queueOrderMail renders one email and queues it in tx for the client's
// contact address, once per order and kind. A client without a contact
// address gets none; the operator's page then shows that nothing was queued.
func (s *Foundation) queueOrderMail(ctx context.Context, tx pgx.Tx, client, kind string, mc mailContext, data orderMail) error {
	lang := "en"
	if mc.Lang == "da" {
		lang = "da"
	}
	if strings.HasPrefix(mc.Portal, "https://") {
		data.Portal = mc.Portal + "/requests"
	}
	if strings.HasPrefix(s.site, "https://") {
		data.Terms = s.site + map[string]string{"da": "/da/terms/", "en": "/terms/"}[lang]
	}
	var body strings.Builder
	if err := mailTemplates.ExecuteTemplate(&body, kind+"-"+lang, data); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO portal_mail_outbox(id,order_id,kind,recipient,subject,body) SELECT $1,$2,$3,c.email,$4,$5 FROM portal_contacts c WHERE c.client_id=$6 ON CONFLICT(order_id,kind) DO NOTHING`,
		secret(), data.OrderID, kind, mailSubjects[kind+"-"+lang], body.String(), client); err != nil {
		return errTriageStorage
	}
	return nil
}

// SendQueuedMail sends every queued email not yet delivered, oldest first, and
// records each delivery once. It stops at the first failure and tries again
// on the next pass; a message whose delivery record was lost may be sent
// twice. It is a no-op without a sender, while another replica sends, or while
// the portal's identity gate is closed: closing the portal stops delivery.
func (s *Foundation) SendQueuedMail(ctx context.Context) error {
	m := s.mail.Load()
	if m == nil {
		return errors.New("mail is not configured")
	}
	if !s.ready(ctx) {
		return nil
	}
	return s.withRunLock(ctx, mailLock, func() error {
		rows, err := s.pool.Query(ctx, `SELECT o.id,o.recipient,o.subject,o.body FROM portal_mail_outbox o WHERE NOT EXISTS (SELECT 1 FROM portal_mail_deliveries d WHERE d.outbox_id=o.id) ORDER BY o.queued_at,o.id LIMIT 20`)
		if err != nil {
			return errTriageStorage
		}
		queued, err := pgx.CollectRows(rows, pgx.RowToStructByPos[mail.Message])
		if err != nil {
			return errTriageStorage
		}
		for _, msg := range queued {
			if !s.ready(ctx) {
				return nil
			}
			if err = m.Send(ctx, msg); err != nil {
				return err
			}
			if _, err = s.pool.Exec(ctx, `INSERT INTO portal_mail_deliveries(outbox_id) VALUES($1) ON CONFLICT DO NOTHING`, msg.ID); err != nil {
				return errTriageStorage
			}
		}
		return nil
	})
}

// RunMail sends queued email at once and then on every tick until ctx ends.
func (s *Foundation) RunMail(ctx context.Context, every time.Duration) {
	for {
		run, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if err := s.SendQueuedMail(run); err != nil {
			log.Printf("portal: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// orderMailState is one queued email of an order, for the operator's page.
type orderMailState struct {
	Kind      string
	Delivered *time.Time
}

// readOrderMail adds the order's queued emails to an order read in tx.
func readOrderMail(ctx context.Context, tx pgx.Tx, o *projectOrder) error {
	rows, err := tx.Query(ctx, `SELECT o.kind,d.delivered_at FROM portal_mail_outbox o LEFT JOIN portal_mail_deliveries d ON d.outbox_id=o.id WHERE o.order_id=$1 ORDER BY o.queued_at`, o.ID)
	if err != nil {
		return errTriageStorage
	}
	if o.Mail, err = pgx.CollectRows(rows, pgx.RowToStructByPos[orderMailState]); err != nil {
		return errTriageStorage
	}
	return nil
}
