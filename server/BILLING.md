# Portal billing

[ADR 0030](../docs/adr/0030-numina-first-billing.md) decides how the portal collects money:
Numina, the registered bookkeeping system, issues invoices and matches bank payments, and
Stripe only collects cards. This page describes what the source does today. Nothing here runs
in production: the gates below remain false by default and require their own
explicit production settings and the separate reviewed activation under #21.

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

The client can instead pay by card or MobilePay. Stripe issues and emails a full VAT invoice for
each payment, and Numina's own Stripe integration books the sale, fee and payout from Stripe's
payout. The portal makes no Numina invoice for a card order, so nothing is booked twice: the
Stripe invoice is the sale's document, and Numina books the money once.

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
  - Amounts are VAT-inclusive DKK, and every line carries the configured tax rate: a manual,
    inclusive 25% Danish VAT rate (`PORTAL_STRIPE_TAX_RATE`). Stripe Tax is not used. Before
    its first Checkout, the server reads that rate from Stripe and refuses to create any session
    until it is active, inclusive and exactly 25%. An exclusive rate would add VAT on top of the
    agreed price, and another percentage would put the wrong VAT on the invoice.
  - **A full VAT invoice for every payment.** Checkout requires the billing address and offers a
    business its legal name and VAT number (`tax_id_collection`). The one-time session sets
    `invoice_creation[enabled]`, so Stripe issues a paid invoice that shows line amounts without
    VAT (`amount_tax_display=exclude_tax`), the VAT rate and the VAT amount. A subscription
    already gets an invoice for every charge. That invoice is the full invoice a business
    customer needs under the VAT order, section 58(1); a card receipt alone is not. Stripe charges
    0.4% of the amount, at most USD 2, for each one-time invoice; subscription invoices are part
    of Stripe Billing's fee.
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

## Launch discount

The published launch offer (`launchDiscount` in `internal/identity/offerings.json`, which
mirrors the public site) takes 25% off the one-time price for the first 50 customers who order
with hosting. The portal applies exactly that rule and refuses to start with any other
`appliesTo` or `requiresHosting`:

- An order qualifies when it has both a one-time and a monthly (hosting) price. The discount is
  `percent` of the one-time price, rounded to whole kroner. Hosting always keeps its full price.
- Places count customers, not orders. A client takes at most one place, with their first
  qualifying order, and only that order is discounted. A place is never reused: a withdrawn or
  ended order keeps it, and the retention purge removes only who held it.
- The order takes its place in the same transaction that inserts it, under a transaction-scoped
  advisory lock, so two orders never take the same place and the 51st order finds none
  (`portal_launch_places`, migration v24, with a unique place number as a second guard).
- The order stores the discounted one-time price, so invoices, card payments, work funding,
  withdrawals and settlements use it unchanged.
- The shared proposal, the order summary before the button and the order receipt show the
  discount as its own line, with the offer's sentence. The order form carries whether it
  showed the discount. If the last place went in between, the order is refused and the receipt
  shows the full price with the reason, so nobody is charged a price they did not see.
- The Numina invoice line and the Stripe Checkout line carry the discounted unit price, and
  their description names the full price and the discount, for example
  `… — normalpris 6000 DKK, lanceringsrabat 25 % −1500 DKK`. A unit price that already includes
  the discount is valid on a Danish invoice. No Stripe coupon or negative Numina line is needed.
- The order confirmation email names the discount, and the operator's page shows the place.

## Add-ons

The standard add-ons (`addons` in `internal/identity/offerings.json`, which mirrors the public
site) are the only extras with fixed prices. Anything else is a fixed quote. Each has an id, a
kind (`one-time` or `monthly`), a VAT-inclusive price in whole kroner, the offerings it fits,
an optional unit limit (`max`, for example per extra language) and EN/DA text. The portal
refuses to start with an add-on it cannot sell.

- The receipt lists the add-ons that fit the request's offering, monthly ones only when the
  proposal includes hosting. The client ticks them and shows the price again: the summary
  before the order button then lists each add-on and the total. The order form carries the
  choice, and a retry with another choice is a conflict.
- At most one feature-request priority tier (`tier`: priority or express) per order.
- The order stores each add-on with its unit price at the time (`portal_order_addons`,
  migration v25), and its one-time and monthly prices include them. Invoices, card payments,
  funding and withdrawals therefore use the totals unchanged.
- The launch discount takes off the proposal's own one-time price only, never an add-on.
- The Numina invoice has the order's own line, then one line per add-on the period charges:
  one-time add-ons on the first invoice, monthly ones on every monthly invoice. Stripe Checkout
  has the same lines, one-time ones on the payment and monthly ones on the subscription.
