# Numina-first billing, with Stripe as the card gateway

- Status: Accepted, 2026-11-13 (maintainer direction)
- Scope: #21, #549, #117, #120, #154 and the proposal work (#65, #78, #106); work
  approvals (#268); the business bookkeeping epic devantler-tech/monorepo#4994

## Context

The portal's billing pieces are merged source code behind default-off gates, and
none is deployed. Today:

- the operator writes proposal versions by hand ([`server/PROPOSALS.md`](../../server/PROPOSALS.md));
- invoices issued elsewhere are recorded ([`server/INVOICES.md`](../../server/INVOICES.md));
- bank credits are recorded manually ([`server/CREDITS.md`](../../server/CREDITS.md));
- a work approval needs a fully credited invoice record ([`server/WORK-APPROVALS.md`](../../server/WORK-APPROVALS.md)).

No payment, checkout or subscription code exists.

Numina is the business's registered bookkeeping system. It holds the bank
connection, matches payments from the bank feed and has a native Stripe
integration. Production Stripe is activated. The maintainer asked to integrate
with Numina wherever it can do the work, instead of building bespoke billing.
He does not want to write quotes; they should be generated, and accepting
them stays in the portal.

Numina's public API, read on 2026-11-13 from `developers.numina.app/openapi.json`:

- **Invoices:** create, book, send by email (up to five addresses), credit and
  download as PDF. Create, credit and send accept an idempotency key. Lines are
  free text.
- **Contacts and products:** full API support. A contact's `e_invoice_number`
  and `e_invoice_scheme` (CVR or GLN) route EAN/OIOUBL/Peppol delivery through
  NemHandel.
- **Payments:** matched from the bank feed. An invoice exposes `payment_state`
  and `outstanding_balance`.
- **Offers (quotes):** read-only. They are created in the Numina UI.
- **Not in the API:** subscriptions and reminders, which are UI only. There are
  no webhooks, payment links or customer portal.
- **Stripe:** the native integration books Stripe sales, fees and payouts. It is
  switched on by contacting Numina.

## Decision

1. **One collection path per order, chosen by the customer when accepting.** An
   order never has both a Numina invoice and a Stripe charge, so nothing is
   booked twice.
   - **Invoice (default; bank transfer or EAN).** On acceptance the portal
     creates or updates the customer's Numina contact. It then creates, books and
     sends the invoice through the API, keyed for idempotency on the accepted
     proposal version. The order is funded when Numina reports the invoice
     settled (`payment_state` closed or paid, nothing outstanding). Numina has no
     webhooks, so the portal reads that state by polling. For monthly hosting the
     portal creates each period's invoice itself, because Numina subscriptions
     have no API. This is #549.
   - **Card or MobilePay.** Stripe Checkout takes one-off payments; a Stripe
     subscription takes monthly card payments. MobilePay is single-use only, so
     it is never offered for a subscription. Stripe sends the receipt, and
     Numina's Stripe integration books the sale, fee and payout. The order is
     funded by a signature-verified Stripe event for its exact Checkout Session
     or subscription invoice; a browser return is never proof.
2. **Quotes are generated, not written.** The portal generates each proposal
   from the customer's request and the published offerings and VAT-inclusive
   prices (#154). The customer accepts that exact version in the portal, as
   today. The portal holds the quote because Numina's API cannot create offers.
   Moving quotes into Numina is a later decision if that API appears.
3. **Superseded and retired:** the external invoice records (`portal-invoices`,
   #117) and the bank-credit evidence ledger (`portal-credit-reconciliation`,
   #120). Neither was ever deployed. Numina is the record for invoices and
   payments.
4. **Work approval keeps its rule and changes its evidence.** Paid work still
   needs an accepted version and settled funding, but the funding now comes from
   the order's one collection path (decision 1), not from the retired ledger.
5. **Stripe Invoicing is not used.** Numina issues invoices and Stripe only
   collects cards. This replaces the Stripe Invoicing preference in #21.

## Unchanged

- **#21 gates:** the legal, consumer, privacy and VAT gates still apply to every
  path. They include binding-order wording, withdrawal and cancellation rights,
  and recurring-payment consent.
- **Credentials:** live Stripe keys, the Stripe webhook signing secret and a
  Numina API key are created and stored by the maintainer in OpenBao. The Numina
  key's scopes are `invoices:write`, `invoices:book`, `invoices:read` and
  `contacts:write`. Credentials never appear in source, chat or public records.
- **Development:** uses Stripe test mode and a Numina sandbox company.

## Consequences

- **Removed:** two portal modules, and the manual credit entry that came with them.
- **Added:** two outbound integrations (Numina API, Stripe Checkout and
  subscriptions) and one inbound one (Stripe webhooks).
- **Polling:** invoice payments are seen only when the portal next polls Numina,
  not instantly.
- **Manual in Numina:** reminders (rykker) stay a Numina UI task.
- **Test limits:**
  - A Numina sandbox company cannot send email, so tests of `send` stop at
    booking.
  - That EAN delivery happens on booking is inferred from the documentation, and
    must be checked with a real public customer before relying on it.
- **Maintainer actions before the card path goes live:** switch on Numina's
  Stripe integration through Numina support, and store the credentials above.

## Alternatives

- **Keep the bespoke invoice and credit modules:** these duplicate Numina's
  invoices and bank matching, and leave a manual credit entry the maintainer
  does not want.
- **Stripe Invoicing for all orders:** gives a second invoice system beside the
  registered bookkeeping one, and its payment status is not bank-credit evidence.
- **Numina subscriptions for monthly hosting:** they have no API, so the portal
  could not create or follow them.
