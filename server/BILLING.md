# Portal billing

[ADR 0030](../docs/adr/0030-numina-first-billing.md) decides how the portal collects money:
Numina, the registered bookkeeping system, issues invoices and matches bank payments, and
Stripe only collects cards. This page describes what the source does today. Nothing here runs
in production: the gate below is false by default and has no environment mapping.

## Orders: one collection path

Paths below are as the portal serves them at the root of its own host, `portal.devantler.tech`.
When it shares a host with the public site (local runs and most tests), each is under `/portal`.

After the client accepts the current proposal, the receipt page asks how they want to pay.
Just before the button it repeats the scope, the one-time and monthly prices and their terms.
The button reads **Bestil med betalingspligt** / **Order with obligation to pay**, as the
Consumer Contracts Act, section 12, requires of an order button.
Placing the order binds that acceptance to exactly one collection path, so an order never has
both a Numina invoice and a Stripe charge.

- `POST /requests/{id}/orders` takes only the acceptance ID, the chosen `collection`,
  the CSRF token and the language. It uses the same request lock and current client membership
  as acceptance, and only the **current** acceptance with a non-zero price can be ordered.
- One order per request (`portal_orders`, migration v16). A retry with the same acceptance and
  path returns the existing order; a different path or acceptance is a conflict. A later
  iteration is a new request.
- The order copies the accepted one-time and monthly amounts. Monthly billing starts on the 1st
  of the month after the order.
- The paths are **invoice** (bank transfer or EAN), the default, and **card** (card or
  MobilePay through Stripe). Each has its own gate; the client sees only the paths that are on.

## The invoice path (Numina)

`Foundation.SyncInvoiceBilling` runs every 15 minutes in the server process when a Numina key
is configured. One replica runs each pass (a PostgreSQL advisory lock); the others skip it.
For each invoice-path order it:

1. Records each due **charge** before calling Numina (`portal_order_charges`): the acceptance
   invoice for the one-time amount, then one charge per month from the first billed month to
   the current Copenhagen month. Each charge has a durable identity,
   `portal-<order>-<period>`, and exact net and VAT amounts inside the VAT-inclusive price.
2. Finds or creates the client's **Numina contact** from the portal contact profile (company or
   name, email, attention person). The portal profile has no country, so a new contact has
   none: the operator confirms it in Numina, and only a contact whose country is `DK` is
   invoiced automatically with Danish VAT. Numina does not protect contact creation against
   retries, so a contact with the same email is reused. Later passes update only name and email,
   so EAN, CVR and address details set in Numina are kept.
3. Creates the **draft invoice** with the identity as `Idempotency-Key` and in the memo. Before
   creating, it lists the contact's invoices and reuses one whose memo carries the identity, so
   a lost reply never creates a second invoice. Issue date is today, due date 14 days later.
   One line: the accepted scope's first line (plus the month for monthly charges), at the net
   price, with the configured sales account and the VAT code set on the line itself, so the
   account's own default cannot change the total.
4. **Books** the invoice (safe to repeat), then **sends** it by email with its own idempotency
   key. EAN delivery follows from the Numina contact's e-invoice settings when it is booked.
   A Numina sandbox cannot send email (`403`); booking and polling still proceed.
5. **Polls** the invoice. The charge is settled only when Numina reports `payment_state`
   `paid` and nothing outstanding. Any other state, including
   `covered_by_unmatched_payments`, waits until the payment is matched in Numina.

A charge is **held** when the portal must not decide it alone: the client has no contact
profile yet, two Numina contacts share the client's email, the Numina contact's country is not
confirmed as Denmark (another country needs reverse charge or another VAT treatment), or a
draft's total or currency differs from the charge. The operator page shows each held
charge's reference. Invoice it by hand in Numina with that reference in the memo: the portal
adopts an invoice (never a credit note) whose memo carries it, a booked one before a draft. A
charge that already has a rejected draft switches to a booked replacement. It follows the invoice's payment and leaves
sending to whoever booked it, and it records that together with the reference, so a failed step
afterwards never sends it again. A draft is booked only for a contact confirmed as Denmark and
only when its total is exactly the charge in DKK. Each pass tries held charges again, so fixing
the contact or the draft resumes them, and no second invoice is created once one carries the
reference.