- The confirmation email, the receipt and the operator's page list the add-ons.
- **Ending one add-on** (Consumer Contracts Act, section 28 a): the receipt offers to end each monthly
  add-on after the current month (`POST /requests/{id}/addon-end`, `portal_addon_ends`, migration
  v27). It is billed through that month; later monthly charges, invoices and card renewals leave it
  out. A card order's subscription then loses the add-on's item (its product carries
  `portal_addon` metadata), without proration. The end is recorded first, and Stripe's confirmation
  on its own (`portal_addon_removals`): if Stripe fails or its answer is lost, the operator's page
  offers to ask Stripe again until it confirms. An add-on ended while the monthly Checkout is still
  open is removed when its subscription completes, before that event counts as processed. Ending Priority or Express drops the client's level after that month.

## Feature-request priority and fair use

Hosted projects' feature requests are picked up in a monthly round (Standard). The monthly
add-ons Priority and Express (`tier` in the catalogue) raise that level; the terms describe
what each means.

- **Level.** A client's level is the highest tier among the monthly add-ons of their orders
  that still bill: no withdrawal and no end before this month. It is derived, not stored, so it
  follows orders, ends and withdrawals without a separate record. The operator's request page
  shows it.
- **Fair use.** A client with hosting has one request in progress at a time. A new request is
  refused (409, with the reason) while another of theirs is not marked finished. The check
  runs in the request's own transaction under a per-client advisory lock, so two at once
  cannot both pass. A retry of the same submission is not a second request.
- **Finished.** The operator marks a request finished, or not going ahead, on its page
  (`POST /operator/requests/{id}/finished`, `portal_request_closures`, migration v26). The client
  can then send the next request. Without hosting there is no limit; such requests are quoted.
- Both need the order gates. With them off there are no orders and no limit.

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
    the order. The period ends at midnight in Copenhagen after the 14th day. A last day that
    is a weekend, a public holiday, Constitution Day, 24 or 31 December moves to the next
    working day (section 19(6)).
  - The withdrawal is recorded once (`portal_order_withdrawals`, migration v21) and stops all
    further billing. The invoice path creates, books and sends nothing more, not even the
    first invoice or one held before the withdrawal; a booked invoice is only followed to its
    payment. The card path offers no Checkout, and the card subscription ends when it would
    first charge, or at once if a month was already charged. That end takes precedence over
    one scheduled earlier, unless the earlier one comes first, and Stripe's confirmation is
    recorded on its own (`portal_withdrawal_cancellations`); the earlier record stays as it
    was. No end of monthly billing is admitted after a withdrawal, so nothing can push that
    end back. If Stripe does not confirm, the operator's page says so, to finish in the Stripe
    Dashboard. A renewal after it never funds a month, and a withdrawn order funds no work
    approval.
  - Refunds stay with the operator, whose page shows the withdrawal. They refund what was paid
    without undue delay and within 14 days by the same payment method: a Stripe refund, or a
    Numina credit note. They also confirm the withdrawal to the client by email. The client's
    receipt shows the time and asks them to keep it.
- **Starting work within the withdrawal period.** No work starts within the 14 days unless
  the client asks for it (Consumer Contracts Act, sections 18(2)(2), 21 and 25).
  - The order step offers one optional box, never ticked in advance: **Begynd arbejdet inden for
    fortrydelsesfristen på 14 dage. Jeg er klar over, at jeg betaler for det udførte arbejde,
    hvis jeg fortryder, og at jeg mister fortrydelsesretten, når arbejdet er helt udført.** /
    **Start the work within the 14-day withdrawal period. I understand that if I withdraw, I
    pay for the work done until then, and that I lose the right of withdrawal once the work is
    completely finished.** A ticked box is recorded on the order with its own timestamp
    (`early_start_at`, migration v22). A retry cannot change it, and the receipt and the
    operator's page show it.
  - Without it, the receipt says when work may start, and no work approval is admitted
    before the withdrawal period has ended (`WORK-APPROVALS.md`).
  - With it, the operator marks the work **completely finished** once it is
    (`portal_order_completions`). That ends the right of withdrawal: the receipt says so,
    and a withdrawal is refused.
  - A client who withdraws after an early start pays for the work done until then, in
    proportion to the whole agreement. The operator records that amount once, in whole DKK
    and at most the order's one-time and monthly amounts (`portal_withdrawal_settlements`).
    The receipt then shows it, and says that the rest is refunded only when the client paid
    more. Without an early start the client owes nothing, so no amount can be recorded.
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

## Emails

The portal sends two emails itself, in the language the client used:

