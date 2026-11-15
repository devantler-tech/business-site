package identity

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/numina"
	"github.com/jackc/pgx/v5"
)

// NuminaAPI is the part of the Numina client the invoice path uses.
type NuminaAPI interface {
	CreateContact(context.Context, numina.Contact) (numina.Contact, error)
	GetContact(context.Context, string) (numina.Contact, error)
	UpdateContact(ctx context.Context, id, name, email string) error
	ListContacts(context.Context) ([]numina.Contact, error)
	CreateInvoice(ctx context.Context, key string, invoice numina.NewInvoice) (numina.Invoice, error)
	BookInvoice(context.Context, string) (numina.Invoice, error)
	SendInvoice(ctx context.Context, key, id string) error
	GetInvoice(context.Context, string) (numina.Invoice, error)
	ListInvoices(ctx context.Context, contactID, from, to string) ([]numina.Invoice, error)
}

// InvoiceBilling configures the invoice collection path (ADR 0030, #549).
type InvoiceBilling struct {
	Numina       NuminaAPI
	SalesAccount int    // Numina sales account for portal sales
	VATCode      string // Numina sales VAT code for 25% Danish VAT
	Now          func() time.Time
}

// invoiceBillingLock makes one replica run each sync; others skip the tick.
const invoiceBillingLock = 1869820520

// vatPercent is the Danish VAT included in every portal price.
const vatPercent = 25

// errChargeHeld marks a charge the portal must not invoice by itself.
var errChargeHeld = errors.New("charge needs a manual invoice")

// errBillingPaused stops a charge when the gate closes mid-run; not a failure.
var errBillingPaused = errors.New("invoice billing paused")

type billingOrder struct {
	ID, Client, Scope          string
	OneTimeMinor, MonthlyMinor int64
	MonthlyFrom, Placed        time.Time
	LastMonth                  *time.Time // the last billed month, once ended (#591)
}

type billingCharge struct {
	Order                   billingOrder
	Period, Identity, State string
	Reference               string
	GrossMinor, NetMinor    int64
	Sent                    bool
}