The client's receipt shows each invoice as being prepared, invoiced and awaiting payment, or
paid. The operator's request page shows the order, each charge's state, Numina invoice number
and whether it was sent. Reminders (rykker) stay a Numina task.

## The card path (Stripe)

The client can instead pay by card or MobilePay. Stripe sends the receipt, and Numina's own
Stripe integration books the sale, fee and payout. The portal makes no Numina invoice for a
card order, so nothing is booked twice.

- **Checkout.** Placing a card order creates one Stripe Checkout Session per amount and shows
  each as a **Continue to payment** link on the receipt. The portal's CSP allows form posts only
  to itself, so the order form never redirects to Stripe.
  - The one-time amount uses `mode=payment`, by card or MobilePay.
  - The monthly amount uses `mode=subscription`, card only, with `proration_behavior=none` and
    its billing anchor at 00:00 UTC on the 1st of the first billed month (01:00 or 02:00 in
    Copenhagen). Nothing is charged before the anchor; Stripe then charges on the 1st of each
    month, and renews on the anchor's UTC day. If that month has already begun, the anchor is
    the next 1st.
  - The two are separate sessions because Stripe refuses one-time prices in a subscription
    without proration. Payment methods come from the Stripe Dashboard: no session sets
    `payment_method_types`, and Stripe offers MobilePay only for one-off payments.
  - Amounts are VAT-inclusive DKK; Stripe Tax is not used.
- **Sessions are kept.** Every session is stored with its order, purpose and attempt
  (`portal_card_checkouts`), and moves between `open`, `processing`, `paid` and `closed`. Only
  Stripe closes a session: when it reports the session expired or its asynchronous payment
  failed, the receipt offers **Start card payment**, which creates a new attempt with `Idempotency-Key`
  `portal-<order>-<purpose>-checkout-<n>` plus a fresh random suffix for each try. Stripe caches a
  failed reply, even a `500`, under its key, so a fixed key could never recover. A session whose
  reply was lost was never shown to anyone and only expires, and one session is stored per attempt. An open, processing or paid session is never
  replaced, not even past its expiry time, because Stripe never expires a completed session and
  its completion may still be on its way. A closed session stays closed when an unpaid completion
  arrives after its failure, and a replaced one still resolves to its own order.