- an **order confirmation** when an order is placed (Consumer Contracts Act, section 13). It
  restates the order, its scope, prices and terms, the payment path, the early-start request
  or the first day work may begin, the last day to withdraw and how, and links to the terms
  with the model withdrawal form. The complete terms and form, including the
  existing privately projected trader address, are copied into the email from
  the runtime's immutable public serving snapshot. The original acceptance ID,
  proposal version/content reference and terms reference are retained with it;
- a **withdrawal receipt** when the client withdraws (section 20 a), with what happens to
  what they paid.

Each is queued in the same transaction as the order or withdrawal that causes it
(`portal_mail_outbox`, migration v23), once per order and kind, to the client's contact address.
New orders require a configured sender, complete terms and a contact address;
otherwise the order and its side effects roll back. The client can read the full
terms before ordering, and a changed terms reference requires a fresh review.
An existing order's retry acknowledges the original order without replacing its
frozen confirmation, even after the terms or contact change. Withdrawal itself
does not depend on those new-order prerequisites. Its receipt uses the current
contact address, or the original order-confirmation recipient if that contact
has been removed. One replica at a time (an
advisory lock) sends the queue every minute and records each delivery
(`portal_mail_deliveries`). A failure stops the pass and is tried again on the next one; a
message whose delivery record was lost may arrive twice. The operator's page shows each email
as queued or sent.

Emails go through the site's send-only SES sender (platform#5400): SMTP submission on port 587
with STARTTLS required before AUTH, From `no-reply@devantler.tech` only. The sender starts only
when `PORTAL_SMTP_USERNAME_FILE` and `PORTAL_SMTP_PASSWORD_FILE` name the projected credential;
`PORTAL_SMTP_ADDR` (default `email-smtp.eu-central-1.amazonaws.com:587`) and `PORTAL_MAIL_FROM`
(default `no-reply@devantler.tech`) are optional. The credential lives in OpenBao at
`apps/business-site/smtp` (`username`, `password`); the platform adds it to
`business-site-runtime` (platform#6368). While SES is in its sandbox, it delivers only to
verified addresses.

The runtime obtains confirmation terms only from its already-admitted serving
artifact after the company disclosure projection, never by fetching the website.
Missing or ambiguous documents keep new ordering closed while sign-in can remain
available. The outbox retains the complete original text under the existing portal
retention policy. A submitted SMTP message is not proof that the customer received
it; real receipt remains an acceptance gate. See [commerce acceptance](COMMERCE-ACCEPTANCE.md).

## Gate and configuration

Every step requires the existing identity, request, triage, proposal, sharing and acceptance
gates plus the false-default `portal-invoice-billing` OpenFeature flag. Its explicit
production setting is `PORTAL_INVOICE_BILLING_FLAG`, using the maintained-provider
JSON documented in [README.md](README.md#explicit-production-identity-profile).
Missing, wrong-type or
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

The card path has its own false-default `portal-card-billing` flag and independent
`PORTAL_CARD_BILLING_FLAG` production setting. It is enabled only when `PORTAL_STRIPE_KEY_FILE` and
`PORTAL_STRIPE_WEBHOOK_SECRET_FILE` name projected files and `PORTAL_STRIPE_TAX_RATE` names the
VAT tax rate (`txr_…`); `PORTAL_STRIPE_API_URL` is optional, HTTPS only. In production all of
them come from the portal settings projection, and the two files are separate projected
secrets such as `/run/webapp-inputs/stripe-api-key`. Turning the flag off hides the card choice and the payment link, refuses card
orders and answers the webhook with `404`, so Stripe retries until it is on again.

Stripe secrets belong in the OpenBao KV-v2 `secret` mount at `apps/business-site/stripe`:

- property `api-key`: a **restricted** key (`rk_live_…`) with write access to Checkout Sessions
  and Subscriptions and read access to Invoices and Tax Rates, or `rk_test_…` from a Stripe
  sandbox for development;
- property `webhook-secret`: the `whsec_…` signing secret of a webhook endpoint at
  `https://portal.devantler.tech/stripe/webhook`, created with API version
  `2026-10-28.endive` and the events `checkout.session.completed`,
  `checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`,
  `checkout.session.expired`, `invoice.paid`, `customer.subscription.deleted`,
  `charge.refunded` and `charge.dispute.created`.

For the invoice, the maintainer also sets up the Stripe account; none of this is a secret:

- a tax rate *Moms*, 25%, **inclusive**, country DK, whose `txr_…` ID is
  `PORTAL_STRIPE_TAX_RATE`;
- the public business details Stripe prints as the seller: legal name and address;
- the company's VAT number (DK46830385) as the account's default tax ID on invoices;
- invoice numbers that run in one sequence across the account;
- the default invoice template showing amounts without VAT, which subscription invoices use;
- emailed receipts and invoices for successful payments.

Before the card path goes live, the maintainer also asks Numina support to switch on Numina's
Stripe integration.

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
