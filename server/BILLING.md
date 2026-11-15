# Portal billing

[ADR 0030](../docs/adr/0030-numina-first-billing.md) decides how the portal collects money:
Numina, the registered bookkeeping system, issues invoices and matches bank payments, and
Stripe only collects cards. This page describes what the source does today. Nothing here runs
in production: the gate below is false by default and has no environment mapping.

## Orders: one collection path

After the client accepts the current proposal, the receipt page asks how they want to pay.
Placing the order binds that acceptance to exactly one collection path, so an order never has
both a Numina invoice and a Stripe charge.

- `POST /portal/requests/{id}/orders` takes only the acceptance ID, the chosen `collection`,
  the CSRF token and the language. It uses the same request lock and current client membership
  as acceptance, and only the **current** acceptance with a non-zero price can be ordered.
- One order per request (`portal_orders`, migration v16). A retry with the same acceptance and
  path returns the existing order; a different path or acceptance is a conflict. A later
  iteration is a new request.
- The order copies the accepted one-time and monthly amounts. Monthly billing starts on the 1st
  of the month after the order.
- Today the only path is **invoice** (bank transfer or EAN). Card and MobilePay come with #553.

## The invoice path (Numina)

`Foundation.SyncInvoiceBilling` runs every 15 minutes in the server process when a Numina key
is configured. One replica runs each pass (a PostgreSQL advisory lock); the others skip it.
For each invoice-path order it:

1. Records each due **charge** before calling Numina (`portal_order_charges`): the acceptance
   invoice for the one-time amount, then one charge per month from the first billed month to
   the current Copenhagen month. Each charge has a durable identity,
   `portal-<order>-<period>`, and exact net and VAT amounts inside the VAT-inclusive price.
2. Finds or creates the client's **Numina contact** from the portal contact profile (company or
   name, email, attention person, country DK). Numina does not protect contact creation against
   retries, so a contact with the same email is reused. Later passes update only name and email,
   so EAN, CVR and address details set in Numina are kept.
3. Creates the **draft invoice** with the identity as `Idempotency-Key` and in the memo. Before
   creating, it lists the contact's invoices and reuses one whose memo carries the identity, so
   a lost reply never creates a second invoice. Issue date is today, due date 14 days later.
   One line: the accepted scope's first line (plus the month for monthly charges), at the net
   price, with the configured sales account and VAT code.
4. **Books** the invoice (safe to repeat), then **sends** it by email with its own idempotency
   key. EAN delivery follows from the Numina contact's e-invoice settings when it is booked.
   A Numina sandbox cannot send email (`403`); booking and polling still proceed.
5. **Polls** the invoice. The charge is settled only when Numina reports `payment_state`
   `paid` and nothing outstanding. Any other state, including
   `covered_by_unmatched_payments`, waits until the payment is matched in Numina.

A charge is **held** when the portal must not decide it alone: the client has no contact
profile yet, or the Numina contact's country is not Denmark, which needs reverse charge or
another VAT treatment. The operator invoices a held charge by hand in Numina. Each pass tries
held charges again, so fixing the contact resumes them.

The client's receipt shows each invoice as being prepared, invoiced and awaiting payment, or
paid. The operator's request page shows the order, each charge's state, Numina invoice number
and whether it was sent. Reminders (rykker) stay a Numina task.

## Gate and configuration

Every step requires the existing identity, request, triage, proposal, sharing and acceptance
gates plus the false-default `portal-invoice-billing` OpenFeature flag. Missing, wrong-type or
error evaluations deny. Turning the flag off stops order placement, hides the order and stops
the sync between calls.

The sync starts only when the portal's own dependencies are configured and
`PORTAL_NUMINA_TOKEN_FILE` names an absolute path to the projected API key. It uses the same
non-root, root/current-user-owned projection reader as the other portal secrets. Optional
settings, refused unless the key file is set:

| Setting | Default | Meaning |
|---|---|---|
| `PORTAL_NUMINA_API_URL` | `https://api.numina.app` | Numina API origin, HTTPS only |
| `PORTAL_NUMINA_SALES_ACCOUNT` | `1000` | Sales account for portal sales |
| `PORTAL_NUMINA_VAT_CODE` | `U25` | Sales VAT code for 25% Danish VAT |

The key belongs in the OpenBao KV-v2 `secret` mount at `apps/business-site/numina`, property
`api-key`, beside the existing `apps/business-site/public-disclosure` entry. It needs the
scopes `invoices:write`, `invoices:book`, `invoices:read`, `contacts:write` and
`contacts:read` (Numina write scopes do not include read access). The maintainer creates and
stores it; it never appears in source, logs or chat.

Before the gate is switched on, the deploy package also needs: an ExternalSecret and
read-only projection for that entry, the platform role's read permission on it, an egress
allow rule to `api.numina.app` (the package denies egress by default), and the runtime grants
below. None of these is added here.

## Storage and privileges

Migration v16 adds `portal_orders` (SELECT/INSERT), `portal_order_charges`
(SELECT/INSERT/UPDATE, for state changes) and `portal_numina_contacts` (SELECT/INSERT) after
unchanged v1–v15. The runtime never deletes them. Grant these to the runtime role before the
gate is switched on; until then the tables stay empty.

## Tests

`TestInvoiceBilling*` run the real HTTP journey, the real database and the real Numina client
against an in-memory Numina with its documented retry rules: order placement and retries, a
refused second path, creation, booking and sending once, an unmatched payment that does not
settle, a matched one that does, the next month's invoice, a lost create reply found by its
memo, a foreign customer held, a sandbox that cannot email, and the closed gate.
`internal/numina` tests the client itself. Real Numina sandbox proof is a launch step.