// SyncInvoiceBilling creates, books and sends the Numina invoice for every due
// period of every invoice-path order, then polls unsettled invoices. It is a
// no-op while the gate is off or another replica holds the run lock.
func (s *Foundation) SyncInvoiceBilling(ctx context.Context, cfg InvoiceBilling) error {
	if cfg.Numina == nil || cfg.SalesAccount <= 0 || cfg.VATCode == "" || cfg.Now == nil {
		return errors.New("invoice billing is not configured")
	}
	if !s.invoiceBillingReady(ctx) {
		return nil
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return errTriageStorage
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, invoiceBillingLock).Scan(&locked); err != nil {
		return errTriageStorage
	}
	if !locked {
		return nil
	}
	defer func() {
		// Bounded even after cancellation; a connection that may still hold the
		// lock is closed rather than returned to the pool.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock($1)`, invoiceBillingLock); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	now := cfg.Now()
	if err = s.recordDueCharges(ctx, now); err != nil {
		return err
	}
	charges, err := s.openCharges(ctx)
	if err != nil {
		return err
	}
	var failed int
	for _, c := range charges {
		// Recheck the gate so turning it off stops a long run between calls.
		if !s.invoiceBillingReady(ctx) {
			return nil
		}
		if err := s.driveCharge(ctx, cfg, now, c); err != nil && !errors.Is(err, errChargeHeld) && !errors.Is(err, errBillingPaused) {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("invoice billing: %d charges not advanced", failed)
	}
	return nil
}

// RunInvoiceBilling syncs at once and then on every tick until ctx ends.
func (s *Foundation) RunInvoiceBilling(ctx context.Context, cfg InvoiceBilling, every time.Duration) {
	for {
		run, cancel := context.WithTimeout(ctx, 10*time.Minute)
		if err := s.SyncInvoiceBilling(run, cfg); err != nil {
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

func monthPeriod(t time.Time) string { return t.Format("2006-01") }

// recordDueCharges records each due period's identity before any Numina call:
// the acceptance invoice, then each month from the order's first billed month
// up to the current Copenhagen month or the order's last billed month,
// whichever comes first. Amounts include 25% VAT.
func (s *Foundation) recordDueCharges(ctx context.Context, now time.Time) error {
	orders, err := s.invoiceOrders(ctx)
	if err != nil {
		return err
	}
	local := now.In(copenhagen)
	current := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, o := range orders {
		periods := map[string]int64{}
		if o.OneTimeMinor > 0 {
			periods["acceptance"] = o.OneTimeMinor
		}
		if o.MonthlyMinor > 0 {
			for m := o.MonthlyFrom; !m.After(current) && (o.LastMonth == nil || !m.After(*o.LastMonth)); m = m.AddDate(0, 1, 0) {
				periods[monthPeriod(m)] = o.MonthlyMinor
			}
		}
		for period, gross := range periods {
			net, vat, ok := splitVAT(gross)
			if !ok {
				return errors.New("invoice billing: amount has no exact net")
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO portal_order_charges(order_id,period,identity,gross_minor,net_minor,vat_minor) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
				o.ID, period, "portal-"+o.ID+"-"+period, gross, net, vat); err != nil {
				return errTriageStorage
			}
		}
	}
	return nil
}

// splitVAT returns the exact net and VAT inside a VAT-inclusive total.
func splitVAT(gross int64) (int64, int64, bool) {
	if gross <= 0 || (gross*100)%(100+vatPercent) != 0 {
		return 0, 0, false
	}
	net := gross * 100 / (100 + vatPercent)
	return net, gross - net, true
}

func (s *Foundation) invoiceOrders(ctx context.Context) ([]billingOrder, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.client_id,p.scope,o.one_time_minor,o.monthly_minor,o.monthly_from,o.placed_at,(SELECT e.last_month FROM portal_order_ends e WHERE e.order_id=o.id) FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id JOIN portal_proposals p ON p.request_id=a.request_id AND p.version=a.proposal_version WHERE o.collection='invoice' ORDER BY o.placed_at,o.id`)
	if err != nil {
		return nil, errTriageStorage
	}
	defer rows.Close()
	var result []billingOrder
	for rows.Next() {
		var o billingOrder
		if err = rows.Scan(&o.ID, &o.Client, &o.Scope, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom, &o.Placed, &o.LastMonth); err != nil {
			return nil, errTriageStorage
		}
		result = append(result, o)
	}
	if rows.Err() != nil {
		return nil, errTriageStorage
	}
	return result, nil
}

func (s *Foundation) openCharges(ctx context.Context) ([]billingCharge, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.client_id,p.scope,o.one_time_minor,o.monthly_minor,o.monthly_from,o.placed_at,c.period,c.identity,c.state,coalesce(c.provider_ref,''),c.gross_minor,c.net_minor,c.sent FROM portal_order_charges c JOIN portal_orders o ON o.id=c.order_id JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id JOIN portal_proposals p ON p.request_id=a.request_id AND p.version=a.proposal_version WHERE o.collection='invoice' AND c.state<>'settled' ORDER BY c.state='held',o.placed_at,o.id,c.period`)
	if err != nil {
		return nil, errTriageStorage
	}
	defer rows.Close()
	var result []billingCharge
	for rows.Next() {
		var c billingCharge
		o := &c.Order
		if err = rows.Scan(&o.ID, &o.Client, &o.Scope, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom, &o.Placed, &c.Period, &c.Identity, &c.State, &c.Reference, &c.GrossMinor, &c.NetMinor, &c.Sent); err != nil {
			return nil, errTriageStorage
		}
		result = append(result, c)
	}
	if rows.Err() != nil {
		return nil, errTriageStorage
	}
	return result, nil
}

func (s *Foundation) setCharge(ctx context.Context, c billingCharge, set string, args ...any) error {
	args = append([]any{c.Order.ID, c.Period}, args...)
	if _, err := s.pool.Exec(ctx, `UPDATE portal_order_charges SET `+set+`,updated_at=clock_timestamp() WHERE order_id=$1 AND period=$2`, args...); err != nil {
		return errTriageStorage
	}
	return nil
}

// driveCharge advances one charge by every step that is still missing. Each
// step is safe to repeat: an invoice is found by its memo identity before one
// is created, booking is idempotent at Numina, and sending carries its own key.
// The gate is rechecked before every call that changes something at Numina.
func (s *Foundation) driveCharge(ctx context.Context, cfg InvoiceBilling, now time.Time, c billingCharge) error {
	if c.Reference == "" || c.State == "held" {
		err := s.attachInvoice(ctx, cfg, now, &c)
		if errors.Is(err, errChargeHeld) {
			return s.holdCharge(ctx, c)
		}
		if errors.Is(err, errBillingPaused) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	invoice, err := cfg.Numina.GetInvoice(ctx, c.Reference)
	if err != nil {
		return err
	}
	if invoice.Type != "invoice" {
		return s.holdCharge(ctx, c)
	}
	if c.State != "booked" {
		if invoice.Status == "draft" {
			// Book only a Danish customer's draft whose total is exactly the
			// charge. Anything else (another VAT treatment, a typo) waits for
			// the operator.
			if total, ok := numina.Minor(invoice.Total); !ok || total != c.GrossMinor || invoice.Currency != "DKK" {
				return s.holdCharge(ctx, c)
			}
			dk, err := danish(ctx, cfg.Numina, invoice.ContactID)
			if err != nil {
				return err
			}
			if !dk {
				return s.holdCharge(ctx, c)
			}
			if !s.invoiceBillingReady(ctx) {
				return nil
			}
			if invoice, err = cfg.Numina.BookInvoice(ctx, c.Reference); err != nil {
				return err
			}
			if err = s.setCharge(ctx, c, `state='booked',provider_number=$3`, invoice.InvoiceNumber); err != nil {
				return err
			}
		} else if c.State == "held" {
			// Booked by the operator: follow its payment, leave sending to them.
			if err = s.setCharge(ctx, c, `state='booked',provider_number=$3,sent=true`, invoice.InvoiceNumber); err != nil {
				return err
			}
			c.Sent = true
		} else if err = s.setCharge(ctx, c, `state='booked',provider_number=$3`, invoice.InvoiceNumber); err != nil {
			// Booked by this sync, but its reply or record was lost: it still
			// needs sending, under the same idempotency key.
			return err
		}
		c.State = "booked"
	}
	if !c.Sent {
		if !s.invoiceBillingReady(ctx) {
			return nil
		}
		// A sandbox company cannot send email (403); polling still proceeds.
		switch err := cfg.Numina.SendInvoice(ctx, c.Identity+"-send", c.Reference); {
		case err == nil:
			if err = s.setCharge(ctx, c, `sent=true`); err != nil {
				return err
			}
		case numina.IsStatus(err, 403):
		default:
			return err
		}
	}
	if invoice.Settled() {
		return s.setCharge(ctx, c, `state='settled',settled_at=clock_timestamp()`)
	}
	return nil
}

// holdCharge marks a charge the operator must finish by hand and reports it.
func (s *Foundation) holdCharge(ctx context.Context, c billingCharge) error {
	if c.State != "held" {
		return errors.Join(errChargeHeld, s.setCharge(ctx, c, `state='held'`))
	}
	return errChargeHeld
}

// attachInvoice gives the charge its Numina invoice. An invoice to the contact
// whose memo carries the charge identity comes first, a booked one before a
// draft: the operator's own for a held charge, or one whose create reply was
// lost. Whoever booked an invoice also sends it, and that is saved with its
// reference, so a retry never sends it again. A held charge keeps its own
// invoice until the operator books one; otherwise a new invoice is created,
// but only for a customer the operator has confirmed as Danish.
func (s *Foundation) attachInvoice(ctx context.Context, cfg InvoiceBilling, now time.Time, c *billingCharge) error {
	contactID, err := s.numinaContact(ctx, cfg.Numina, c.Order.Client)
	if err != nil {
		return err
	}
	today := now.In(copenhagen)
	from := c.Order.Placed.In(copenhagen).AddDate(0, 0, -1).Format(time.DateOnly)
	existing, err := cfg.Numina.ListInvoices(ctx, contactID, from, today.AddDate(0, 0, 1).Format(time.DateOnly))
	if err != nil {
		return err
	}
	found, ok := memoInvoice(existing, c.Identity, c.Reference)
	switch {
	case ok && found.Status != "draft":
		c.Reference, c.State, c.Sent = found.ID, "booked", true
		return s.setCharge(ctx, *c, `state='booked',provider_ref=$3,provider_number=$4,sent=true`, found.ID, found.InvoiceNumber)
	case ok && c.Reference == "":
		c.Reference, c.State = found.ID, "created"
		return s.setCharge(ctx, *c, `state='created',provider_ref=$3`, found.ID)
	case c.Reference != "":
		return nil
	}
	dk, err := danish(ctx, cfg.Numina, contactID)
	if err != nil {
		return err
	}
	if !dk {
		return errChargeHeld
	}
	if !s.invoiceBillingReady(ctx) {
		return errBillingPaused
	}
	description := invoiceLine(c.Order.Scope)
	if c.Period != "acceptance" {
		description += " — " + c.Period
	}
	created, err := cfg.Numina.CreateInvoice(ctx, c.Identity, numina.NewInvoice{
		ContactID:          contactID,
		IssueDate:          today.Format(time.DateOnly),
		DueDate:            today.AddDate(0, 0, 14).Format(time.DateOnly),
		Currency:           "DKK",
		SalesAccountNumber: cfg.SalesAccount,
		VATCode:            cfg.VATCode,
		Items:              []numina.Line{{Description: description, Quantity: 1, UnitPrice: numina.Amount(c.NetMinor), VATCode: cfg.VATCode}},
		Memo:               "Portal reference: " + c.Identity,
	})
	if err != nil {
		return err
	}
	if created.ID == "" {
		return errors.New("numina: created invoice has no id")
	}
	c.Reference, c.State = created.ID, "created"
	return s.setCharge(ctx, *c, `state='created',provider_ref=$3`, created.ID)
}

// memoInvoice returns the newest invoice other than skip whose memo carries
// the charge identity, a booked one before a draft. Credit notes inherit the
// memo of the invoice they credit, so only invoices count.
func memoInvoice(list []numina.Invoice, identity, skip string) (numina.Invoice, bool) {
	var draft numina.Invoice
	for _, invoice := range list {
		if invoice.Type != "invoice" || invoice.ID == "" || invoice.ID == skip || !strings.Contains(invoice.Memo, identity) {
			continue
		}
		if invoice.Status != "draft" {
			return invoice, true
		}
		if draft.ID == "" {
			draft = invoice
		}
	}
	return draft, draft.ID != ""
}

// danish reports whether the operator has confirmed the Numina contact's
// country as DK. Only then may an invoice carry Danish VAT automatically: the
// portal profile has no country, and a customer elsewhere needs reverse charge
// or another treatment.
func danish(ctx context.Context, api NuminaAPI, contactID string) (bool, error) {
	if contactID == "" {
		return false, nil
	}
	contact, err := api.GetContact(ctx, contactID)
	if err != nil {
		return false, err
	}
	return contact.CountryCode == "DK", nil
}

// invoiceLine is the first line of the accepted scope, bounded for the invoice.
func invoiceLine(scope string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(scope), "\n")
	line = strings.TrimSpace(line)
	for utf8.RuneCountInString(line) > 200 {
		_, size := utf8.DecodeLastRuneInString(line)
		line = line[:len(line)-size]
	}
	if line == "" {
		return "Accepted proposal"
	}
	return line
}

// numinaContact returns the client's Numina contact, creating or updating it
// from the portal contact profile. Numina does not protect contact creation
// against retries, and names change, so an existing contact is found by email.
func (s *Foundation) numinaContact(ctx context.Context, api NuminaAPI, client string) (string, error) {
	var name, email, company string
	if err := s.pool.QueryRow(ctx, `SELECT name,email,company FROM portal_contacts WHERE client_id=$1`, client).Scan(&name, &email, &company); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Without contact details there is nobody to invoice yet.
			return "", errChargeHeld
		}
		return "", errTriageStorage
	}
	display, attn := name, ""
	if company != "" {
		display, attn = company, name
	}
	var contactID string
	err := s.pool.QueryRow(ctx, `SELECT contact_id FROM portal_numina_contacts WHERE client_id=$1`, client).Scan(&contactID)
	if err == nil {
		if !s.invoiceBillingReady(ctx) {
			return "", errBillingPaused
		}
		return contactID, api.UpdateContact(ctx, contactID, display, email)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", errTriageStorage
	}
	all, err := api.ListContacts(ctx)
	if err != nil {
		return "", err
	}
	for _, contact := range all {
		if contact.ID == "" || !strings.EqualFold(strings.TrimSpace(contact.Email), strings.TrimSpace(email)) {
			continue
		}
		if contactID != "" {
			// Two Numina contacts share the email; the operator decides which.
			return "", errChargeHeld
		}
		contactID = contact.ID
	}
	if contactID == "" {
		if !s.invoiceBillingReady(ctx) {
			return "", errBillingPaused
		}
		// No country: the operator confirms it in Numina before invoicing.
		created, err := api.CreateContact(ctx, numina.Contact{Name: display, Email: email, AttnPerson: attn})
		if err != nil {
			return "", err
		}
		if created.ID == "" {
			return "", errors.New("numina: created contact has no id")
		}
		contactID = created.ID
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO portal_numina_contacts(client_id,contact_id) VALUES($1,$2) ON CONFLICT(client_id) DO NOTHING`, client, contactID); err != nil {
		return "", errTriageStorage
	}
	return contactID, nil
}