- **Funding comes only from signed webhooks.** `POST /stripe/webhook` sits outside
  sessions and CSRF, reads at most 256 KiB, verifies `Stripe-Signature` (HMAC-SHA256, five
  minutes' tolerance) and applies each event exactly once (`portal_stripe_events`). A failed
  step answers `500`, so Stripe retries. Events are resolved only through the portal's own
  stored sessions and subscriptions; any other session is ignored.
  - `checkout.session.completed` with `payment_status` `paid`, or
    `checkout.session.async_payment_succeeded`, funds the one-time amount. An unpaid
    completion marks the session as processing.
  - A subscription's completion records its subscription ID. Each `invoice.paid` with
    `billing_reason` `subscription_cycle` funds its month. An invoice that arrives before the
    completion event is deferred with `500` when it carries the portal's order marker, and
    ignored when it does not (another integration on the same account).
  - `checkout.session.expired` and `checkout.session.async_payment_failed` never fund; they
    close the session.
  - The paid amount and currency must equal the order's amount for that period exactly.
    A browser return to the receipt is never proof of payment.

## Ending and reversing orders

- **Ending monthly billing.** On the request page, an operator chooses an order's last billed
  month, from the current month on, and records it once (`portal_order_ends`). The same month
  again is a retry; a different one is refused.
  - On the invoice path, the Numina sync creates no monthly charge after that month.
  - On the card path, the portal asks Stripe to end the subscription at 00:00 UTC on the 1st
    after that month, exactly when it would renew, without proration. Stripe's confirmed date
    is recorded (`portal_card_cancellations`) and shown. Until Stripe confirms, the page
    offers to ask again, even after that month. Once the renewal has passed unconfirmed, the
    portal ends the subscription at once; a month it charged never funds the order and is
    refunded by hand. A subscription that completes after the order ended is asked to end as
    soon as it is recorded.
  - A renewal paid after the last billed month never funds a month, and an ended order offers
    no new subscription Checkout.
- **The client ends monthly billing.** A consumer must be able to end a subscription where
  they agreed to it, here the portal (Consumer Contracts Act, section 28 a). On the receipt,
  **Opsig månedlig betaling** / **End monthly billing** (`POST /requests/{id}/order-end`)
  makes the current month the last billed one. The notice is never more than the rest of that
  month, and nothing already invoiced or paid changes. It uses the same request lock and
  current client membership as the order, and the end is recorded once with the client's
  identity (`source` `client`, migration v20). It then works exactly like an operator's end on
  both paths. An end that is already recorded stands, and the form is shown only while no end
  is recorded and the order's own path is on. If Stripe does not confirm the card
  subscription's end, the client's end still stands and the operator's page offers to ask
  Stripe again.
- **The client withdraws.** A consumer may withdraw from the agreement for 14 days after the
  order, through a withdrawal function in the portal (Consumer Contracts Act, sections 18,
  19 and 20 a). The portal cannot tell a business from a consumer, so every client gets it.
  - The receipt shows **Fortryd aftale** / **Withdraw from contract** with the last day. It
    opens a separate **Bekræft fortrydelse** / **Confirm withdrawal**
    (`POST /requests/{id}/withdrawal`), under the same request lock and client membership as
    the order. The period ends at midnight in Copenhagen after the 14th day.
  - The withdrawal is recorded once (`portal_order_withdrawals`, migration v21) and stops all
    further billing. The invoice path creates no new invoice, not even the first, and the card
    path offers no Checkout. Monthly billing ends before its first month: a card subscription
    ends when it would first charge, or at once if a month was already charged. A renewal after
    it never funds a month, and a withdrawn order funds no work approval.
  - Refunds stay with the operator, whose page shows the withdrawal. They refund what was paid
    within 14 days by the same payment method: a Stripe refund, or a Numina credit note. They
    also confirm the withdrawal to the client by email. The client's receipt shows the time
    and asks them to keep it.
- **Stripe ending a subscription on its own.** `customer.subscription.deleted`, for example
  after failed renewals, records the order's end, unless the operator or the client already
  ended it. The last billed month is the one the subscription had paid in advance when it
  ended. An end that arrives before the subscription's Checkout completion answers `500` when
  it carries the portal's order marker, so Stripe retries it.
- **Refunds and disputes.** `charge.refunded` (any refunded amount) and
  `charge.dispute.created` record a reversal against the funded charge
  (`portal_charge_reversals`). The charge keeps its settlement, but no longer funds the order,
  and the client and the operator see it as refunded or disputed.
  - A charge names only its PaymentIntent, so the portal asks Stripe which Checkout Session or
    invoice that PaymentIntent paid, and only references the portal stored count. Anything
    else belongs to another integration and is ignored.
  - A refund or dispute of one of the portal's own payments that is not funded yet answers
    `500`, so Stripe retries it after the payment event: a one-time Checkout Session the portal
    stored, or an invoice of one of its subscriptions, known or carrying its order marker.
  - A won dispute is not restored automatically; the operator decides.
- **Numina credit notes stay manual.** The operator issues a credit note in Numina by hand.

## Gate and configuration

Every step requires the existing identity, request, triage, proposal, sharing and acceptance
gates plus the false-default `portal-invoice-billing` OpenFeature flag. Missing, wrong-type or
error evaluations deny. Turning the flag off stops order placement, hides the order and stops
the sync between calls.

The sync starts only when the portal's own dependencies are configured and
`PORTAL_NUMINA_TOKEN_FILE` names an absolute path to the projected API key. It uses the same
non-root, root/current-user-owned projection reader as the other portal secrets. In production
these settings come from the portal settings projection (`--portal-settings-projection`), like
the sign-in settings; the key itself is a separate projected file such as
`/run/webapp-inputs/numina-api-key`. Optional settings, refused unless the key file is set:

| Setting                       | Default                  | Meaning                           |
|-------------------------------|--------------------------|-----------------------------------|
| `PORTAL_NUMINA_API_URL`       | `https://api.numina.app` | Numina API origin, HTTPS only     |
| `PORTAL_NUMINA_SALES_ACCOUNT` | `1000`                   | Sales account for portal sales    |
| `PORTAL_NUMINA_VAT_CODE`      | `U25`                    | Sales VAT code for 25% Danish VAT |

The key belongs in the OpenBao KV-v2 `secret` mount at `apps/business-site/numina`, property
`api-key`, beside the existing `apps/business-site/public-disclosure` entry. It needs the
scopes `invoices:write`, `invoices:book`, `invoices:read`, `contacts:write` and
`contacts:read` (Numina write scopes do not include read access). The maintainer creates and
stores it; it never appears in source, logs or chat.

Before the gate is switched on, the deploy package also needs: an ExternalSecret and
read-only projection for that entry, the platform role's read permission on it, an egress
allow rule to `api.numina.app` (the package denies egress by default), and the runtime grants
below. None of these is added here.

The card path has its own false-default `portal-card-billing` flag, also without an environment
mapping. It is enabled only when both `PORTAL_STRIPE_KEY_FILE` and
`PORTAL_STRIPE_WEBHOOK_SECRET_FILE` name projected files (`PORTAL_STRIPE_API_URL` is optional,
HTTPS only). In production all three come from the portal settings projection, and the two
files are separate projected secrets such as `/run/webapp-inputs/stripe-api-key`. Turning the flag off hides the card choice and the payment link, refuses card
orders and answers the webhook with `404`, so Stripe retries until it is on again.

Stripe secrets belong in the OpenBao KV-v2 `secret` mount at `apps/business-site/stripe`:

- property `api-key`: a **restricted** key (`rk_live_…`) with write access to Checkout Sessions
  and Subscriptions and read access to Invoices, or `rk_test_…` from a Stripe sandbox for
  development;
- property `webhook-secret`: the `whsec_…` signing secret of a webhook endpoint at
  `https://portal.devantler.tech/stripe/webhook`, created with API version
  `2026-10-28.endive` and the events `checkout.session.completed`,
  `checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`,
  `checkout.session.expired`, `invoice.paid`, `customer.subscription.deleted`,
  `charge.refunded` and `charge.dispute.created`.

Before the card path goes live, the maintainer also asks Numina support to switch on Numina's
Stripe integration, and turns on Stripe's emailed receipts.

## Storage and privileges

Migration v16 adds `portal_orders` (SELECT/INSERT), `portal_order_charges`
(SELECT/INSERT/UPDATE, for state changes) and `portal_numina_contacts` (SELECT/INSERT) after
unchanged v1–v15. Migration v17 adds `portal_card_checkouts` (SELECT/INSERT/UPDATE, for state changes)
and `portal_stripe_events` (SELECT/INSERT). The migration grants exactly these to the runtime
role. Migration v18 adds `portal_order_ends`, `portal_card_cancellations` and
`portal_charge_reversals` (SELECT/INSERT each). The runtime refuses to start if it can change
orders, the contact map, Stripe events, order ends, cancellations or reversals,
or delete, truncate or pass on access to any billing table, directly or through another role.
Until a gate is switched on, the tables stay empty.

## Tests

`TestInvoiceBilling*` run the real HTTP journey, the real database and the real Numina client
against an in-memory Numina with its documented retry rules: order placement and retries, a
refused second path, creation, booking and sending once, an unmatched payment that does not
settle, a matched one that does, the next month's invoice, a lost create reply found by its
memo, a foreign customer held even with a Danish draft, an operator's replacement invoice
followed, a sandbox that cannot email, and the closed gate.
`TestPrivateCard*` send genuinely signed events to the real HTTPS handler and a fake Stripe
API: one-off and subscription Checkout shapes, a forged signature, an unpaid then
asynchronously paid session, a replayed event, an early subscription invoice retried after
completion, a foreign subscription ignored, a monthly invoice, a wrong amount, a session past
its expiry time kept until Stripe expires it, a failed payment restarted even when its unpaid
completion arrives late, and the closed gate. The order-end tests cover an invoice order that
stops after its last month, refused and repeated end forms, a card subscription ended at its next
renewal after a failed first request, a refund and a dispute that stop funding, another
integration's refund ignored, and a subscription Stripe ended on its own. `internal/numina` and `internal/stripe` test the clients. Real Numina
sandbox and Stripe test-mode proof against the real services are launch steps.
